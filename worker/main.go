package main

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/redis/go-redis/v9"
)

type worker struct {
	db          *pgxpool.Pool
	queue       *redis.Client
	objectStore *minio.Client
	bucket      string
	queueName   string
}

type bundleBuild struct {
	data             []byte
	validationStatus string
	warnings         []string
}

func main() {
	ctx := context.Background()
	databaseURL := os.Getenv("DATABASE_URL")
	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		log.Fatal("no se pudo crear el pool de PostgreSQL: ", err)
	}
	defer db.Close()
	if err := db.Ping(ctx); err != nil {
		log.Fatal("no se pudo conectar a PostgreSQL: ", err)
	}

	objectStore, err := minio.New(os.Getenv("OBJECT_STORAGE_ENDPOINT"), &minio.Options{
		Creds:  credentials.NewStaticV4(os.Getenv("OBJECT_STORAGE_ACCESS_KEY"), os.Getenv("OBJECT_STORAGE_SECRET_KEY"), ""),
		Secure: false,
	})
	if err != nil {
		log.Fatal("no se pudo crear el cliente de MinIO: ", err)
	}

	jobWorker := &worker{
		db:          db,
		queue:       redis.NewClient(&redis.Options{Addr: os.Getenv("REDIS_ADDR")}),
		objectStore: objectStore,
		bucket:      os.Getenv("OBJECT_STORAGE_BUCKET"),
		queueName:   os.Getenv("JOB_QUEUE"),
	}
	defer jobWorker.queue.Close()
	if err := jobWorker.queue.Ping(ctx).Err(); err != nil {
		log.Fatal("no se pudo conectar a Redis: ", err)
	}

	log.Println("worker listo, esperando trabajos")
	for {
		message, err := jobWorker.queue.BRPop(ctx, 0, jobWorker.queueName).Result()
		if err != nil {
			log.Println("error leyendo la cola: ", err)
			continue
		}
		if err := jobWorker.process(ctx, message[1]); err != nil {
			log.Printf("trabajo %s fallido: %v\n", message[1], err)
		}
	}
}

func (jobWorker *worker) process(ctx context.Context, jobID string) error {
	result, err := jobWorker.db.Exec(ctx, `
		UPDATE jobs
		SET status = 'processing',
			attempts = attempts + 1,
			processing_started_at = COALESCE(processing_started_at, NOW()),
			updated_at = NOW()
		WHERE id = $1 AND status = 'queued'`, jobID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return nil
	}
	var attempts int
	var maxAttempts int
	err = jobWorker.db.QueryRow(ctx, `SELECT attempts, max_attempts FROM jobs WHERE id = $1`, jobID).Scan(&attempts, &maxAttempts)
	if err != nil {
		return err
	}
	log.Printf("procesando trabajo %s, intento %d/%d\n", jobID, attempts, maxAttempts)

	var storageKey string
	var originalName string
	err = jobWorker.db.QueryRow(ctx, `
		SELECT d.storage_key, d.original_name
		FROM jobs j JOIN documents d ON d.id = j.document_id
		WHERE j.id = $1`, jobID).Scan(&storageKey, &originalName)
	if err != nil {
		return jobWorker.fail(ctx, jobID, err)
	}

	object, err := jobWorker.objectStore.GetObject(ctx, jobWorker.bucket, storageKey, minio.GetObjectOptions{})
	if err != nil {
		return jobWorker.fail(ctx, jobID, err)
	}
	defer object.Close()
	source, err := io.ReadAll(object)
	if err != nil {
		return jobWorker.fail(ctx, jobID, err)
	}

	bundle, err := buildBundle(originalName, source)
	if err != nil {
		return jobWorker.fail(ctx, jobID, err)
	}
	if err := validateBundle(bundle.data); err != nil {
		return jobWorker.fail(ctx, jobID, err)
	}
	bundleKey := path.Join("bundles", jobID, "bundle.zip")
	_, err = jobWorker.objectStore.PutObject(ctx, jobWorker.bucket, bundleKey, bytes.NewReader(bundle.data), int64(len(bundle.data)), minio.PutObjectOptions{ContentType: "application/zip"})
	if err != nil {
		return jobWorker.fail(ctx, jobID, err)
	}

	_, err = jobWorker.db.Exec(ctx, `
		INSERT INTO bundles (job_id, storage_key, validation_status)
		VALUES ($1, $2, $3)
		ON CONFLICT (job_id) DO NOTHING`, jobID, bundleKey, bundle.validationStatus)
	if err != nil {
		return jobWorker.fail(ctx, jobID, err)
	}
	
	result, err = jobWorker.db.Exec(ctx, `
	UPDATE jobs SET status = 'completed', completed_at = NOW(), updated_at = NOW()
	WHERE id = $1 AND status = 'processing'`, jobID)

	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		log.Printf("trabajo %s no se marcó completed (probablemente fue cancelado mientras se procesaba)\n", jobID)
	}
	return nil
}

func (jobWorker *worker) fail(ctx context.Context, jobID string, cause error) error {
	var nextStatus string
	updateErr := jobWorker.db.QueryRow(ctx, `
		UPDATE jobs
		SET status = CASE WHEN attempts < max_attempts THEN 'queued' ELSE 'failed' END,
			error_message = $2,
			completed_at = CASE WHEN attempts >= max_attempts THEN NOW() ELSE completed_at END,
			updated_at = NOW()
		WHERE id = $1
		RETURNING status`, jobID, cause.Error()).Scan(&nextStatus)
	if updateErr != nil {
		return fmt.Errorf("%v; además no se pudo marcar como fallido: %w", cause, updateErr)
	}
	if nextStatus == "queued" {
		if err := jobWorker.queue.LPush(ctx, jobWorker.queueName, jobID).Err(); err != nil {
			return fmt.Errorf("%v; además no se pudo reencolar: %w", cause, err)
		}
		log.Printf("trabajo %s reencolado para otro intento\n", jobID)
	} else {
		log.Printf("trabajo %s agotó sus reintentos\n", jobID)
	}
	return cause
}

func buildBundle(originalName string, source []byte) (bundleBuild, error) {
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	concepts := splitMarkdown(source)
	warnings := bundleWarnings(source, concepts)
	validationStatus := "valid"
	if len(warnings) > 0 {
		validationStatus = "valid_with_warnings"
	}
	var index strings.Builder
	index.WriteString("# Bundle\n\n")
	for position, concept := range concepts {
		conceptName := "documento.md"
		if len(concepts) > 1 {
			conceptName = fmt.Sprintf("capitulo-%02d.md", position+1)
		}
		index.WriteString(fmt.Sprintf("- [Unidad %d](%s)\n", position+1, conceptName))
		if err := writeZipFile(archive, conceptName, concept); err != nil {
			return bundleBuild{}, err
		}
	}
	if err := writeZipFile(archive, "index.md", []byte(index.String())); err != nil {
		return bundleBuild{}, err
	}
	logContent := buildLog(originalName, len(concepts), validationStatus, warnings)
	if err := writeZipFile(archive, "log.md", []byte(logContent)); err != nil {
		return bundleBuild{}, err
	}
	if err := archive.Close(); err != nil {
		return bundleBuild{}, err
	}
	return bundleBuild{data: output.Bytes(), validationStatus: validationStatus, warnings: warnings}, nil
}

func buildLog(originalName string, conceptCount int, validationStatus string, warnings []string) string {
	var logContent strings.Builder
	logContent.WriteString("# Conversion log\n\n")
	logContent.WriteString(fmt.Sprintf("- Documento original: `%s`\n", originalName))
	logContent.WriteString(fmt.Sprintf("- Unidades detectadas: %d\n", conceptCount))
	logContent.WriteString("- Transformacion: segmentacion Markdown por encabezados\n")
	logContent.WriteString("- Validacion: estructura minima y enlaces del indice verificados\n")
	logContent.WriteString(fmt.Sprintf("- Resultado de validacion: %s\n", validationStatus))
	if len(warnings) > 0 {
		logContent.WriteString("\n## Advertencias\n\n")
		for _, warning := range warnings {
			logContent.WriteString(fmt.Sprintf("- %s\n", warning))
		}
	}
	return logContent.String()
}

func bundleWarnings(source []byte, concepts [][]byte) []string {
	text := strings.TrimSpace(string(source))
	if text == "" {
		return []string{"El documento original no contiene texto legible."}
	}
	if len(concepts) == 1 && !hasMarkdownHeading(source) {
		return []string{"No se detectaron encabezados Markdown; se genero un unico concepto a partir del documento completo."}
	}
	return nil
}

func hasMarkdownHeading(source []byte) bool {
	text := strings.ReplaceAll(string(source), "\r\n", "\n")
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") && (len(trimmed) == 1 || trimmed[1] == ' ' || trimmed[1] == '#') {
			return true
		}
	}
	return false
}

func splitMarkdown(source []byte) [][]byte {
	text := strings.ReplaceAll(string(source), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	var concepts [][]string
	current := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		isHeading := strings.HasPrefix(trimmed, "#") && (len(trimmed) == 1 || trimmed[1] == ' ' || trimmed[1] == '#')
		if isHeading && len(current) > 0 {
			concepts = append(concepts, current)
			current = make([]string, 0, len(lines))
		}
		current = append(current, line)
	}
	if len(current) > 0 {
		concepts = append(concepts, current)
	}
	if len(concepts) == 0 {
		return [][]byte{source}
	}
	result := make([][]byte, 0, len(concepts))
	for _, concept := range concepts {
		result = append(result, []byte(strings.Join(concept, "\n")))
	}
	return result
}

func writeZipFile(archive *zip.Writer, name string, content []byte) error {
	entry, err := archive.Create(name)
	if err != nil {
		return err
	}
	_, err = entry.Write(content)
	return err
}

func validateBundle(bundle []byte) error {
	archive, err := zip.NewReader(bytes.NewReader(bundle), int64(len(bundle)))
	if err != nil {
		return fmt.Errorf("bundle inválido: no es un ZIP válido")
	}

	files := make(map[string][]byte, len(archive.File))
	for _, file := range archive.File {
		reader, err := file.Open()
		if err != nil {
			return fmt.Errorf("bundle inválido: no se pudo leer %s", file.Name)
		}
		content, readErr := io.ReadAll(reader)
		reader.Close()
		if readErr != nil {
			return fmt.Errorf("bundle inválido: no se pudo leer %s", file.Name)
		}
		files[file.Name] = content
	}
	if _, ok := files["index.md"]; !ok {
		return fmt.Errorf("bundle inválido: falta index.md")
	}
	if _, ok := files["log.md"]; !ok {
		return fmt.Errorf("bundle inválido: falta log.md")
	}
	conceptCount := 0
	for name := range files {
		if strings.HasSuffix(name, ".md") && name != "index.md" && name != "log.md" {
			conceptCount++
		}
	}
	if conceptCount == 0 {
		return fmt.Errorf("bundle inválido: no contiene conceptos Markdown")
	}

	for _, line := range strings.Split(string(files["index.md"]), "\n") {
		start := strings.Index(line, "](")
		if start == -1 {
			continue
		}
		start += 2
		end := strings.Index(line[start:], ")")
		if end == -1 {
			return fmt.Errorf("bundle inválido: enlace sin cierre en index.md")
		}
		target := line[start : start+end]
		if _, ok := files[target]; !ok {
			return fmt.Errorf("bundle inválido: el enlace %s no existe", target)
		}
	}
	return nil
}
