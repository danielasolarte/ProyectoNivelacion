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
- Endpoint `GET /jobs/{job_id}` para consultar el estado.
- Endpoint `GET /jobs/{job_id}/download` para descargar el bundle.
- Registro de los metadatos del documento en PostgreSQL.
- Almacenamiento del archivo original en MinIO.
- Registro de la ruta del objeto en `documents.storage_key`.
- Creacion de un trabajo con estado `queued`.
- Generacion de identificadores UUID desde PostgreSQL.
- Redis como cola de trabajos.
- Worker independiente en Go.
- Procesamiento asincrono del trabajo.
- Segmentacion de documentos Markdown por encabezados.
- Generacion de un bundle ZIP con `index.md`, `log.md` y `documento.md`.
- Validacion de la estructura minima y de los enlaces de `index.md` antes de publicar.
- Registro del bundle en MinIO y actualizacion del trabajo a `completed`.
- Registro y login de usuarios.
- Tokens firmados para autorizar operaciones por propietario.
- Reintentos de trabajos fallidos hasta `max_attempts = 3`.
- Idempotencia frente a reentregas del mismo `job_id`.
- Frontend web para registro, login, carga, seguimiento y descarga.

La autenticacion usa tokens con vigencia de 24 horas. Un bundle que no cumpla la estructura o tenga enlaces rotos queda en estado `failed` y no se registra como publicado.

## Arquitectura planificada

- **Backend:** API y workers independientes implementados en Go.
- **Cola de mensajes:** Redis.
- **Base de datos:** PostgreSQL para metadatos de usuarios, documentos, trabajos y bundles.
- **Almacenamiento de objetos:** MinIO para documentos originales y bundles.
- **Worker:** servicio independiente en Go que consume trabajos desde Redis.
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
├── frontend/
│   ├── Dockerfile
│   ├── index.html
│   ├── styles.css
│   └── app.js
├── worker/
│   ├── Dockerfile
│   ├── go.mod
│   ├── go.sum
│   └── main.go
├── db/
│   └── init/
│       ├── 001_schema.sql
│       ├── 002_auth.sql
│       └── 003_job_retries.sql
├── docker-compose.yml
├── prueba.md
└── README.md
```

## Requisitos

- Docker Desktop con Docker Compose.

No es necesario instalar Go localmente para ejecutar la aplicacion, porque la API se compila dentro del Dockerfile.

## Guia paso a paso

### 1. Iniciar Docker Desktop

Abre Docker Desktop y espera a que el motor indique que esta activo. La aplicacion necesita Docker Compose para construir y ejecutar todos los servicios.

### 2. Abrir la carpeta del proyecto

En PowerShell, ubicate en la raiz del repositorio:

```powershell
Set-Location "C:\ruta\al\ProyectoNivelacion"
```

La carpeta correcta contiene `docker-compose.yml`, `api`, `worker`, `frontend` y `db`.

### 3. Construir y levantar la aplicacion

Ejecuta el siguiente comando la primera vez y cada vez que cambie el codigo:

```powershell
docker compose up --build -d
```

Este comando construye y levanta:

- Frontend en Nginx.
- API en Go.
- Worker en Go.
- PostgreSQL.
- Redis.
- MinIO.

### 4. Verificar los servicios

```powershell
docker compose ps
```

Debes ver estos servicios activos:

```text
api
frontend
minio
postgres
redis
worker
```

PostgreSQL y Redis deben mostrar el estado `healthy`.

Si un servicio no inicia, revisa sus logs:

```powershell
docker compose logs api
docker compose logs worker
docker compose logs postgres
```

### 5. Abrir el frontend

Abre esta direccion en el navegador:

```text
http://localhost:3000
```

El frontend muestra el formulario de registro y login.

### 6. Crear una cuenta

Desde el frontend:

1. Escribe un email.
2. Escribe una contraseña de al menos 8 caracteres.
3. Presiona `Make an account`.

Al terminar, la aplicacion guarda el token de sesión en el navegador y muestra el espacio de carga.

### 7. Cargar un documento

1. Presiona `Choose a tiny Markdown file`.
2. Selecciona `prueba.md` o cualquier archivo `.md`.
3. Presiona `Make my bundle`.

La API responde inmediatamente con un trabajo en estado `queued`. Luego el worker:

1. Lee el archivo desde MinIO.
2. Segmenta el Markdown por encabezados.
3. Genera el bundle.
4. Valida `index.md`, `log.md` y sus enlaces.
5. Guarda el ZIP en MinIO.
6. Cambia el trabajo a `completed`.

El frontend consulta el estado automáticamente. Cuando termina, aparece `Take my cute bundle.zip`.

### 8. Descargar el bundle

Presiona `Take my cute bundle.zip`. El navegador descargara un archivo llamado `bundle.zip`.

El ZIP debe contener:

```text
index.md
log.md
capitulo-01.md
capitulo-02.md
...
```

Un documento sin encabezados genera un unico `documento.md`.

### 9. Detener la aplicacion

Para detener los contenedores sin eliminar los datos:

```powershell
docker compose down
```

Para volver a iniciar sin reconstruir:

```powershell
docker compose up -d
```

### 10. Reiniciar desde cero

Este comando elimina tambien los volumenes de PostgreSQL y MinIO. Los documentos y usuarios guardados se perderan:

```powershell
docker compose down -v
docker compose up --build -d
```

## Direcciones de los servicios

```text
Frontend:      http://localhost:3000
API:           http://localhost:8080
API health:    http://localhost:8080/health
MinIO API:     http://localhost:9000
MinIO consola: http://localhost:9001
PostgreSQL:    localhost:5432
Redis:         localhost:6379
```

## Pruebas actuales

### Healthcheck de la API

```bash
curl http://localhost:8080/health
```

Respuesta esperada:

```json
{"status":"ok","service":"api"}
```

### Registrar un usuario

```bash
curl -X POST http://localhost:8080/auth/register -H "Content-Type: application/json" -d "{\"email\":\"ana@example.com\",\"password\":\"password123\"}"
```

La respuesta incluye un `token`. Debe enviarse como `Bearer` en las operaciones protegidas.

### Iniciar sesión

```bash
curl -X POST http://localhost:8080/auth/login -H "Content-Type: application/json" -d "{\"email\":\"ana@example.com\",\"password\":\"password123\"}"
```

### Cargar un documento

El campo multipart debe llamarse `document`:

```bash
curl -X POST http://localhost:8080/upload -H "Authorization: Bearer <token>" -F "document=@prueba.md"
```

Respuesta esperada:

```json
{"job_id":"<uuid>","status":"queued"}
```

La API registra el usuario autenticado, el documento y el trabajo en una transaccion de PostgreSQL, guarda el archivo original en MinIO y publica el `job_id` en Redis. El worker consume el trabajo y genera el bundle de forma independiente.

El estado final esperado para una carga exitosa es `completed` y el bundle queda registrado en la tabla `bundles`.

Cada trabajo registra `attempts` y `max_attempts`. Si ocurre un fallo antes de publicar el bundle, el worker lo devuelve a `queued` hasta tres veces. Una reentrega de un trabajo ya procesado no crea un segundo bundle.

### Consultar el estado de un trabajo

```bash
curl http://localhost:8080/jobs/<job_id> -H "Authorization: Bearer <token>"
```

Respuesta esperada:

```json
{"job_id":"<uuid>","original_name":"prueba.md","status":"completed","error":null}
```

### Descargar el bundle

Solo los trabajos completados permiten descargar el resultado:

```bash
curl -o bundle.zip http://localhost:8080/jobs/<job_id>/download -H "Authorization: Bearer <token>"
```

El archivo descargado contiene `index.md`, `log.md` y `documento.md`.

### Consultar las tablas

```bash
docker compose exec postgres psql -U okf -d okf -c "\\dt"
```

Consultar los trabajos creados:

```bash
docker compose exec postgres psql -U okf -d okf -c "SELECT d.original_name, j.id, j.status FROM documents d JOIN jobs j ON j.document_id = d.id ORDER BY j.created_at DESC;"
```

Consultar los bundles generados:

```bash
docker compose exec postgres psql -U okf -d okf -c "SELECT job_id, storage_key, validation_status FROM bundles ORDER BY created_at DESC;"
```

## Solucion de problemas

### La pagina no carga

Comprueba que el frontend este activo:

```powershell
docker compose ps frontend
docker compose logs frontend
```

Si no esta activo, ejecuta:

```powershell
docker compose up --build -d frontend
```

### Aparece `autenticacion requerida` al descargar

No abras directamente la URL `http://localhost:8080/jobs/<job_id>/download` desde el navegador. Esa ruta exige el token `Bearer`.

Usa el boton `Take my cute bundle.zip` dentro del frontend. Si el navegador conserva una version anterior, haz una recarga completa con `Ctrl + F5`, cierra sesion e inicia sesion nuevamente.

### La carga no termina

Revisa el estado de API, Redis y worker:

```powershell
docker compose ps
docker compose logs api --tail 30
docker compose logs worker --tail 30
docker compose logs redis --tail 30
```

El trabajo puede pasar por `queued` y `processing` antes de llegar a `completed`.

### Error de conexión con PostgreSQL

Espera a que el servicio aparezca como `healthy` y vuelve a levantar la API:

```powershell
docker compose up -d postgres
docker compose up -d api worker
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

Los bundles se guardan con una ruta similar a:

```text
bundles/<job-id>/bundle.zip
```

El ZIP minimo contiene:

```text
index.md
log.md
documento.md
```

## Pendiente

1. Mejorar la interfaz y agregar soporte para mas formatos de entrada.
2. Agregar observabilidad y metricas del procesamiento.
