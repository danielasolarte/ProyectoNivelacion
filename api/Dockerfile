# --- Etapa 1: build ---
# Usamos una imagen que ya trae el compilador de Go.
# Es la etapa "de trabajo": aquí compilamos, pero no es lo que se ejecuta al final.
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Copiamos primero el go.mod (y el go.sum si existiera) para aprovechar
# el cache de capas de Docker: si no cambian las dependencias, Docker no
# vuelve a descargarlas en cada build.
COPY go.mod ./
RUN go mod download

# Ahora sí copiamos el resto del código.
COPY . .

# Compilamos un binario estático llamado "api".
# CGO_ENABLED=0 evita dependencias del sistema operativo en el binario,
# para que corra sin problemas en la imagen mínima de la etapa 2.
RUN CGO_ENABLED=0 GOOS=linux go build -o api-bin main.go

# --- Etapa 2: imagen final ---
# alpine es una imagen de Linux minimalista (pocos MB).
# No necesitamos el compilador de Go aquí, solo el binario ya compilado.
FROM alpine:latest

WORKDIR /app

# Copiamos SOLO el binario compilado desde la etapa "builder".
# Así la imagen final es pequeña: no lleva el código fuente ni el compilador.
COPY --from=builder /app/api-bin .

EXPOSE 8080

CMD ["./api-bin"]
