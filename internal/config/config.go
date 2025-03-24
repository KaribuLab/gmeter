package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

// Config representa la configuración principal de la aplicación
type Config struct {
	LogFile         string     `mapstructure:"log_file"`
	ResponseLogFile string     `mapstructure:"response_log_file"` // Archivo de log para las respuestas HTTP
	ReportDir       string     `mapstructure:"report_dir"`
	GlobalConfig    RunConfig  `mapstructure:"global"`
	Services        []*Service `mapstructure:"services"`
	DataSources     DataSource `mapstructure:"data_sources"`

	// Configuración original para reportes (sin reemplazar variables)
	OriginalConfig string
}

// RunConfig representa la configuración de ejecución
type RunConfig struct {
	ThreadsPerSecond int    `mapstructure:"threads_per_second"` // Para compatibilidad con versiones anteriores
	MinThreads       int    `mapstructure:"min_threads"`        // Número mínimo de hilos al iniciar
	MaxThreads       int    `mapstructure:"max_threads"`        // Número máximo de hilos a alcanzar
	BatchSize        int    `mapstructure:"batch_size"`         // Tamaño de los lotes de hilos durante el ramp-up
	Duration         string `mapstructure:"duration"`
	RampUp           string `mapstructure:"ramp_up"`
}

// Service representa un servicio HTTP a probar
type Service struct {
	Name             string            `mapstructure:"name"`
	URL              string            `mapstructure:"url"`
	Method           string            `mapstructure:"method"`
	Headers          map[string]string `mapstructure:"headers"`
	Body             string            `mapstructure:"body"`               // Cuerpo de la solicitud (puede contener plantillas)
	FormData         map[string]string `mapstructure:"form_data"`          // Datos para peticiones form-urlencoded
	FormDataTemplate map[string]string `mapstructure:"form_data_template"` // Plantilla para datos form-urlencoded
	ContentType      string            `mapstructure:"content_type"`       // Tipo de contenido: json, form
	DependsOn        string            `mapstructure:"depends_on"`
	ExtractToken     string            `mapstructure:"extract_token"`
	TokenName        string            `mapstructure:"token_name"`
	CacheToken       bool              `mapstructure:"cache_token"`     // Indica si se debe cachear el token
	TokenExpiry      string            `mapstructure:"token_expiry"`    // Duración de validez del token (ej: "30m", "1h")
	IsAuthService    bool              `mapstructure:"is_auth_service"` // Indica si este servicio proporciona autenticación global
	DataSourceName   string            `mapstructure:"data_source"`
	ThreadsPerSecond int               `mapstructure:"threads_per_second"` // Para compatibilidad con versiones anteriores
	MinThreads       int               `mapstructure:"min_threads"`        // Número mínimo de hilos al iniciar
	MaxThreads       int               `mapstructure:"max_threads"`        // Número máximo de hilos a alcanzar
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
	// Cargar variables de entorno desde .env si existe
	_ = godotenv.Load() // Ignoramos el error si el archivo no existe

	v := viper.New()

	// Configuración por defecto
	v.SetDefault("log_file", "gmeter.log")
	v.SetDefault("response_log_file", "gmeter_responses.log") // Valor por defecto para el log de respuestas
	v.SetDefault("report_dir", "reports")
	v.SetDefault("global.threads_per_second", 10)
	v.SetDefault("global.duration", "1m")
	v.SetDefault("global.ramp_up", "10s")
	v.SetDefault("global.batch_size", 50) // Valor por defecto para el tamaño de lotes

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

	// Guardar la configuración original para reportes
	configPath := v.ConfigFileUsed()
	originalConfig, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("error al leer el archivo de configuración original: %w", err)
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

	// Guardar la configuración original
	cfg.OriginalConfig = string(originalConfig)

	// Reemplazar variables de entorno en la configuración
	if err := replaceEnvVars(&cfg); err != nil {
		return nil, fmt.Errorf("error al reemplazar variables de entorno: %w", err)
	}

	// Validar la configuración
	if err := validateConfig(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// replaceEnvVars reemplaza las referencias a variables de entorno en la configuración
func replaceEnvVars(cfg *Config) error {
	// Patrón para detectar referencias a variables de entorno: {{variable}}
	pattern := regexp.MustCompile(`\{\{([^{}]+)\}\}`)

	// Función para reemplazar una variable en un string
	replaceVar := func(s string) string {
		return pattern.ReplaceAllStringFunc(s, func(match string) string {
			// Extraer el nombre de la variable (quitar {{ y }})
			varName := strings.TrimSpace(match[2 : len(match)-2])

			// Obtener el valor de la variable de entorno
			value := os.Getenv(varName)
			if value == "" {
				// Si no se encuentra, devolver la referencia original
				return match
			}
			return value
		})
	}

	// Reemplazar variables en los servicios
	for _, service := range cfg.Services {
		// Reemplazar en URL
		service.URL = replaceVar(service.URL)

		// Reemplazar en Body
		service.Body = replaceVar(service.Body)

		// Reemplazar en Headers
		for key, value := range service.Headers {
			service.Headers[key] = replaceVar(value)
		}

		// Reemplazar en FormData
		for key, value := range service.FormData {
			service.FormData[key] = replaceVar(value)
		}

		// Reemplazar en FormDataTemplate
		for key, value := range service.FormDataTemplate {
			service.FormDataTemplate[key] = replaceVar(value)
		}
	}

	return nil
}

// validateConfig valida la configuración
func validateConfig(cfg *Config) error {
	if len(cfg.Services) == 0 {
		return fmt.Errorf("no se han definido servicios")
	}

	// Validar límites de hilos por segundo global
	if cfg.GlobalConfig.ThreadsPerSecond < 1 {
		cfg.GlobalConfig.ThreadsPerSecond = 1 // Mínimo 1 hilo por segundo
	} else if cfg.GlobalConfig.ThreadsPerSecond > 1000 {
		cfg.GlobalConfig.ThreadsPerSecond = 1000 // Máximo 1000 hilos por segundo
	}

	// Validar configuración de min/max hilos global
	if cfg.GlobalConfig.MinThreads == 0 && cfg.GlobalConfig.MaxThreads == 0 {
		// Si no se especifican, usar ThreadsPerSecond para ambos (comportamiento tradicional)
		cfg.GlobalConfig.MinThreads = cfg.GlobalConfig.ThreadsPerSecond
		cfg.GlobalConfig.MaxThreads = cfg.GlobalConfig.ThreadsPerSecond
	} else {
		// Validar min_threads
		if cfg.GlobalConfig.MinThreads < 1 {
			cfg.GlobalConfig.MinThreads = 1 // Mínimo 1 hilo
		} else if cfg.GlobalConfig.MinThreads > 1000 {
			cfg.GlobalConfig.MinThreads = 1000 // Máximo 1000 hilos
		}

		// Validar max_threads
		if cfg.GlobalConfig.MaxThreads < 1 {
			cfg.GlobalConfig.MaxThreads = 1 // Mínimo 1 hilo
		} else if cfg.GlobalConfig.MaxThreads > 1000 {
			cfg.GlobalConfig.MaxThreads = 1000 // Máximo 1000 hilos
		}

		// Asegurar que min <= max
		if cfg.GlobalConfig.MinThreads > cfg.GlobalConfig.MaxThreads {
			cfg.GlobalConfig.MinThreads = cfg.GlobalConfig.MaxThreads
		}

		// Si ThreadsPerSecond está configurado pero min/max no, usar ese valor
		if cfg.GlobalConfig.ThreadsPerSecond > 0 && cfg.GlobalConfig.MinThreads == 0 && cfg.GlobalConfig.MaxThreads == 0 {
			cfg.GlobalConfig.MinThreads = cfg.GlobalConfig.ThreadsPerSecond
			cfg.GlobalConfig.MaxThreads = cfg.GlobalConfig.ThreadsPerSecond
		}
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

		// Validar la configuración de caché de tokens
		if svc.TokenExpiry != "" {
			// Verificar que TokenExpiry sea una duración válida
			_, err := time.ParseDuration(svc.TokenExpiry)
			if err != nil {
				return fmt.Errorf("el servicio %s tiene un valor inválido para token_expiry: %s", svc.Name, err)
			}
			// Si se define TokenExpiry, asumir que se desea cachear el token
			svc.CacheToken = true
		}

		// Validar límites de hilos por segundo por servicio
		if svc.ThreadsPerSecond < 0 {
			svc.ThreadsPerSecond = 0 // 0 significa usar el valor global
		} else if svc.ThreadsPerSecond > 1000 {
			svc.ThreadsPerSecond = 1000 // Máximo 1000 hilos por segundo
		}

		// Validar configuración de min/max hilos por servicio
		if svc.MinThreads == 0 && svc.MaxThreads == 0 {
			// Si no se especifican, usar los valores globales o ThreadsPerSecond
			if svc.ThreadsPerSecond > 0 {
				svc.MinThreads = svc.ThreadsPerSecond
				svc.MaxThreads = svc.ThreadsPerSecond
			} else {
				svc.MinThreads = cfg.GlobalConfig.MinThreads
				svc.MaxThreads = cfg.GlobalConfig.MaxThreads
			}
		} else {
			// Validar min_threads
			if svc.MinThreads < 0 {
				svc.MinThreads = 0 // 0 significa usar el valor global
			} else if svc.MinThreads > 1000 {
				svc.MinThreads = 1000 // Máximo 1000 hilos
			}

			// Validar max_threads
			if svc.MaxThreads < 0 {
				svc.MaxThreads = 0 // 0 significa usar el valor global
			} else if svc.MaxThreads > 1000 {
				svc.MaxThreads = 1000 // Máximo 1000 hilos
			}

			// Asegurar que min <= max
			if svc.MinThreads > 0 && svc.MaxThreads > 0 && svc.MinThreads > svc.MaxThreads {
				svc.MinThreads = svc.MaxThreads
			}

			// Si solo se especifica uno de los dos, derivar el otro
			if svc.MinThreads > 0 && svc.MaxThreads == 0 {
				svc.MaxThreads = svc.MinThreads
			} else if svc.MinThreads == 0 && svc.MaxThreads > 0 {
				svc.MinThreads = svc.MaxThreads
			}
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
    body: |
      {
        "username": "{{.username}}",
        "password": "{{.password}}"
      }
    extract_token: "token"
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
    body: |
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
