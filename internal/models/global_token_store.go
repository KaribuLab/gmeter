package models

import (
	"sync"
	"time"
)

// GlobalTokenStore es un almacén global de tokens (patrón Singleton)
// Se asegura de que todos los hilos compartan los mismos tokens
type GlobalTokenStore struct {
	Tokens        map[string]string
	Expiration    map[string]time.Time
	DefaultExpiry time.Duration
	mutex         sync.RWMutex
}

// Instancia singleton del almacén global de tokens
var (
	instance     *GlobalTokenStore
	instanceOnce sync.Once
)

// GetGlobalTokenStore devuelve la única instancia del almacén global de tokens
func GetGlobalTokenStore() *GlobalTokenStore {
	instanceOnce.Do(func() {
		instance = &GlobalTokenStore{
			Tokens:        make(map[string]string),
			Expiration:    make(map[string]time.Time),
			DefaultExpiry: 30 * time.Minute, // Valor predeterminado de 30 minutos
			mutex:         sync.RWMutex{},
		}
	})
	return instance
}

// SetToken establece un token en el almacén global
func (gts *GlobalTokenStore) SetToken(name, value string) {
	gts.mutex.Lock()
	defer gts.mutex.Unlock()

	gts.Tokens[name] = value
	// No establecemos expiración por defecto al usar el método simple
}

// SetTokenWithExpiry establece un token con tiempo de expiración
func (gts *GlobalTokenStore) SetTokenWithExpiry(name, value string, expiry time.Duration) {
	gts.mutex.Lock()
	defer gts.mutex.Unlock()

	gts.Tokens[name] = value
	if expiry > 0 {
		gts.Expiration[name] = time.Now().Add(expiry)
	}
}

// GetToken obtiene un token del almacén global
func (gts *GlobalTokenStore) GetToken(name string) (string, bool) {
	gts.mutex.RLock()
	defer gts.mutex.RUnlock()

	// Verificar si el token existe
	value, ok := gts.Tokens[name]
	if !ok {
		return "", false
	}

	// Verificar si el token tiene una fecha de expiración
	expiry, hasExpiry := gts.Expiration[name]
	if hasExpiry && time.Now().After(expiry) {
		// El token ha expirado, eliminarlo y devolver no encontrado
		// Nota: Necesitamos un nuevo lock para modificar el mapa
		gts.mutex.RUnlock()
		gts.mutex.Lock()
		delete(gts.Tokens, name)
		delete(gts.Expiration, name)
		gts.mutex.Unlock()
		gts.mutex.RLock()
		return "", false
	}

	return value, true
}

// IsTokenExpired verifica si un token ha expirado
func (gts *GlobalTokenStore) IsTokenExpired(name string) bool {
	gts.mutex.RLock()
	defer gts.mutex.RUnlock()

	expiry, hasExpiry := gts.Expiration[name]
	if !hasExpiry {
		return false // Sin expiración configurada
	}
	return time.Now().After(expiry)
}

// RemoveToken elimina un token del almacén global
func (gts *GlobalTokenStore) RemoveToken(name string) {
	gts.mutex.Lock()
	defer gts.mutex.Unlock()

	delete(gts.Tokens, name)
	delete(gts.Expiration, name)
}

// SetDefaultExpiry establece el tiempo de expiración por defecto
func (gts *GlobalTokenStore) SetDefaultExpiry(expiry time.Duration) {
	gts.mutex.Lock()
	defer gts.mutex.Unlock()

	if expiry > 0 {
		gts.DefaultExpiry = expiry
	}
}
