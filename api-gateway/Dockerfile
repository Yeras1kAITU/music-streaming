FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /app/api-gateway

# Copy go.mod and go.sum first for better caching
COPY api-gateway/go.mod api-gateway/go.sum ./

# Copy proto directory
COPY proto /app/proto

# Copy the rest of the api-gateway code
COPY api-gateway/ .

# Download dependencies (the replace directive will use local ../proto)
RUN go mod download

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o api-gateway ./cmd/main.go

FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

COPY --from=builder /app/api-gateway/api-gateway .

EXPOSE 8080

CMD ["./api-gateway"]
