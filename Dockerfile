FROM golang:1.23.6-alpine AS builder

WORKDIR /app
RUN apk add --no-cache git protobuf-dev

RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
RUN go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

COPY go.mod go.sum* ./
RUN go mod download
COPY . .

RUN protoc --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    pb/pubsub.proto

RUN CGO_ENABLED=0 GOOS=linux go build -o pubsub_service

FROM alpine:latest

WORKDIR /app
COPY --from=builder /app/pubsub_service .
COPY --from=builder /app/config.yaml .

RUN mkdir -p /app/logs

EXPOSE 50051

CMD ["./pubsub_service"]