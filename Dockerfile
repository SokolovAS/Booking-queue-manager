FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o queue-manager cmd/main.go

FROM alpine:3.17
RUN apk add --no-cache ca-certificates
COPY --from=builder /app/queue-manager /usr/local/bin/queue-manager
EXPOSE 8090
ENTRYPOINT [ "queue-manager" ]
