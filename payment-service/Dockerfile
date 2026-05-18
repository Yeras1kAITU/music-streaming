FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /app/payment-service

# Copy go.mod and go.sum first for better caching
COPY payment-service/go.mod payment-service/go.sum ./

# Copy proto directory
COPY proto /app/proto

# Copy the rest of the payment-service code
COPY payment-service/ .

# Download dependencies (the replace directive will use local ../proto)
RUN go mod download

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o payment-service ./cmd/main.go

FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

COPY --from=builder /app/payment-service/payment-service .

EXPOSE 50053

CMD ["./payment-service"]
