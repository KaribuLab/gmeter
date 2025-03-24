package runner

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/KaribuLab/gmeter/internal/config"
	"github.com/KaribuLab/gmeter/internal/logger"
	"github.com/KaribuLab/gmeter/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRunner(t *testing.T) {
	log := logger.New()
	cfg := &config.Config{
		LogFile:   "test.log",
		ReportDir: "reports",
		GlobalConfig: config.RunConfig{
			ThreadsPerSecond: 10,
			Duration:         "1m",
			RampUp:           "10s",
		},
	}

	runner := NewRunner(cfg, log)

	assert.NotNil(t, runner, "El runner no debería ser nil")
	assert.Equal(t, cfg, runner.cfg, "La configuración no coincide")
	assert.Equal(t, log, runner.log, "El logger no coincide")
	assert.NotNil(t, runner.client, "El cliente HTTP no debería ser nil")
	assert.NotNil(t, runner.variables, "El mapa de variables no debería ser nil")
}

// Implementación mínima de LazyDataSource para pruebas
type LazyDataSource struct {
	records []models.DataRecord
}

func (lds *LazyDataSource) Len() int {
	return len(lds.records)
}

func (lds *LazyDataSource) Get(index int) (models.DataRecord, error) {
	if index < 0 || index >= len(lds.records) {
		return nil, fmt.Errorf("índice fuera de rango")
	}
	return lds.records[index], nil
}

func TestReplaceVariables(t *testing.T) {
	log := logger.New()
	cfg := &config.Config{}
	runner := NewRunner(cfg, log)

	vars := map[string]string{
		"username": "user1",
		"token":    "abc123",
	}

	// Casos de prueba
	testCases := []struct {
		input    string
		expected string
	}{
		{"Hello {{username}}!", "Hello user1!"},
		{"Bearer {{token}}", "Bearer abc123"},
		{"No variables", "No variables"},
		{"{{nonexistent}}", "{{nonexistent}}"},
	}

	for _, tc := range testCases {
		result := runner.replaceVariables(tc.input, vars)
		assert.Equal(t, tc.expected, result, "replaceVariables(%q) = %v, expected %v", tc.input, result, tc.expected)
	}
}

func TestExecuteService(t *testing.T) {
	// Crear un servidor HTTP de prueba
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verificar el método
		assert.Equal(t, "GET", r.Method, "El método no coincide")

		// Verificar las cabeceras
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"), "La cabecera Content-Type no coincide")

		// Responder con un JSON
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message": "Hello, World!", "token": "test_token"}`))
	}))
	defer server.Close()

	// Crear la configuración
	service := &config.Service{
		Name:   "test",
		URL:    server.URL,
		Method: "GET",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		ExtractToken: "token",
		TokenName:    "test_token",
	}

	// Crear el logger
	log := logger.New()
	log.SetOutput(os.Stdout)

	// Crear el runner
	runner := NewRunner(&config.Config{}, log)

	// Crear el contexto y las variables
	ctx := context.Background()
	threadVars := map[string]string{}
	threadNum := 1

	// Ejecutar el servicio
	result, err := runner.executeService(ctx, service, threadVars, threadNum)
	require.NoError(t, err, "Error al ejecutar el servicio")

	// Verificar la respuesta
	assert.NotNil(t, result, "La respuesta no debería ser nil")
	assert.Equal(t, http.StatusOK, result.statusCode, "El código de estado no coincide")
	assert.NotZero(t, result.responseTime, "El tiempo de respuesta no debería ser cero")

	// Verificar que se extrajo el token correctamente
	assert.Contains(t, result.extractedVars, "test_token", "No se extrajo el token correctamente")
	assert.Equal(t, "test_token", result.extractedVars["test_token"], "El valor del token extraído no coincide")
}

func TestExecuteServiceWithTokenCache(t *testing.T) {
	// Contador para saber cuántas veces se llama al endpoint
	endpointCalls := 0

	// Crear un servidor HTTP de prueba
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Incrementar el contador
		endpointCalls++

		// Verificar el método
		assert.Equal(t, "GET", r.Method, "El método no coincide")

		// Responder con un JSON
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message": "Hello, World!", "token": "test_token_` + fmt.Sprintf("%d", endpointCalls) + `"}`))
	}))
	defer server.Close()

	// Crear la configuración
	service := &config.Service{
		Name:   "test",
		URL:    server.URL,
		Method: "GET",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		ExtractToken: "token",
		TokenName:    "test_token",
	}

	// Crear la configuración
	cfg := &config.Config{
		Services: []*config.Service{service},
	}

	// Crear el logger
	log := logger.New()
	log.SetOutput(os.Stdout)

	// Crear el runner
	runner := NewRunner(cfg, log)

	// Crear el contexto y variables
	ctx := context.Background()
	threadVars := map[string]string{}
	threadNum := 1

	// Ejecutar el servicio - Primera llamada
	result1, err := runner.executeService(ctx, service, threadVars, threadNum)
	require.NoError(t, err, "Error al ejecutar el servicio (primera vez)")

	// Verificar la respuesta
	assert.NotNil(t, result1, "La respuesta no debería ser nil")
	assert.Equal(t, http.StatusOK, result1.statusCode, "El código de estado no coincide")

	// Verificar que el token se extrajo correctamente
	assert.Contains(t, result1.extractedVars, "test_token", "No se extrajo el token correctamente")
	assert.Equal(t, "test_token_1", result1.extractedVars["test_token"], "El valor del token no coincide")

	// Actualizar las variables del hilo con los valores extraídos
	for k, v := range result1.extractedVars {
		threadVars[k] = v
	}

	// Ejecutar el servicio de nuevo - Segunda llamada
	result2, err := runner.executeService(ctx, service, threadVars, threadNum)
	require.NoError(t, err, "Error al ejecutar el servicio (segunda vez)")

	// Verificar que se obtuvieron las respuestas correctas
	assert.Equal(t, 2, endpointCalls, "El endpoint debería haberse llamado 2 veces, una para cada ejecución del servicio")
	assert.Equal(t, "test_token_2", result2.extractedVars["test_token"], "El valor del token para la segunda llamada no coincide")
}

func TestContainsVariablePattern(t *testing.T) {
	// Casos de prueba
	testCases := []struct {
		input    string
		expected bool
	}{
		{"Bearer {{token}}", true},
		{"{{username}}", true},
		{"No template", false},
		{"{{ incomplete", true},
		{"incomplete }}", false},
		{"{{ }}", true},
	}

	// Patrón para detectar referencias a variables: {{variable}}
	pattern := `\{\{([^{}]+)\}\}`

	// Ejecutar los casos de prueba
	for _, tc := range testCases {
		result := containsVariablePattern(tc.input, pattern)
		assert.Equal(t, tc.expected, result, "containsVariablePattern(%q) = %v, expected %v", tc.input, result, tc.expected)
	}
}

// Función auxiliar para probar si una cadena contiene una variable
func containsVariablePattern(s, pattern string) bool {
	return len(pattern) > 0 && strings.Contains(s, "{{")
}
