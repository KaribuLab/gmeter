package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

func main() {
	// Configurar las rutas
	http.HandleFunc("/auth", handleAuth)
	http.HandleFunc("/auth_form", handleAuthForm)
	http.HandleFunc("/profile", handleProfile)
	http.HandleFunc("/update", handleUpdate)
	http.HandleFunc("/update_form", handleUpdateForm)

	// Iniciar el servidor
	fmt.Println("Servidor mock iniciado en http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func handleAuth(w http.ResponseWriter, r *http.Request) {
	// Verificar el método
	if r.Method != "POST" {
		http.Error(w, "Método no permitido", http.StatusMethodNotAllowed)
		return
	}

	// Simular un retraso
	time.Sleep(100 * time.Millisecond)

	// Responder con un token
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"token": "mock_token_12345",
	})
}

func handleAuthForm(w http.ResponseWriter, r *http.Request) {
	// Verificar el método
	if r.Method != "POST" {
		http.Error(w, "Método no permitido", http.StatusMethodNotAllowed)
		return
	}

	// Verificar el tipo de contenido
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/x-www-form-urlencoded" {
		http.Error(w, "Tipo de contenido no soportado", http.StatusUnsupportedMediaType)
		return
	}

	// Parsear el formulario
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Error al parsear el formulario", http.StatusBadRequest)
		return
	}

	// Obtener los datos del formulario
	username := r.FormValue("username")
	password := r.FormValue("password")
	grantType := r.FormValue("grant_type")
	clientID := r.FormValue("client_id")

	// Validar los datos
	if username == "" || password == "" {
		http.Error(w, "Usuario o contraseña no proporcionados", http.StatusBadRequest)
		return
	}

	// Simular un retraso
	time.Sleep(100 * time.Millisecond)

	// Responder con un token
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"token":      "form_token_" + username,
		"grant_type": grantType,
		"client_id":  clientID,
		"expires_in": 3600,
		"token_type": "Bearer",
	})
}

func handleProfile(w http.ResponseWriter, r *http.Request) {
	// Verificar el método
	if r.Method != "GET" {
		http.Error(w, "Método no permitido", http.StatusMethodNotAllowed)
		return
	}

	// Verificar la autorización
	auth := r.Header.Get("Authorization")
	if auth == "" {
		http.Error(w, "No autorizado", http.StatusUnauthorized)
		return
	}

	// Simular un retraso
	time.Sleep(50 * time.Millisecond)

	// Responder con un perfil
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":       123,
		"username": "usuario_test",
		"email":    "test@example.com",
		"created":  time.Now().Format(time.RFC3339),
	})
}

func handleUpdate(w http.ResponseWriter, r *http.Request) {
	// Verificar el método
	if r.Method != "PUT" && r.Method != "POST" {
		http.Error(w, "Método no permitido", http.StatusMethodNotAllowed)
		return
	}

	// Verificar la autorización
	auth := r.Header.Get("Authorization")
	if auth == "" {
		http.Error(w, "No autorizado", http.StatusUnauthorized)
		return
	}

	// Simular un retraso
	time.Sleep(150 * time.Millisecond)

	// Responder con éxito
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Perfil actualizado correctamente",
		"updated": time.Now().Format(time.RFC3339),
	})
}

func handleUpdateForm(w http.ResponseWriter, r *http.Request) {
	// Verificar el método
	if r.Method != "POST" {
		http.Error(w, "Método no permitido", http.StatusMethodNotAllowed)
		return
	}

	// Verificar el tipo de contenido
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/x-www-form-urlencoded" {
		http.Error(w, "Tipo de contenido no soportado", http.StatusUnsupportedMediaType)
		return
	}

	// Verificar la autorización
	auth := r.Header.Get("Authorization")
	if auth == "" {
		http.Error(w, "No autorizado", http.StatusUnauthorized)
		return
	}

	// Parsear el formulario
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Error al parsear el formulario", http.StatusBadRequest)
		return
	}

	// Obtener los datos del formulario
	name := r.FormValue("name")
	email := r.FormValue("email")

	// Simular un retraso
	time.Sleep(150 * time.Millisecond)

	// Responder con éxito
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Perfil actualizado correctamente mediante formulario",
		"name":    name,
		"email":   email,
		"updated": time.Now().Format(time.RFC3339),
	})
}
