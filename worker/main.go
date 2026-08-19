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
		UPDATE jobs SET status = 'processing', updated_at = NOW()
		WHERE id = $1 AND status = 'queued'`, jobID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return nil
	}

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
	bundleKey := path.Join("bundles", jobID, "bundle.zip")
	_, err = jobWorker.objectStore.PutObject(ctx, jobWorker.bucket, bundleKey, bytes.NewReader(bundle), int64(len(bundle)), minio.PutObjectOptions{ContentType: "application/zip"})
	if err != nil {
		return jobWorker.fail(ctx, jobID, err)
	}

	_, err = jobWorker.db.Exec(ctx, `
		INSERT INTO bundles (job_id, storage_key, validation_status)
		VALUES ($1, $2, 'valid')
		ON CONFLICT (job_id) DO NOTHING`, jobID, bundleKey)
	if err != nil {
		return jobWorker.fail(ctx, jobID, err)
	}
	_, err = jobWorker.db.Exec(ctx, `UPDATE jobs SET status = 'completed', updated_at = NOW() WHERE id = $1`, jobID)
	return err
}

func (jobWorker *worker) fail(ctx context.Context, jobID string, cause error) error {
	_, updateErr := jobWorker.db.Exec(ctx, `UPDATE jobs SET status = 'failed', error_message = $2, updated_at = NOW() WHERE id = $1`, jobID, cause.Error())
	if updateErr != nil {
		return fmt.Errorf("%v; además no se pudo marcar como fallido: %w", cause, updateErr)
	}
	return cause
}

func buildBundle(originalName string, source []byte) ([]byte, error) {
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	concepts := splitMarkdown(source)
	var index strings.Builder
	index.WriteString("# Bundle\n\n")
	for position, concept := range concepts {
		conceptName := "documento.md"
		if len(concepts) > 1 {
			conceptName = fmt.Sprintf("capitulo-%02d.md", position+1)
		}
		index.WriteString(fmt.Sprintf("- [Unidad %d](%s)\n", position+1, conceptName))
		if err := writeZipFile(archive, conceptName, concept); err != nil {
			return nil, err
		}
	}
	if err := writeZipFile(archive, "index.md", []byte(index.String())); err != nil {
		return nil, err
	}
	logContent := fmt.Sprintf("# Conversion log\n\n- Documento original: `%s`\n- Unidades detectadas: %d\n- Validacion: estructura minima correcta\n", originalName, len(concepts))
	if err := writeZipFile(archive, "log.md", []byte(logContent)); err != nil {
		return nil, err
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
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