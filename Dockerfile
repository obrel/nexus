# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -o /nexus_bin ./cmd/nexus

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy the binary from the builder stage
COPY --from=builder /nexus_bin ./nexus

# Copy the example config
COPY config.yaml.example ./config.yaml

# Expose ports for HTTP and WS
EXPOSE 8080 8081

# Default command (can be overridden by docker-compose)
ENTRYPOINT ["./nexus"]
