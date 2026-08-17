# Proyecto por Daniela Solarte y Samara Martinez

# Plataforma de conversión documental a bundles OKF

Plataforma web multiusuario que recibe documentos, los procesa de forma asíncrona mediante
workers independientes de la API, y genera como resultado un *bundle* de conocimiento
compatible con Open Knowledge Format (OKF): una carpeta con `index.md`, `log.md` y uno
o más documentos de concepto en Markdown.

## Flujo
1. El usuario carga un documento → la API responde de inmediato con un identificador de trabajo (job ID).
2. El trabajo se encola y es procesado en segundo plano por un worker independiente.
3. El worker segmenta el documento en unidades lógicas y genera el bundle OKF.
4. El bundle se valida (estructura mínima + enlaces del índice) antes de publicarse.
5. El usuario consulta el estado del trabajo y descarga el bundle completo.

## Arquitectura
- **Backend (API + workers):** Go
- **Cola de mensajes:** Redis
- **Base de datos:** PostgreSQL (metadatos de usuarios, documentos, trabajos y bundles)
- **Almacenamiento de objetos:** MinIO (documentos originales y bundles)
- **Despliegue:** Docker Compose — todo el sistema se levanta con `docker compose up`

Proyecto de nivelación — ISIS4426 Desarrollo de Soluciones Cloud.
