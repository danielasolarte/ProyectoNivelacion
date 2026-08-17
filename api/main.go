// punto de entrada del servicio del API
package main

import (
	"encoding/json"
	"log"
	"net/http"
)

// healthHandler responde a peticiones , llama automaticamente cada 
// vez que llega una petición a una ruta específica

// - w: por aquí escribes la respuesta que se le devuelve al cliente.
// - r: aquí llega la información de la petición entrante.
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	response := map[string]string{
		"status":  "ok",
		"service": "api",
	}
	// json.NewEncoder(w).Encode(...) convierte el map de Go a JSON
	// y lo escribe directamente en la respuesta HTTP.
	json.NewEncoder(w).Encode(response)
}

func main() {
	// http.HandleFunc registra qué función debe atender cada ruta.
	http.HandleFunc("/health", healthHandler)

	log.Println("API escuchando en http://localhost:8080")

	// ListenAndServe bloquea el programa y empieza a atender peticiones.
	// El segundo argumento nil significa "usa el router por defecto"
	// (el que llenamos arriba con http.HandleFunc).
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal(err)
	}
}
