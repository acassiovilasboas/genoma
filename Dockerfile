# ============================================
# Genoma Framework — Core Server
# Multi-stage build for minimal production image
# ============================================

# Stage 1: Build
FROM golang:1.22-alpine AS builder

RUN apk --no-cache add ca-certificates git

WORKDIR /build

# Copy go.mod first, generate go.sum, then download deps (layer caching)
COPY go.mod ./
RUN go mod download || true

# Copy all source code
COPY . .

# Ensure go.sum is correct and all deps are downloaded
RUN go mod tidy

# Build static binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -o /app/genoma \
    ./cmd/genoma

# Stage 2: Runtime
FROM alpine:3.19

RUN apk --no-cache add ca-certificates docker-cli

WORKDIR /app

COPY --from=builder /app/genoma .
COPY scripts/ /app/scripts/
COPY internal/persistence/migrations/ /app/migrations/

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:8080/health || exit 1

USER 65534:65534

ENTRYPOINT ["./genoma"]
