// Este es el punto de entrada del servicio API.
// En Go, todo programa ejecutable empieza en el paquete "main"
// y en una función llamada "main()".
package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

var db *pgxpool.Pool

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

// uploadHandler registra los metadatos del documento y crea un trabajo pendiente.
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

	log.Printf("archivo recibido: %s (%d bytes)\n", header.Filename, header.Size)

	ctx := r.Context()
	userID, err := ensureDefaultUser(ctx)
	if err != nil {
		http.Error(w, "no se pudo identificar al usuario: "+err.Error(), http.StatusInternalServerError)
		return
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		http.Error(w, "no se pudo iniciar la transacción", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(ctx)

	var documentID string
	err = tx.QueryRow(ctx, `
		INSERT INTO documents (user_id, original_name, content_type, size_bytes)
		VALUES ($1, $2, $3, $4)
		RETURNING id`, userID, header.Filename, header.Header.Get("Content-Type"), header.Size).Scan(&documentID)
	if err != nil {
		http.Error(w, "no se pudo registrar el documento", http.StatusInternalServerError)
		return
	}

	var jobID string
	err = tx.QueryRow(ctx, `
		INSERT INTO jobs (document_id, status)
		VALUES ($1, 'queued')
		RETURNING id`, documentID).Scan(&jobID)
	if err != nil {
		http.Error(w, "no se pudo crear el trabajo", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		http.Error(w, "no se pudo confirmar el trabajo", http.StatusInternalServerError)
		return
	}

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

func ensureDefaultUser(ctx context.Context) (string, error) {
	email := os.Getenv("DEFAULT_USER_EMAIL")
	if email == "" {
		email = "demo@example.com"
	}

	var userID string
	err := db.QueryRow(ctx, `
		INSERT INTO users (email)
		VALUES ($1)
		ON CONFLICT (email) DO UPDATE SET email = EXCLUDED.email
		RETURNING id`, email).Scan(&userID)
	return userID, err
}

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL no está configurada")
	}

	var err error
	db, err = pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		log.Fatal("no se pudo crear el pool de PostgreSQL: ", err)
	}
	defer db.Close()

	if err := db.Ping(context.Background()); err != nil {
		log.Fatal("no se pudo conectar a PostgreSQL: ", err)
	}

	// http.HandleFunc registra qué función debe atender cada ruta.
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/upload", uploadHandler)

	log.Println("API escuchando en http://localhost:8080")

	// ListenAndServe bloquea el programa y empieza a atender peticiones.
	// El segundo argumento nil significa "usa el router por defecto"
	// (el que llenamos arriba con http.HandleFunc).
	err = http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal(err)
	}
}
