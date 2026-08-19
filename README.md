# Plataforma de conversion documental a bundles OKF

Proyecto de nivelacion de Daniela Solarte y Samara Martinez para la asignatura ISIS4426 Desarrollo de Soluciones Cloud.

La meta del proyecto es construir una plataforma web multiusuario que reciba documentos, los procese de forma asincrona mediante workers y genere bundles de conocimiento compatibles con Open Knowledge Format (OKF).

## Estado actual

En esta etapa ya estan implementados:

- API HTTP en Go.
- PostgreSQL 16 ejecutandose mediante Docker Compose.
- Volumen persistente para los datos de PostgreSQL.
- Esquema inicial para usuarios, documentos, trabajos y bundles.
- Conexion de la API con PostgreSQL mediante `pgx`.
- Endpoint `GET /health`.
- Endpoint `POST /upload`.
- Registro de los metadatos del documento en PostgreSQL.
- Almacenamiento del archivo original en MinIO.
- Registro de la ruta del objeto en `documents.storage_key`.
- Creacion de un trabajo con estado `queued`.
- Generacion de identificadores UUID desde PostgreSQL.

Actualmente se usa el usuario demo `demo@example.com`. La autenticacion real, el procesamiento asincrono y la generacion del bundle aun no estan implementados.

## Arquitectura planificada

- **Backend:** API y workers independientes implementados en Go.
- **Cola de mensajes:** Redis, pendiente de integrar.
- **Base de datos:** PostgreSQL para metadatos de usuarios, documentos, trabajos y bundles.
- **Almacenamiento de objetos:** MinIO para documentos originales y bundles.
- **Despliegue:** Docker Compose.

El flujo final esperado es:

```text
Frontend -> API Go -> Redis -> Worker Go
			  |                 |
			  v                 v
		  PostgreSQL          MinIO
```

La API debe responder rapidamente despues de registrar y encolar el trabajo. La conversion no debe ejecutarse dentro de la peticion HTTP.

## Estructura actual

```text
.
├── api/
│   ├── Dockerfile
│   ├── go.mod
│   ├── go.sum
│   └── main.go
├── db/
│   └── init/
│       └── 001_schema.sql
├── docker-compose.yml
├── prueba.md
└── README.md
```

## Requisitos

- Docker Desktop con Docker Compose.

No es necesario instalar Go localmente para ejecutar la aplicacion, porque la API se compila dentro del Dockerfile.

## Ejecucion

Desde la raiz del proyecto:

```bash
docker compose up --build
```

La API queda disponible en `http://localhost:8080` y PostgreSQL en el puerto `5432`.

Para ejecutar los servicios en segundo plano:

```bash
docker compose up --build -d
```

Para revisar el estado:

```bash
docker compose ps
```

PostgreSQL debe aparecer como `healthy`.

## Pruebas actuales

### Healthcheck de la API

```bash
curl http://localhost:8080/health
```

Respuesta esperada:

```json
{"status":"ok","service":"api"}
```

### Cargar un documento

El campo multipart debe llamarse `document`:

```bash
curl -X POST http://localhost:8080/upload -F "document=@prueba.md"
```

Respuesta esperada:

```json
{"job_id":"<uuid>","status":"queued"}
```

La API registra el usuario demo, el documento y el trabajo en una transaccion de PostgreSQL, y guarda el archivo original en MinIO. En esta etapa aun no inicia su conversion.

### Consultar las tablas

```bash
docker compose exec postgres psql -U okf -d okf -c "\\dt"
```

Consultar los trabajos creados:

```bash
docker compose exec postgres psql -U okf -d okf -c "SELECT d.original_name, j.id, j.status FROM documents d JOIN jobs j ON j.document_id = d.id ORDER BY j.created_at DESC;"
```

## Base de datos

La configuracion local de PostgreSQL es:

```text
Base de datos: okf
Usuario: okf
Contrasena: okf_dev_password
Puerto: 5432
```

El esquema inicial crea estas tablas:

- `users`: usuarios de la plataforma.
- `documents`: metadatos de los documentos y su propietario.
- `jobs`: trabajos de conversion y sus estados.
- `bundles`: resultado y estado de validacion del bundle.

Los datos se conservan en el volumen Docker `postgres_data`. Los scripts de `db/init` se ejecutan automaticamente cuando se crea la base por primera vez.

## Almacenamiento de objetos

MinIO esta disponible en:

```text
API S3: http://localhost:9000
Consola web: http://localhost:9001
Usuario: minio
Contrasena: minio_dev_password
Bucket: documents
```

Los objetos se guardan con una ruta similar a:

```text
users/<user-id>/documents/<document-id>/prueba.md
```

El volumen Docker `minio_data` conserva los archivos aunque los contenedores se reinicien.

## Pendiente

1. Implementar registro, autenticacion y autorizacion por propietario.
2. Agregar Redis y publicar cada trabajo desde la API.
3. Crear el worker independiente en Go.
4. Leer y segmentar documentos Markdown.
5. Generar `index.md`, `log.md` y los documentos de concepto en MinIO.
6. Validar la estructura y los enlaces del bundle.
7. Agregar consulta de estado y descarga del bundle.
8. Implementar idempotencia y reintentos.
9. Crear el frontend y las pruebas de aislamiento multiusuario.

## Detener los servicios

```bash
docker compose down
```

Para eliminar tambien los datos persistidos de PostgreSQL:

```bash
docker compose down -v
```
