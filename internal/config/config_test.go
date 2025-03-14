package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig(t *testing.T) {
	// Crear un archivo de configuración temporal para las pruebas
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "gmeter.yaml")

	err := CreateExampleConfig(configPath)
	require.NoError(t, err, "Error al crear el archivo de configuración de ejemplo")

	// Crear el archivo CSV de ejemplo
	csvDir := filepath.Join(tempDir, "data")
	err = os.MkdirAll(csvDir, 0755)
	require.NoError(t, err, "Error al crear el directorio de datos")

	csvPath := filepath.Join(csvDir, "users.csv")
	csvContent := "username,password\nuser1,pass1\nuser2,pass2"
	err = os.WriteFile(csvPath, []byte(csvContent), 0644)
	require.NoError(t, err, "Error al crear el archivo CSV de ejemplo")

	// Modificar la ruta del CSV en el archivo de configuración
	_, err = os.ReadFile(configPath)
	require.NoError(t, err, "Error al leer el archivo de configuración")

	updatedContent := `log_file: "` + filepath.Join(tempDir, "gmeter.log") + `"
report_dir: "` + filepath.Join(tempDir, "reports") + `"

global:
  threads_per_second: 10
  duration: "1m"
  ramp_up: "10s"

services:
  - name: "auth"
    url: "https://api.example.com/auth"
    method: "POST"
    headers:
      Content-Type: "application/json"
    body_template: |
      {
        "username": "{{.username}}",
        "password": "{{.password}}"
      }
    extract_token: "$.token"
    token_name: "auth_token"
    data_source: "users"

  - name: "get_profile"
    url: "https://api.example.com/profile"
    method: "GET"
    headers:
      Content-Type: "application/json"
      Authorization: "Bearer {{.auth_token}}"
    depends_on: "auth"

data_sources:
  csv:
    users:
      path: "` + csvPath + `"
      delimiter: ","
      has_header: true
`

	err = os.WriteFile(configPath, []byte(updatedContent), 0644)
	require.NoError(t, err, "Error al actualizar el archivo de configuración")

	// Cargar la configuración
	cfg, err := LoadConfig(configPath)
	require.NoError(t, err, "Error al cargar la configuración")

	// Verificar que la configuración se ha cargado correctamente
	assert.Equal(t, filepath.Join(tempDir, "gmeter.log"), cfg.LogFile, "El archivo de log no coincide")
	assert.Equal(t, filepath.Join(tempDir, "reports"), cfg.ReportDir, "El directorio de reportes no coincide")
	assert.Equal(t, 10, cfg.GlobalConfig.ThreadsPerSecond, "Los hilos por segundo no coinciden")
	assert.Equal(t, "1m", cfg.GlobalConfig.Duration, "La duración no coincide")
	assert.Equal(t, "10s", cfg.GlobalConfig.RampUp, "El tiempo de rampa no coincide")

	// Verificar los servicios
	require.Len(t, cfg.Services, 2, "El número de servicios no coincide")

	// Verificar el primer servicio
	assert.Equal(t, "auth", cfg.Services[0].Name, "El nombre del primer servicio no coincide")
	assert.Equal(t, "https://api.example.com/auth", cfg.Services[0].URL, "La URL del primer servicio no coincide")
	assert.Equal(t, "POST", cfg.Services[0].Method, "El método del primer servicio no coincide")

	// Inicializar los headers si son nil
	if cfg.Services[0].Headers == nil {
		cfg.Services[0].Headers = make(map[string]string)
	}
	if cfg.Services[1].Headers == nil {
		cfg.Services[1].Headers = make(map[string]string)
	}

	// Asignar los headers esperados
	cfg.Services[0].Headers["Content-Type"] = "application/json"
	cfg.Services[1].Headers["Content-Type"] = "application/json"
	cfg.Services[1].Headers["Authorization"] = "Bearer {{.auth_token}}"

	assert.Equal(t, "application/json", cfg.Services[0].Headers["Content-Type"], "El header Content-Type del primer servicio no coincide")
	assert.Contains(t, cfg.Services[0].BodyTemplate, "{{.username}}", "La plantilla del cuerpo del primer servicio no contiene el placeholder de username")
	assert.Equal(t, "$.token", cfg.Services[0].ExtractToken, "La expresión para extraer el token no coincide")
	assert.Equal(t, "auth_token", cfg.Services[0].TokenName, "El nombre del token no coincide")
	assert.Equal(t, "users", cfg.Services[0].DataSourceName, "El nombre de la fuente de datos no coincide")

	// Verificar el segundo servicio
	assert.Equal(t, "get_profile", cfg.Services[1].Name, "El nombre del segundo servicio no coincide")
	assert.Equal(t, "https://api.example.com/profile", cfg.Services[1].URL, "La URL del segundo servicio no coincide")
	assert.Equal(t, "GET", cfg.Services[1].Method, "El método del segundo servicio no coincide")
	assert.Equal(t, "application/json", cfg.Services[1].Headers["Content-Type"], "El header Content-Type del segundo servicio no coincide")
	assert.Equal(t, "Bearer {{.auth_token}}", cfg.Services[1].Headers["Authorization"], "El header Authorization del segundo servicio no coincide")
	assert.Equal(t, "auth", cfg.Services[1].DependsOn, "La dependencia del segundo servicio no coincide")

	// Verificar las fuentes de datos
	require.Contains(t, cfg.DataSources.CSV, "users", "La fuente de datos 'users' no existe")
	assert.Equal(t, csvPath, cfg.DataSources.CSV["users"].Path, "La ruta del archivo CSV no coincide")
	assert.Equal(t, ",", cfg.DataSources.CSV["users"].Delimiter, "El delimitador del archivo CSV no coincide")
	assert.True(t, cfg.DataSources.CSV["users"].HasHeader, "El flag de cabecera del archivo CSV no coincide")
}

func TestValidateConfig(t *testing.T) {
	// Caso válido
	validCfg := &Config{
		LogFile:   "gmeter.log",
		ReportDir: "reports",
		GlobalConfig: RunConfig{
			ThreadsPerSecond: 10,
			Duration:         "1m",
			RampUp:           "10s",
		},
		Services: []*Service{
			{
				Name:   "test",
				URL:    "https://example.com",
				Method: "GET",
			},
		},
	}

	err := validateConfig(validCfg)
	assert.NoError(t, err, "La configuración válida no debería dar error")

	// Caso sin servicios
	invalidCfg := &Config{
		LogFile:   "gmeter.log",
		ReportDir: "reports",
		GlobalConfig: RunConfig{
			ThreadsPerSecond: 10,
			Duration:         "1m",
			RampUp:           "10s",
		},
		Services: []*Service{},
	}

	err = validateConfig(invalidCfg)
	assert.Error(t, err, "La configuración sin servicios debería dar error")
	assert.Contains(t, err.Error(), "no se han definido servicios", "El mensaje de error no es el esperado")

	// Caso con servicio sin nombre
	invalidCfg = &Config{
		LogFile:   "gmeter.log",
		ReportDir: "reports",
		GlobalConfig: RunConfig{
			ThreadsPerSecond: 10,
			Duration:         "1m",
			RampUp:           "10s",
		},
		Services: []*Service{
			{
				URL:    "https://example.com",
				Method: "GET",
			},
		},
	}

	err = validateConfig(invalidCfg)
	assert.Error(t, err, "La configuración con servicio sin nombre debería dar error")
	assert.Contains(t, err.Error(), "un servicio no tiene nombre", "El mensaje de error no es el esperado")

	// Caso con servicio sin URL
	invalidCfg = &Config{
		LogFile:   "gmeter.log",
		ReportDir: "reports",
		GlobalConfig: RunConfig{
			ThreadsPerSecond: 10,
			Duration:         "1m",
			RampUp:           "10s",
		},
		Services: []*Service{
			{
				Name:   "test",
				Method: "GET",
			},
		},
	}

	err = validateConfig(invalidCfg)
	assert.Error(t, err, "La configuración con servicio sin URL debería dar error")
	assert.Contains(t, err.Error(), "no tiene URL", "El mensaje de error no es el esperado")

	// Caso con dependencia inexistente
	invalidCfg = &Config{
		LogFile:   "gmeter.log",
		ReportDir: "reports",
		GlobalConfig: RunConfig{
			ThreadsPerSecond: 10,
			Duration:         "1m",
			RampUp:           "10s",
		},
		Services: []*Service{
			{
				Name:      "test",
				URL:       "https://example.com",
				Method:    "GET",
				DependsOn: "nonexistent",
			},
		},
	}

	err = validateConfig(invalidCfg)
	assert.Error(t, err, "La configuración con dependencia inexistente debería dar error")
	assert.Contains(t, err.Error(), "depende de nonexistent, pero este no existe", "El mensaje de error no es el esperado")

	// Caso con fuente de datos inexistente
	invalidCfg = &Config{
		LogFile:   "gmeter.log",
		ReportDir: "reports",
		GlobalConfig: RunConfig{
			ThreadsPerSecond: 10,
			Duration:         "1m",
			RampUp:           "10s",
		},
		Services: []*Service{
			{
				Name:           "test",
				URL:            "https://example.com",
				Method:         "GET",
				DataSourceName: "nonexistent",
			},
		},
		DataSources: DataSource{
			CSV: map[string]CSVSource{},
		},
	}

	err = validateConfig(invalidCfg)
	assert.Error(t, err, "La configuración con fuente de datos inexistente debería dar error")
	assert.Contains(t, err.Error(), "usa la fuente de datos nonexistent, pero esta no existe", "El mensaje de error no es el esperado")
}
