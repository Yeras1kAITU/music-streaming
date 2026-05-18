FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /app/music-service

# Copy go.mod and go.sum first for better caching
COPY music-service/go.mod music-service/go.sum ./

# Copy proto directory
COPY proto /app/proto

# Copy the rest of the music-service code
COPY music-service/ .

# Download dependencies (the replace directive will use local ../proto)
RUN go mod download

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o music-service ./cmd/main.go

FROM alpine:latest

RUN apk --no-cache add ca-certificates ffmpeg

WORKDIR /root/

COPY --from=builder /app/music-service/music-service .

RUN mkdir -p /uploads

EXPOSE 50052

CMD ["./music-service"]
