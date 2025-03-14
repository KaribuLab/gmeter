package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
)

type AuthRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type AuthResponse struct {
	Token string `json:"token"`
}

type ProfileResponse struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Phone string `json:"phone"`
}

type UpdateProfileRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Phone string `json:"phone"`
}

func main() {
	// Manejador para la autenticación
	http.HandleFunc("/auth", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Método no permitido", http.StatusMethodNotAllowed)
			return
		}

		var authReq AuthRequest
		err := json.NewDecoder(r.Body).Decode(&authReq)
		if err != nil {
			http.Error(w, "Error al decodificar la solicitud", http.StatusBadRequest)
			return
		}

		// Validación simple
		if authReq.Username == "" || authReq.Password == "" {
			http.Error(w, "Usuario o contraseña no proporcionados", http.StatusBadRequest)
			return
		}

		// Generar token simple
		token := fmt.Sprintf("token_%s_%s", authReq.Username, authReq.Password)

		response := AuthResponse{
			Token: token,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	})

	// Manejador para obtener perfil
	http.HandleFunc("/profile", func(w http.ResponseWriter, r *http.Request) {
		// Verificar token de autorización
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, "Token de autorización no proporcionado", http.StatusUnauthorized)
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")

		// Extraer username del token (formato: token_username_password)
		parts := strings.Split(token, "_")
		if len(parts) < 2 {
			http.Error(w, "Token inválido", http.StatusUnauthorized)
			return
		}

		username := parts[1]

		if r.Method == "GET" {
			// Respuesta para GET
			profile := ProfileResponse{
				ID:    1,
				Name:  "Usuario " + username,
				Email: username + "@example.com",
				Phone: "+123456789",
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(profile)
		} else if r.Method == "PUT" {
			// Respuesta para PUT
			var updateReq UpdateProfileRequest
			err := json.NewDecoder(r.Body).Decode(&updateReq)
			if err != nil {
				http.Error(w, "Error al decodificar la solicitud", http.StatusBadRequest)
				return
			}

			// Respuesta actualizada
			profile := ProfileResponse{
				ID:    1,
				Name:  updateReq.Name,
				Email: updateReq.Email,
				Phone: updateReq.Phone,
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(profile)
		} else {
			http.Error(w, "Método no permitido", http.StatusMethodNotAllowed)
		}
	})

	fmt.Println("Servidor mock iniciado en http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
