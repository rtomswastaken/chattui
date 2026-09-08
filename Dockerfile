# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Copy dependency manifests
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build statically-linked server binary without CGO (pure Go modernc.org/sqlite)
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/chattui-server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/chattui-client ./cmd/client

# Final minimal runtime image
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app

# Copy server binary from builder
COPY --from=builder /app/chattui-server /app/chattui-server
COPY --from=builder /app/chattui-client /app/chattui-client

# Create persistent data directory
RUN mkdir -p /app/data

ENV DB_PATH=/app/data/chattui.db
ENV PORT=8443

EXPOSE 8443

VOLUME ["/app/data"]

ENTRYPOINT ["/app/chattui-server"]
CMD ["-port", "8443", "-db", "/app/data/chattui.db"]
