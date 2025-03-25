package runner

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"text/template"
	"time"

	"github.com/KaribuLab/gmeter/internal/config"
	"github.com/KaribuLab/gmeter/internal/logger"
	"github.com/KaribuLab/gmeter/internal/models"
	"github.com/KaribuLab/gmeter/internal/reporter"
)

// Runner ejecuta pruebas de stress
type Runner struct {
	cfg           *config.Config
	log           *logger.Logger
	responseLog   *logger.Logger // Logger específico para las respuestas HTTP
	client        *http.Client
	variables     map[string]string     // Para almacenar variables extraídas entre solicitudes
	mutex         sync.RWMutex          // Para acceso seguro a variables compartidas
	activeThreads int32                 // Contador atómico para el número de hilos activos
	csvData       map[string][][]string // Datos cargados desde archivos CSV
	csvHeaders    map[string][]string   // Encabezados de los archivos CSV

	// Variables para limitación de tasa
	globalRateLimiter   *RateLimiter
	serviceRateLimiters map[string]*RateLimiter

	// Variables para estadísticas
	statsMutex sync.Mutex
	testResult *models.TestResult
}

// RateLimiter implementa un limitador de tasa usando el algoritmo token bucket
type RateLimiter struct {
	rate       float64   // Tokens por segundo
	bucketSize int       // Capacidad máxima del bucket
	tokens     float64   // Tokens actuales en el bucket
	lastRefill time.Time // Último momento en que se rellenaron tokens
	mutex      sync.Mutex
}

// NewRateLimiter crea un nuevo limitador de tasa
func NewRateLimiter(rate int) *RateLimiter {
	if rate <= 0 {
		return nil // Sin limitación
	}

	return &RateLimiter{
		rate:       float64(rate),
		bucketSize: rate,          // El tamaño máximo del bucket es igual a la tasa
		tokens:     float64(rate), // Inicializar con el bucket lleno
		lastRefill: time.Now(),
	}
}

// Allow verifica si se permite realizar una solicitud
func (rl *RateLimiter) Allow() bool {
	if rl == nil {
		return true // Si no hay limitador, siempre permitir
	}

	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	// Rellenar tokens basado en el tiempo transcurrido
	now := time.Now()
	elapsed := now.Sub(rl.lastRefill).Seconds()
	rl.lastRefill = now

	// Añadir tokens basados en el tiempo transcurrido y la tasa
	rl.tokens += elapsed * rl.rate

	// Limitar al tamaño máximo del bucket
	if rl.tokens > float64(rl.bucketSize) {
		rl.tokens = float64(rl.bucketSize)
	}

	// Verificar si hay suficientes tokens
	if rl.tokens < 1 {
		return false // No hay suficientes tokens
	}

	// Consumir un token
	rl.tokens--
	return true
}

// WaitUntilAllowed espera hasta que haya un token disponible
func (rl *RateLimiter) WaitUntilAllowed() {
	if rl == nil {
		return // Sin limitación, no hay necesidad de esperar
	}

	for {
		if rl.Allow() {
			return
		}
		// Esperar un poco antes de intentar de nuevo
		time.Sleep(10 * time.Millisecond)
	}
}

func NewRunner(cfg *config.Config, log *logger.Logger) *Runner {
	runner := &Runner{
		cfg:        cfg,
		log:        log,
		client:     &http.Client{Timeout: 30 * time.Second},
		variables:  make(map[string]string),
		csvData:    make(map[string][][]string),
		csvHeaders: make(map[string][]string),
		testResult: &models.TestResult{
			StartTime:    time.Now(),
			ServiceStats: make(map[string]*models.ServiceStats),
			Config:       cfg,
		},
		serviceRateLimiters: make(map[string]*RateLimiter),
	}

	// Inicializar el logger de respuestas si se ha configurado un archivo
	if cfg.ResponseLogFile != "" {
		responseLog := logger.New()
		responseLog.SetLogFile(cfg.ResponseLogFile, false) // Solo salida a archivo, sin consola
		runner.responseLog = responseLog
		log.Info(fmt.Sprintf("Registrando respuestas HTTP en: %s", cfg.ResponseLogFile))
	}

	// Cargar datos desde archivos CSV
	if err := runner.loadCSVData(); err != nil {
		log.Warn(fmt.Sprintf("Error al cargar datos CSV: %s", err.Error()))
	}

	// Inicializar limitador de tasa global si está configurado
	if cfg.GlobalConfig.RequestsPerSecond > 0 {
		runner.globalRateLimiter = NewRateLimiter(cfg.GlobalConfig.RequestsPerSecond)
		log.Info(fmt.Sprintf("Limitación global configurada: %d solicitudes por segundo", cfg.GlobalConfig.RequestsPerSecond))
	}

	// Inicializar limitadores de tasa por servicio si están configurados
	for _, svc := range cfg.Services {
		if svc.RequestsPerSecond > 0 {
			runner.serviceRateLimiters[svc.Name] = NewRateLimiter(svc.RequestsPerSecond)
			log.Info(fmt.Sprintf("Limitación para servicio %s configurada: %d solicitudes por segundo",
				svc.Name, svc.RequestsPerSecond))
		}
	}

	return runner
}

// loadCSVData carga los datos desde los archivos CSV configurados
func (r *Runner) loadCSVData() error {
	for name, source := range r.cfg.DataSources.CSV {
		r.log.Info(fmt.Sprintf("Cargando datos CSV desde %s", source.Path))

		// Abrir el archivo CSV
		file, err := os.Open(source.Path)
		if err != nil {
			return fmt.Errorf("error al abrir archivo CSV %s: %w", source.Path, err)
		}
		defer file.Close()

		// Configurar el lector CSV
		delimiter := ','
		if source.Delimiter != "" {
			delimiter = rune(source.Delimiter[0])
		}
		reader := csv.NewReader(file)
		reader.Comma = delimiter
		reader.LazyQuotes = true // Para manejar comillas de manera más flexible

		// Leer todos los registros
		records, err := reader.ReadAll()
		if err != nil {
			return fmt.Errorf("error al leer archivo CSV %s: %w", source.Path, err)
		}

		// Si no hay registros, continuar con el siguiente archivo
		if len(records) == 0 {
			r.log.Warn(fmt.Sprintf("El archivo CSV %s no contiene datos", source.Path))
			continue
		}

		// Procesar encabezados si es necesario
		var headers []string
		var dataRows [][]string

		if source.HasHeader {
			// El primer registro son los encabezados
			headers = records[0]
			dataRows = records[1:]
		} else {
			// Si no hay encabezados, generarlos (col_1, col_2, etc.)
			headers = make([]string, len(records[0]))
			for i := range headers {
				headers[i] = fmt.Sprintf("col_%d", i+1)
			}
			dataRows = records
		}

		// Almacenar los datos
		r.csvData[name] = dataRows
		r.csvHeaders[name] = headers

		r.log.Info(fmt.Sprintf("Cargados %d registros desde %s", len(dataRows), source.Path))
	}

	return nil
}

// getCSVDataForThread obtiene un registro CSV para un hilo específico
func (r *Runner) getCSVDataForThread(dataSourceName string, threadNum int) map[string]string {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	data := make(map[string]string)

	// Verificar si la fuente de datos existe
	rows, ok := r.csvData[dataSourceName]
	if !ok || len(rows) == 0 {
		r.log.Warn(fmt.Sprintf("Fuente de datos CSV %s no disponible o vacía", dataSourceName))
		return data
	}

	headers := r.csvHeaders[dataSourceName]

	// Seleccionar un registro basado en el número de hilo (circular)
	rowIndex := threadNum % len(rows)
	row := rows[rowIndex]

	// Generar el mapa de variables
	for i, header := range headers {
		if i < len(row) {
			data[header] = row[i]
		}
	}

	return data
}

// Run ejecuta la prueba de stress
func (r *Runner) Run(ctx context.Context) error {
	// Registrar hora de inicio
	r.testResult.StartTime = time.Now()

	// Inicializar el mapa de resumen de códigos de estado
	r.testResult.StatusCodeSummary = make(map[int]int)

	// Inicializar la lista de datos de actividad de hilos
	r.testResult.ThreadActivity = make([]models.ThreadActivityData, 0)

	// Crear un contexto cancelable
	ctx, cancel := context.WithCancel(ctx)
	defer cancel() // Asegurar que se llame a cancel en todas las rutas de salida

	// Parámetros de ejecución
	var minThreads, maxThreads int
	var duration, rampUp time.Duration
	var err error

	// Obtener la configuración de ejecución
	cfg := r.cfg.GlobalConfig

	// Determinar el número de hilos
	minThreads = cfg.MinThreads
	if minThreads <= 0 {
		minThreads = 1 // Al menos 1 hilo
	}

	maxThreads = cfg.MaxThreads
	if maxThreads <= 0 {
		// Si no se especifica maxThreads, usar threadsPerSecond para compatibilidad
		if cfg.ThreadsPerSecond > 0 {
			maxThreads = cfg.ThreadsPerSecond
		} else {
			maxThreads = minThreads // Si no se especifica ninguno, usar minThreads
		}
	}

	// Duración de la prueba
	if cfg.Duration != "" {
		duration, err = time.ParseDuration(cfg.Duration)
		if err != nil {
			return fmt.Errorf("error al analizar la duración: %w", err)
		}
	} else {
		duration = 60 * time.Second // 1 minuto por defecto
	}

	// Tiempo de rampa (incremento gradual de threads)
	if cfg.RampUp != "" {
		rampUp, err = time.ParseDuration(cfg.RampUp)
		if err != nil {
			return fmt.Errorf("error al analizar el tiempo de rampa: %w", err)
		}
	} else {
		rampUp = 0 // Sin rampa por defecto
	}

	r.log.Info("Iniciando prueba de stress con configuración:")
	r.log.Info("- Hilos mínimos:", minThreads)
	r.log.Info("- Hilos máximos:", maxThreads)
	r.log.Info("- Duración:", duration)
	r.log.Info("- Tiempo de rampa:", rampUp)

	startTime := time.Now()
	endTime := startTime.Add(duration)
	rampEndTime := startTime

	// Si hay rampa, calcular cuando terminará
	if rampUp > 0 {
		rampEndTime = startTime.Add(rampUp)
	}

	// Variables para controlar la ejecución
	var threadCount int = 0
	var wg sync.WaitGroup

	// Variables para calcular solicitudes por segundo en intervalos
	lastRequestCount := 0
	lastSampleTime := time.Now()

	// Variables para el control preciso de la rampa
	var totalThreadsToLaunch int
	var remainingThreadsToLaunch int
	var threadBatchInterval time.Duration
	var nextBatchTime time.Time
	var threadsPerBatch int

	// Si hay rampa, preparar el lanzamiento por lotes
	if rampUp > 0 && maxThreads > minThreads {
		// Calcular cuántos hilos hay que lanzar durante la rampa
		totalThreadsToLaunch = maxThreads - minThreads
		remainingThreadsToLaunch = totalThreadsToLaunch

		// Obtener el tamaño de lote configurado
		batchSize := cfg.BatchSize

		// Si el tamaño de lote no está configurado o es inválido, usar un valor por defecto
		if batchSize <= 0 {
			batchSize = 50 // Valor por defecto
		}

		// Calcular cuántos lotes necesitamos según el tamaño configurado
		numBatches := totalThreadsToLaunch / batchSize
		if numBatches < 1 {
			numBatches = 1
		}

		// Recalcular el tamaño real de cada lote para distribuir uniformemente
		threadsPerBatch = totalThreadsToLaunch / numBatches
		if threadsPerBatch < 1 {
			threadsPerBatch = 1
		}

		// Calcular el intervalo entre lotes
		threadBatchInterval = rampUp / time.Duration(numBatches)

		// Establecer cuándo se lanzará el primer lote
		nextBatchTime = startTime.Add(threadBatchInterval)

		r.log.Info(fmt.Sprintf("Configuración de rampa: lanzando ~%d hilos cada %s (configurado: %d hilos por lote)",
			threadsPerBatch, threadBatchInterval, batchSize))
	}

	// Control de la tasa de creación de hilos
	ticker := time.NewTicker(100 * time.Millisecond) // Aumentamos la frecuencia para mayor precisión
	defer ticker.Stop()

	// Ticker para muestrear la actividad de hilos
	threadSampler := time.NewTicker(1 * time.Second)
	defer threadSampler.Stop()

	// Crear hilos iniciales (minThreads)
	for i := 0; i < minThreads; i++ {
		wg.Add(1)
		threadCount++

		// Incrementar el contador de hilos activos
		atomic.AddInt32(&r.activeThreads, 1)

		go func(threadNum int) {
			defer wg.Done()
			defer atomic.AddInt32(&r.activeThreads, -1)

			// Ejecutar el hilo
			r.executeThread(ctx, threadNum)
		}(threadCount)
	}

	// Informar sobre los hilos iniciales
	r.log.Info(fmt.Sprintf("%d hilos iniciales lanzados. Iniciando fase de rampa...", minThreads))

mainLoop:
	for {
		select {
		case <-ctx.Done():
			// Contexto cancelado (por ejemplo, Ctrl+C)
			r.log.Info("Recibida señal de cancelación, deteniendo prueba...")
			break mainLoop

		case <-threadSampler.C:
			// Registrar datos de actividad de hilos cada segundo
			r.statsMutex.Lock()

			// Obtener cantidad de hilos activos
			activeThreads := int(atomic.LoadInt32(&r.activeThreads))

			// Actualizar el máximo de hilos activos si es necesario
			if activeThreads > r.testResult.MaxActiveThreads {
				r.testResult.MaxActiveThreads = activeThreads
			}

			// Calcular las solicitudes por segundo
			now := time.Now()
			elapsedSeconds := now.Sub(lastSampleTime).Seconds()
			requestsPerSecond := 0.0

			if elapsedSeconds > 0 {
				requestDelta := r.testResult.TotalRequests - lastRequestCount
				requestsPerSecond = float64(requestDelta) / elapsedSeconds

				// Ajustar la tasa si parece anormalmente baja comparada con el número de hilos activos
				// Si hay muchos hilos activos pero pocas solicitudes, podría haber un problema de medición
				if activeThreads > 10 && requestsPerSecond < float64(activeThreads)/2 {
					r.log.Debug(fmt.Sprintf("Posible subestimación de solicitudes/s: %d hilos activos pero solo %.2f solicitudes/s",
						activeThreads, requestsPerSecond))
				}

				lastRequestCount = r.testResult.TotalRequests
				lastSampleTime = now
			}

			// Registrar datos de actividad
			activityData := models.ThreadActivityData{
				Timestamp:     now,
				ActiveThreads: activeThreads,
				RequestsPS:    requestsPerSecond,
			}

			r.testResult.ThreadActivity = append(r.testResult.ThreadActivity, activityData)

			// Verificar si todos los hilos han sido lanzados y procesados
			// Si ya alcanzamos el máximo de hilos configurado y no quedan más por lanzar
			if threadCount >= maxThreads && remainingThreadsToLaunch <= 0 {
				// Y si la actividad de hilos ha caído por debajo de un umbral mínimo (20% del máximo)
				minimumActiveThreshold := int(float64(maxThreads) * 0.2)
				if activeThreads <= minimumActiveThreshold {
					r.log.Info(fmt.Sprintf("Todos los hilos lanzados (%d) y procesados. Finalizando prueba anticipadamente...", threadCount))
					r.statsMutex.Unlock()
					break mainLoop
				}
			}

			r.statsMutex.Unlock()

			// Mostrar información detallada en el log
			r.log.Info(fmt.Sprintf("Hilos activos: %d/%d (%.1f%%), Solicitudes/s: %.2f, Total solicitudes: %d",
				activeThreads, maxThreads, float64(activeThreads)/float64(maxThreads)*100,
				requestsPerSecond, r.testResult.TotalRequests))

		case <-ticker.C:
			now := time.Now()

			// Verificar si hemos excedido la duración total
			if now.After(endTime) {
				r.log.Info("Alcanzada duración de la prueba, deteniendo lanzamiento de nuevos hilos...")
				break mainLoop
			}

			// Manejar el lanzamiento de hilos durante la rampa
			if rampUp > 0 && now.Before(rampEndTime) && remainingThreadsToLaunch > 0 {
				// Verificar si es hora de lanzar un nuevo lote
				if now.After(nextBatchTime) || now.Equal(nextBatchTime) {
					// Calcular cuántos hilos lanzar en este lote
					batchSize := threadsPerBatch

					// En los primeros lotes, podemos ser más agresivos
					// Usar un tamaño de lote mayor para alcanzar más rápido el máximo
					if threadCount < (maxThreads / 2) {
						// Si estamos por debajo del 50% del objetivo, incrementar el tamaño del lote
						batchSize = threadsPerBatch * 2
					}

					if batchSize > remainingThreadsToLaunch {
						batchSize = remainingThreadsToLaunch
					}

					r.log.Info(fmt.Sprintf("Lanzando lote de %d hilos adicionales", batchSize))

					// Lanzar el lote de hilos con un poco de paralelismo para acelerar
					// el lanzamiento de los hilos
					launchWg := sync.WaitGroup{}
					currentThreadCount := threadCount // Guardar el valor actual para calcular índices
					threadCount += batchSize          // Actualizar antes para evitar condiciones de carrera

					// Usamos goroutines para lanzar los hilos en paralelo en grupos de 20
					for i := 0; i < batchSize; i += 20 {
						launchWg.Add(1)
						go func(startIdx, count int) {
							defer launchWg.Done()

							endIdx := startIdx + count
							if endIdx > batchSize {
								endIdx = batchSize
							}

							for j := startIdx; j < endIdx; j++ {
								wg.Add(1)
								localThreadNum := currentThreadCount + 1 + j

								// Incrementar el contador de hilos activos
								atomic.AddInt32(&r.activeThreads, 1)

								go func(threadNum int) {
									defer wg.Done()
									defer atomic.AddInt32(&r.activeThreads, -1)

									// Ejecutar el hilo
									r.executeThread(ctx, threadNum)
								}(localThreadNum)
							}
						}(i, 20)
					}

					// Esperar a que todos los hilos se hayan lanzado
					launchWg.Wait()

					// Actualizar contadores y programar el próximo lote
					remainingThreadsToLaunch -= batchSize
					nextBatchTime = nextBatchTime.Add(threadBatchInterval)

					r.log.Info(fmt.Sprintf("Total de hilos lanzados: %d, Restantes por lanzar: %d",
						threadCount, remainingThreadsToLaunch))
				}
			} else if rampUp > 0 && now.After(rampEndTime) && threadCount < maxThreads {
				// Si ya pasó el tiempo de rampa pero aún no se han lanzado todos los hilos,
				// lanzar los hilos restantes de una vez
				remainingThreads := maxThreads - threadCount

				if remainingThreads > 0 {
					r.log.Info(fmt.Sprintf("Fin de rampa. Lanzando %d hilos restantes para alcanzar el máximo", remainingThreads))

					// Lanzamiento paralelo de los hilos restantes
					launchWg := sync.WaitGroup{}
					currentThreadCount := threadCount
					threadCount += remainingThreads

					// Lanzar los hilos restantes en paralelo en grupos de 20
					for i := 0; i < remainingThreads; i += 20 {
						launchWg.Add(1)
						go func(startIdx, count int) {
							defer launchWg.Done()

							endIdx := startIdx + count
							if endIdx > remainingThreads {
								endIdx = remainingThreads
							}

							for j := startIdx; j < endIdx; j++ {
								wg.Add(1)
								localThreadNum := currentThreadCount + 1 + j

								// Incrementar el contador de hilos activos
								atomic.AddInt32(&r.activeThreads, 1)

								go func(threadNum int) {
									defer wg.Done()
									defer atomic.AddInt32(&r.activeThreads, -1)

									// Ejecutar el hilo
									r.executeThread(ctx, threadNum)
								}(localThreadNum)
							}
						}(i, 20)
					}

					// Esperar a que se lancen todos los hilos
					launchWg.Wait()

					r.log.Info(fmt.Sprintf("Todos los %d hilos han sido lanzados", maxThreads))
				}
			}
		}
	}

	// Esperar a que todos los hilos terminen
	activeThreads := atomic.LoadInt32(&r.activeThreads)
	r.log.Info(fmt.Sprintf("Esperando a que terminen %d hilos activos...", activeThreads))

	// Proporcionar un periodo de gracia para que terminen las peticiones en curso
	gracePeriod := 5 * time.Second
	r.log.Info(fmt.Sprintf("Esperando un periodo de gracia de %s para terminar las peticiones en curso...", gracePeriod))
	time.Sleep(gracePeriod)

	// Ahora sí podemos cancelar el contexto
	cancel()

	// Esperar a que terminen todos los hilos
	wg.Wait()

	// Registrar hora de finalización
	r.testResult.EndTime = time.Now()

	// Generar el reporte
	if r.cfg.ReportDir != "" {
		r.log.Info(fmt.Sprintf("Generando reporte en %s...", r.cfg.ReportDir))
		reportPath, err := reporter.GenerateReport(r.testResult, r.cfg.ReportDir, r.cfg.ReportName)
		if err != nil {
			r.log.Error(fmt.Sprintf("Error al generar el reporte: %s", err.Error()))
		} else {
			r.log.Info(fmt.Sprintf("Reporte generado: %s", reportPath))
		}
	}

	r.log.Info(fmt.Sprintf("Prueba de stress completada. Total hilos ejecutados: %d", threadCount))
	r.log.Info(fmt.Sprintf("Total solicitudes: %d (Exitosas: %d, Fallidas: %d)",
		r.testResult.TotalRequests, r.testResult.SuccessRequests, r.testResult.FailedRequests))

	return nil
}

// executeThread ejecuta un hilo de prueba procesando todos los servicios configurados
func (r *Runner) executeThread(ctx context.Context, threadNum int) {
	// Clonar el mapa de variables para este hilo
	threadVars := r.cloneVariables()

	// Agregar un mapa para llevar registro de los servicios ejecutados en este hilo
	executedServices := make(map[string]bool)

	r.log.Debug(fmt.Sprintf("Hilo %d iniciado", threadNum))

	// En lugar de un número fijo de ciclos, ejecutamos hasta que el contexto sea cancelado
	cycleCount := 0

	for {
		// Verificar si el contexto ya fue cancelado
		select {
		case <-ctx.Done():
			r.log.Debug(fmt.Sprintf("Hilo %d cancelado (fin de prueba) después de %d ciclos", threadNum, cycleCount))
			return
		default:
			// Continuar con la ejecución
		}

		cycleCount++
		r.log.Debug(fmt.Sprintf("Hilo %d: Iniciando ciclo %d", threadNum, cycleCount))

		// Reiniciar el mapa de servicios ejecutados al comienzo de cada ciclo
		for k := range executedServices {
			delete(executedServices, k)
		}

		// Ejecutar cada servicio en el orden definido
		for i, service := range r.cfg.Services {
			// Verificar si el contexto ya fue cancelado
			select {
			case <-ctx.Done():
				r.log.Debug(fmt.Sprintf("Hilo %d cancelado (fin de prueba) en ciclo %d, servicio %s",
					threadNum, cycleCount, service.Name))
				return
			default:
				// Continuar con la ejecución
			}

			// Loguear progreso de servicios
			r.log.Debug(fmt.Sprintf("Hilo %d, Ciclo %d: Ejecutando servicio %d/%d: %s",
				threadNum, cycleCount, i+1, len(r.cfg.Services), service.Name))

			// Si el servicio depende de otro, verificar que se haya ejecutado
			if service.DependsOn != "" {
				// Comprobar si el servicio del que depende ya fue ejecutado en este ciclo
				if !executedServices[service.DependsOn] {
					r.log.Debug(fmt.Sprintf("Hilo %d: Omitiendo servicio %s porque depende de %s que no ha sido ejecutado en este ciclo",
						threadNum, service.Name, service.DependsOn))
					continue
				}

				// Si también se requiere una variable específica, verificar que exista
				if threadVars[service.DependsOn] == "" {
					r.log.Debug(fmt.Sprintf("Hilo %d: El servicio %s requiere la variable %s que no está disponible a pesar de haberse ejecutado el servicio dependiente",
						threadNum, service.Name, service.DependsOn))
					// Continuamos de todas formas, ya que el servicio dependiente fue ejecutado
				}
			}

			// Cargar datos de la fuente de datos para este servicio
			if service.DataSourceName != "" {
				csvData := r.getCSVDataForThread(service.DataSourceName, threadNum)
				// Añadir los datos al mapa de variables del hilo
				for k, v := range csvData {
					threadVars[k] = v
					r.log.Debug(fmt.Sprintf("Hilo %d: Cargando variable %s = %s desde CSV %s",
						threadNum, k, v, service.DataSourceName))
				}
			}

			// Ejecutar el servicio
			result, err := r.executeService(ctx, service, threadVars, threadNum)
			if err != nil {
				// Comprobar si es un error de cancelación (fin de prueba)
				if ctx.Err() != nil || strings.Contains(err.Error(), "cancelad") {
					r.log.Debug(fmt.Sprintf("Hilo %d: Servicio %s cancelado (fin de prueba)", threadNum, service.Name))
					return // Terminar el hilo completamente
				} else {
					r.log.Warn(fmt.Sprintf("Hilo %d: Error al ejecutar el servicio %s: %s", threadNum, service.Name, err.Error()))

					// Registrar el error en las estadísticas
					r.registerServiceResult(service.Name, 0, 0, err.Error(), threadNum)
				}
				continue
			}

			// Marcar este servicio como ejecutado en este ciclo
			executedServices[service.Name] = true

			// Registrar el resultado exitoso
			statusCode := 0
			responseTime := time.Duration(0)
			if result != nil {
				statusCode = result.statusCode
				responseTime = result.responseTime
			}
			r.registerServiceResult(service.Name, statusCode, responseTime, "", threadNum)

			// Si el servicio extrajo variables, actualizar el mapa de variables del hilo
			if result != nil && len(result.extractedVars) > 0 {
				for k, v := range result.extractedVars {
					threadVars[k] = v
					r.log.Debug(fmt.Sprintf("Hilo %d: Variable extraída %s = %s", threadNum, k, v))
				}
			}

			// Pausa breve entre servicios para simular pensamiento/procesamiento
			// Variable según el número de hilo para evitar sincronización
			pauseMs := 50 + (threadNum % 50) // Reducir la pausa para aumentar el rendimiento
			time.Sleep(time.Duration(pauseMs) * time.Millisecond)
		}

		r.log.Debug(fmt.Sprintf("Hilo %d: Completado ciclo %d", threadNum, cycleCount))

		// Pausa breve entre ciclos
		pauseMs := 100 + (threadNum % 400) // Reducir la pausa entre ciclos
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(pauseMs) * time.Millisecond):
			// Continuar después de la pausa
		}
	}
}

// Estructura para almacenar el resultado de la ejecución de un servicio
type serviceResult struct {
	statusCode    int
	responseTime  time.Duration
	extractedVars map[string]string
}

// executeService ejecuta un servicio HTTP y devuelve la respuesta
func (r *Runner) executeService(ctx context.Context, service *config.Service, vars map[string]string, threadNum int) (*serviceResult, error) {
	// Verificar si el contexto fue cancelado antes de empezar
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Registrar el inicio de la ejecución del servicio
	r.log.Debug(fmt.Sprintf("Hilo %d: Iniciando ejecución del servicio %s", threadNum, service.Name))

	// Aplicar limitación de tasa global si está configurada
	if r.globalRateLimiter != nil {
		r.log.Debug(fmt.Sprintf("Hilo %d: Esperando permiso del limitador global para ejecutar %s",
			threadNum, service.Name))
		r.globalRateLimiter.WaitUntilAllowed()
	}

	// Aplicar limitación de tasa específica del servicio si está configurada
	if limiter, exists := r.serviceRateLimiters[service.Name]; exists {
		r.log.Debug(fmt.Sprintf("Hilo %d: Esperando permiso del limitador específico para ejecutar %s",
			threadNum, service.Name))
		limiter.WaitUntilAllowed()
	}

	// Registrar el momento en que se inicia la solicitud
	startTime := time.Now()

	// Preparar la URL con las variables
	urlStr := r.replaceVariables(service.URL, vars)

	// Crear la solicitud HTTP
	method := strings.ToUpper(service.Method)
	if method == "" {
		method = "GET"
	}

	var reqBody io.Reader

	// Manejar diferentes tipos de contenido
	contentType := service.ContentType
	if contentType == "" {
		contentType = "json" // Por defecto
	}

	switch contentType {
	case "form":
		// Preparar datos de formulario
		form := url.Values{}

		// Procesar los datos del formulario
		for key, value := range service.FormData {
			form.Add(key, r.replaceVariables(value, vars))
		}

		// Procesar los datos del formulario basados en plantillas
		for key, tpl := range service.FormDataTemplate {
			form.Add(key, r.replaceVariables(tpl, vars))
		}

		reqBody = strings.NewReader(form.Encode())
	default:
		// JSON u otro tipo de contenido (texto plano)
		if service.Body != "" {
			// Aplicar templates al cuerpo JSON
			processedBody := r.replaceVariables(service.Body, vars)
			reqBody = strings.NewReader(processedBody)

			// Guardar el cuerpo procesado para el log
			service.ProcessedBody = processedBody
		}
	}

	// Crear la solicitud HTTP con un timeout específico
	req, err := http.NewRequestWithContext(ctx, method, urlStr, reqBody)
	if err != nil {
		return nil, fmt.Errorf("error al crear la solicitud: %w", err)
	}

	// Agregar encabezados
	for key, value := range service.Headers {
		req.Header.Set(key, r.replaceVariables(value, vars))
	}

	// Establecer el tipo de contenido predeterminado para POST/PUT
	if (method == "POST" || method == "PUT") && req.Header.Get("Content-Type") == "" {
		if contentType == "form" {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		} else {
			req.Header.Set("Content-Type", "application/json")
		}
	}

	// Usar un cliente con un timeout razonable para esta petición específica
	// para evitar que las peticiones se queden colgadas
	clientTimeout := 30 * time.Second
	client := &http.Client{
		Timeout:   clientTimeout,
		Transport: r.client.Transport,
	}

	// Ejecutar la solicitud HTTP
	resp, err := client.Do(req)

	// Si el error está relacionado con un contexto cancelado, manejarlo de forma más amigable
	if err != nil {
		if ctx.Err() != nil {
			// El contexto fue cancelado, lo cual es esperado al finalizar la prueba
			return nil, fmt.Errorf("petición cancelada (fin de prueba)")
		}
		return nil, fmt.Errorf("error al ejecutar la solicitud: %w", err)
	}

	defer resp.Body.Close()

	// Calcular el tiempo de respuesta
	responseTime := time.Since(startTime)

	// Leer el cuerpo de la respuesta
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error al leer la respuesta: %w", err)
	}

	// Registrar la respuesta
	r.log.Debug(fmt.Sprintf("Hilo %d: Servicio %s respondió con código %d en %v",
		threadNum, service.Name, resp.StatusCode, responseTime))

	// Extraer variables de la respuesta si es necesario
	extractedVars := make(map[string]string)

	bodyStr := string(responseBody)

	// Extraer variables usando el nuevo mecanismo ExtractVars
	if len(service.ExtractVars) > 0 {
		for varName, regexPattern := range service.ExtractVars {
			// Compilar la expresión regular
			pattern, err := regexp.Compile(regexPattern)
			if err != nil {
				r.log.Warn(fmt.Sprintf("Hilo %d: Error al compilar regex '%s' para variable %s: %s",
					threadNum, regexPattern, varName, err.Error()))
				continue
			}

			// Intentar extraer el valor usando la regex
			matches := pattern.FindStringSubmatch(bodyStr)

			if len(matches) > 1 {
				// El primer grupo capturado es el valor que queremos extraer
				extractedVars[varName] = matches[1]
				r.log.Debug(fmt.Sprintf("Hilo %d: Variable extraída %s = %s usando regex: %s",
					threadNum, varName, matches[1], regexPattern))
			} else {
				r.log.Debug(fmt.Sprintf("Hilo %d: No se pudo extraer la variable %s usando regex: %s",
					threadNum, varName, regexPattern))
			}
		}
	}

	// Por retrocompatibilidad, seguimos soportando ExtractToken/TokenName
	if service.ExtractToken != "" && service.TokenName != "" && extractedVars[service.TokenName] == "" {
		rule := service.ExtractToken
		varName := service.TokenName

		r.log.Debug(fmt.Sprintf("Hilo %d: Intentando extraer token con método legacy", threadNum))

		// Extraer value del JSON usando una regex simple
		jsonPattern := fmt.Sprintf(`"%s"\s*:\s*"([^"]+)"`, rule)
		pattern := regexp.MustCompile(jsonPattern)
		matches := pattern.FindStringSubmatch(bodyStr)

		if len(matches) > 1 {
			// El primer grupo capturado es el valor que queremos extraer
			extractedVars[varName] = matches[1]
			r.log.Debug(fmt.Sprintf("Hilo %d: Token extraído para %s: %s", threadNum, varName, matches[1]))
		} else {
			// Si no funciona con la regex sencilla, intentar con otras técnicas
			r.log.Debug(fmt.Sprintf("Hilo %d: No se pudo extraer el token usando regex sencilla. Probando otras técnicas", threadNum))

			// Intentar con la regex original proporcionada
			originalPattern := regexp.MustCompile(rule)
			origMatches := originalPattern.FindStringSubmatch(bodyStr)

			if len(origMatches) > 1 {
				extractedVars[varName] = origMatches[1]
				r.log.Debug(fmt.Sprintf("Hilo %d: Token extraído con regex original para %s: %s", threadNum, varName, origMatches[1]))
			} else {
				r.log.Debug(fmt.Sprintf("Hilo %d: No se pudo extraer el token usando ninguna técnica",
					threadNum))
			}
		}
	}

	// Añadir una variable especial con el nombre del servicio para indicar que se ejecutó correctamente
	// Esto permite que otros servicios puedan depender de este sin necesidad de extraer variables específicas
	extractedVars[service.Name] = "executed"
	r.log.Debug(fmt.Sprintf("Hilo %d: Servicio %s marcado como ejecutado", threadNum, service.Name))

	// Registrar la respuesta en el archivo de log si está configurado
	r.logResponse(threadNum, service, req, resp, responseBody, responseTime, extractedVars)

	return &serviceResult{
		statusCode:    resp.StatusCode,
		responseTime:  responseTime,
		extractedVars: extractedVars,
	}, nil
}

// logResponse registra la respuesta HTTP en el archivo de log
func (r *Runner) logResponse(threadNum int, service *config.Service, req *http.Request, resp *http.Response, responseBody []byte, responseTime time.Duration, extractedVars map[string]string) {
	// Si no hay un logger de respuestas configurado, no hacer nada
	if r.responseLog == nil {
		return
	}

	// Crear un resumen de la respuesta para el log
	logEntry := map[string]interface{}{
		"timestamp":     time.Now().Format(time.RFC3339),
		"thread_id":     threadNum,
		"service":       service.Name,
		"method":        req.Method,
		"url":           req.URL.String(),
		"status_code":   resp.StatusCode,
		"response_time": responseTime.Milliseconds(),
	}

	// Añadir los headers de la solicitud
	requestHeaders := make(map[string]string)
	for key, values := range req.Header {
		if len(values) > 0 {
			requestHeaders[key] = values[0]
		}
	}
	logEntry["request_headers"] = requestHeaders

	// Añadir el cuerpo de la solicitud si existe
	if service.ProcessedBody != "" {
		// Usar el cuerpo procesado que guardamos durante executeService
		requestBodyStr := service.ProcessedBody

		// Limitar el tamaño del cuerpo
		maxReqBodySize := 4096 // 4KB máximo
		if len(requestBodyStr) > maxReqBodySize {
			requestBodyStr = requestBodyStr[:maxReqBodySize] + "... [truncado]"
		}

		logEntry["request_body"] = requestBodyStr
	} else if req.Body != nil && req.ContentLength > 0 {
		// Estamos en el caso de un formulario
		var requestBodyStr string

		switch strings.ToLower(service.ContentType) {
		case "form", "form-urlencoded", "x-www-form-urlencoded":
			// Reconstruir el formulario a partir de los datos del servicio
			formValues := url.Values{}
			for key, value := range service.FormData {
				formValues.Add(key, value)
			}
			requestBodyStr = formValues.Encode()
		default:
			// Para JSON u otros tipos, usar el cuerpo configurado
			requestBodyStr = service.Body
		}

		// Limitar el tamaño del cuerpo de la solicitud para el log
		maxReqBodySize := 4096 // 4KB máximo
		if len(requestBodyStr) > maxReqBodySize {
			requestBodyStr = requestBodyStr[:maxReqBodySize] + "... [truncado]"
		}

		logEntry["request_body"] = requestBodyStr
	}

	// Añadir los headers de la respuesta
	responseHeaders := make(map[string]string)
	for key, values := range resp.Header {
		if len(values) > 0 {
			responseHeaders[key] = values[0]
		}
	}
	logEntry["response_headers"] = responseHeaders

	// Limitar el tamaño del cuerpo de la respuesta para evitar logs demasiado grandes
	maxBodySize := 4096 // 4KB máximo
	responseBodyStr := string(responseBody)
	if len(responseBodyStr) > maxBodySize {
		responseBodyStr = responseBodyStr[:maxBodySize] + "... [truncado]"
	}
	logEntry["response_body"] = responseBodyStr

	// Añadir variables extraídas si hay alguna
	if len(extractedVars) > 0 {
		logEntry["extracted_vars"] = extractedVars
	}

	// Registrar la solicitud y respuesta en formato JSON
	r.responseLog.WithFields(logEntry).Info(fmt.Sprintf("HTTP [%s] %s - Hilo %d - Servicio %s - Código %d - Tiempo %dms",
		req.Method, req.URL.String(), threadNum, service.Name, resp.StatusCode, responseTime.Milliseconds()))
}

// registerServiceResult registra el resultado de un servicio para su inclusión en el reporte
func (r *Runner) registerServiceResult(serviceName string, statusCode int, responseTime time.Duration, errStr string, threadNum int) {
	r.statsMutex.Lock()
	defer r.statsMutex.Unlock()

	// Incrementar contadores globales
	r.testResult.TotalRequests++
	if errStr == "" && statusCode >= 200 && statusCode < 400 {
		r.testResult.SuccessRequests++
		// Log de éxito detallado
		r.log.Debug(fmt.Sprintf("Hilo %d: Servicio %s completado con éxito - Código: %d, Tiempo: %v",
			threadNum, serviceName, statusCode, responseTime))
	} else {
		r.testResult.FailedRequests++
		// Log de error detallado
		if errStr != "" {
			r.log.Warn(fmt.Sprintf("Hilo %d: Servicio %s falló - Error: %s",
				threadNum, serviceName, errStr))
		} else {
			r.log.Warn(fmt.Sprintf("Hilo %d: Servicio %s falló - Código: %d, Tiempo: %v",
				threadNum, serviceName, statusCode, responseTime))
		}
	}

	// Actualizar el resumen de códigos de estado
	if statusCode > 0 {
		r.testResult.StatusCodeSummary[statusCode]++
	}

	// Obtener o crear las estadísticas del servicio
	stats, ok := r.testResult.ServiceStats[serviceName]
	if !ok {
		stats = &models.ServiceStats{
			ServiceName: serviceName,
			StartTime:   time.Now(),
			StatusCodes: make(map[int]int),
			Errors:      make(map[string]int),
		}
		r.testResult.ServiceStats[serviceName] = stats
	}

	// Actualizar estadísticas del servicio
	stats.TotalRequests++
	stats.EndTime = time.Now()

	if errStr == "" && statusCode >= 200 && statusCode < 400 {
		stats.SuccessRequests++
	} else {
		stats.FailedRequests++
	}

	// Registrar el código de estado
	if statusCode > 0 {
		stats.StatusCodes[statusCode]++
	}

	// Registrar el error si hubo
	if errStr != "" {
		stats.Errors[errStr]++
	}

	// Actualizar tiempos de respuesta
	if responseTime > 0 {
		if stats.TotalRequests == 1 || responseTime < stats.MinResponseTime {
			stats.MinResponseTime = responseTime
		}
		if responseTime > stats.MaxResponseTime {
			stats.MaxResponseTime = responseTime
		}

		// Cálculo acumulativo del promedio
		stats.AvgResponseTime = stats.AvgResponseTime + (responseTime-stats.AvgResponseTime)/time.Duration(stats.TotalRequests)
	}

	// Actualizar solicitudes por segundo
	timeElapsed := stats.EndTime.Sub(stats.StartTime).Seconds()
	if timeElapsed > 0 {
		stats.RequestsPerSecond = float64(stats.TotalRequests) / timeElapsed
	}
}

// replaceVariables reemplaza las variables en un string con sus valores correspondientes
func (r *Runner) replaceVariables(input string, vars map[string]string) string {
	if input == "" {
		return input
	}

	// Primero, reemplazamos variables especiales como {{uuid}}
	specialPattern := regexp.MustCompile(`\{\{uuid\}\}`)
	input = specialPattern.ReplaceAllStringFunc(input, func(match string) string {
		return generateUUID()
	})

	// Crear funciones personalizadas para el template
	funcMap := template.FuncMap{
		"uuid": generateUUID,
	}

	// Preparar el template
	tmpl, err := template.New("template").Funcs(funcMap).Parse(input)
	if err != nil {
		// Si hay un error al analizar el template, intentar con el método anterior
		r.log.Debug(fmt.Sprintf("Error al procesar template: %s - Usando método alternativo", err.Error()))
		return r.legacyReplaceVariables(input, vars)
	}

	// Crear un buffer para almacenar el resultado
	var buf bytes.Buffer

	// Ejecutar el template con las variables
	err = tmpl.Execute(&buf, vars)
	if err != nil {
		// Si hay un error al ejecutar el template, intentar con el método anterior
		r.log.Debug(fmt.Sprintf("Error al ejecutar template: %s - Usando método alternativo", err.Error()))
		return r.legacyReplaceVariables(input, vars)
	}

	return buf.String()
}

// legacyReplaceVariables es el método antiguo de reemplazo usando expresiones regulares
// Se usa como fallback si el método de templates falla
func (r *Runner) legacyReplaceVariables(input string, vars map[string]string) string {
	result := input

	// Patrón para detectar referencias a variables: {{variable}} o {{.variable}}
	pattern := regexp.MustCompile(`\{\{([^{}]+)\}\}`)

	return pattern.ReplaceAllStringFunc(result, func(match string) string {
		// Extraer el nombre de la variable (quitar {{ y }})
		varName := strings.TrimSpace(match[2 : len(match)-2])

		// Caso especial: generar un UUID v4
		if varName == "uuid" {
			return generateUUID()
		}

		// Caso especial: variables de data_source con formato {{.variable}}
		if strings.HasPrefix(varName, ".") {
			dataSourceVar := strings.TrimPrefix(varName, ".")
			// Buscar en el mapa de variables con el nombre sin el punto
			if value, ok := vars[dataSourceVar]; ok {
				return value
			}
			// Si no se encuentra, intentar con el nombre completo (incluyendo el punto)
			if value, ok := vars[varName]; ok {
				return value
			}
			r.log.Debug(fmt.Sprintf("Variable de data_source no encontrada: %s", dataSourceVar))
			return match // Mantener la referencia original si no se encuentra
		}

		// Caso normal: buscar la variable en el mapa
		if value, ok := vars[varName]; ok {
			return value
		}

		// Si no se encuentra, devolver la referencia original
		return match
	})
}

// generateUUID genera un UUID v4 único
func generateUUID() string {
	// Implementar usando un algoritmo simple sin dependencias externas
	// Este es un método básico para generar un ID único similar a un UUID v4
	// Para entornos de producción, considerar usar github.com/google/uuid

	// Formato UUID: 8-4-4-4-12 caracteres (hexadecimal)
	const chars = "0123456789abcdef"
	uuid := make([]byte, 36)

	// Generar caracteres aleatorios
	for i := 0; i < 36; i++ {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			uuid[i] = '-'
		} else if i == 14 {
			uuid[i] = '4' // Versión 4
		} else if i == 19 {
			uuid[i] = chars[8+(time.Now().Nanosecond()%4)] // 8, 9, a, o b
		} else {
			uuid[i] = chars[time.Now().Nanosecond()%16]
			// Pequeña pausa para evitar repeticiones
			time.Sleep(time.Nanosecond)
		}
	}

	return string(uuid)
}

// cloneVariables crea una copia del mapa de variables compartidas
func (r *Runner) cloneVariables() map[string]string {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	clone := make(map[string]string, len(r.variables))
	for k, v := range r.variables {
		clone[k] = v
	}
	return clone
}
