FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /app/event-subscriber

# Copy go.mod and go.sum first for better caching
COPY event-subscriber/go.mod event-subscriber/go.sum ./

# Copy proto directory
COPY proto /app/proto

# Copy the rest of the event-subscriber code
COPY event-subscriber/ .

RUN go mod download

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o event-subscriber ./cmd/main.go

FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

COPY --from=builder /app/event-subscriber/event-subscriber .

CMD ["./event-subscriber"]
