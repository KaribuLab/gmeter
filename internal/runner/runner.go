package runner

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/KaribuLab/gmeter/internal/config"
	"github.com/KaribuLab/gmeter/internal/logger"
	"github.com/KaribuLab/gmeter/internal/models"
	"github.com/KaribuLab/gmeter/internal/reporter"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
)

// Runner es el ejecutor principal de las pruebas
type Runner struct {
	Config     *config.Config
	Logger     *logger.Logger
	Client     *http.Client
	Results    chan *models.Response
	DataSource map[string]interface{}
	WaitGroup  sync.WaitGroup
	StartTime  time.Time
	EndTime    time.Time
}

// NewRunner crea un nuevo ejecutor de pruebas
func NewRunner(cfg *config.Config, logger *logger.Logger) *Runner {
	return &Runner{
		Config: cfg,
		Logger: logger,
		Client: &http.Client{
			Timeout: 30 * time.Second,
		},
		Results:    make(chan *models.Response, 1000),
		DataSource: make(map[string]interface{}),
	}
}

// Run ejecuta las pruebas
func (r *Runner) Run() error {
	r.Logger.Info("Iniciando pruebas de stress")

	// Cargar las fuentes de datos
	if err := r.loadDataSources(); err != nil {
		return fmt.Errorf("error al cargar las fuentes de datos: %w", err)
	}

	// Iniciar el recolector de resultados
	resultCollector := make(chan *models.TestResult)
	go r.collectResults(resultCollector)

	// Ejecutar las pruebas
	r.StartTime = time.Now()

	// Calcular la duración total
	duration, err := time.ParseDuration(r.Config.GlobalConfig.Duration)
	if err != nil {
		return fmt.Errorf("error al parsear la duración: %w", err)
	}

	// Calcular el tiempo de rampa
	rampUp, err := time.ParseDuration(r.Config.GlobalConfig.RampUp)
	if err != nil {
		return fmt.Errorf("error al parsear el tiempo de rampa: %w", err)
	}

	// Iniciar los hilos para cada servicio
	for _, service := range r.Config.Services {
		// Determinar el número de hilos mínimo y máximo para este servicio
		minThreads := r.Config.GlobalConfig.MinThreads
		maxThreads := r.Config.GlobalConfig.MaxThreads

		// Si se especifican a nivel de servicio, sobreescribir los valores globales
		if service.MinThreads > 0 {
			minThreads = service.MinThreads
		}
		if service.MaxThreads > 0 {
			maxThreads = service.MaxThreads
		}

		// Para compatibilidad con versiones anteriores, si solo se especifica ThreadsPerSecond
		if service.ThreadsPerSecond > 0 && service.MinThreads == 0 && service.MaxThreads == 0 {
			minThreads = service.ThreadsPerSecond
			maxThreads = service.ThreadsPerSecond
		}

		r.Logger.Infof("Servicio %s configurado con mínimo %d y máximo %d hilos", service.Name, minThreads, maxThreads)

		// Verificar si hay datos suficientes para este servicio
		if service.DataSourceName != "" {
			if dataSource, ok := r.DataSource[service.DataSourceName]; ok {
				// Verificar si es un LazyDataSource
				if lazyDS, ok := dataSource.(*LazyDataSource); ok {
					if lazyDS.Len() < maxThreads {
						fmt.Printf("ADVERTENCIA: El servicio %s tiene menos registros (%d) que hilos máximos (%d). Los datos se reciclarán.\n",
							service.Name, lazyDS.Len(), maxThreads)
						r.Logger.Warnf("El servicio %s tiene menos registros (%d) que hilos máximos (%d). Los datos se reciclarán.",
							service.Name, lazyDS.Len(), maxThreads)
					}
				} else if records, ok := dataSource.([]models.DataRecord); ok {
					// Compatibilidad con el código existente
					if len(records) < maxThreads {
						fmt.Printf("ADVERTENCIA: El servicio %s tiene menos registros (%d) que hilos máximos (%d). Los datos se reciclarán.\n",
							service.Name, len(records), maxThreads)
						r.Logger.Warnf("El servicio %s tiene menos registros (%d) que hilos máximos (%d). Los datos se reciclarán.",
							service.Name, len(records), maxThreads)
					}
				}
			}
		}

		// Si min y max son iguales o no hay tiempo de rampa, iniciar todos los hilos directamente
		if minThreads == maxThreads || rampUp == 0 {
			r.Logger.Infof("Iniciando %d hilos por segundo para el servicio %s", maxThreads, service.Name)

			// Iniciar todos los hilos a la vez
			for i := 0; i < maxThreads; i++ {
				r.WaitGroup.Add(1)
				go r.runServiceThread(i+1, service)
			}
		} else {
			// Implementar rampa desde min hasta max hilos
			r.Logger.Infof("Iniciando rampa de %d a %d hilos para el servicio %s en %s",
				minThreads, maxThreads, service.Name, rampUp)

			// Iniciar los hilos mínimos inmediatamente
			for i := 0; i < minThreads; i++ {
				r.WaitGroup.Add(1)
				go r.runServiceThread(i+1, service)
			}

			// Si hay diferencia entre min y max, distribuir los hilos adicionales
			// durante el tiempo de rampa
			threadsToRamp := maxThreads - minThreads
			if threadsToRamp > 0 {
				// Calculamos el tiempo de espera entre hilos durante la rampa
				interval := rampUp / time.Duration(threadsToRamp)

				// Iniciar los hilos adicionales gradualmente
				for i := 0; i < threadsToRamp; i++ {
					time.Sleep(interval)
					r.WaitGroup.Add(1)
					go r.runServiceThread(minThreads+i+1, service)
				}
			}
		}
	}

	// Esperar a que terminen todos los hilos
	done := make(chan struct{})
	go func() {
		r.WaitGroup.Wait()
		close(done)
	}()

	// Esperar a que termine la duración o todos los hilos
	select {
	case <-time.After(duration):
		r.Logger.Info("Tiempo de prueba completado")
	case <-done:
		r.Logger.Info("Todos los hilos han terminado")
	}

	// Cerrar el canal de resultados
	close(r.Results)

	// Esperar a que termine el recolector de resultados
	result := <-resultCollector
	r.EndTime = time.Now()

	// Generar el reporte
	r.Logger.Info("Generando reporte")
	if err := r.generateReport(result); err != nil {
		return fmt.Errorf("error al generar el reporte: %w", err)
	}

	r.Logger.Info("Pruebas completadas")
	return nil
}

// loadDataSources carga las fuentes de datos
func (r *Runner) loadDataSources() error {
	for name, source := range r.Config.DataSources.CSV {
		r.Logger.Infof("Cargando fuente de datos CSV: %s", name)

		// Abrir el archivo CSV
		file, err := os.Open(source.Path)
		if err != nil {
			return fmt.Errorf("error al abrir el archivo CSV %s: %w", source.Path, err)
		}

		// Crear el lector CSV
		reader := csv.NewReader(file)
		if source.Delimiter != "" {
			reader.Comma = rune(source.Delimiter[0])
		}

		// Leer las cabeceras
		var headers []string
		if source.HasHeader {
			headers, err = reader.Read()
			if err != nil {
				file.Close()
				return fmt.Errorf("error al leer las cabeceras del CSV %s: %w", source.Path, err)
			}
		}

		// Contar el número de registros sin cargarlos todos en memoria
		var recordCount int

		// Crear un nuevo archivo para contar registros
		countFile, err := os.Open(source.Path)
		if err != nil {
			file.Close()
			return fmt.Errorf("error al abrir el archivo CSV para contar registros %s: %w", source.Path, err)
		}

		countReader := csv.NewReader(countFile)
		if source.Delimiter != "" {
			countReader.Comma = rune(source.Delimiter[0])
		}

		// Saltar la cabecera si existe
		if source.HasHeader {
			_, err = countReader.Read()
			if err != nil {
				file.Close()
				countFile.Close()
				return fmt.Errorf("error al saltar la cabecera para contar registros %s: %w", source.Path, err)
			}
		}

		// Contar registros
		for {
			_, err := countReader.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				file.Close()
				countFile.Close()
				return fmt.Errorf("error al contar registros del CSV %s: %w", source.Path, err)
			}
			recordCount++
		}
		countFile.Close()

		// Calcular el tamaño óptimo del bloque y la caché
		blockSize := 1000
		maxCacheSize := 10000 // Limitar la caché a 10,000 registros para evitar consumo excesivo de memoria

		// Para archivos muy grandes, ajustar el tamaño del bloque
		if recordCount > 100000 {
			blockSize = 5000
		}

		// Crear un DataSource que implementa carga perezosa con caché por bloques y LRU
		r.DataSource[name] = &LazyDataSource{
			file:         file,
			reader:       reader,
			headers:      headers,
			hasHeader:    source.HasHeader,
			recordCount:  recordCount,
			path:         source.Path,
			cache:        make(map[int]models.DataRecord),
			blockSize:    blockSize,
			delimiter:    reader.Comma,
			maxCacheSize: maxCacheSize,
			lruList:      make([]int, 0, maxCacheSize),
		}

		r.Logger.Infof("Fuente de datos CSV %s preparada con %d registros disponibles", name, recordCount)
	}

	return nil
}

// LazyDataSource implementa una fuente de datos con carga perezosa y caché por bloques
type LazyDataSource struct {
	file         *os.File
	reader       *csv.Reader
	headers      []string
	hasHeader    bool
	recordCount  int
	path         string
	mutex        sync.Mutex
	cache        map[int]models.DataRecord // Caché de registros por índice
	blockSize    int                       // Tamaño del bloque para la carga
	delimiter    rune                      // Delimitador para el CSV
	maxCacheSize int                       // Tamaño máximo de la caché
	lruList      []int                     // Lista de índices usados recientemente (LRU)
}

// Len devuelve el número de registros en la fuente de datos
func (lds *LazyDataSource) Len() int {
	return lds.recordCount
}

// Get obtiene un registro por su índice, cargándolo si es necesario
func (lds *LazyDataSource) Get(index int) (models.DataRecord, error) {
	// Verificar que el índice sea válido
	if index < 0 || index >= lds.recordCount {
		return nil, fmt.Errorf("índice fuera de rango: %d (máximo: %d)", index, lds.recordCount-1)
	}

	lds.mutex.Lock()
	defer lds.mutex.Unlock()

	// Verificar si el registro ya está en caché
	if record, exists := lds.cache[index]; exists {
		// Actualizar la lista LRU (mover este índice al final)
		lds.updateLRU(index)
		return record, nil
	}

	// Calcular el bloque al que pertenece este índice
	blockStart := (index / lds.blockSize) * lds.blockSize

	// Cerrar y reabrir el archivo para posicionarnos al inicio
	lds.file.Close()
	file, err := os.Open(lds.path)
	if err != nil {
		return nil, fmt.Errorf("error al reabrir el archivo CSV %s: %w", lds.path, err)
	}
	lds.file = file

	// Recrear el lector
	lds.reader = csv.NewReader(lds.file)
	if lds.delimiter != 0 {
		lds.reader.Comma = lds.delimiter
	}

	// Saltar la cabecera si existe
	if lds.hasHeader {
		_, err = lds.reader.Read()
		if err != nil {
			return nil, fmt.Errorf("error al saltar la cabecera: %w", err)
		}
	}

	// Saltar registros hasta llegar al inicio del bloque
	for i := 0; i < blockStart; i++ {
		_, err = lds.reader.Read()
		if err != nil {
			if err == io.EOF {
				// Si llegamos al final, volver al inicio y empezar de nuevo
				return lds.Get(index % blockStart) // Reciclar registros
			}
			return nil, fmt.Errorf("error al saltar al bloque %d: %w", blockStart, err)
		}
	}

	// Cargar el bloque completo o hasta el final del archivo
	blockEnd := blockStart + lds.blockSize
	if blockEnd > lds.recordCount {
		blockEnd = lds.recordCount
	}

	// Verificar si necesitamos liberar espacio en la caché
	if lds.maxCacheSize > 0 && len(lds.cache)+(blockEnd-blockStart) > lds.maxCacheSize {
		lds.evictLRU(blockEnd - blockStart)
	}

	// Leer los registros del bloque
	for i := blockStart; i < blockEnd; i++ {
		record, err := lds.reader.Read()
		if err != nil {
			if err == io.EOF {
				// Si llegamos al final antes de lo esperado, puede ser un error en el conteo
				break
			}
			return nil, fmt.Errorf("error al leer el registro %d: %w", i, err)
		}

		// Crear el registro de datos
		dataRecord := make(models.DataRecord)

		// Si hay cabeceras, usar los nombres de las columnas
		if lds.hasHeader {
			for j, value := range record {
				if j < len(lds.headers) {
					dataRecord[lds.headers[j]] = value
				}
			}
		} else {
			// Si no hay cabeceras, usar los índices como nombres
			for j, value := range record {
				dataRecord[fmt.Sprintf("column%d", j+1)] = value
			}
		}

		// Añadir el registro a la caché
		lds.cache[i] = dataRecord
		// Actualizar la lista LRU
		lds.lruList = append(lds.lruList, i)
	}

	// Verificar si el registro solicitado está ahora en caché
	if record, exists := lds.cache[index]; exists {
		return record, nil
	}

	// Si no está en caché, puede ser que hayamos llegado al final del archivo
	// En este caso, reciclamos y devolvemos un registro del inicio
	if len(lds.cache) > 0 {
		// Buscar el primer registro disponible en la caché
		for i := 0; i < lds.recordCount; i++ {
			if record, exists := lds.cache[i]; exists {
				return record, nil
			}
		}
	}

	return nil, fmt.Errorf("no se pudo cargar el registro %d", index)
}

// updateLRU actualiza la lista LRU moviendo el índice al final
func (lds *LazyDataSource) updateLRU(index int) {
	// Buscar el índice en la lista LRU
	for i, idx := range lds.lruList {
		if idx == index {
			// Eliminar el índice de su posición actual
			lds.lruList = append(lds.lruList[:i], lds.lruList[i+1:]...)
			// Añadirlo al final (más recientemente usado)
			lds.lruList = append(lds.lruList, index)
			break
		}
	}
}

// evictLRU elimina los registros menos usados recientemente de la caché
func (lds *LazyDataSource) evictLRU(needSpace int) {
	// Eliminar registros hasta tener suficiente espacio
	for i := 0; i < needSpace && len(lds.lruList) > 0; i++ {
		// Eliminar el primer elemento (menos usado recientemente)
		lruIndex := lds.lruList[0]
		delete(lds.cache, lruIndex)
		lds.lruList = lds.lruList[1:]
	}
}

// Close cierra el archivo CSV
func (lds *LazyDataSource) Close() error {
	return lds.file.Close()
}

// runServiceThread ejecuta un hilo de prueba para un servicio específico
func (r *Runner) runServiceThread(id int, service *config.Service) {
	defer r.WaitGroup.Done()

	r.Logger.Infof("Iniciando hilo %d para el servicio %s", id, service.Name)

	// Crear el contexto del hilo
	var threadContext *models.ThreadContext

	// Si el servicio depende de otro, verificar que se haya ejecutado
	if service.DependsOn != "" {
		// Buscar el servicio del que depende
		var dependentService *config.Service
		for _, s := range r.Config.Services {
			if s.Name == service.DependsOn {
				dependentService = s
				break
			}
		}

		if dependentService == nil {
			r.Logger.Warnf("El servicio %s depende de %s, pero este no existe", service.Name, service.DependsOn)
			return
		}

		// Ejecutar el servicio del que depende para obtener el token
		tempThreadContext := models.NewThreadContext(id, nil)

		// Si el servicio dependiente usa una fuente de datos, obtener un registro
		if dependentService.DataSourceName != "" {
			if dataSource, ok := r.DataSource[dependentService.DataSourceName]; ok {
				// Verificar si es un LazyDataSource
				if lazyDS, ok := dataSource.(*LazyDataSource); ok {
					// Seleccionar un registro basado en el ID del hilo, ciclando si es necesario
					recordIndex := (id - 1) % lazyDS.Len()
					data, err := lazyDS.Get(recordIndex)
					if err != nil {
						r.Logger.WithError(err).Errorf("Error al obtener el registro %d para el servicio dependiente %s",
							recordIndex, dependentService.Name)
						return
					}
					tempThreadContext.Data = data
					r.Logger.Debugf("Hilo %d usando registro %d de %d para el servicio dependiente %s",
						id, recordIndex+1, lazyDS.Len(), dependentService.Name)
				} else {
					// Compatibilidad con el código existente (registros ya cargados)
					records, ok := dataSource.([]models.DataRecord)
					if !ok {
						r.Logger.Warnf("Tipo de fuente de datos no compatible para el servicio %s", dependentService.Name)
						return
					}

					if len(records) > 0 {
						recordIndex := (id - 1) % len(records)
						data := records[recordIndex]
						tempThreadContext.Data = data
						r.Logger.Debugf("Hilo %d usando registro %d de %d para el servicio dependiente %s",
							id, recordIndex+1, len(records), dependentService.Name)
					} else {
						r.Logger.Warnf("No hay datos disponibles para el servicio %s", dependentService.Name)
						return
					}
				}
			} else {
				r.Logger.Warnf("No hay datos disponibles para el servicio %s", dependentService.Name)
				return
			}
		}

		// Ejecutar el servicio dependiente
		resp, err := r.executeService(dependentService, tempThreadContext)
		if err != nil {
			r.Logger.WithError(err).Errorf("Error al ejecutar el servicio dependiente %s", dependentService.Name)
			return
		}

		// Enviar el resultado
		r.Results <- resp

		// Extraer el token
		if dependentService.ExtractToken != "" && dependentService.TokenName != "" {
			// Parsear la respuesta como JSON
			if resp.Body != nil {
				// Extraer el token usando gjson
				token := gjson.GetBytes(resp.Body, dependentService.ExtractToken).String()
				if token != "" {
					tempThreadContext.TokenStore.SetToken(dependentService.TokenName, token)
					r.Logger.Infof("Token extraído para %s: %s", dependentService.TokenName, token)
				} else {
					r.Logger.Warnf("No se pudo extraer el token para %s", dependentService.TokenName)
					return
				}
			}
		}

		// Usar el contexto del hilo temporal
		threadContext = tempThreadContext
	}

	// Si el servicio usa una fuente de datos, obtener un registro
	var data models.DataRecord
	if service.DataSourceName != "" {
		if dataSource, ok := r.DataSource[service.DataSourceName]; ok {
			// Verificar si es un LazyDataSource
			if lazyDS, ok := dataSource.(*LazyDataSource); ok {
				// Verificar si hay menos registros que hilos y mostrar advertencia
				threadsPerSecond := service.ThreadsPerSecond
				if threadsPerSecond == 0 {
					threadsPerSecond = r.Config.GlobalConfig.ThreadsPerSecond
				}
				if lazyDS.Len() < threadsPerSecond {
					r.Logger.Warnf("El servicio %s tiene menos registros (%d) que hilos (%d). Los datos se reciclarán.",
						service.Name, lazyDS.Len(), threadsPerSecond)
				}

				// Seleccionar un registro basado en el ID del hilo, ciclando si es necesario
				recordIndex := (id - 1) % lazyDS.Len()
				var err error
				data, err = lazyDS.Get(recordIndex)
				if err != nil {
					r.Logger.WithError(err).Errorf("Error al obtener el registro %d para el servicio %s",
						recordIndex, service.Name)
					return
				}

				// Mostrar información detallada sobre el registro utilizado
				r.Logger.Infof("Hilo %d usando registro %d de %d para el servicio %s",
					id, recordIndex+1, lazyDS.Len(), service.Name)

				// Mostrar los datos del registro
				for k, v := range data {
					r.Logger.Infof("  - %s: %s", k, v)
				}
			} else {
				// Compatibilidad con el código existente (registros ya cargados)
				records, ok := dataSource.([]models.DataRecord)
				if !ok {
					r.Logger.Warnf("Tipo de fuente de datos no compatible para el servicio %s", service.Name)
					return
				}

				if len(records) > 0 {
					// Verificar si hay menos registros que hilos y mostrar advertencia
					threadsPerSecond := service.ThreadsPerSecond
					if threadsPerSecond == 0 {
						threadsPerSecond = r.Config.GlobalConfig.ThreadsPerSecond
					}
					if len(records) < threadsPerSecond {
						r.Logger.Warnf("El servicio %s tiene menos registros (%d) que hilos (%d). Los datos se reciclarán.",
							service.Name, len(records), threadsPerSecond)
					}

					// Seleccionar un registro basado en el ID del hilo, ciclando si es necesario
					recordIndex := (id - 1) % len(records)
					data = records[recordIndex]

					// Mostrar información detallada sobre el registro utilizado
					r.Logger.Infof("Hilo %d usando registro %d de %d para el servicio %s",
						id, recordIndex+1, len(records), service.Name)

					// Mostrar los datos del registro
					for k, v := range data {
						r.Logger.Infof("  - %s: %s", k, v)
					}
				} else {
					r.Logger.Warnf("No hay datos disponibles para el servicio %s", service.Name)
					return
				}
			}
		} else {
			r.Logger.Warnf("No hay datos disponibles para el servicio %s", service.Name)
			return
		}
	}

	// Crear el contexto del hilo si no existe
	if threadContext == nil {
		threadContext = models.NewThreadContext(id, data)
	} else {
		// Actualizar los datos del contexto
		if threadContext.Data == nil {
			threadContext.Data = make(models.DataRecord)
		}
		for k, v := range data {
			threadContext.Data[k] = v
		}
	}

	// Ejecutar el servicio
	resp, err := r.executeService(service, threadContext)
	if err != nil {
		r.Logger.WithError(err).Errorf("Error al ejecutar el servicio %s", service.Name)
		return
	}

	// Enviar el resultado
	r.Results <- resp

	// Si el servicio extrae un token, guardarlo
	if service.ExtractToken != "" && service.TokenName != "" {
		// Parsear la respuesta como JSON
		if resp.Body != nil {
			// Extraer el token usando gjson
			token := gjson.GetBytes(resp.Body, service.ExtractToken).String()
			if token != "" {
				threadContext.TokenStore.SetToken(service.TokenName, token)
				r.Logger.Infof("Token extraído para %s: %s", service.TokenName, token)
			} else {
				r.Logger.Warnf("No se pudo extraer el token para %s", service.TokenName)
			}
		}
	}

	r.Logger.Infof("Hilo %d completado para el servicio %s", id, service.Name)
}

// executeService ejecuta un servicio
func (r *Runner) executeService(service *config.Service, ctx *models.ThreadContext) (*models.Response, error) {
	// Preparar la URL
	url := service.URL

	// Preparar el cuerpo de la solicitud
	var body []byte
	var err error
	var req *http.Request

	// Determinar el tipo de contenido
	contentType := service.ContentType
	if contentType == "" {
		// Si no se especifica, intentar inferirlo de las cabeceras
		if ct, ok := service.Headers["Content-Type"]; ok {
			if strings.Contains(ct, "application/x-www-form-urlencoded") {
				contentType = "form"
			} else if strings.Contains(ct, "application/json") {
				contentType = "json"
			}
		} else {
			// Por defecto, usar JSON
			contentType = "json"
		}
	}

	// Manejar peticiones form-urlencoded
	if contentType == "form" {
		if len(service.FormData) > 0 || len(service.FormDataTemplate) > 0 {
			formValues := make(map[string]string)

			// Procesar datos de formulario estáticos
			for key, value := range service.FormData {
				formValues[key] = value
			}

			// Procesar plantillas de datos de formulario
			for key, templateValue := range service.FormDataTemplate {
				if containsTemplate(templateValue) {
					// Crear funciones personalizadas para la plantilla
					funcMap := template.FuncMap{
						"uuid": func() string {
							uuid := uuid.New().String()
							r.Logger.Infof("Generando UUID para campo de formulario %s: %s", key, uuid)
							return uuid
						},
					}

					// Compilar la plantilla con las funciones personalizadas
					tmpl, err := template.New("form_field").Funcs(funcMap).Parse(templateValue)
					if err != nil {
						return nil, fmt.Errorf("error al compilar la plantilla del campo de formulario %s: %w", key, err)
					}

					// Crear un buffer para la salida
					buf := new(bytes.Buffer)

					// Ejecutar la plantilla con los datos del contexto
					data := make(map[string]interface{})

					// Agregar los datos del contexto
					for k, v := range ctx.Data {
						data[k] = v
					}

					// Agregar los tokens
					for k, v := range ctx.TokenStore.Tokens {
						data[k] = v
					}

					if err := tmpl.Execute(buf, data); err != nil {
						return nil, fmt.Errorf("error al ejecutar la plantilla del campo de formulario %s: %w", key, err)
					}

					formValues[key] = buf.String()
				} else {
					formValues[key] = templateValue
				}
			}

			// Codificar los valores del formulario
			formData := ""
			i := 0
			for key, value := range formValues {
				if i > 0 {
					formData += "&"
				}
				formData += key + "=" + strings.ReplaceAll(value, " ", "+")
				i++
			}

			// Crear la solicitud con los datos del formulario
			req, err = http.NewRequest(service.Method, url, strings.NewReader(formData))
			if err != nil {
				return nil, fmt.Errorf("error al crear la solicitud form-urlencoded: %w", err)
			}

			// Asegurar que se establece la cabecera Content-Type correcta
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	} else {
		// Manejar peticiones JSON (comportamiento original)
		if service.Body != "" {
			// Verificar si el cuerpo contiene plantillas
			if containsTemplate(service.Body) {
				// Crear funciones personalizadas para la plantilla
				funcMap := template.FuncMap{
					"uuid": func() string {
						uuid := uuid.New().String()
						r.Logger.Infof("Generando UUID para cuerpo: %s", uuid)
						return uuid
					},
				}

				// Compilar la plantilla con las funciones personalizadas
				tmpl, err := template.New("body").Funcs(funcMap).Parse(service.Body)
				if err != nil {
					return nil, fmt.Errorf("error al compilar la plantilla del cuerpo: %w", err)
				}

				// Crear un buffer para la salida
				buf := new(bytes.Buffer)

				// Ejecutar la plantilla con los datos del contexto
				data := make(map[string]interface{})

				// Agregar los datos del contexto
				for k, v := range ctx.Data {
					data[k] = v
				}

				// Agregar los tokens
				for k, v := range ctx.TokenStore.Tokens {
					data[k] = v
				}

				if err := tmpl.Execute(buf, data); err != nil {
					return nil, fmt.Errorf("error al ejecutar la plantilla del cuerpo: %w", err)
				}

				body = buf.Bytes()
			} else {
				// Si no contiene plantillas, usar el cuerpo tal cual
				body = []byte(service.Body)
			}
		}

		// Crear la solicitud
		req, err = http.NewRequest(service.Method, url, bytes.NewBuffer(body))
		if err != nil {
			return nil, fmt.Errorf("error al crear la solicitud: %w", err)
		}
	}

	// Agregar las cabeceras
	for k, v := range service.Headers {
		// Si la cabecera contiene una plantilla, procesarla
		if containsTemplate(v) {
			// Crear funciones personalizadas para la plantilla
			funcMap := template.FuncMap{
				"uuid": func() string {
					uuid := uuid.New().String()
					r.Logger.Infof("Generando UUID para cabecera %s: %s", k, uuid)
					return uuid
				},
			}

			// Compilar la plantilla con las funciones personalizadas
			tmpl, err := template.New("header").Funcs(funcMap).Parse(v)
			if err != nil {
				return nil, fmt.Errorf("error al compilar la plantilla de la cabecera %s: %w", k, err)
			}

			// Crear un buffer para la salida
			buf := new(bytes.Buffer)

			// Ejecutar la plantilla con los datos del contexto
			data := make(map[string]interface{})

			// Agregar los datos del contexto
			for k, v := range ctx.Data {
				data[k] = v
			}

			// Agregar los tokens
			for k, v := range ctx.TokenStore.Tokens {
				data[k] = v
			}

			if err := tmpl.Execute(buf, data); err != nil {
				return nil, fmt.Errorf("error al ejecutar la plantilla de la cabecera %s: %w", k, err)
			}

			req.Header.Set(k, buf.String())
		} else {
			req.Header.Set(k, v)
		}
	}

	// Agregar el ID de correlación
	req.Header.Set("X-Correlation-ID", ctx.CorrelationID)

	// Ejecutar la solicitud
	start := time.Now()

	// Loguear la petición HTTP
	r.Logger.Infof("Enviando petición %s %s", req.Method, req.URL.String())
	r.Logger.Debugf("Headers: %v", req.Header)
	if len(body) > 0 {
		if len(body) > 1000 {
			r.Logger.Debugf("Body: %s... (truncado)", string(body[:1000]))
		} else {
			r.Logger.Debugf("Body: %s", string(body))
		}
	}

	resp, err := r.Client.Do(req)
	responseTime := time.Since(start)

	// Crear la respuesta
	response := &models.Response{
		RequestTime:   start,
		ResponseTime:  responseTime,
		ServiceName:   service.Name,
		ThreadID:      ctx.ID,
		CorrelationID: ctx.CorrelationID,
	}

	if err != nil {
		response.Error = err
		r.Logger.Warnf("Error en petición %s %s: %v", req.Method, req.URL.String(), err)
		return response, nil
	}

	// Leer el cuerpo de la respuesta
	defer resp.Body.Close()
	response.Body, err = io.ReadAll(resp.Body)
	if err != nil {
		response.Error = fmt.Errorf("error al leer el cuerpo de la respuesta: %w", err)
		r.Logger.Warnf("Error al leer respuesta: %v", err)
		return response, nil
	}

	response.StatusCode = resp.StatusCode
	response.Headers = resp.Header

	// Loguear la respuesta HTTP
	r.Logger.Infof("Respuesta %s %s: %d %s (%s)", req.Method, req.URL.String(),
		resp.StatusCode, http.StatusText(resp.StatusCode), responseTime)
	r.Logger.Debugf("Headers respuesta: %v", resp.Header)
	if len(response.Body) > 0 {
		if len(response.Body) > 1000 {
			r.Logger.Debugf("Body respuesta: %s... (truncado)", string(response.Body[:1000]))
		} else {
			r.Logger.Debugf("Body respuesta: %s", string(response.Body))
		}
	}

	return response, nil
}

// collectResults recolecta los resultados de las pruebas
func (r *Runner) collectResults(resultChan chan<- *models.TestResult) {
	// Crear el resultado de la prueba
	result := &models.TestResult{
		StartTime:    r.StartTime,
		ServiceStats: make(map[string]*models.ServiceStats),
		Config:       r.Config,
	}

	// Recolectar los resultados
	for resp := range r.Results {
		// Incrementar el contador de solicitudes
		result.TotalRequests++

		// Verificar si es una solicitud exitosa
		if resp.Error == nil && resp.StatusCode >= 200 && resp.StatusCode < 400 {
			result.SuccessRequests++
		} else {
			result.FailedRequests++
		}

		// Obtener o crear las estadísticas del servicio
		stats, ok := result.ServiceStats[resp.ServiceName]
		if !ok {
			stats = &models.ServiceStats{
				ServiceName:     resp.ServiceName,
				StatusCodes:     make(map[int]int),
				Errors:          make(map[string]int),
				StartTime:       resp.RequestTime,
				MinResponseTime: resp.ResponseTime,
				MaxResponseTime: resp.ResponseTime,
			}
			result.ServiceStats[resp.ServiceName] = stats
		}

		// Actualizar las estadísticas
		stats.TotalRequests++

		if resp.Error == nil && resp.StatusCode >= 200 && resp.StatusCode < 400 {
			stats.SuccessRequests++
		} else {
			stats.FailedRequests++
		}

		// Actualizar los tiempos de respuesta
		if resp.ResponseTime < stats.MinResponseTime {
			stats.MinResponseTime = resp.ResponseTime
		}
		if resp.ResponseTime > stats.MaxResponseTime {
			stats.MaxResponseTime = resp.ResponseTime
		}

		// Actualizar los códigos de estado
		if resp.StatusCode > 0 {
			stats.StatusCodes[resp.StatusCode]++
		}

		// Actualizar los errores
		if resp.Error != nil {
			stats.Errors[resp.Error.Error()]++
		}

		// Actualizar el tiempo de fin
		if resp.RequestTime.After(stats.EndTime) {
			stats.EndTime = resp.RequestTime
		}
	}

	// Calcular las estadísticas por servicio
	for _, stats := range result.ServiceStats {
		// Calcular el tiempo promedio de respuesta
		if stats.TotalRequests > 0 {
			stats.AvgResponseTime = (stats.MinResponseTime + stats.MaxResponseTime) / 2
		}

		// Calcular las solicitudes por segundo
		duration := stats.EndTime.Sub(stats.StartTime)
		if duration > 0 {
			stats.RequestsPerSecond = float64(stats.TotalRequests) / duration.Seconds()
		}
	}

	// Establecer la fecha de fin del resultado
	result.EndTime = time.Now()

	// Enviar el resultado
	resultChan <- result
	close(resultChan)
}

// generateReport genera un reporte de las pruebas
func (r *Runner) generateReport(result *models.TestResult) error {
	// Crear el directorio de reportes si no existe
	if err := os.MkdirAll(r.Config.ReportDir, 0755); err != nil {
		return fmt.Errorf("error al crear el directorio de reportes: %w", err)
	}

	// Generar el nombre del reporte
	reportName := fmt.Sprintf("report_%s.md", time.Now().Format("20060102_150405"))
	reportPath := fmt.Sprintf("%s/%s", r.Config.ReportDir, reportName)

	// Crear el archivo de reporte
	file, err := os.Create(reportPath)
	if err != nil {
		return fmt.Errorf("error al crear el archivo de reporte: %w", err)
	}
	defer file.Close()

	// Escribir el encabezado del reporte
	fmt.Fprintf(file, "# Reporte de Pruebas de Stress\n\n")
	fmt.Fprintf(file, "## Resumen\n\n")
	fmt.Fprintf(file, "- **Fecha de inicio:** %s\n", result.StartTime.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(file, "- **Fecha de fin:** %s\n", result.EndTime.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(file, "- **Duración:** %s\n", reporter.FormatDuration(result.EndTime.Sub(result.StartTime)))
	fmt.Fprintf(file, "- **Total de solicitudes:** %d\n", result.TotalRequests)
	fmt.Fprintf(file, "- **Solicitudes exitosas:** %d (%.2f%%)\n", result.SuccessRequests, float64(result.SuccessRequests)/float64(result.TotalRequests)*100)
	fmt.Fprintf(file, "- **Solicitudes fallidas:** %d (%.2f%%)\n", result.FailedRequests, float64(result.FailedRequests)/float64(result.TotalRequests)*100)

	// Generar y añadir el gráfico de resumen de solicitudes
	svgChart := generateRequestsSummaryChart(result)
	fmt.Fprintf(file, "\n## Gráfico de Solicitudes\n\n")
	fmt.Fprintf(file, "%s\n", svgChart)

	// Escribir las estadísticas por servicio
	fmt.Fprintf(file, "\n## Estadísticas por Servicio\n\n")

	for _, stats := range result.ServiceStats {
		fmt.Fprintf(file, "### %s\n\n", stats.ServiceName)
		fmt.Fprintf(file, "- **Total de solicitudes:** %d\n", stats.TotalRequests)
		fmt.Fprintf(file, "- **Solicitudes exitosas:** %d (%.2f%%)\n", stats.SuccessRequests, float64(stats.SuccessRequests)/float64(stats.TotalRequests)*100)
		fmt.Fprintf(file, "- **Solicitudes fallidas:** %d (%.2f%%)\n", stats.FailedRequests, float64(stats.FailedRequests)/float64(stats.TotalRequests)*100)
		fmt.Fprintf(file, "- **Tiempo mínimo de respuesta:** %s\n", reporter.FormatDuration(stats.MinResponseTime))
		fmt.Fprintf(file, "- **Tiempo máximo de respuesta:** %s\n", reporter.FormatDuration(stats.MaxResponseTime))
		fmt.Fprintf(file, "- **Tiempo promedio de respuesta:** %s\n", reporter.FormatDuration(stats.AvgResponseTime))
		fmt.Fprintf(file, "- **Solicitudes por segundo:** %.2f\n", stats.RequestsPerSecond)

		// Generar y añadir el gráfico de tiempos de respuesta para este servicio
		responseTimeChart := generateResponseTimeChart(stats)
		fmt.Fprintf(file, "\n#### Gráfico de Tiempos de Respuesta\n\n")
		fmt.Fprintf(file, "%s\n", responseTimeChart)

		// Escribir los códigos de estado
		fmt.Fprintf(file, "\n#### Códigos de Estado\n\n")
		for code, count := range stats.StatusCodes {
			fmt.Fprintf(file, "- **%d:** %d (%.2f%%)\n", code, count, float64(count)/float64(stats.TotalRequests)*100)
		}

		// Generar y añadir el gráfico de códigos de estado
		statusCodeChart := generateStatusCodeChart(stats)
		fmt.Fprintf(file, "\n#### Gráfico de Códigos de Estado\n\n")
		fmt.Fprintf(file, "%s\n", statusCodeChart)

		// Escribir los errores
		if len(stats.Errors) > 0 {
			fmt.Fprintf(file, "\n#### Errores\n\n")
			for err, count := range stats.Errors {
				fmt.Fprintf(file, "- **%s:** %d (%.2f%%)\n", err, count, float64(count)/float64(stats.TotalRequests)*100)
			}
		}
	}

	// Escribir la configuración
	fmt.Fprintf(file, "\n## Configuración\n\n")
	fmt.Fprintf(file, "```yaml\n")

	// Usar la configuración original si está disponible
	if cfg, ok := result.Config.(*config.Config); ok && cfg.OriginalConfig != "" {
		// Escribir la configuración original (con referencias a variables de entorno)
		fmt.Fprintf(file, "%s\n", cfg.OriginalConfig)
	} else {
		// Intentar leer el archivo gmeter.yaml
		configPath := "gmeter.yaml"
		if fileExists(configPath) {
			// Leer el contenido del archivo de configuración
			configContent, err := os.ReadFile(configPath)
			if err == nil {
				// Escribir el contenido original del archivo YAML
				fmt.Fprintf(file, "%s\n", string(configContent))
			} else {
				// Si hay un error al leer el archivo, usar la versión JSON
				writeJSONConfig(file, r.Config)
			}
		} else {
			// Si no se encuentra el archivo, usar la versión JSON
			writeJSONConfig(file, r.Config)
		}
	}

	fmt.Fprintf(file, "```\n")

	r.Logger.Infof("Reporte generado: %s", reportPath)
	return nil
}

// fileExists verifica si un archivo existe
func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	return err == nil
}

// writeJSONConfig escribe la configuración en formato JSON (método anterior)
func writeJSONConfig(file *os.File, cfg *config.Config) {
	// Simplificar la configuración para el reporte
	simplifiedConfig := map[string]interface{}{
		"global":   cfg.GlobalConfig,
		"services": cfg.Services,
	}

	// Convertir la configuración simplificada a JSON
	simplifiedJSON, err := json.MarshalIndent(simplifiedConfig, "", "  ")
	if err != nil {
		fmt.Fprintf(file, "Error al convertir la configuración a JSON: %v\n", err)
		return
	}

	fmt.Fprintf(file, "%s\n", simplifiedJSON)
}

// generateRequestsSummaryChart genera un gráfico SVG con el resumen de solicitudes
func generateRequestsSummaryChart(result *models.TestResult) string {
	// Definir dimensiones y colores
	width := 600
	height := 400
	barWidth := 40
	spacing := 60

	// Calcular la posición inicial de las barras
	startX := 100

	// Crear el SVG
	svg := fmt.Sprintf(`<svg width="%d" height="%d" xmlns="http://www.w3.org/2000/svg">`, width, height)

	// Añadir título
	svg += fmt.Sprintf(`<text x="%d" y="30" font-family="Arial" font-size="16" text-anchor="middle" font-weight="bold">Resumen de Solicitudes</text>`, width/2)

	// Dibujar ejes
	svg += fmt.Sprintf(`<line x1="50" y1="%d" x2="%d" y1="%d" y2="%d" stroke="black" stroke-width="2"/>`, height-50, width-50, height-50, height-50) // Eje X
	svg += fmt.Sprintf(`<line x1="50" y1="50" x2="50" y1="%d" y2="%d" stroke="black" stroke-width="2"/>`, height-50, height-50)                      // Eje Y

	// Calcular la escala para el eje Y
	maxValue := float64(result.TotalRequests)
	if maxValue == 0 {
		maxValue = 1 // Evitar división por cero
	}

	// Dibujar barras para cada servicio
	x := startX

	// Ordenar los servicios por nombre para consistencia
	var serviceNames []string
	for name := range result.ServiceStats {
		serviceNames = append(serviceNames, name)
	}
	sort.Strings(serviceNames)

	for _, name := range serviceNames {
		stats := result.ServiceStats[name]

		// Calcular altura de la barra (proporcional al número de solicitudes)
		barHeight := int(float64(stats.TotalRequests) / maxValue * 300)

		// Dibujar la barra
		svg += fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="%d" fill="#4285F4"/>`,
			x, height-50-barHeight, barWidth, barHeight)

		// Añadir etiqueta con el número de solicitudes
		svg += fmt.Sprintf(`<text x="%d" y="%d" font-family="Arial" font-size="12" text-anchor="middle">%d</text>`,
			x+barWidth/2, height-55-barHeight, stats.TotalRequests)

		// Añadir etiqueta con el nombre del servicio
		svg += fmt.Sprintf(`<text x="%d" y="%d" font-family="Arial" font-size="12" text-anchor="middle" transform="rotate(-45, %d, %d)">%s</text>`,
			x+barWidth/2, height-35, x+barWidth/2, height-35, name)

		// Avanzar a la siguiente posición
		x += spacing + barWidth
	}

	// Cerrar el SVG
	svg += `</svg>`

	return svg
}

// generateResponseTimeChart genera un gráfico SVG con los tiempos de respuesta
func generateResponseTimeChart(stats *models.ServiceStats) string {
	// Definir dimensiones y colores
	width := 600
	height := 300

	// Crear el SVG
	svg := fmt.Sprintf(`<svg width="%d" height="%d" xmlns="http://www.w3.org/2000/svg">`, width, height)

	// Añadir título
	svg += fmt.Sprintf(`<text x="%d" y="30" font-family="Arial" font-size="14" text-anchor="middle" font-weight="bold">Tiempos de Respuesta - %s</text>`, width/2, stats.ServiceName)

	// Dibujar ejes
	svg += fmt.Sprintf(`<line x1="50" y1="%d" x2="%d" y1="%d" y2="%d" stroke="black" stroke-width="2"/>`, height-50, width-50, height-50, height-50) // Eje X
	svg += fmt.Sprintf(`<line x1="50" y1="50" x2="50" y1="%d" y2="%d" stroke="black" stroke-width="2"/>`, height-50, height-50)                      // Eje Y

	// Etiquetas de los ejes
	svg += `<text x="50" y="40" font-family="Arial" font-size="12" text-anchor="middle">ms</text>`
	svg += `<text x="300" y="280" font-family="Arial" font-size="12" text-anchor="middle">Tipo</text>`

	// Dibujar barras para min, avg, max
	barWidth := 80
	spacing := 50

	// Convertir durations a milisegundos para mejor visualización
	minMs := float64(stats.MinResponseTime.Microseconds()) / 1000.0
	avgMs := float64(stats.AvgResponseTime.Microseconds()) / 1000.0
	maxMs := float64(stats.MaxResponseTime.Microseconds()) / 1000.0

	// Encontrar el valor máximo para escalar
	maxValue := maxMs
	if maxValue == 0 {
		maxValue = 1 // Evitar división por cero
	}

	// Calcular alturas de las barras
	minHeight := int(minMs / maxValue * 200)
	avgHeight := int(avgMs / maxValue * 200)
	maxHeight := int(maxMs / maxValue * 200)

	// Dibujar las barras
	x := 100

	// Barra de mínimo
	svg += fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="%d" fill="#34A853"/>`,
		x, height-50-minHeight, barWidth, minHeight)
	svg += fmt.Sprintf(`<text x="%d" y="%d" font-family="Arial" font-size="12" text-anchor="middle">%.2f ms</text>`,
		x+barWidth/2, height-55-minHeight, minMs)
	svg += fmt.Sprintf(`<text x="%d" y="%d" font-family="Arial" font-size="12" text-anchor="middle">Mínimo</text>`,
		x+barWidth/2, height-30)

	// Barra de promedio
	x += barWidth + spacing
	svg += fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="%d" fill="#FBBC05"/>`,
		x, height-50-avgHeight, barWidth, avgHeight)
	svg += fmt.Sprintf(`<text x="%d" y="%d" font-family="Arial" font-size="12" text-anchor="middle">%.2f ms</text>`,
		x+barWidth/2, height-55-avgHeight, avgMs)
	svg += fmt.Sprintf(`<text x="%d" y="%d" font-family="Arial" font-size="12" text-anchor="middle">Promedio</text>`,
		x+barWidth/2, height-30)

	// Barra de máximo
	x += barWidth + spacing
	svg += fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="%d" fill="#EA4335"/>`,
		x, height-50-maxHeight, barWidth, maxHeight)
	svg += fmt.Sprintf(`<text x="%d" y="%d" font-family="Arial" font-size="12" text-anchor="middle">%.2f ms</text>`,
		x+barWidth/2, height-55-maxHeight, maxMs)
	svg += fmt.Sprintf(`<text x="%d" y="%d" font-family="Arial" font-size="12" text-anchor="middle">Máximo</text>`,
		x+barWidth/2, height-30)

	// Cerrar el SVG
	svg += `</svg>`

	return svg
}

// generateStatusCodeChart genera un gráfico SVG con los códigos de estado
func generateStatusCodeChart(stats *models.ServiceStats) string {
	// Si no hay códigos de estado, devolver un mensaje
	if len(stats.StatusCodes) == 0 {
		return "<p>No hay datos de códigos de estado disponibles.</p>"
	}

	// Definir dimensiones
	width := 400
	height := 400
	centerX := width / 2
	centerY := height / 2
	radius := float64(150) // Convertir a float64 para operaciones matemáticas

	// Crear el SVG
	svg := fmt.Sprintf(`<svg width="%d" height="%d" xmlns="http://www.w3.org/2000/svg">`, width, height)

	// Añadir título
	svg += fmt.Sprintf(`<text x="%d" y="30" font-family="Arial" font-size="14" text-anchor="middle" font-weight="bold">Códigos de Estado - %s</text>`, width/2, stats.ServiceName)

	// Colores para el gráfico de pastel
	colors := []string{"#4285F4", "#34A853", "#FBBC05", "#EA4335", "#5F6368", "#185ABC", "#137333", "#EA8600", "#B31412", "#3C4043"}

	// Calcular el total de solicitudes para los porcentajes
	total := 0
	for _, count := range stats.StatusCodes {
		total += count
	}

	// Ordenar los códigos para consistencia
	var codes []int
	for code := range stats.StatusCodes {
		codes = append(codes, code)
	}
	sort.Ints(codes)

	// Dibujar el gráfico de pastel
	startAngle := 0.0
	legendY := 50

	for i, code := range codes {
		count := stats.StatusCodes[code]
		percentage := float64(count) / float64(total)
		endAngle := startAngle + percentage*360.0

		// Convertir ángulos a radianes
		startRad := startAngle * math.Pi / 180.0
		endRad := endAngle * math.Pi / 180.0

		// Determinar si el arco es mayor a 180 grados
		largeArcFlag := 0
		if endAngle-startAngle > 180 {
			largeArcFlag = 1
		}

		// Calcular puntos del arco
		x1 := float64(centerX) + radius*math.Sin(startRad)
		y1 := float64(centerY) - radius*math.Cos(startRad)
		x2 := float64(centerX) + radius*math.Sin(endRad)
		y2 := float64(centerY) - radius*math.Cos(endRad)

		// Dibujar el sector
		color := colors[i%len(colors)]
		pathData := fmt.Sprintf("M%d,%d L%.1f,%.1f A%.1f,%.1f 0 %d 1 %.1f,%.1f Z",
			centerX, centerY, x1, y1, radius, radius, largeArcFlag, x2, y2)

		svg += fmt.Sprintf(`<path d="%s" fill="%s" stroke="white" stroke-width="1"/>`, pathData, color)

		// Añadir etiqueta en el centro del sector
		labelAngle := (startAngle + endAngle) / 2
		labelRad := labelAngle * math.Pi / 180.0
		labelDistance := radius * 0.7 // Usar directamente el valor float64
		labelX := float64(centerX) + labelDistance*math.Sin(labelRad)
		labelY := float64(centerY) - labelDistance*math.Cos(labelRad)

		if percentage > 0.05 { // Solo mostrar etiqueta si el sector es suficientemente grande
			svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="Arial" font-size="12" text-anchor="middle" fill="white">%d%%</text>`,
				labelX, labelY, int(percentage*100))
		}

		// Añadir leyenda
		svg += fmt.Sprintf(`<rect x="%d" y="%d" width="15" height="15" fill="%s"/>`, width-100, legendY, color)
		svg += fmt.Sprintf(`<text x="%d" y="%d" font-family="Arial" font-size="12">%d (%d)</text>`,
			width-80, legendY+12, code, count)

		legendY += 25
		startAngle = endAngle
	}

	// Cerrar el SVG
	svg += `</svg>`

	return svg
}

// containsTemplate verifica si una cadena contiene una plantilla
func containsTemplate(s string) bool {
	// Verificar si la cadena contiene "{{" y "}}" para ser considerada una plantilla
	return strings.Contains(s, "{{") && strings.Contains(s, "}}")
}
