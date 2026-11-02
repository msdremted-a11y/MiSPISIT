# Веб-часть «Системного монитора на базе eBPF + Qt», версия 0.0.4.
# Окно Qt из ЛР1 — клиентский интерфейс, его Dockerfile лежит в qt-client/.

# ---------- Этап 1: сборка ----------
# Сборка идёт на платформе сборщика, Go сам собирает бинарник под amd64 и arm64.
FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS builder
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/monitor-web ./cmd/web

# ---------- Этап 2: запуск ----------
FROM alpine:3.20
WORKDIR /app
COPY --from=builder /out/monitor-web /usr/local/bin/monitor-web
COPY templates ./templates
COPY static ./static
ENV APP_DIR=/app PORT=8080
EXPOSE 8080
CMD ["monitor-web"]
