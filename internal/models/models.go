package models

import (
	"net/http"
	"sync"
	"time"
)

// Request representa una solicitud HTTP
type Request struct {
	URL     string
	Method  string
	Headers map[string]string
	Body    []byte
}

// Response representa una respuesta HTTP
type Response struct {
	StatusCode    int
	Headers       http.Header
	Body          []byte
	ResponseTime  time.Duration
	Error         error
	RequestTime   time.Time
	ServiceName   string
	ThreadID      int
	CorrelationID string
}

// ServiceResult representa el resultado de la ejecución de un servicio
type ServiceResult struct {
	ServiceName   string
	URL           string
	Method        string
	StatusCode    int
	ResponseTime  time.Duration
	Error         string
	RequestTime   time.Time
	ThreadID      int
	CorrelationID string
}

// ServiceStats representa las estadísticas de un servicio
type ServiceStats struct {
	ServiceName       string
	TotalRequests     int
	SuccessRequests   int
	FailedRequests    int
	MinResponseTime   time.Duration
	MaxResponseTime   time.Duration
	AvgResponseTime   time.Duration
	Percentile50      time.Duration
	Percentile90      time.Duration
	Percentile95      time.Duration
	Percentile99      time.Duration
	RequestsPerSecond float64
	StartTime         time.Time
	EndTime           time.Time
	StatusCodes       map[int]int
	Errors            map[string]int
}

// TestResult representa el resultado de una prueba completa
type TestResult struct {
	StartTime       time.Time
	EndTime         time.Time
	TotalRequests   int
	SuccessRequests int
	FailedRequests  int
	ServiceStats    map[string]*ServiceStats
	Config          interface{}
}

// DataRecord representa un registro de datos para las pruebas
type DataRecord map[string]string

// TokenStore almacena los tokens extraídos de las respuestas
type TokenStore struct {
	Tokens        map[string]string
	Expiration    map[string]time.Time // Mapa para almacenar las fechas de expiración de los tokens
	DefaultExpiry time.Duration        // Tiempo de expiración por defecto para los tokens
	mu            sync.RWMutex
}

// NewTokenStore crea un nuevo almacén de tokens
func NewTokenStore() *TokenStore {
	return &TokenStore{
		Tokens:        make(map[string]string),
		Expiration:    make(map[string]time.Time),
		DefaultExpiry: 30 * time.Minute, // Valor predeterminado de 30 minutos
	}
}

// SetToken establece un token en el almacén
func (ts *TokenStore) SetToken(name, value string) {
	ts.Tokens[name] = value
	// No establecemos expiración por defecto al usar el método simple
}

// SetTokenWithExpiry establece un token con tiempo de expiración
func (ts *TokenStore) SetTokenWithExpiry(name, value string, expiry time.Duration) {
	ts.Tokens[name] = value
	if expiry > 0 {
		ts.Expiration[name] = time.Now().Add(expiry)
	}
}

// GetToken obtiene un token del almacén
func (ts *TokenStore) GetToken(name string) (string, bool) {
	// Verificar si el token existe
	value, ok := ts.Tokens[name]
	if !ok {
		return "", false
	}

	// Verificar si el token tiene una fecha de expiración
	expiry, hasExpiry := ts.Expiration[name]
	if hasExpiry && time.Now().After(expiry) {
		// El token ha expirado, eliminarlo y devolver no encontrado
		delete(ts.Tokens, name)
		delete(ts.Expiration, name)
		return "", false
	}

	return value, true
}

// IsTokenExpired verifica si un token ha expirado
func (ts *TokenStore) IsTokenExpired(name string) bool {
	expiry, hasExpiry := ts.Expiration[name]
	if !hasExpiry {
		return false // Sin expiración configurada
	}
	return time.Now().After(expiry)
}

// RemoveToken elimina un token del almacén
func (ts *TokenStore) RemoveToken(name string) {
	delete(ts.Tokens, name)
	delete(ts.Expiration, name)
}

// SetDefaultExpiry establece el tiempo de expiración por defecto
func (ts *TokenStore) SetDefaultExpiry(expiry time.Duration) {
	if expiry > 0 {
		ts.DefaultExpiry = expiry
	}
}

// ThreadContext representa el contexto de un hilo de ejecución
type ThreadContext struct {
	ID            int
	TokenStore    *TokenStore
	Data          DataRecord
	CorrelationID string
}

// NewThreadContext crea un nuevo contexto de hilo
func NewThreadContext(id int, data DataRecord) *ThreadContext {
	// Crear un nuevo TokenStore
	tokenStore := NewTokenStore()

	// Obtener el almacén global de tokens
	globalTokens := GetGlobalTokenStore()

	// Si hay tokens en el almacén global, copiarlos al TokenStore local
	// Esto permite que los nuevos hilos empiecen con los tokens existentes
	if globalTokens != nil {
		globalTokens.mutex.RLock()
		for tokenName, tokenValue := range globalTokens.Tokens {
			tokenStore.SetToken(tokenName, tokenValue)
		}

		// También copiar información de expiración si existe
		for tokenName, expiry := range globalTokens.Expiration {
			if !expiry.IsZero() {
				// Verificar si el token ya expiró
				if time.Now().Before(expiry) {
					// Solo copiar tokens no expirados
					tokenStore.Expiration[tokenName] = expiry
				}
			}
		}
		globalTokens.mutex.RUnlock()
	}

	return &ThreadContext{
		ID:            id,
		TokenStore:    tokenStore,
		Data:          data,
		CorrelationID: generateCorrelationID(),
	}
}

// generateCorrelationID genera un ID de correlación único
func generateCorrelationID() string {
	return time.Now().Format("20060102150405") + "-" + randomString(8)
}

// randomString genera una cadena aleatoria de la longitud especificada
func randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[time.Now().UnixNano()%int64(len(charset))]
	}
	return string(b)
}
