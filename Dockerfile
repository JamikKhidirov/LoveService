ARG GO_VERSION=1.23
FROM golang:${GO_VERSION}-alpine AS build
ARG VERSION=dev
WORKDIR /src
RUN apk add --no-cache git ca-certificates tzdata
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -trimpath -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/loveservice ./cmd/api

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata wget \
    && addgroup -S app && adduser -S -G app -u 10001 app
WORKDIR /app
COPY --from=build /out/loveservice /app/loveservice
COPY docs /app/docs
RUN mkdir -p /app/media && chown -R app:app /app
USER app
EXPOSE 8080
ENV HTTP_ADDR=:8080 \
    DOCS_DIR=/app/docs \
    MEDIA_DIR=/app/media
HEALTHCHECK --interval=15s --timeout=3s --start-period=20s --retries=5 \
    CMD wget -qO- http://127.0.0.1:8080/health >/dev/null || exit 1
ENTRYPOINT ["/app/loveservice"]
