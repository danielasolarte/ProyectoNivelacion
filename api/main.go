// Este es el punto de entrada del servicio API.
// En Go, todo programa ejecutable empieza en el paquete "main"
// y en una función llamada "main()".
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// healthHandler responde a peticiones GET /health.
// En Go, un "handler" HTTP es simplemente una función con esta firma:
// func(w http.ResponseWriter, r *http.Request)
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

// uploadHandler responde a peticiones POST /upload.
// Por ahora es un MOCK: no guarda el archivo, no lo procesa, no habla con
// ninguna cola ni base de datos. Solo recibe el archivo, inventa un ID
// de trabajo y lo devuelve de inmediato. El objetivo de este paso es
// practicar cómo se recibe un archivo en Go, no la lógica real todavía.
func uploadHandler(w http.ResponseWriter, r *http.Request) {
	// Un endpoint de carga solo debe aceptar POST. Si alguien intenta
	// un GET u otro método, respondemos con 405 (Method Not Allowed).
	if r.Method != http.MethodPost {
		http.Error(w, "método no permitido, usa POST", http.StatusMethodNotAllowed)
		return
	}

	// ParseMultipartForm procesa una petición de tipo "multipart/form-data",
	// que es como los navegadores (y curl con -F) envían archivos.
	// El argumento (10 << 20) es el límite en bytes que Go puede guardar
	// en memoria mientras procesa la petición: aquí, 10 MB.
	err := r.ParseMultipartForm(10 << 20)
	if err != nil {
		http.Error(w, "no se pudo leer el formulario: "+err.Error(), http.StatusBadRequest)
		return
	}

	// FormFile busca, dentro del formulario recibido, el archivo que se
	// envió bajo el nombre de campo "document". Este nombre es un contrato:
	// quien nos mande el archivo (curl, el frontend, Postman) debe usar
	// exactamente ese nombre de campo.
	file, header, err := r.FormFile("document")
	if err != nil {
		http.Error(w, "no se encontró el archivo en el campo 'document'", http.StatusBadRequest)
		return
	}
	// defer significa "ejecuta esto cuando la función termine, sin importar
	// por dónde salga". Aquí garantiza que cerremos el archivo siempre.
	defer file.Close()

	// Todavía NO leemos ni guardamos el contenido del archivo — eso vendrá
	// cuando conectemos con la base de datos y el almacenamiento de objetos.
	// Por ahora solo confirmamos que llegó y de qué tamaño es.
	log.Printf("archivo recibido: %s (%d bytes)\n", header.Filename, header.Size)

	// ID de trabajo INVENTADO, basado en la hora actual en nanosegundos.
	// Es un mock temporal: cuando exista la base de datos, este ID lo va
	// a generar Postgres (o se generará antes de insertar el registro),
	// no lo vamos a improvisar así en el código de la API.
	jobID := fmt.Sprintf("job-%d", time.Now().UnixNano())

	w.Header().Set("Content-Type", "application/json")
	// 202 Accepted: "recibí tu petición, la voy a procesar, pero no la
	// terminé todavía". Es el código correcto para un flujo asíncrono,
	// distinto del 200 OK que usamos en /health.
	w.WriteHeader(http.StatusAccepted)

	response := map[string]string{
		"job_id": jobID,
		"status": "queued",
	}
	json.NewEncoder(w).Encode(response)
}

func main() {
	// http.HandleFunc registra qué función debe atender cada ruta.
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/upload", uploadHandler)

	log.Println("API escuchando en http://localhost:8080")

	// ListenAndServe bloquea el programa y empieza a atender peticiones.
	// El segundo argumento nil significa "usa el router por defecto"
	// (el que llenamos arriba con http.HandleFunc).
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal(err)
	}
}
