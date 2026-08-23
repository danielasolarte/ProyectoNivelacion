# MAGICA — Plataforma de conversión documental a bundles OKF

Proyecto de nivelación de Daniela Solarte y Samara Martinez para ISIS4426 Desarrollo de Soluciones Cloud.

Recibe documentos Markdown, los convierte de forma asíncrona mediante workers independientes en Go, y genera bundles de conocimiento (`index.md`, `log.md` y uno o más conceptos) siguiendo la definición operativa de Open Knowledge Format del enunciado.

## Arquitectura

```text
Frontend -> API (Go, sin estado) -> Redis -> Worker (Go)
                  |                            |
                  v                            v
              PostgreSQL                     MinIO
```

La API solo registra metadatos, guarda el original en MinIO y encola el trabajo en Redis — nunca convierte dentro de la petición HTTP. El worker consume la cola, genera y valida el bundle, y publica el resultado. Todo el estado vive en Postgres/Redis/MinIO, así que la API y el worker escalan de forma independiente.

## Requisitos

- Docker Desktop (incluye Docker Compose).
- No hace falta instalar Go localmente.

## Despliegue

El sistema se levanta de forma reproducible, desde cero, con un solo comando ejecutado en la raíz del repositorio:

```powershell
docker compose up --build -d
```

Verificar que los 6 servicios están arriba (postgres y redis, además, en `healthy`):

```powershell
docker compose ps
```

```text
api  frontend  worker  postgres  redis  minio
```

- Frontend: http://localhost:3000
- API: http://localhost:8080
- Consola de MinIO: http://localhost:9001 (usuario `minio`, contraseña `minio_dev_password`)
- Usuario admin ya creado: `admin@magica.local` / `admin12345`

Reiniciar completamente desde cero, borrando todos los datos:

```powershell
docker compose down -v
docker compose up --build -d
```

Detener sin borrar datos:

```powershell
docker compose down
```

## Configuración

Toda la configuración vive en variables de entorno dentro de `docker-compose.yml`, separada del código: ningún handler del backend tiene una credencial o un endpoint escrito directamente en el código Go — todo se lee en tiempo de ejecución con `os.Getenv(...)`.

| Variable | Servicio | Uso |
|---|---|---|
| `DATABASE_URL` | api, worker | Conexión a PostgreSQL |
| `OBJECT_STORAGE_ENDPOINT` / `_ACCESS_KEY` / `_SECRET_KEY` / `_BUCKET` | api, worker | Conexión a MinIO |
| `REDIS_ADDR`, `JOB_QUEUE` | api, worker | Dirección de Redis y nombre de la cola |
| `AUTH_SECRET` | api | Clave para firmar los tokens de sesión |
| `ADMIN_EMAIL`, `ADMIN_PASSWORD` | api | Si están presentes, crean o actualizan el usuario admin al arrancar |

Los valores en `docker-compose.yml` son de desarrollo, pensados para que el proyecto se pueda levantar sin pasos adicionales.

## Pruebas

### Desde el frontend

1. Crear cuenta o iniciar sesión en http://localhost:3000.
2. Subir un archivo `.md` y presionar `Create bundle`.
3. Ver el estado del trabajo (Queued → Processing → Completed) y descargar con `Download bundle`.
4. Revisar el historial en `Your bundles` o buscar un bundle por su `bundle_id`.
5. Con el usuario admin, ver el panel `Admin metrics`.

### Por API

```bash
# Salud
curl http://localhost:8080/health

# Registro
curl -X POST http://localhost:8080/auth/register -H "Content-Type: application/json" \
  -d "{\"email\":\"ana@example.com\",\"password\":\"password123\"}"

# Carga (responde con job_id de inmediato, sin esperar la conversión)
curl -X POST http://localhost:8080/upload -H "Authorization: Bearer <token>" -F "document=@prueba.md"

# Estado y descarga
curl http://localhost:8080/jobs/<job_id> -H "Authorization: Bearer <token>"
curl -o bundle.zip http://localhost:8080/jobs/<job_id>/download -H "Authorization: Bearer <token>"

# Reintentar o cancelar un trabajo
curl -X POST http://localhost:8080/jobs/<job_id>/retry -H "Authorization: Bearer <token>"
curl -X POST http://localhost:8080/jobs/<job_id>/cancel -H "Authorization: Bearer <token>"

# Bundles del usuario y métricas
curl http://localhost:8080/bundles -H "Authorization: Bearer <token>"
curl http://localhost:8080/metrics
```

Referencia de endpoints:

| Método | Ruta | Requiere token |
|---|---|---|
| POST | `/auth/register`, `/auth/login` | No |
| POST | `/upload` | Sí |
| GET | `/jobs/{id}`, `/jobs/{id}/download` | Sí (dueño) |
| POST | `/jobs/{id}/retry`, `/jobs/{id}/cancel` | Sí (dueño) |
| GET | `/bundles`, `/bundles/{id}`, `/bundles/{id}/download` | Sí (dueño) |
| GET | `/metrics` | No |
| GET | `/admin/metrics` | Sí (admin) |

Un usuario que intenta consultar o descargar un `job_id` o `bundle_id` ajeno recibe `404`; sin token, `401`.

### Tests automatizados

```powershell
docker run --rm -v "${PWD}:/app" -w /app/worker golang:1.24-alpine go test ./... -v
```

9 tests cubren: bundle válido, bundle incompleto (falta `index.md`, falta `log.md`, sin conceptos, enlace roto en el índice), segmentación de un documento estructurado, documento breve sin encabezados (`valid`, sin advertencias), documento sin contenido legible (`valid_with_warnings`) y documento con encabezados (`valid`).

## Estructura del bundle

```text
Documento breve           Documento estructurado
bundle/                   bundle/
|-- index.md              |-- index.md
|-- log.md                |-- log.md
`-- documento.md          |-- capitulo-01.md
                           |-- capitulo-02.md
                           `-- capitulo-03.md
```

`index.md` enlaza los conceptos en orden; `log.md` registra el documento original, las unidades detectadas y el resultado de la validación (`valid`, `valid_with_warnings` o `invalid`).

## Alcance opcional implementado

Además del alcance mínimo: reintento idempotente de trabajos fallidos (vinculado al original vía `retried_from_job_id`), cancelación de trabajos en curso, métricas y observabilidad (`/metrics` y `/admin/metrics`), descarga por streaming sin materializar el bundle completo en memoria, panel de administrador, listado/búsqueda de bundles por ID y clasificación de validación en tres niveles.

No implementado por decisión de alcance: soporte de más formatos de entrada, extracción de recursos a `assets/`, cálculo de conformidad OKF por separado.

## Pendiente

- Grabar y entregar el video de sustentación.
