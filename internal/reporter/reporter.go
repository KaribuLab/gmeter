package reporter

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
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

// GenerateReport genera un reporte de las pruebas
func GenerateReport(result *models.TestResult, reportDir string) (string, error) {
	// Crear el directorio de reportes si no existe
	if err := os.MkdirAll(reportDir, 0755); err != nil {
		return "", fmt.Errorf("error al crear el directorio de reportes: %w", err)
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

	if result.TotalRequests > 0 {
		fmt.Fprintf(file, "- **Solicitudes exitosas:** %d (%.2f%%)\n", result.SuccessRequests, float64(result.SuccessRequests)/float64(result.TotalRequests)*100)
		fmt.Fprintf(file, "- **Solicitudes fallidas:** %d (%.2f%%)\n", result.FailedRequests, float64(result.FailedRequests)/float64(result.TotalRequests)*100)
	}

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

			// Generar el gráfico de tiempos de respuesta
			// TODO: Implementar la generación de gráficos SVG
		}
	}

	// Escribir la configuración
	fmt.Fprintf(file, "\n## Configuración\n\n")
	fmt.Fprintf(file, "```yaml\n")
	fmt.Fprintf(file, "# Configuración de la prueba\n")
	fmt.Fprintf(file, "```\n")

	return reportPath, nil
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
