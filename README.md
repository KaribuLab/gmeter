# GMeter - Herramienta de Pruebas de Stress HTTP

GMeter es una herramienta de línea de comandos para realizar pruebas de stress en servicios HTTP. Permite configurar múltiples servicios, hilos por segundo, y generar reportes detallados.

## Características

- Configuración mediante archivo YAML
- Soporte para múltiples servicios HTTP
- Control de hilos por segundo
- Tiempo de rampa configurable
- Fuentes de datos CSV
- Dependencias entre servicios
- Extracción de tokens de respuestas
- Generación de reportes detallados con gráficos SVG integrados
- Sistema de logs con colores y rotación de archivos

## Instalación

### Desde el código fuente

```bash
git clone https://github.com/KaribuLab/gmeter.git
cd gmeter
go build -o gmeter ./cmd/gmeter
```

### Usando Go Install

```bash
go install github.com/KaribuLab/gmeter/cmd/gmeter@latest
```

## Uso

### Ejecutar con el archivo de configuración por defecto

```bash
./gmeter
```

### Especificar un archivo de configuración

```bash
./gmeter --config mi_configuracion.yaml
```

### Opciones de línea de comandos

```bash
./gmeter --help
```

Opciones disponibles:

- `--config`: Archivo de configuración (por defecto es ./gmeter.yaml)
- `--verbose, -v`: Mostrar logs en consola (por defecto: true)
- `--log-level`: Nivel de log (trace, debug, info, warn, error, fatal, panic) (por defecto: info)
- `--log-max-size`: Tamaño máximo del archivo de log en MB antes de rotar (por defecto: 10)
- `--log-max-backups`: Número máximo de archivos de respaldo (por defecto: 5)
- `--log-max-age`: Días máximos para mantener los archivos de log (por defecto: 30)
- `--log-compress`: Comprimir los archivos de log rotados (por defecto: true)

## Archivo de Configuración

GMeter utiliza un archivo de configuración YAML para definir los servicios a probar y los parámetros de la prueba. Por defecto, busca un archivo llamado `gmeter.yaml` en el directorio actual.

Ejemplo de archivo de configuración:

```yaml
# Configuración de GMeter
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

  - name: "get_profile"
    url: "https://api.example.com/profile"
    method: "GET"
    headers:
      Content-Type: "application/json"
      Authorization: "Bearer {{.auth_token}}"
    depends_on: "auth"

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
```

### Parámetros de Configuración

#### Configuración Global

- `log_file`: Ruta del archivo de log
- `report_dir`: Directorio donde se generarán los reportes

#### Configuración de Ejecución

- `threads_per_second`: Número de hilos por segundo
- `duration`: Duración total de la prueba (formato: "1h", "30m", "1m30s", etc.)
- `ramp_up`: Tiempo de rampa para alcanzar el número total de hilos (formato: "10s", "1m", etc.)

#### Servicios

- `name`: Nombre del servicio
- `url`: URL del servicio
- `method`: Método HTTP (GET, POST, PUT, DELETE, etc.)
- `headers`: Cabeceras HTTP
- `body`: Cuerpo de la solicitud (texto plano)
- `body_template`: Plantilla para el cuerpo de la solicitud (usando la sintaxis de plantillas de Go)
- `depends_on`: Nombre del servicio del que depende
- `extract_token`: Expresión JSONPath para extraer un token de la respuesta
- `token_name`: Nombre con el que se guardará el token extraído
- `data_source`: Nombre de la fuente de datos a utilizar

#### Fuentes de Datos

- `csv`: Fuentes de datos CSV
  - `path`: Ruta del archivo CSV
  - `delimiter`: Delimitador del archivo CSV
  - `has_header`: Indica si el archivo CSV tiene cabecera

## Sistema de Logs

GMeter incluye un sistema de logs avanzado con las siguientes características:

- Logs con colores en la consola para mejor legibilidad
- Rotación de archivos de log para evitar archivos demasiado grandes
- Configuración de niveles de log (trace, debug, info, warn, error, fatal, panic)
- Compresión de archivos de log rotados

La configuración de logs se puede ajustar mediante parámetros de línea de comandos:

```bash
./gmeter --log-level debug --log-max-size 5 --log-max-backups 3 --log-max-age 7
```

## Reportes

GMeter genera reportes detallados en formato Markdown con gráficos SVG integrados. Los reportes incluyen:

- Resumen de la prueba
- Gráfico de solicitudes por servicio
- Estadísticas por servicio
  - Total de solicitudes
  - Solicitudes exitosas y fallidas
  - Tiempos de respuesta (mínimo, máximo, promedio)
  - Solicitudes por segundo
  - Gráfico de tiempos de respuesta
  - Códigos de estado
  - Gráfico de distribución de códigos de estado
  - Errores
- Configuración utilizada en formato YAML

Los gráficos SVG se integran directamente en el archivo Markdown, lo que permite visualizarlos fácilmente en cualquier visor de Markdown que soporte SVG, como GitHub, GitLab, o editores como Visual Studio Code.

## Licencia

Este proyecto está licenciado bajo la licencia MIT - ver el archivo [LICENSE](LICENSE) para más detalles.

## Contribuir

Las contribuciones son bienvenidas. Por favor, abre un issue o un pull request para contribuir al proyecto. 