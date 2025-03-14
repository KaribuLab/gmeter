package reporter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KaribuLab/gmeter/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatDuration(t *testing.T) {
	testCases := []struct {
		duration time.Duration
		expected string
	}{
		{1 * time.Hour, "1h"},
		{1*time.Hour + 30*time.Minute, "1h 30m"},
		{1*time.Hour + 30*time.Minute + 15*time.Second, "1h 30m 15s"},
		{1*time.Hour + 30*time.Minute + 15*time.Second + 500*time.Millisecond, "1h 30m 15s 500ms"},
		{30 * time.Minute, "30m"},
		{30*time.Minute + 15*time.Second, "30m 15s"},
		{30*time.Minute + 15*time.Second + 500*time.Millisecond, "30m 15s 500ms"},
		{15 * time.Second, "15s"},
		{15*time.Second + 500*time.Millisecond, "15s 500ms"},
		{500 * time.Millisecond, "500ms"},
		{0, "0ms"},
	}

	for _, tc := range testCases {
		result := FormatDuration(tc.duration)
		assert.Equal(t, tc.expected, result, "FormatDuration(%v) = %s, expected %s", tc.duration, result, tc.expected)
	}
}

func TestGenerateTimeSeriesChart(t *testing.T) {
	// Crear un generador de gráficos
	chartGenerator := NewSVGChartGenerator(800, 400, 50)

	// Crear datos de prueba
	data := map[string][]float64{
		"Service 1": {1.0, 2.0, 3.0, 4.0, 5.0},
		"Service 2": {5.0, 4.0, 3.0, 2.0, 1.0},
	}
	xLabels := []string{"10:00", "10:01", "10:02", "10:03", "10:04"}

	// Generar el gráfico
	svg, err := chartGenerator.GenerateTimeSeriesChart("Test Chart", data, xLabels)
	require.NoError(t, err, "Error al generar el gráfico")

	// Verificar que el SVG se ha generado correctamente
	assert.Contains(t, svg, "<svg", "El SVG no contiene la etiqueta <svg>")
	assert.Contains(t, svg, "Test Chart", "El SVG no contiene el título")
	assert.Contains(t, svg, "Service 1", "El SVG no contiene la leyenda del servicio 1")
	assert.Contains(t, svg, "Service 2", "El SVG no contiene la leyenda del servicio 2")
	assert.Contains(t, svg, "10:00", "El SVG no contiene la etiqueta del eje X")
	assert.Contains(t, svg, "10:04", "El SVG no contiene la etiqueta del eje X")
}

func TestGenerateReport(t *testing.T) {
	// Crear un directorio temporal para las pruebas
	tempDir := t.TempDir()

	// Crear un resultado de prueba
	startTime := time.Now().Add(-1 * time.Hour)
	endTime := time.Now()

	result := &models.TestResult{
		StartTime:       startTime,
		EndTime:         endTime,
		TotalRequests:   100,
		SuccessRequests: 90,
		FailedRequests:  10,
		ServiceStats: map[string]*models.ServiceStats{
			"service1": {
				ServiceName:       "service1",
				TotalRequests:     50,
				SuccessRequests:   45,
				FailedRequests:    5,
				MinResponseTime:   100 * time.Millisecond,
				MaxResponseTime:   500 * time.Millisecond,
				AvgResponseTime:   300 * time.Millisecond,
				RequestsPerSecond: 0.5,
				StartTime:         startTime,
				EndTime:           endTime,
				StatusCodes: map[int]int{
					200: 45,
					500: 5,
				},
				Errors: map[string]int{
					"connection refused": 5,
				},
			},
			"service2": {
				ServiceName:       "service2",
				TotalRequests:     50,
				SuccessRequests:   45,
				FailedRequests:    5,
				MinResponseTime:   200 * time.Millisecond,
				MaxResponseTime:   600 * time.Millisecond,
				AvgResponseTime:   400 * time.Millisecond,
				RequestsPerSecond: 0.5,
				StartTime:         startTime,
				EndTime:           endTime,
				StatusCodes: map[int]int{
					200: 45,
					500: 5,
				},
				Errors: map[string]int{
					"connection refused": 5,
				},
			},
		},
	}

	// Generar el reporte
	reportPath, err := GenerateReport(result, tempDir)
	require.NoError(t, err, "Error al generar el reporte")

	// Verificar que el reporte se ha generado correctamente
	assert.True(t, strings.HasPrefix(filepath.Base(reportPath), "report_"), "El nombre del reporte no tiene el prefijo esperado")
	assert.True(t, strings.HasSuffix(filepath.Base(reportPath), ".md"), "El nombre del reporte no tiene la extensión esperada")

	// Leer el contenido del reporte
	content, err := os.ReadFile(reportPath)
	require.NoError(t, err, "Error al leer el reporte")

	// Verificar el contenido del reporte
	contentStr := string(content)
	assert.Contains(t, contentStr, "# Reporte de Pruebas de Stress", "El reporte no contiene el título")
	assert.Contains(t, contentStr, "## Resumen", "El reporte no contiene la sección de resumen")
	assert.Contains(t, contentStr, "## Estadísticas por Servicio", "El reporte no contiene la sección de estadísticas por servicio")
	assert.Contains(t, contentStr, "### service1", "El reporte no contiene las estadísticas del servicio 1")
	assert.Contains(t, contentStr, "### service2", "El reporte no contiene las estadísticas del servicio 2")
	assert.Contains(t, contentStr, "## Configuración", "El reporte no contiene la sección de configuración")
}

func TestGenerateCharts(t *testing.T) {
	// Crear un directorio temporal para las pruebas
	tempDir := t.TempDir()

	// Crear un resultado de prueba
	startTime := time.Now().Add(-1 * time.Hour)
	endTime := time.Now()

	result := &models.TestResult{
		StartTime:       startTime,
		EndTime:         endTime,
		TotalRequests:   100,
		SuccessRequests: 90,
		FailedRequests:  10,
		ServiceStats: map[string]*models.ServiceStats{
			"service1": {
				ServiceName:       "service1",
				TotalRequests:     50,
				SuccessRequests:   45,
				FailedRequests:    5,
				MinResponseTime:   100 * time.Millisecond,
				MaxResponseTime:   500 * time.Millisecond,
				AvgResponseTime:   300 * time.Millisecond,
				RequestsPerSecond: 0.5,
				StartTime:         startTime,
				EndTime:           endTime,
			},
			"service2": {
				ServiceName:       "service2",
				TotalRequests:     50,
				SuccessRequests:   45,
				FailedRequests:    5,
				MinResponseTime:   200 * time.Millisecond,
				MaxResponseTime:   600 * time.Millisecond,
				AvgResponseTime:   400 * time.Millisecond,
				RequestsPerSecond: 0.5,
				StartTime:         startTime,
				EndTime:           endTime,
			},
		},
	}

	// Generar los gráficos
	err := GenerateCharts(result, tempDir)
	require.NoError(t, err, "Error al generar los gráficos")

	// Verificar que los gráficos se han generado correctamente
	chartsDir := filepath.Join(tempDir, "charts")
	assert.DirExists(t, chartsDir, "El directorio de gráficos no existe")

	requestsPerSecondChart := filepath.Join(chartsDir, "requests_per_second.svg")
	assert.FileExists(t, requestsPerSecondChart, "El gráfico de solicitudes por segundo no existe")

	responseTimesChart := filepath.Join(chartsDir, "response_times.svg")
	assert.FileExists(t, responseTimesChart, "El gráfico de tiempos de respuesta no existe")

	// Leer el contenido de los gráficos
	content, err := os.ReadFile(requestsPerSecondChart)
	require.NoError(t, err, "Error al leer el gráfico de solicitudes por segundo")
	assert.Contains(t, string(content), "Solicitudes por Segundo", "El gráfico no contiene el título esperado")

	content, err = os.ReadFile(responseTimesChart)
	require.NoError(t, err, "Error al leer el gráfico de tiempos de respuesta")
	assert.Contains(t, string(content), "Tiempos de Respuesta", "El gráfico no contiene el título esperado")
}
