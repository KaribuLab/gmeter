package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// Config representa la configuración principal de la aplicación
type Config struct {
	LogFile      string     `mapstructure:"log_file"`
	ReportDir    string     `mapstructure:"report_dir"`
	GlobalConfig RunConfig  `mapstructure:"global"`
	Services     []*Service `mapstructure:"services"`
	DataSources  DataSource `mapstructure:"data_sources"`
}

// RunConfig representa la configuración de ejecución
type RunConfig struct {
	ThreadsPerSecond int    `mapstructure:"threads_per_second"`
	Duration         string `mapstructure:"duration"`
	RampUp           string `mapstructure:"ramp_up"`
}

// Service representa un servicio HTTP a probar
type Service struct {
	Name             string            `mapstructure:"name"`
	URL              string            `mapstructure:"url"`
	Method           string            `mapstructure:"method"`
	Headers          map[string]string `mapstructure:"headers"`
	Body             string            `mapstructure:"body"`
	BodyTemplate     string            `mapstructure:"body_template"`
	DependsOn        string            `mapstructure:"depends_on"`
	ExtractToken     string            `mapstructure:"extract_token"`
	TokenName        string            `mapstructure:"token_name"`
	DataSourceName   string            `mapstructure:"data_source"`
	ThreadsPerSecond int               `mapstructure:"threads_per_second"`
}

// DataSource representa las fuentes de datos para las pruebas
type DataSource struct {
	CSV map[string]CSVSource `mapstructure:"csv"`
}

// CSVSource representa una fuente de datos CSV
type CSVSource struct {
	Path      string `mapstructure:"path"`
	Delimiter string `mapstructure:"delimiter"`
	HasHeader bool   `mapstructure:"has_header"`
}

// LoadConfig carga la configuración desde un archivo
func LoadConfig(cfgFile string) (*Config, error) {
	v := viper.New()

	// Configuración por defecto
	v.SetDefault("log_file", "gmeter.log")
	v.SetDefault("report_dir", "reports")
	v.SetDefault("global.threads_per_second", 10)
	v.SetDefault("global.duration", "1m")
	v.SetDefault("global.ramp_up", "10s")

	// Buscar el archivo de configuración
	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
	} else {
		v.SetConfigName("gmeter")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
	}

	// Leer la configuración
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			return nil, fmt.Errorf("archivo de configuración no encontrado: %w", err)
		}
		return nil, fmt.Errorf("error al leer el archivo de configuración: %w", err)
	}

	// Crear el directorio de reportes si no existe
	reportDir := v.GetString("report_dir")
	if reportDir != "" {
		if err := os.MkdirAll(reportDir, 0755); err != nil {
			return nil, fmt.Errorf("error al crear el directorio de reportes: %w", err)
		}
	}

	// Parsear la configuración
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("error al parsear la configuración: %w", err)
	}

	// Validar la configuración
	if err := validateConfig(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// validateConfig valida la configuración
func validateConfig(cfg *Config) error {
	if len(cfg.Services) == 0 {
		return fmt.Errorf("no se han definido servicios")
	}

	// Validar que los servicios tengan nombre y URL
	for _, svc := range cfg.Services {
		if svc.Name == "" {
			return fmt.Errorf("un servicio no tiene nombre")
		}
		if svc.URL == "" {
			return fmt.Errorf("el servicio %s no tiene URL", svc.Name)
		}
		if svc.Method == "" {
			svc.Method = "GET" // Método por defecto
		}

		// Validar que el servicio dependiente exista
		if svc.DependsOn != "" {
			found := false
			for _, s := range cfg.Services {
				if s.Name == svc.DependsOn {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("el servicio %s depende de %s, pero este no existe", svc.Name, svc.DependsOn)
			}
		}

		// Validar que la fuente de datos exista
		if svc.DataSourceName != "" {
			if _, ok := cfg.DataSources.CSV[svc.DataSourceName]; !ok {
				return fmt.Errorf("el servicio %s usa la fuente de datos %s, pero esta no existe", svc.Name, svc.DataSourceName)
			}
		}
	}

	// Validar las fuentes de datos CSV
	for name, src := range cfg.DataSources.CSV {
		if src.Path == "" {
			return fmt.Errorf("la fuente de datos CSV %s no tiene ruta", name)
		}

		// Comprobar que el archivo existe
		if _, err := os.Stat(src.Path); os.IsNotExist(err) {
			return fmt.Errorf("el archivo CSV %s no existe", src.Path)
		}

		if src.Delimiter == "" {
			// No podemos modificar directamente el valor en el mapa
			// Creamos una copia, la modificamos y la asignamos de nuevo
			csvSource := src
			csvSource.Delimiter = ","
			cfg.DataSources.CSV[name] = csvSource
		}
	}

	return nil
}

// GetConfigFilePath devuelve la ruta del archivo de configuración
func GetConfigFilePath() string {
	return viper.ConfigFileUsed()
}

// CreateExampleConfig crea un archivo de configuración de ejemplo
func CreateExampleConfig(path string) error {
	exampleConfig := `# Configuración de GMeter
log_file: "gmeter.log"
report_dir: "reports"

# Configuración global
global:
  threads_per_second: 10
  duration: "1m"
  ramp_up: "10s"

# Servicios a probar
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
    threads_per_second: 1  # Solo necesitamos un hilo para autenticación

  - name: "get_profile"
    url: "https://api.example.com/profile"
    method: "GET"
    headers:
      Content-Type: "application/json"
      Authorization: "Bearer {{.auth_token}}"
    depends_on: "auth"
    threads_per_second: 5  # Usar 5 hilos por segundo para este servicio

  - name: "update_profile"
    url: "https://api.example.com/profile"
    method: "PUT"
    headers:
      Content-Type: "application/json"
      Authorization: "Bearer {{.auth_token}}"
    body_template: |
      {
        "name": "{{.name}}",
        "email": "{{.email}}",
        "phone": "{{.phone}}"
      }
    depends_on: "auth"
    data_source: "profiles"
    # Si no se especifica threads_per_second, se usa el valor global (10)

# Fuentes de datos
data_sources:
  csv:
    users:
      path: "data/users.csv"
      delimiter: ","
      has_header: true
    profiles:
      path: "data/profiles.csv"
      delimiter: ","
      has_header: true
`

	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("error al crear el directorio para el archivo de configuración: %w", err)
		}
	}

	return os.WriteFile(path, []byte(exampleConfig), 0644)
}
