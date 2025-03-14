package models

import (
	"net/http"
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
	Tokens map[string]string
}

// NewTokenStore crea un nuevo almacén de tokens
func NewTokenStore() *TokenStore {
	return &TokenStore{
		Tokens: make(map[string]string),
	}
}

// SetToken establece un token en el almacén
func (ts *TokenStore) SetToken(name, value string) {
	ts.Tokens[name] = value
}

// GetToken obtiene un token del almacén
func (ts *TokenStore) GetToken(name string) (string, bool) {
	value, ok := ts.Tokens[name]
	return value, ok
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
	return &ThreadContext{
		ID:            id,
		TokenStore:    NewTokenStore(),
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
