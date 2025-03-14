package models

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTokenStore(t *testing.T) {
	ts := NewTokenStore()

	// Verificar que el almacén de tokens se inicializa correctamente
	assert.NotNil(t, ts, "El almacén de tokens no debería ser nil")
	assert.NotNil(t, ts.Tokens, "El mapa de tokens no debería ser nil")
	assert.Len(t, ts.Tokens, 0, "El mapa de tokens debería estar vacío inicialmente")

	// Establecer un token
	ts.SetToken("test_token", "test_value")

	// Verificar que el token se ha establecido correctamente
	assert.Len(t, ts.Tokens, 1, "El mapa de tokens debería tener un elemento")
	assert.Equal(t, "test_value", ts.Tokens["test_token"], "El valor del token no coincide")

	// Obtener un token existente
	value, ok := ts.GetToken("test_token")
	assert.True(t, ok, "El token debería existir")
	assert.Equal(t, "test_value", value, "El valor del token no coincide")

	// Obtener un token inexistente
	value, ok = ts.GetToken("nonexistent_token")
	assert.False(t, ok, "El token no debería existir")
	assert.Equal(t, "", value, "El valor del token debería ser una cadena vacía")
}

func TestThreadContext(t *testing.T) {
	data := DataRecord{
		"username": "test_user",
		"password": "test_password",
	}

	tc := NewThreadContext(1, data)

	// Verificar que el contexto de hilo se inicializa correctamente
	assert.NotNil(t, tc, "El contexto de hilo no debería ser nil")
	assert.Equal(t, 1, tc.ID, "El ID del hilo no coincide")
	assert.NotNil(t, tc.TokenStore, "El almacén de tokens no debería ser nil")
	assert.Equal(t, data, tc.Data, "Los datos no coinciden")
	assert.NotEmpty(t, tc.CorrelationID, "El ID de correlación no debería estar vacío")

	// Establecer un token en el contexto
	tc.TokenStore.SetToken("test_token", "test_value")

	// Verificar que el token se ha establecido correctamente
	value, ok := tc.TokenStore.GetToken("test_token")
	assert.True(t, ok, "El token debería existir")
	assert.Equal(t, "test_value", value, "El valor del token no coincide")
}

func TestGenerateCorrelationID(t *testing.T) {
	id1 := generateCorrelationID()
	time.Sleep(10 * time.Millisecond) // Esperar un poco para asegurar que los IDs sean diferentes
	id2 := generateCorrelationID()

	assert.NotEmpty(t, id1, "El ID de correlación no debería estar vacío")
	assert.NotEmpty(t, id2, "El ID de correlación no debería estar vacío")
	assert.NotEqual(t, id1, id2, "Los IDs de correlación deberían ser diferentes")
}

func TestRandomString(t *testing.T) {
	s1 := randomString(8)
	s2 := randomString(8)

	assert.NotEmpty(t, s1, "La cadena aleatoria no debería estar vacía")
	assert.NotEmpty(t, s2, "La cadena aleatoria no debería estar vacía")
	assert.Len(t, s1, 8, "La longitud de la cadena aleatoria debería ser 8")
	assert.Len(t, s2, 8, "La longitud de la cadena aleatoria debería ser 8")
	assert.NotEqual(t, s1, s2, "Las cadenas aleatorias deberían ser diferentes")
}
