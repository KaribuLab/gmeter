package runner

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	assert.Equal(t, cfg, runner.Config, "La configuración no coincide")
	assert.Equal(t, log, runner.Logger, "El logger no coincide")
	assert.NotNil(t, runner.Client, "El cliente HTTP no debería ser nil")
	assert.NotNil(t, runner.Results, "El canal de resultados no debería ser nil")
	assert.NotNil(t, runner.DataSource, "El mapa de fuentes de datos no debería ser nil")
}

func TestLoadDataSources(t *testing.T) {
	// Crear un directorio temporal para las pruebas
	tempDir := t.TempDir()

	// Crear un archivo CSV de ejemplo
	csvPath := filepath.Join(tempDir, "users.csv")
	csvContent := "username,password\nuser1,pass1\nuser2,pass2"
	err := os.WriteFile(csvPath, []byte(csvContent), 0644)
	require.NoError(t, err, "Error al crear el archivo CSV de ejemplo")

	// Crear la configuración
	cfg := &config.Config{
		LogFile:   filepath.Join(tempDir, "test.log"),
		ReportDir: filepath.Join(tempDir, "reports"),
		GlobalConfig: config.RunConfig{
			ThreadsPerSecond: 1,
			Duration:         "1s",
			RampUp:           "0s",
		},
		DataSources: config.DataSource{
			CSV: map[string]config.CSVSource{
				"users": {
					Path:      csvPath,
					Delimiter: ",",
					HasHeader: true,
				},
			},
		},
	}

	// Crear el logger
	logger := logger.New()
	logger.SetOutput(os.Stdout)

	// Crear el runner
	runner := NewRunner(cfg, logger)

	// Cargar las fuentes de datos
	err = runner.loadDataSources()
	require.NoError(t, err, "Error al cargar las fuentes de datos")

	// Verificar que se han cargado los datos
	assert.Contains(t, runner.DataSource, "users", "La fuente de datos 'users' no existe")

	// Comprobar el tipo de los datos cargados
	lazyDataSource, ok := runner.DataSource["users"].(*LazyDataSource)
	require.True(t, ok, "La fuente de datos 'users' no es del tipo *LazyDataSource")
	assert.Equal(t, 2, lazyDataSource.Len(), "La fuente de datos 'users' debería tener 2 registros")

	// Verificar el contenido de los registros
	record1, err := lazyDataSource.Get(0)
	require.NoError(t, err, "Error al obtener el primer registro")
	assert.Equal(t, "user1", record1["username"], "El username del primer registro no coincide")
	assert.Equal(t, "pass1", record1["password"], "El password del primer registro no coincide")

	record2, err := lazyDataSource.Get(1)
	require.NoError(t, err, "Error al obtener el segundo registro")
	assert.Equal(t, "user2", record2["username"], "El username del segundo registro no coincide")
	assert.Equal(t, "pass2", record2["password"], "El password del segundo registro no coincide")
}

func TestExecuteService(t *testing.T) {
	// Crear un servidor HTTP de prueba
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verificar el método
		assert.Equal(t, "GET", r.Method, "El método no coincide")

		// Verificar las cabeceras
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"), "La cabecera Content-Type no coincide")
		assert.NotEmpty(t, r.Header.Get("X-Correlation-ID"), "La cabecera X-Correlation-ID no debería estar vacía")

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

	// Crear el contexto del hilo
	threadContext := models.NewThreadContext(1, nil)

	// Ejecutar el servicio
	resp, err := runner.executeService(service, threadContext)
	require.NoError(t, err, "Error al ejecutar el servicio")

	// Verificar la respuesta
	assert.NotNil(t, resp, "La respuesta no debería ser nil")
	assert.Equal(t, http.StatusOK, resp.StatusCode, "El código de estado no coincide")
	assert.Equal(t, "test", resp.ServiceName, "El nombre del servicio no coincide")
	assert.Equal(t, 1, resp.ThreadID, "El ID del hilo no coincide")
	assert.Equal(t, threadContext.CorrelationID, resp.CorrelationID, "El ID de correlación no coincide")
	assert.NotZero(t, resp.ResponseTime, "El tiempo de respuesta no debería ser cero")
	assert.NotNil(t, resp.Body, "El cuerpo de la respuesta no debería ser nil")
	assert.Contains(t, string(resp.Body), "Hello, World!", "El cuerpo de la respuesta no contiene el mensaje esperado")
	assert.Contains(t, string(resp.Body), "test_token", "El cuerpo de la respuesta no contiene el token esperado")
}

func TestContainsTemplate(t *testing.T) {
	// Casos de prueba
	testCases := []struct {
		input    string
		expected bool
	}{
		{"Bearer {{.token}}", true},
		{"{{.username}}", true},
		{"No template", false},
		{"{{ incomplete", false},
		{"incomplete }}", false},
		{"{{ }}", true},
	}

	// Ejecutar los casos de prueba
	for _, tc := range testCases {
		result := containsTemplate(tc.input)
		assert.Equal(t, tc.expected, result, "containsTemplate(%q) = %v, expected %v", tc.input, result, tc.expected)
	}
}
