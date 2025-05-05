FROM golang:1.23.6-alpine AS builder

WORKDIR /app

# Установка необходимых зависимостей
RUN apk add --no-cache git protobuf-dev

# Установка protoc-gen-go и protoc-gen-go-grpc
RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
RUN go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Копирование модулей и скачивание зависимостей
COPY go.mod go.sum* ./
RUN go mod download

# Копирование исходного кода
COPY . .

# Генерация gRPC кода
RUN protoc --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    pb/pubsub.proto

# Сборка приложения
RUN CGO_ENABLED=0 GOOS=linux go build -o pubsub_service

# Финальный образ
FROM alpine:latest

WORKDIR /app

# Копирование бинарного файла из предыдущего этапа
COPY --from=builder /app/pubsub_service .
COPY --from=builder /app/config.yaml .

# Создание директории для логов
RUN mkdir -p /app/logs

# Экспорт порта
EXPOSE 50051

# Запуск сервиса
CMD ["./pubsub_service"]