// Este es el punto de entrada del servicio API.
// En Go, todo programa ejecutable empieza en el paquete "main"
// y en una función llamada "main()".
package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
)

var db *pgxpool.Pool
var objectStore *minio.Client
var objectBucket string
var jobQueue *redis.Client
var jobQueueName string
var authSecret []byte

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


// metricsHandler
func metricsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	rows, err := db.Query(ctx, `SELECT status, COUNT(*) FROM jobs GROUP BY status`)
	if err != nil {
		http.Error(w, "no se pudieron calcular las métricas", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	jobsByStatus := map[string]int{}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			http.Error(w, "no se pudieron leer las métricas", http.StatusInternalServerError)
			return
		}
		jobsByStatus[status] = count
	}

	var avgSeconds *float64
	err = db.QueryRow(ctx, `
		SELECT AVG(EXTRACT(EPOCH FROM (updated_at - created_at)))
		FROM jobs WHERE status = 'completed'`).Scan(&avgSeconds)
	if err != nil {
		http.Error(w, "no se pudo calcular el tiempo promedio", http.StatusInternalServerError)
		return
	}

	var jobsRetried int
	err = db.QueryRow(ctx, `
		SELECT COUNT(*) FROM jobs WHERE retried_from_job_id IS NOT NULL`).Scan(&jobsRetried)
	if err != nil {
		http.Error(w, "no se pudo contar los reintentos", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"jobs_by_status":         jobsByStatus,
		"avg_processing_seconds": avgSeconds,
		"jobs_retried":           jobsRetried,
	})
}



func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "http://localhost:3000")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
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
	userID, err := authenticatedUserID(r)
	if err != nil {
		http.Error(w, "autenticación requerida", http.StatusUnauthorized)
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
		INSERT INTO documents (user_id, original_name, content_type, size_bytes, storage_key)
		VALUES ($1, $2, $3, $4, '')
		RETURNING id`, userID, header.Filename, header.Header.Get("Content-Type"), header.Size).Scan(&documentID)
	if err != nil {
		http.Error(w, "no se pudo registrar el documento", http.StatusInternalServerError)
		return
	}

	storageKey := path.Join("users", userID, "documents", documentID, path.Base(header.Filename))
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	_, err = objectStore.PutObject(ctx, objectBucket, storageKey, file, header.Size, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		http.Error(w, "no se pudo guardar el archivo", http.StatusInternalServerError)
		return
	}

	_, err = tx.Exec(ctx, `UPDATE documents SET storage_key = $1 WHERE id = $2`, storageKey, documentID)
	if err != nil {
		http.Error(w, "no se pudo actualizar la ubicacion del archivo", http.StatusInternalServerError)
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
	if err := jobQueue.LPush(ctx, jobQueueName, jobID).Err(); err != nil {
		http.Error(w, "no se pudo encolar el trabajo", http.StatusServiceUnavailable)
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

func jobsHandler(w http.ResponseWriter, r *http.Request) {
	jobPath := strings.TrimPrefix(r.URL.Path, "/jobs/")
	parts := strings.Split(strings.Trim(jobPath, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}

	switch {
	case len(parts) == 1 && r.Method == http.MethodGet:
		jobStatusHandler(w, r, parts[0])
	case len(parts) == 2 && parts[1] == "download" && r.Method == http.MethodGet:
		jobDownloadHandler(w, r, parts[0])
	case len(parts) == 2 && parts[1] == "retry" && r.Method == http.MethodPost:
		jobRetryHandler(w, r, parts[0])
	default:
		http.NotFound(w, r)
	}
}

func registerHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "método no permitido, usa POST", http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.Email == "" || len(input.Password) < 8 {
		http.Error(w, "email y contraseña de al menos 8 caracteres son obligatorios", http.StatusBadRequest)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "no se pudo proteger la contraseña", http.StatusInternalServerError)
		return
	}
	var userID string
	err = db.QueryRow(r.Context(), `
		INSERT INTO users (email, password_hash)
		VALUES ($1, $2)
		RETURNING id`, strings.ToLower(strings.TrimSpace(input.Email)), string(hash)).Scan(&userID)
	if err != nil {
		http.Error(w, "el email ya está registrado", http.StatusConflict)
		return
	}
	token, err := createToken(userID)
	if err != nil {
		http.Error(w, "no se pudo crear la sesión", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"user_id": userID, "token": token})
}

func loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "método no permitido, usa POST", http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "cuerpo JSON inválido", http.StatusBadRequest)
		return
	}
	var userID string
	var passwordHash *string
	err := db.QueryRow(r.Context(), `SELECT id, password_hash FROM users WHERE email = $1`, strings.ToLower(strings.TrimSpace(input.Email))).Scan(&userID, &passwordHash)
	if err != nil || passwordHash == nil || bcrypt.CompareHashAndPassword([]byte(*passwordHash), []byte(input.Password)) != nil {
		http.Error(w, "credenciales inválidas", http.StatusUnauthorized)
		return
	}
	token, err := createToken(userID)
	if err != nil {
		http.Error(w, "no se pudo crear la sesión", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"user_id": userID, "token": token})
}

func jobStatusHandler(w http.ResponseWriter, r *http.Request, jobID string) {
	userID, err := authenticatedUserID(r)
	if err != nil {
		http.Error(w, "autenticación requerida", http.StatusUnauthorized)
		return
	}

	var originalName string
	var status string
	var errorMessage *string
	err = db.QueryRow(r.Context(), `
		SELECT d.original_name, j.status, j.error_message
		FROM jobs j
		JOIN documents d ON d.id = j.document_id
		WHERE j.id = $1 AND d.user_id = $2`, jobID, userID).Scan(&originalName, &status, &errorMessage)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"job_id":        jobID,
		"original_name": originalName,
		"status":        status,
		"error":         errorMessage,
	})
}

func jobDownloadHandler(w http.ResponseWriter, r *http.Request, jobID string) {
	userID, err := authenticatedUserID(r)
	if err != nil {
		http.Error(w, "autenticación requerida", http.StatusUnauthorized)
		return
	}

	var storageKey string
	err = db.QueryRow(r.Context(), `
		SELECT b.storage_key
		FROM bundles b
		JOIN jobs j ON j.id = b.job_id
		JOIN documents d ON d.id = j.document_id
		WHERE b.job_id = $1 AND d.user_id = $2 AND j.status = 'completed'`, jobID, userID).Scan(&storageKey)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	object, err := objectStore.GetObject(r.Context(), objectBucket, storageKey, minio.GetObjectOptions{})
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer object.Close()
	if _, err := object.Stat(); err != nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="bundle.zip"`)
	if _, err := io.Copy(w, object); err != nil {
		log.Printf("error enviando bundle %s: %v\n", jobID, err)
	}
}

// jobRetryHandler crea un job nuevo vinculado a un job que falló, para
// reintentarlo. No modifica el job original: crea uno nuevo, vinculado
// por retried_from_job_id, para dejar trazabilidad de que es un reintento.
func jobRetryHandler(w http.ResponseWriter, r *http.Request, jobID string) {
	userID, err := authenticatedUserID(r)
	if err != nil {
		http.Error(w, "autenticación requerida", http.StatusUnauthorized)
		return
	}

	ctx := r.Context()

	// Buscamos el job original, pero SOLO si es del usuario autenticado
	// (mismo patrón de aislamiento que ya usas en jobStatusHandler:
	// el JOIN con documents y el WHERE d.user_id = $2) y SOLO si está
	// en estado 'failed'. Si no cumple, devolvemos 404: no le decimos
	// al cliente si el job existe pero no es suyo, o si no está failed.
	var documentID string
	err = db.QueryRow(ctx, `
		SELECT j.document_id
		FROM jobs j
		JOIN documents d ON d.id = j.document_id
		WHERE j.id = $1 AND d.user_id = $2 AND j.status = 'failed'`, jobID, userID).Scan(&documentID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	var newJobID string
	err = db.QueryRow(ctx, `
		INSERT INTO jobs (document_id, status, retried_from_job_id)
		VALUES ($1, 'queued', $2)
		RETURNING id`, documentID, jobID).Scan(&newJobID)
	if err != nil {
		http.Error(w, "no se pudo crear el reintento", http.StatusInternalServerError)
		return
	}

	if err := jobQueue.LPush(ctx, jobQueueName, newJobID).Err(); err != nil {
		http.Error(w, "no se pudo encolar el reintento", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"job_id":              newJobID,
		"status":              "queued",
		"retried_from_job_id": jobID,
	})
}

func authenticatedUserID(r *http.Request) (string, error) {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return "", errors.New("token ausente")
	}
	return parseToken(strings.TrimPrefix(header, "Bearer "))
}

func createToken(userID string) (string, error) {
	header, err := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(map[string]any{"sub": userID, "exp": time.Now().Add(24 * time.Hour).Unix()})
	if err != nil {
		return "", err
	}
	encodedHeader := base64.RawURLEncoding.EncodeToString(header)
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	message := encodedHeader + "." + encodedPayload
	mac := hmac.New(sha256.New, authSecret)
	mac.Write([]byte(message))
	return message + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func parseToken(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", errors.New("token inválido")
	}
	mac := hmac.New(sha256.New, authSecret)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(signature, mac.Sum(nil)) {
		return "", errors.New("firma inválida")
	}
	var payload struct {
		Subject string `json:"sub"`
		Expires int64  `json:"exp"`
	}
	decoded, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || json.Unmarshal(decoded, &payload) != nil || payload.Subject == "" || payload.Expires < time.Now().Unix() {
		return "", errors.New("token expirado o inválido")
	}
	return payload.Subject, nil
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
	authSecret = []byte(os.Getenv("AUTH_SECRET"))
	if len(authSecret) < 16 {
		log.Fatal("AUTH_SECRET debe tener al menos 16 caracteres")
	}

	objectStore, err = minio.New(os.Getenv("OBJECT_STORAGE_ENDPOINT"), &minio.Options{
		Creds:  credentials.NewStaticV4(os.Getenv("OBJECT_STORAGE_ACCESS_KEY"), os.Getenv("OBJECT_STORAGE_SECRET_KEY"), ""),
		Secure: false,
	})
	if err != nil {
		log.Fatal("no se pudo crear el cliente de almacenamiento: ", err)
	}
	objectBucket = os.Getenv("OBJECT_STORAGE_BUCKET")
	if objectBucket == "" {
		log.Fatal("OBJECT_STORAGE_BUCKET no esta configurada")
	}
	bucketExists, err := objectStore.BucketExists(context.Background(), objectBucket)
	if err != nil {
		log.Fatal("no se pudo comprobar el bucket: ", err)
	}
	if !bucketExists {
		if err := objectStore.MakeBucket(context.Background(), objectBucket, minio.MakeBucketOptions{}); err != nil {
			log.Fatal("no se pudo crear el bucket: ", err)
		}
	}

	jobQueue = redis.NewClient(&redis.Options{Addr: os.Getenv("REDIS_ADDR")})
	jobQueueName = os.Getenv("JOB_QUEUE")
	if jobQueueName == "" {
		log.Fatal("JOB_QUEUE no esta configurada")
	}
	if err := jobQueue.Ping(context.Background()).Err(); err != nil {
		log.Fatal("no se pudo conectar a Redis: ", err)
	}
	defer jobQueue.Close()

	// http.HandleFunc registra qué función debe atender cada ruta.
	http.Handle("/health", withCORS(http.HandlerFunc(healthHandler)))
	http.Handle("/auth/register", withCORS(http.HandlerFunc(registerHandler)))
	http.Handle("/auth/login", withCORS(http.HandlerFunc(loginHandler)))
	http.Handle("/upload", withCORS(http.HandlerFunc(uploadHandler)))
	http.Handle("/jobs/", withCORS(http.HandlerFunc(jobsHandler)))
	http.Handle("/metrics", withCORS(http.HandlerFunc(metricsHandler)))

	log.Println("API escuchando en http://localhost:8080")

	// ListenAndServe bloquea el programa y empieza a atender peticiones.
	// El segundo argumento nil significa "usa el router por defecto"
	// (el que llenamos arriba con http.HandleFunc).
	err = http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal(err)
	}
}
