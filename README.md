# MAGICA - Plataforma de conversion documental a bundles OKF

Proyecto de nivelacion de Daniela Solarte y Samara Martinez para ISIS4426 Desarrollo de Soluciones Cloud.

MAGICA es una plataforma web multiusuario que recibe documentos Markdown, los procesa de forma asincrona mediante workers y genera bundles de conocimiento compatibles con la definicion operativa de Open Knowledge Format usada en el enunciado.

## Estado actual

Implementado:

- API HTTP en Go.
- Worker independiente en Go.
- Frontend web MAGICA en HTML, CSS y JavaScript.
- PostgreSQL 16 para usuarios, documentos, trabajos y bundles.
- Redis como cola de trabajos.
- MinIO como almacenamiento de objetos para originales y bundles.
- Docker Compose para levantar todo el sistema.
- Registro y login de usuarios.
- Tokens firmados con vigencia de 24 horas.
- Aislamiento por propietario en consulta, descarga, reintento y cancelacion.
- Endpoint `GET /health`.
- Endpoint `POST /upload`.
- Endpoint `GET /jobs/{job_id}`.
- Endpoint `GET /jobs/{job_id}/download`.
- Endpoint `POST /jobs/{job_id}/retry`.
- Endpoint `POST /jobs/{job_id}/cancel`.
- Endpoint `GET /bundles`.
- Endpoint `GET /bundles/{bundle_id}`.
- Endpoint `GET /bundles/{bundle_id}/download`.
- Endpoint `GET /metrics`.
- Endpoint protegido `GET /admin/metrics`.
- Carga de documentos Markdown o texto plano.
- Respuesta inmediata con `job_id` y estado `queued`.
- Procesamiento asincrono fuera de la peticion HTTP.
- Segmentacion Markdown por encabezados.
- Bundle ZIP con `index.md`, `log.md` y uno o mas conceptos.
- Identificador unico por bundle publicado (`bundle_id`).
- Validacion minima de estructura y enlaces del indice antes de publicar.
- Clasificacion de validacion como `valid`, `valid_with_warnings` o `invalid`.
- Reintentos automaticos hasta `max_attempts = 3`.
- Reintento manual de trabajos fallidos, vinculado con `retried_from_job_id`.
- Cancelacion de trabajos en `queued` o `processing`.
- Idempotencia frente a reentregas del mismo `job_id`.
- Descarga por streaming desde MinIO hacia el cliente.
- Metricas agregadas: trabajos por estado, bundles por validacion, tiempo promedio y trabajos reintentados.
- Usuario administrador con panel de metricas visuales.

No implementado por decision de alcance:

- Soporte para PDF, DOCX, EPUB u otros formatos ricos.
- Extraccion de imagenes a `assets/`.

## Arquitectura

```text
Frontend MAGICA -> API Go -> Redis -> Worker Go
                     |                 |
                     v                 v
                 PostgreSQL          MinIO
```

Reglas importantes:

- La API no guarda archivos ni trabajos en memoria o disco local del contenedor.
- La API registra metadatos, guarda el original en MinIO, encola el trabajo en Redis y retorna `202 Accepted`.
- El worker consume la cola, descarga el original desde MinIO, genera el ZIP, valida el bundle y publica el resultado.
- Los datos sobreviven reinicios gracias a los volumenes de PostgreSQL y MinIO.
- Cada bundle publicado tiene un `bundle_id` propio para consulta y descarga posterior.

## Estructura del bundle

Documento breve sin divisiones:

```text
bundle/
|-- index.md
|-- log.md
`-- documento.md
```

Documento con varias secciones:

```text
bundle/
|-- index.md
|-- log.md
|-- capitulo-01.md
|-- capitulo-02.md
`-- capitulo-03.md
```

`index.md` enumera y enlaza los conceptos en orden. `log.md` registra documento original, unidades detectadas, transformacion aplicada, validacion y advertencias.

## Clasificacion de validacion

- `valid`: estructura minima correcta, enlaces resueltos y documento con estructura Markdown detectable.
- `valid_with_warnings`: bundle descargable, pero con advertencias. Ejemplo: documento sin encabezados Markdown; se genera un unico concepto y se deja advertencia en `log.md`.
- `invalid`: falta `index.md`, falta `log.md`, no hay conceptos Markdown o el indice enlaza archivos inexistentes. En este caso el trabajo queda `failed` y no se habilita la descarga.

## Frontend

La interfaz usa la marca **MAGICA** con una direccion editorial, sobria y funcional:

- Wordmark MAGICA en serif editorial.
- Paleta cream, dark brown, burgundy y neutrales calidos.
- Formularios centrados y legibles.
- Flujo completo desde el navegador: registro, login, carga, seguimiento, cancelacion, reintento, descarga y nuevo documento.
- Listado de bundles del usuario autenticado.
- Busqueda y consulta de bundles por `bundle_id`.
- Panel de administrador con metricas visuales para usuarios admin.
- Sin glows, gradientes, sparkles decorativos ni look de dashboard generico.

## Requisitos

- Docker Desktop.
- Docker Compose.

No es necesario instalar Go localmente para ejecutar la aplicacion.

## Levantar el sistema

Desde la raiz del repositorio:

```powershell
docker compose up --build -d
```

Servicios esperados:

```powershell
docker compose ps
```

Debes ver:

```text
api
frontend
worker
postgres
redis
minio
```

Abrir la app:

```text
http://localhost:3000
```

API:

```text
http://localhost:8080
```

MinIO:

```text
http://localhost:9001
Usuario: minio
Password: minio_dev_password
```

Usuario administrador creado por defecto:

```text
Email: admin@magica.local
Password: admin12345
```

## Flujo desde el frontend

1. Crear cuenta o iniciar sesion.
2. Seleccionar un archivo `.md`.
3. Presionar `Create bundle`.
4. Ver el estado del trabajo.
5. Descargar con `Download bundle`.
6. Usar `New document` para iniciar otra conversion.
7. Revisar el historial en `Your bundles`.
8. Consultar un bundle especifico por `bundle_id`.

Si se inicia sesion como administrador, el frontend muestra el panel `Admin metrics` con graficas de trabajos por estado y bundles por validacion.

## Pruebas por API

Healthcheck:

```bash
curl http://localhost:8080/health
```

Registro:

```bash
curl -X POST http://localhost:8080/auth/register \
  -H "Content-Type: application/json" \
  -d "{\"email\":\"ana@example.com\",\"password\":\"password123\"}"
```

Login:

```bash
curl -X POST http://localhost:8080/auth/login \
  -H "Content-Type: application/json" \
  -d "{\"email\":\"ana@example.com\",\"password\":\"password123\"}"
```

Carga:

```bash
curl -X POST http://localhost:8080/upload \
  -H "Authorization: Bearer <token>" \
  -F "document=@prueba.md"
```

Respuesta esperada:

```json
{"job_id":"<uuid>","status":"queued"}
```

Estado:

```bash
curl http://localhost:8080/jobs/<job_id> \
  -H "Authorization: Bearer <token>"
```

Respuesta esperada:

```json
{
  "job_id": "<uuid>",
  "original_name": "prueba.md",
  "status": "completed",
  "error": null,
  "validation_status": "valid",
  "bundle_id": "<uuid>"
}
```

Descarga:

```bash
curl -o bundle.zip http://localhost:8080/jobs/<job_id>/download \
  -H "Authorization: Bearer <token>"
```

Listar bundles del usuario autenticado:

```bash
curl http://localhost:8080/bundles \
  -H "Authorization: Bearer <token>"
```

Consultar un bundle por ID:

```bash
curl http://localhost:8080/bundles/<bundle_id> \
  -H "Authorization: Bearer <token>"
```

Descargar un bundle por ID:

```bash
curl -o bundle.zip http://localhost:8080/bundles/<bundle_id>/download \
  -H "Authorization: Bearer <token>"
```

Metricas:

```bash
curl http://localhost:8080/metrics
```

Metricas de administrador:

```bash
curl http://localhost:8080/admin/metrics \
  -H "Authorization: Bearer <admin_token>"
```

Ejemplo:

```json
{
  "jobs_by_status": {"completed": 4, "queued": 0, "processing": 0, "failed": 0, "cancelled": 0},
  "bundles_by_validation": {"valid": 3, "valid_with_warnings": 1},
  "avg_processing_seconds": 0.12,
  "jobs_retried": 1
}
```

## Condiciones verificables del enunciado

- Asincronia efectiva: `POST /upload` responde `202 Accepted` con `job_id`; el worker continua en segundo plano.
- Documento breve: un archivo sin divisiones produce `index.md`, `log.md` y `documento.md`.
- Documento estructurado: headings Markdown producen `capitulo-01.md`, `capitulo-02.md`, etc.
- Bundle incompleto: `validateBundle` rechaza ZIPs sin `index.md`, sin `log.md`, sin conceptos o con enlaces rotos.
- Aislamiento: las consultas filtran por `d.user_id = usuario autenticado`; un usuario no puede consultar ni descargar jobs ajenos.
- Consulta por bundle: cada bundle publicado tiene `bundle_id` y puede consultarse o descargarse sin depender visualmente del `job_id`.
- Administracion: solo usuarios admin pueden consumir `GET /admin/metrics`; usuarios normales reciben `403 Forbidden`.
- Ausencia de duplicados: el worker reclama jobs solo en `queued` y la tabla `bundles` tiene `UNIQUE(job_id)` con `ON CONFLICT DO NOTHING`.

## Tests

Como Go no es requisito local, los tests pueden ejecutarse con Docker:

```powershell
docker run --rm -v "${PWD}:/app" -w /app/worker golang:1.24-alpine go test ./...
```

Cubren:

- Bundle valido.
- Falta de `index.md`.
- Falta de `log.md`.
- Bundle sin conceptos.
- Enlaces rotos en `index.md`.
- Segmentacion de documento estructurado.
- Documento sin encabezados como `valid_with_warnings`.
- Documento con encabezados como `valid`.

## Comandos utiles

Logs:

```powershell
docker compose logs api --tail 50
docker compose logs worker --tail 50
```

Consultar trabajos:

```powershell
docker compose exec postgres psql -U okf -d okf -c "SELECT d.original_name, j.id, j.status, b.validation_status FROM documents d JOIN jobs j ON j.document_id = d.id LEFT JOIN bundles b ON b.job_id = j.id ORDER BY j.created_at DESC;"
```

Detener:

```powershell
docker compose down
```

Reiniciar desde cero, borrando datos:

```powershell
docker compose down -v
docker compose up --build -d
```

## Sustentacion

El video es obligatorio segun el enunciado. Se recomienda mostrar:

1. `docker compose up --build -d`.
2. Diagrama de servicios y explicacion de API sin estado.
3. Carga desde frontend con respuesta inmediata.
4. Seguimiento del estado y descarga del ZIP.
5. Apertura de `index.md`, `log.md` y conceptos.
6. Caso con varias secciones.
7. Caso sin encabezados que queda `valid_with_warnings`.
8. Intento de acceso a un job de otro usuario.
9. Consulta de bundles por `bundle_id`.
10. Reintento, cancelacion y metricas.
11. Login con admin y visualizacion del panel `Admin metrics`.
12. Validacion negativa con tests del worker.

## Pendiente

- Soporte para PDF, DOCX, EPUB u otros formatos adicionales.
- Extraccion de recursos a `assets/`.
- Video de sustentacion.
