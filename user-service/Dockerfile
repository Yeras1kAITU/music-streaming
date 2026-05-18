FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /app/user-service

# Copy go.mod and go.sum first for better caching
COPY user-service/go.mod user-service/go.sum ./

# Copy proto directory
COPY proto /app/proto

# Copy the rest of the user-service code
COPY user-service/ .

# Download dependencies (the replace directive will use local ../proto)
RUN go mod download

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o user-service ./cmd/main.go

FROM alpine:latest

RUN apk --no-cache add ca-certificates

RUN addgroup -g 1001 -S appuser && \
    adduser -u 1001 -S appuser -G appuser

WORKDIR /app

COPY --from=builder /app/user-service/user-service .
COPY --from=builder /app/user-service/migrations ./migrations

RUN chown -R appuser:appuser /app

USER appuser

EXPOSE 50051

ENTRYPOINT ["./user-service"]
