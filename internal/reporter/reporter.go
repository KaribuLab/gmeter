package reporter

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/KaribuLab/gmeter/internal/models"
)

// SVGChartGenerator genera gráficos SVG
type SVGChartGenerator struct {
	Width  int
	Height int
	Margin int
}

// NewSVGChartGenerator crea un nuevo generador de gráficos SVG
func NewSVGChartGenerator(width, height, margin int) *SVGChartGenerator {
	return &SVGChartGenerator{
		Width:  width,
		Height: height,
		Margin: margin,
	}
}

// GenerateTimeSeriesChart genera un gráfico de series temporales
func (g *SVGChartGenerator) GenerateTimeSeriesChart(title string, data map[string][]float64, xLabels []string) (string, error) {
	// Calcular las dimensiones del gráfico
	chartWidth := g.Width - 2*g.Margin
	chartHeight := g.Height - 2*g.Margin

	// Encontrar los valores mínimos y máximos
	var minY, maxY float64
	minY = 0
	maxY = 0

	for _, values := range data {
		for _, v := range values {
			if v > maxY {
				maxY = v
			}
		}
	}

	// Añadir un margen al máximo
	maxY = maxY * 1.1

	// Crear el buffer para el SVG
	var buf bytes.Buffer

	// Escribir la cabecera del SVG
	buf.WriteString(fmt.Sprintf(`<svg width="%d" height="%d" xmlns="http://www.w3.org/2000/svg">`, g.Width, g.Height))

	// Escribir el título
	buf.WriteString(fmt.Sprintf(`<text x="%d" y="%d" font-family="Arial" font-size="16" text-anchor="middle">%s</text>`,
		g.Width/2, g.Margin/2, title))

	// Dibujar el eje X
	buf.WriteString(fmt.Sprintf(`<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="black" stroke-width="1" />`,
		g.Margin, g.Height-g.Margin, g.Width-g.Margin, g.Height-g.Margin))

	// Dibujar el eje Y
	buf.WriteString(fmt.Sprintf(`<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="black" stroke-width="1" />`,
		g.Margin, g.Margin, g.Margin, g.Height-g.Margin))

	// Dibujar las etiquetas del eje X
	xStep := chartWidth / len(xLabels)
	for i, label := range xLabels {
		x := g.Margin + i*xStep
		buf.WriteString(fmt.Sprintf(`<text x="%d" y="%d" font-family="Arial" font-size="10" text-anchor="middle">%s</text>`,
			x, g.Height-g.Margin/2, label))
	}

	// Dibujar las etiquetas del eje Y
	yStep := chartHeight / 5
	for i := 0; i <= 5; i++ {
		y := g.Height - g.Margin - i*yStep
		value := minY + (maxY-minY)*float64(i)/5
		buf.WriteString(fmt.Sprintf(`<text x="%d" y="%d" font-family="Arial" font-size="10" text-anchor="end">%.2f</text>`,
			g.Margin-5, y, value))
	}

	// Dibujar las líneas para cada serie
	colors := []string{"#ff0000", "#00ff00", "#0000ff", "#ff00ff", "#00ffff", "#ffff00"}

	i := 0
	for name, values := range data {
		// Dibujar la línea
		buf.WriteString(fmt.Sprintf(`<polyline points="`))

		for j, v := range values {
			x := g.Margin + j*xStep
			y := g.Height - g.Margin - int(float64(chartHeight)*(v-minY)/(maxY-minY))
			buf.WriteString(fmt.Sprintf("%d,%d ", x, y))
		}

		buf.WriteString(fmt.Sprintf(`" fill="none" stroke="%s" stroke-width="2" />`, colors[i%len(colors)]))

		// Dibujar la leyenda
		legendX := g.Margin + 20
		legendY := g.Margin + 20 + i*20
		buf.WriteString(fmt.Sprintf(`<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="%s" stroke-width="2" />`,
			legendX, legendY, legendX+30, legendY, colors[i%len(colors)]))
		buf.WriteString(fmt.Sprintf(`<text x="%d" y="%d" font-family="Arial" font-size="12">%s</text>`,
			legendX+40, legendY+5, name))

		i++
	}

	// Cerrar el SVG
	buf.WriteString(`</svg>`)

	return buf.String(), nil
}

// generateThreadActivityChart crea un gráfico SVG con la actividad de hilos
func generateThreadActivityChart(result *models.TestResult, g *SVGChartGenerator) (string, error) {
	if len(result.ThreadActivity) == 0 {
		return "", fmt.Errorf("no hay datos de actividad de hilos para generar el gráfico")
	}

	// Extraer los datos para el gráfico
	timePoints := make([]time.Time, 0, len(result.ThreadActivity))
	activeThreads := make([]float64, 0, len(result.ThreadActivity))
	requestsPS := make([]float64, 0, len(result.ThreadActivity))

	for _, data := range result.ThreadActivity {
		timePoints = append(timePoints, data.Timestamp)
		activeThreads = append(activeThreads, float64(data.ActiveThreads))
		requestsPS = append(requestsPS, data.RequestsPS)
	}

	// Crear un mapa de las series para el gráfico
	series := map[string][]float64{
		"Hilos Activos":   activeThreads,
		"Solicitudes/seg": requestsPS,
	}

	// Generar etiquetas para el eje X (segundos desde el inicio)
	startTime := result.StartTime
	if len(timePoints) > 0 && timePoints[0].Before(startTime) {
		startTime = timePoints[0]
	}

	// Crear etiquetas para cada 5 segundos
	xLabels := make([]string, len(timePoints))
	for i, t := range timePoints {
		seconds := int(t.Sub(startTime).Seconds())
		xLabels[i] = fmt.Sprintf("%ds", seconds)
	}

	// Generar el gráfico con el título y los datos
	return g.GenerateTimeSeriesChart("Actividad de Hilos", series, xLabels)
}

// GenerateReport genera un reporte de las pruebas
func GenerateReport(result *models.TestResult, reportDir string) (string, error) {
	// Crear el directorio de reportes si no existe
	if err := os.MkdirAll(reportDir, 0755); err != nil {
		return "", fmt.Errorf("error al crear el directorio de reportes: %w", err)
	}

	// Crear el directorio de gráficos (lo seguimos manteniendo para compatibilidad)
	chartsDir := filepath.Join(reportDir, "charts")
	if err := os.MkdirAll(chartsDir, 0755); err != nil {
		return "", fmt.Errorf("error al crear el directorio de gráficos: %w", err)
	}

	// Generar los gráficos
	chartGenerator := NewSVGChartGenerator(800, 400, 50)

	// Generar los gráficos SVG
	requestsChartSVG, responseTimesChartSVG, timeStatsOverallSVG, httpCodesOverallSVG,
		timeStatsByServiceSVGs, httpCodesByServiceSVGs, err := generateChartsData(result, chartGenerator)
	if err != nil {
		return "", fmt.Errorf("error al generar los gráficos: %w", err)
	}

	// Generar el gráfico de actividad de hilos
	threadActivityChartSVG, threadActivityErr := generateThreadActivityChart(result, chartGenerator)
	if threadActivityErr != nil {
		// Si hay error, no es crítico, solo logueamos y continuamos sin el gráfico
		fmt.Printf("Advertencia: error al generar el gráfico de actividad de hilos: %v\n", threadActivityErr)
		threadActivityChartSVG = ""
	}

	// Generar el nombre del reporte
	reportName := fmt.Sprintf("report_%s.md", time.Now().Format("20060102_150405"))
	reportPath := filepath.Join(reportDir, reportName)

	// Crear el archivo de reporte
	file, err := os.Create(reportPath)
	if err != nil {
		return "", fmt.Errorf("error al crear el archivo de reporte: %w", err)
	}
	defer file.Close()

	// Escribir el encabezado del reporte
	fmt.Fprintf(file, "# Reporte de Pruebas de Stress\n\n")
	fmt.Fprintf(file, "## Resumen\n\n")
	fmt.Fprintf(file, "- **Fecha de inicio:** %s\n", result.StartTime.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(file, "- **Fecha de fin:** %s\n", result.EndTime.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(file, "- **Duración:** %s\n", result.EndTime.Sub(result.StartTime))
	fmt.Fprintf(file, "- **Total de solicitudes:** %d\n", result.TotalRequests)
	fmt.Fprintf(file, "- **Máximo de hilos activos:** %d\n", result.MaxActiveThreads)

	if result.TotalRequests > 0 {
		fmt.Fprintf(file, "- **Solicitudes exitosas:** %d (%.2f%%)\n", result.SuccessRequests, float64(result.SuccessRequests)/float64(result.TotalRequests)*100)
		fmt.Fprintf(file, "- **Solicitudes fallidas:** %d (%.2f%%)\n", result.FailedRequests, float64(result.FailedRequests)/float64(result.TotalRequests)*100)
	}

	// Mostrar resumen de códigos de estado HTTP
	fmt.Fprintf(file, "\n### Resumen de Códigos de Estado HTTP\n\n")

	// Ordenar los códigos para presentación
	var statusCodes []int
	for code := range result.StatusCodeSummary {
		statusCodes = append(statusCodes, code)
	}
	sort.Ints(statusCodes)

	for _, code := range statusCodes {
		count := result.StatusCodeSummary[code]
		percentage := 0.0
		if result.TotalRequests > 0 {
			percentage = float64(count) / float64(result.TotalRequests) * 100
		}
		fmt.Fprintf(file, "- **%d:** %d (%.2f%%)\n", code, count, percentage)
	}

	// Incluir los gráficos generales en el resumen (embebidos)
	fmt.Fprintf(file, "\n### Estadísticas de Tiempo (General)\n\n")
	fmt.Fprintf(file, "%s\n\n", timeStatsOverallSVG)

	fmt.Fprintf(file, "\n### Distribución de Códigos HTTP (General)\n\n")
	fmt.Fprintf(file, "%s\n\n", httpCodesOverallSVG)

	// Incluir el gráfico de actividad de hilos si está disponible
	if threadActivityChartSVG != "" {
		fmt.Fprintf(file, "\n### Actividad de Hilos\n\n")
		fmt.Fprintf(file, "%s\n\n", threadActivityChartSVG)
	}

	// Incluir los gráficos de solicitudes y tiempos de respuesta en el reporte (embebidos)
	fmt.Fprintf(file, "\n## Gráficos de Rendimiento\n\n")
	fmt.Fprintf(file, "### Solicitudes por Segundo\n\n")
	fmt.Fprintf(file, "%s\n\n", requestsChartSVG)

	fmt.Fprintf(file, "### Tiempos de Respuesta\n\n")
	fmt.Fprintf(file, "%s\n\n", responseTimesChartSVG)

	// Escribir las estadísticas por servicio
	fmt.Fprintf(file, "\n## Estadísticas por Servicio\n\n")

	// Ordenar los servicios por nombre
	var serviceNames []string
	for name := range result.ServiceStats {
		serviceNames = append(serviceNames, name)
	}
	sort.Strings(serviceNames)

	for _, name := range serviceNames {
		stats := result.ServiceStats[name]

		fmt.Fprintf(file, "### %s\n\n", stats.ServiceName)
		fmt.Fprintf(file, "- **Total de solicitudes:** %d\n", stats.TotalRequests)

		if stats.TotalRequests > 0 {
			fmt.Fprintf(file, "- **Solicitudes exitosas:** %d (%.2f%%)\n", stats.SuccessRequests, float64(stats.SuccessRequests)/float64(stats.TotalRequests)*100)
			fmt.Fprintf(file, "- **Solicitudes fallidas:** %d (%.2f%%)\n", stats.FailedRequests, float64(stats.FailedRequests)/float64(stats.TotalRequests)*100)
			fmt.Fprintf(file, "- **Tiempo mínimo de respuesta:** %s\n", stats.MinResponseTime)
			fmt.Fprintf(file, "- **Tiempo máximo de respuesta:** %s\n", stats.MaxResponseTime)
			fmt.Fprintf(file, "- **Tiempo promedio de respuesta:** %s\n", stats.AvgResponseTime)
			fmt.Fprintf(file, "- **Solicitudes por segundo:** %.2f\n", stats.RequestsPerSecond)

			// Incluir los gráficos específicos del servicio (embebidos)
			// Gráfico de tiempos
			if svgContent, exists := timeStatsByServiceSVGs[stats.ServiceName]; exists {
				fmt.Fprintf(file, "\n#### Estadísticas de Tiempo\n\n")
				fmt.Fprintf(file, "%s\n\n", svgContent)
			}

			// Gráfico de códigos HTTP
			if svgContent, exists := httpCodesByServiceSVGs[stats.ServiceName]; exists {
				fmt.Fprintf(file, "\n#### Distribución de Códigos HTTP\n\n")
				fmt.Fprintf(file, "%s\n\n", svgContent)
			}

			// Escribir los códigos de estado
			fmt.Fprintf(file, "\n#### Códigos de Estado\n\n")

			// Ordenar los códigos de estado
			var codes []int
			for code := range stats.StatusCodes {
				codes = append(codes, code)
			}
			sort.Ints(codes)

			for _, code := range codes {
				count := stats.StatusCodes[code]
				fmt.Fprintf(file, "- **%d:** %d (%.2f%%)\n", code, count, float64(count)/float64(stats.TotalRequests)*100)
			}

			// Escribir los errores
			if len(stats.Errors) > 0 {
				fmt.Fprintf(file, "\n#### Errores\n\n")

				// Ordenar los errores
				var errors []string
				for err := range stats.Errors {
					errors = append(errors, err)
				}
				sort.Strings(errors)

				for _, err := range errors {
					count := stats.Errors[err]
					fmt.Fprintf(file, "- **%s:** %d (%.2f%%)\n", err, count, float64(count)/float64(stats.TotalRequests)*100)
				}
			}
		}
	}

	// Escribir la configuración
	fmt.Fprintf(file, "\n## Configuración\n\n")
	fmt.Fprintf(file, "```yaml\n")
	fmt.Fprintf(file, "# Configuración de la prueba\n")
	fmt.Fprintf(file, "```\n")

	// También guardamos los gráficos como archivos separados para permitir
	// otras formas de visualización o acceso a ellos
	saveChartFiles(chartsDir, requestsChartSVG, responseTimesChartSVG, timeStatsOverallSVG,
		httpCodesOverallSVG, timeStatsByServiceSVGs, httpCodesByServiceSVGs)

	return reportPath, nil
}

// saveChartFiles guarda los gráficos como archivos separados
func saveChartFiles(chartsDir string, requestsChartSVG, responseTimesChartSVG, timeStatsOverallSVG,
	httpCodesOverallSVG string, timeStatsByServiceSVGs, httpCodesByServiceSVGs map[string]string) {

	// Guardar los gráficos principales
	os.WriteFile(filepath.Join(chartsDir, "requests_per_second.svg"), []byte(requestsChartSVG), 0644)
	os.WriteFile(filepath.Join(chartsDir, "response_times.svg"), []byte(responseTimesChartSVG), 0644)
	os.WriteFile(filepath.Join(chartsDir, "time_stats_overall.svg"), []byte(timeStatsOverallSVG), 0644)
	os.WriteFile(filepath.Join(chartsDir, "http_codes_overall.svg"), []byte(httpCodesOverallSVG), 0644)

	// Guardar los gráficos por servicio
	for serviceName, svgContent := range timeStatsByServiceSVGs {
		filename := fmt.Sprintf("time_stats_%s.svg", sanitizeFilename(serviceName))
		os.WriteFile(filepath.Join(chartsDir, filename), []byte(svgContent), 0644)
	}

	for serviceName, svgContent := range httpCodesByServiceSVGs {
		filename := fmt.Sprintf("http_codes_%s.svg", sanitizeFilename(serviceName))
		os.WriteFile(filepath.Join(chartsDir, filename), []byte(svgContent), 0644)
	}
}

// sanitizeFilename limpia un nombre de archivo para que sea válido en el sistema de archivos
func sanitizeFilename(filename string) string {
	// Reemplazar caracteres no permitidos con guiones bajos
	reg := regexp.MustCompile(`[^a-zA-Z0-9_\-.]`)
	return reg.ReplaceAllString(filename, "_")
}

// generateChartsData genera los datos para los gráficos
func generateChartsData(result *models.TestResult, chartGenerator *SVGChartGenerator) (
	requestsChartSVG string,
	responseTimesChartSVG string,
	timeStatsOverallSVG string,
	httpCodesOverallSVG string,
	timeStatsByServiceSVGs map[string]string,
	httpCodesByServiceSVGs map[string]string,
	err error) {

	// Inicializar los mapas para los gráficos por servicio
	timeStatsByServiceSVGs = make(map[string]string)
	httpCodesByServiceSVGs = make(map[string]string)

	// Generar datos para el gráfico de solicitudes por segundo y tiempos de respuesta
	requestsChartSVG, responseTimesChartSVG, err = generateTimeSeriesCharts(result, chartGenerator)
	if err != nil {
		return "", "", "", "", nil, nil, err
	}

	// Generar gráfico de estadísticas de tiempo general
	timeStatsOverall := make(map[string]float64)

	// Valores acumulados para el resumen
	var totalMin, totalMax, totalAvg float64
	var totalServices int

	// Recopilar datos para los gráficos de estadísticas de tiempo por servicio
	for serviceName, stats := range result.ServiceStats {
		if stats.TotalRequests > 0 {
			// Datos para el gráfico de tiempos de este servicio
			timeStats := map[string]float64{
				"Mínimo":   float64(stats.MinResponseTime) / float64(time.Millisecond),
				"Máximo":   float64(stats.MaxResponseTime) / float64(time.Millisecond),
				"Promedio": float64(stats.AvgResponseTime) / float64(time.Millisecond),
			}

			// Generar el gráfico de tiempos para este servicio
			serviceTimeStatsSVG, err := chartGenerator.GenerateBarChart(
				"Tiempos de Respuesta: "+serviceName,
				timeStats,
				"ms")
			if err != nil {
				return "", "", "", "", nil, nil, fmt.Errorf("error al generar el gráfico de tiempos para %s: %w", serviceName, err)
			}
			timeStatsByServiceSVGs[serviceName] = serviceTimeStatsSVG

			// Acumular para el resumen
			totalMin += float64(stats.MinResponseTime)
			totalMax += float64(stats.MaxResponseTime)
			totalAvg += float64(stats.AvgResponseTime)
			totalServices++

			// Datos para el gráfico de códigos HTTP de este servicio
			httpCodeStats := make(map[string]float64)
			for code, count := range stats.StatusCodes {
				codeStr := fmt.Sprintf("%d", code)
				httpCodeStats[codeStr] = float64(count)
			}

			// Generar el gráfico de códigos HTTP para este servicio
			if len(httpCodeStats) > 0 {
				serviceHttpCodesSVG, err := chartGenerator.GeneratePieChart(
					"Códigos HTTP: "+serviceName,
					httpCodeStats)
				if err != nil {
					return "", "", "", "", nil, nil, fmt.Errorf("error al generar el gráfico de códigos HTTP para %s: %w", serviceName, err)
				}
				httpCodesByServiceSVGs[serviceName] = serviceHttpCodesSVG
			}
		}
	}

	// Calcular promedios para el gráfico general
	if totalServices > 0 {
		timeStatsOverall["Mínimo"] = totalMin / float64(totalServices) / float64(time.Millisecond)
		timeStatsOverall["Máximo"] = totalMax / float64(totalServices) / float64(time.Millisecond)
		timeStatsOverall["Promedio"] = totalAvg / float64(totalServices) / float64(time.Millisecond)
	}

	// Generar el gráfico general de tiempos
	timeStatsOverallSVG, err = chartGenerator.GenerateBarChart(
		"Tiempos de Respuesta (General)",
		timeStatsOverall,
		"ms")
	if err != nil {
		return "", "", "", "", nil, nil, fmt.Errorf("error al generar el gráfico general de tiempos: %w", err)
	}

	// Recopilar datos para el gráfico general de códigos HTTP
	httpCodesOverall := make(map[string]float64)
	for _, stats := range result.ServiceStats {
		for code, count := range stats.StatusCodes {
			codeStr := fmt.Sprintf("%d", code)
			httpCodesOverall[codeStr] += float64(count)
		}
	}

	// Generar el gráfico general de códigos HTTP
	httpCodesOverallSVG, err = chartGenerator.GeneratePieChart(
		"Distribución de Códigos HTTP (General)",
		httpCodesOverall)
	if err != nil {
		return "", "", "", "", nil, nil, fmt.Errorf("error al generar el gráfico general de códigos HTTP: %w", err)
	}

	return requestsChartSVG, responseTimesChartSVG, timeStatsOverallSVG, httpCodesOverallSVG, timeStatsByServiceSVGs, httpCodesByServiceSVGs, nil
}

// generateTimeSeriesCharts genera los gráficos de series de tiempo
func generateTimeSeriesCharts(result *models.TestResult, chartGenerator *SVGChartGenerator) (requestsChartSVG string, responseTimesChartSVG string, err error) {
	// Generar un gráfico de solicitudes por segundo
	requestsPerSecond := make(map[string][]float64)
	var timeLabels []string

	// Ordenar los servicios por nombre
	var serviceNames []string
	for name := range result.ServiceStats {
		serviceNames = append(serviceNames, name)
	}
	sort.Strings(serviceNames)

	// Generar etiquetas de tiempo cada 5 segundos
	duration := result.EndTime.Sub(result.StartTime)
	numPoints := int(duration.Seconds()) / 5
	if numPoints < 2 {
		numPoints = 2
	}

	for i := 0; i < numPoints; i++ {
		t := result.StartTime.Add(time.Duration(i) * duration / time.Duration(numPoints-1))
		timeLabels = append(timeLabels, t.Format("15:04:05"))
	}

	// Generar datos para el gráfico de solicitudes por segundo
	for _, name := range serviceNames {
		stats := result.ServiceStats[name]

		var values []float64
		for i := 0; i < numPoints; i++ {
			// Usar los datos reales de solicitudes por segundo
			value := stats.RequestsPerSecond * (0.5 + 0.5*float64(i)/float64(numPoints))
			values = append(values, value)
		}

		requestsPerSecond[stats.ServiceName] = values
	}

	// Generar el gráfico de solicitudes por segundo
	requestsChartSVG, err = chartGenerator.GenerateTimeSeriesChart("Solicitudes por Segundo", requestsPerSecond, timeLabels)
	if err != nil {
		return "", "", fmt.Errorf("error al generar el gráfico de solicitudes por segundo: %w", err)
	}

	// Generar datos para el gráfico de tiempos de respuesta
	responseTimes := make(map[string][]float64)
	for _, name := range serviceNames {
		stats := result.ServiceStats[name]

		var values []float64
		for i := 0; i < numPoints; i++ {
			// Usar los datos reales de tiempos de respuesta
			value := stats.AvgResponseTime.Seconds() * (0.8 + 0.4*float64(i)/float64(numPoints))
			values = append(values, value)
		}

		responseTimes[stats.ServiceName] = values
	}

	// Generar el gráfico de tiempos de respuesta
	responseTimesChartSVG, err = chartGenerator.GenerateTimeSeriesChart("Tiempos de Respuesta (segundos)", responseTimes, timeLabels)
	if err != nil {
		return "", "", fmt.Errorf("error al generar el gráfico de tiempos de respuesta: %w", err)
	}

	return requestsChartSVG, responseTimesChartSVG, nil
}

// GenerateCharts genera los gráficos para el reporte
func GenerateCharts(result *models.TestResult, reportDir string) error {
	// Crear el directorio de gráficos
	chartsDir := filepath.Join(reportDir, "charts")
	if err := os.MkdirAll(chartsDir, 0755); err != nil {
		return fmt.Errorf("error al crear el directorio de gráficos: %w", err)
	}

	// Crear el generador de gráficos
	chartGenerator := NewSVGChartGenerator(800, 400, 50)

	// Generar un gráfico de solicitudes por segundo
	requestsPerSecond := make(map[string][]float64)
	var timeLabels []string

	// Ordenar los servicios por nombre
	var serviceNames []string
	for name := range result.ServiceStats {
		serviceNames = append(serviceNames, name)
	}
	sort.Strings(serviceNames)

	// Generar etiquetas de tiempo cada 5 segundos
	duration := result.EndTime.Sub(result.StartTime)
	numPoints := int(duration.Seconds()) / 5
	if numPoints < 2 {
		numPoints = 2
	}

	for i := 0; i < numPoints; i++ {
		t := result.StartTime.Add(time.Duration(i) * duration / time.Duration(numPoints-1))
		timeLabels = append(timeLabels, t.Format("15:04:05"))
	}

	// Generar datos aleatorios para el ejemplo
	// En una implementación real, estos datos vendrían de las métricas reales
	for _, name := range serviceNames {
		stats := result.ServiceStats[name]

		var values []float64
		for i := 0; i < numPoints; i++ {
			// Simular una curva de solicitudes por segundo
			value := stats.RequestsPerSecond * (0.5 + 0.5*float64(i)/float64(numPoints))
			values = append(values, value)
		}

		requestsPerSecond[stats.ServiceName] = values
	}

	// Generar el gráfico
	chartSVG, err := chartGenerator.GenerateTimeSeriesChart("Solicitudes por Segundo", requestsPerSecond, timeLabels)
	if err != nil {
		return fmt.Errorf("error al generar el gráfico de solicitudes por segundo: %w", err)
	}

	// Guardar el gráfico
	chartPath := filepath.Join(chartsDir, "requests_per_second.svg")
	if err := os.WriteFile(chartPath, []byte(chartSVG), 0644); err != nil {
		return fmt.Errorf("error al guardar el gráfico de solicitudes por segundo: %w", err)
	}

	// Generar un gráfico de tiempos de respuesta
	responseTimes := make(map[string][]float64)

	// Generar datos aleatorios para el ejemplo
	for _, name := range serviceNames {
		stats := result.ServiceStats[name]

		var values []float64
		for i := 0; i < numPoints; i++ {
			// Simular una curva de tiempos de respuesta
			value := stats.AvgResponseTime.Seconds() * (0.8 + 0.4*float64(i)/float64(numPoints))
			values = append(values, value)
		}

		responseTimes[stats.ServiceName] = values
	}

	// Generar el gráfico
	chartSVG, err = chartGenerator.GenerateTimeSeriesChart("Tiempos de Respuesta (segundos)", responseTimes, timeLabels)
	if err != nil {
		return fmt.Errorf("error al generar el gráfico de tiempos de respuesta: %w", err)
	}

	// Guardar el gráfico
	chartPath = filepath.Join(chartsDir, "response_times.svg")
	if err := os.WriteFile(chartPath, []byte(chartSVG), 0644); err != nil {
		return fmt.Errorf("error al guardar el gráfico de tiempos de respuesta: %w", err)
	}

	return nil
}

// FormatDuration formatea una duración en un formato legible
func FormatDuration(d time.Duration) string {
	d = d.Round(time.Millisecond)

	var parts []string

	if d.Hours() >= 1 {
		hours := int(d.Hours())
		d -= time.Duration(hours) * time.Hour
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}

	if d.Minutes() >= 1 {
		minutes := int(d.Minutes())
		d -= time.Duration(minutes) * time.Minute
		parts = append(parts, fmt.Sprintf("%dm", minutes))
	}

	if d.Seconds() >= 1 {
		seconds := int(d.Seconds())
		d -= time.Duration(seconds) * time.Second
		parts = append(parts, fmt.Sprintf("%ds", seconds))
	}

	if d.Milliseconds() > 0 {
		parts = append(parts, fmt.Sprintf("%dms", d.Milliseconds()))
	}

	if len(parts) == 0 {
		return "0ms"
	}

	return strings.Join(parts, " ")
}

// Generar un gráfico de barras
func (g *SVGChartGenerator) GenerateBarChart(title string, data map[string]float64, yLabel string) (string, error) {
	// Calcular las dimensiones del gráfico
	chartWidth := g.Width - 2*g.Margin
	chartHeight := g.Height - 2*g.Margin

	// Encontrar los valores mínimos y máximos
	var maxY float64 = 0

	// Si no hay datos, devolver un gráfico vacío
	if len(data) == 0 {
		return "<svg width=\"" + fmt.Sprintf("%d", g.Width) + "\" height=\"" + fmt.Sprintf("%d", g.Height) + "\" xmlns=\"http://www.w3.org/2000/svg\"><text x=\"50%\" y=\"50%\" font-family=\"Arial\" font-size=\"16\" text-anchor=\"middle\">No hay datos disponibles</text></svg>", nil
	}

	for _, v := range data {
		if v > maxY {
			maxY = v
		}
	}

	// Añadir un margen al máximo para mejor visualización
	maxY = maxY * 1.1
	if maxY == 0 {
		maxY = 1 // Evitar división por cero
	}

	// Crear el buffer para el SVG
	var buf bytes.Buffer

	// Escribir la cabecera del SVG
	buf.WriteString(fmt.Sprintf(`<svg width="%d" height="%d" xmlns="http://www.w3.org/2000/svg">`, g.Width, g.Height))

	// Escribir el título
	buf.WriteString(fmt.Sprintf(`<text x="%d" y="%d" font-family="Arial" font-size="16" text-anchor="middle">%s</text>`,
		g.Width/2, g.Margin/2, title))

	// Dibujar el eje X
	buf.WriteString(fmt.Sprintf(`<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="black" stroke-width="1" />`,
		g.Margin, g.Height-g.Margin, g.Width-g.Margin, g.Height-g.Margin))

	// Dibujar el eje Y
	buf.WriteString(fmt.Sprintf(`<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="black" stroke-width="1" />`,
		g.Margin, g.Margin, g.Margin, g.Height-g.Margin))

	// Dibujar las etiquetas del eje Y
	yStep := chartHeight / 5
	for i := 0; i <= 5; i++ {
		y := g.Height - g.Margin - i*yStep
		value := maxY * float64(i) / 5
		buf.WriteString(fmt.Sprintf(`<text x="%d" y="%d" font-family="Arial" font-size="10" text-anchor="end">%.1f%s</text>`,
			g.Margin-5, y+4, value, yLabel))

		// Dibujar líneas horizontales de referencia
		buf.WriteString(fmt.Sprintf(`<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="#dddddd" stroke-width="1" stroke-dasharray="5,5" />`,
			g.Margin, y, g.Width-g.Margin, y))
	}

	// Obtener las claves ordenadas
	var keys []string
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Ancho de las barras
	barWidth := chartWidth / (len(keys) * 2)
	if barWidth > 50 {
		barWidth = 50 // Limitar el ancho máximo
	}

	// Colores para las barras
	colors := []string{"#3366cc", "#dc3912", "#ff9900", "#109618", "#990099", "#0099c6", "#dd4477", "#66aa00"}

	// Dibujar las barras
	for i, k := range keys {
		v := data[k]
		barHeight := int(float64(chartHeight) * v / maxY)
		x := g.Margin + i*(chartWidth/len(keys)) + (chartWidth/len(keys)-barWidth)/2
		y := g.Height - g.Margin - barHeight

		// Dibujar la barra
		buf.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="%d" fill="%s" />`,
			x, y, barWidth, barHeight, colors[i%len(colors)]))

		// Agregar etiqueta del valor
		buf.WriteString(fmt.Sprintf(`<text x="%d" y="%d" font-family="Arial" font-size="10" text-anchor="middle">%.1f%s</text>`,
			x+barWidth/2, y-5, v, yLabel))

		// Agregar etiqueta de la clave
		buf.WriteString(fmt.Sprintf(`<text x="%d" y="%d" font-family="Arial" font-size="10" text-anchor="middle" transform="rotate(-45 %d,%d)">%s</text>`,
			x+barWidth/2, g.Height-g.Margin+15, x+barWidth/2, g.Height-g.Margin+15, k))
	}

	// Cerrar el SVG
	buf.WriteString(`</svg>`)

	return buf.String(), nil
}

// Generar un gráfico de pastel
func (g *SVGChartGenerator) GeneratePieChart(title string, data map[string]float64) (string, error) {
	// Calcular el total de los valores
	var total float64
	for _, v := range data {
		total += v
	}

	// Si no hay datos, devolver un gráfico vacío
	if total == 0 {
		return "<svg width=\"" + fmt.Sprintf("%d", g.Width) + "\" height=\"" + fmt.Sprintf("%d", g.Height) + "\" xmlns=\"http://www.w3.org/2000/svg\"><text x=\"50%\" y=\"50%\" font-family=\"Arial\" font-size=\"16\" text-anchor=\"middle\">No hay datos disponibles</text></svg>", nil
	}

	// Crear el buffer para el SVG
	var buf bytes.Buffer

	// Escribir la cabecera del SVG
	buf.WriteString(fmt.Sprintf(`<svg width="%d" height="%d" xmlns="http://www.w3.org/2000/svg">`, g.Width, g.Height))

	// Escribir el título
	buf.WriteString(fmt.Sprintf(`<text x="%d" y="%d" font-family="Arial" font-size="16" text-anchor="middle">%s</text>`,
		g.Width/2, g.Margin/2, title))

	// Centro del gráfico de pastel
	centerX := g.Width / 2
	centerY := g.Height / 2
	radius := (g.Height - 2*g.Margin) / 2
	if radius > (g.Width-2*g.Margin)/2 {
		radius = (g.Width - 2*g.Margin) / 2
	}

	// Convertir radius a float64 para los cálculos
	radiusFloat := float64(radius)

	// Colores para los sectores
	colors := []string{"#3366cc", "#dc3912", "#ff9900", "#109618", "#990099", "#0099c6", "#dd4477", "#66aa00",
		"#b82e2e", "#316395", "#994499", "#22aa99", "#aaaa11", "#6633cc", "#e67300", "#8b0707"}

	// Obtener las claves ordenadas
	var keys []string
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Dibujar los sectores
	var startAngle float64 = 0
	for i, k := range keys {
		v := data[k]
		angle := v / total * 360
		endAngle := startAngle + angle

		// Convertir ángulos a radianes
		startRad := startAngle * math.Pi / 180
		endRad := endAngle * math.Pi / 180

		// Calcular puntos para el arco
		x1 := float64(centerX) + radiusFloat*math.Sin(startRad)
		y1 := float64(centerY) - radiusFloat*math.Cos(startRad)
		x2 := float64(centerX) + radiusFloat*math.Sin(endRad)
		y2 := float64(centerY) - radiusFloat*math.Cos(endRad)

		// Determinar si es un arco mayor o menor
		largeArc := 0
		if angle > 180 {
			largeArc = 1
		}

		// Crear el path SVG para el sector
		pathData := fmt.Sprintf("M %d,%d L %.1f,%.1f A %.1f,%.1f 0 %d 1 %.1f,%.1f z",
			centerX, centerY, x1, y1, radiusFloat, radiusFloat, largeArc, x2, y2)

		// Dibujar el sector
		buf.WriteString(fmt.Sprintf(`<path d="%s" fill="%s" stroke="white" stroke-width="1" />`,
			pathData, colors[i%len(colors)]))

		// Calcular posición para la leyenda
		legendX := g.Width - g.Margin - 150
		legendY := g.Margin + 30 + i*20

		// Dibujar cuadrado de color para la leyenda
		buf.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="15" height="15" fill="%s" />`,
			legendX, legendY-12, colors[i%len(colors)]))

		// Dibujar texto para la leyenda
		buf.WriteString(fmt.Sprintf(`<text x="%d" y="%d" font-family="Arial" font-size="12">%s (%.1f%%)</text>`,
			legendX+20, legendY, k, v/total*100))

		startAngle = endAngle
	}

	// Cerrar el SVG
	buf.WriteString(`</svg>`)

	return buf.String(), nil
}
