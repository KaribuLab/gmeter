# GMeter - Herramienta de Pruebas de Stress HTTP

<p align="center">
  <img src="gmeter.png" alt="GMeter Logo" width="200"/>
</p>

GMeter es una herramienta de línea de comandos para realizar pruebas de stress en servicios HTTP. Permite configurar múltiples servicios, hilos por segundo, y generar reportes detallados.

## Características

- Configuración mediante archivo YAML
- Soporte para múltiples servicios HTTP
- Control de hilos por segundo (global y por servicio)
- Tiempo de rampa configurable
- Fuentes de datos CSV
- Dependencias entre servicios
- Extracción de tokens de respuestas
- Generación de reportes detallados con gráficos SVG integrados
- Sistema de logs con colores y rotación de archivos
- Soporte para variables de entorno y secretos mediante archivo .env

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
  threads_per_second: 10  # Para compatibilidad con versiones anteriores
  min_threads: 5          # Número mínimo de hilos al inicio
  max_threads: 20         # Número máximo de hilos a alcanzar
  duration: "1m"
  ramp_up: "10s"

# Servicios a probar
services:
  - name: "auth"
    url: "https://api.example.com/auth"
    method: "POST"
    headers:
      Content-Type: "application/json"
      X-Request-ID: "{{uuid}}"  # Generar un UUID único para cada solicitud
    body: |
      {
        "username": "{{.username}}",
        "password": "{{.password}}",
        "request_id": "{{uuid}}"  # Generar un UUID único para cada solicitud
      }
    data_source: "users"
    extract_token: "token"
    token_name: "auth_token"
    threads_per_second: 1  # Solo necesitamos un hilo para autenticación

  # Ejemplo de servicio con form-urlencoded
  - name: "auth_form"
    url: "https://api.example.com/oauth/token"
    method: "POST"
    headers:
      Content-Type: "application/x-www-form-urlencoded"
    content_type: "form"  # Especificar que es un formulario
    form_data:  # Datos estáticos del formulario
      grant_type: "password"
      client_id: "my_client"
    form_data_template:  # Plantilla para datos dinámicos
      username: "{{.username}}"
      password: "{{.password}}"
      request_id: "{{uuid}}"  # Generar un UUID único para cada solicitud
    data_source: "users"
    extract_token: "token"
    token_name: "form_token"
    threads_per_second: 1

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
```

### Parámetros de Configuración

#### Configuración Global

- `log_file`: Ruta del archivo de log
- `report_dir`: Directorio donde se generarán los reportes

#### Configuración de Ejecución

- `threads_per_second`: Número de hilos por segundo (para compatibilidad con versiones anteriores, mínimo 1, máximo 1000)
- `min_threads`: Número mínimo de hilos al iniciar la prueba (mínimo 1, máximo 1000)
- `max_threads`: Número máximo de hilos a alcanzar durante la prueba (mínimo 1, máximo 1000)
- `duration`: Duración total de la prueba (formato: "1h", "30m", "1m30s", etc.)
- `ramp_up`: Tiempo de rampa para incrementar gradualmente desde el mínimo hasta el máximo de hilos (formato: "10s", "1m", etc.)

#### Servicios

- `name`: Nombre del servicio
- `url`: URL del servicio
- `method`: Método HTTP (GET, POST, PUT, DELETE, etc.)
- `headers`: Cabeceras HTTP
- `content_type`: Tipo de contenido de la petición ("json" o "form")
- `body`: Cuerpo de la solicitud (puede contener plantillas usando la sintaxis de Go)
- `form_data`: Datos de formulario estáticos para peticiones form-urlencoded
- `form_data_template`: Plantilla para datos de formulario (usando la sintaxis de plantillas de Go)
- `depends_on`: Nombre del servicio del que depende
- `extract_token`: Nombre del campo para extraer un token de la respuesta
- `token_name`: Nombre con el que se guardará el token extraído
- `data_source`: Nombre de la fuente de datos a utilizar
- `threads_per_second`: Número de hilos por segundo específico para este servicio (para compatibilidad con versiones anteriores)
- `min_threads`: Número mínimo de hilos al iniciar para este servicio (sobreescribe el valor global)
- `max_threads`: Número máximo de hilos a alcanzar para este servicio (sobreescribe el valor global)

#### Fuentes de Datos

- `csv`: Fuentes de datos CSV
  - `path`: Ruta del archivo CSV
  - `delimiter`: Delimitador del archivo CSV
  - `has_header`: Indica si el archivo CSV tiene cabecera

> **Nota**: Si el número de hilos es mayor que el número de registros disponibles en la fuente de datos, GMeter reciclará los datos automáticamente, comenzando desde el primer registro una vez que se hayan utilizado todos.

### Funciones en Plantillas

GMeter soporta las siguientes funciones en las plantillas:

- `{{uuid}}`: Genera un UUID v4 único para cada solicitud. Útil para identificadores de correlación, IDs de solicitud, etc.

Ejemplos de uso:

```yaml
headers:
  X-Request-ID: "{{uuid}}"  # Generar un UUID para la cabecera

body: |
  {
    "request_id": "{{uuid}}",  # Generar un UUID para el cuerpo
    "username": "{{.username}}"
  }

form_data_template:
  request_id: "{{uuid}}"  # Generar un UUID para un campo de formulario
```

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

## Variables de Entorno y Secretos

GMeter permite usar variables de entorno para almacenar información sensible como credenciales, tokens y URLs, evitando que estos datos se incluyan directamente en los archivos de configuración.

### Uso de Variables de Entorno

1. Crea un archivo `.env` en el directorio raíz del proyecto (puedes usar `.env.example` como plantilla)
2. Define tus variables en el formato `NOMBRE=valor`
3. En el archivo de configuración YAML, usa la sintaxis `{{nombre_variable}}` para referenciar las variables

Ejemplo de archivo `.env`:
```
oauth2_client_id=mi_client_id_real
oauth2_client_secret=mi_client_secret_real
api_key=mi_api_key_real
```

Ejemplo de uso en `gmeter.yaml`:
```yaml
services:
  - name: "auth"
    url: "https://api.ejemplo.com/oauth/token"
    method: "POST"
    headers:
      Content-Type: "application/x-www-form-urlencoded"
    content_type: "form"
    form_data:
      grant_type: "client_credentials"
      client_id: "{{oauth2_client_id}}"
      client_secret: "{{oauth2_client_secret}}"
```

### Ventajas

- Mantiene los secretos fuera del control de versiones
- Permite diferentes configuraciones para distintos entornos
- Mejora la seguridad al no exponer información sensible
- Facilita la integración con sistemas de CI/CD

> **Nota**: Asegúrate de añadir el archivo `.env` a tu `.gitignore` para evitar que se suba al repositorio.

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

### Rampa de hilos (Thread Ramping)

GMeter soporta la rampa gradual de hilos, permitiendo comenzar las pruebas con un número mínimo de hilos e ir aumentando gradualmente hasta un número máximo durante el tiempo de rampa especificado.

#### Configuración de la rampa

Para configurar una rampa de hilos:

1. Define el número mínimo de hilos con el parámetro `min_threads` (global o por servicio)
2. Define el número máximo de hilos con el parámetro `max_threads` (global o por servicio)
3. Establece el tiempo de rampa con el parámetro `ramp_up`

Ejemplo de configuración:

```yaml
global:
  min_threads: 5          # Iniciar con 5 hilos
  max_threads: 20         # Aumentar hasta 20 hilos
  duration: "1m"          # Duración total de la prueba
  ramp_up: "10s"          # Tiempo para aumentar de 5 a 20 hilos

services:
  - name: "auth"
    url: "https://api.example.com/auth"
    method: "POST"
    min_threads: 1        # El servicio de auth siempre usa solo 1 hilo
    max_threads: 1

  - name: "get_profile"
    url: "https://api.example.com/profile"
    method: "GET"
    min_threads: 2        # Iniciar con 2 hilos
    max_threads: 10       # Aumentar hasta 10 hilos durante los 10s de rampa
```

#### Comportamiento de la rampa

1. **Fase inicial**: GMeter inicia `min_threads` hilos inmediatamente.
2. **Fase de rampa**: Durante el tiempo de `ramp_up`, GMeter va aumentando gradualmente el número de hilos hasta llegar a `max_threads`.
3. **Fase estable**: Una vez alcanzado el número máximo de hilos, GMeter mantiene este nivel hasta que finalice la prueba.

Si `min_threads` y `max_threads` son iguales, o si `ramp_up` es cero, todos los hilos se inician inmediatamente sin rampa.

> **Nota**: Para compatibilidad con versiones anteriores, si solo se define `threads_per_second`, GMeter lo usará como valor tanto para `min_threads` como para `max_threads`. 