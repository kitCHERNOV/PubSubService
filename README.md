PubSub Service
Сервис подписок, работающий по gRPC, с использованием шины событий subpub.
Обзор архитектуры
Сервис реализует простую систему публикации-подписки (Publisher-Subscriber) с использованием gRPC. Архитектура состоит из следующих компонентов:

Шина событий subpub (разработана в первой части задания)

Обеспечивает асинхронную доставку сообщений
Поддерживает множество подписчиков на один subject
Сохраняет порядок сообщений (FIFO)
Предотвращает блокировку быстрых подписчиков из-за медленных


gRPC-сервис PubSub

Предоставляет API для подписки и публикации событий
Работает поверх шины событий subpub
Поддерживает стриминг событий к клиентам



Структура проекта
.
├── pb/
│   ├── pubsub.proto      # Описание gRPC API
│   └── ... (сгенерированные файлы)
├── subpub/
│   └── subpub.go         # Реализация шины событий subpub
├── pubsub_service.go     # Основной файл сервиса gRPC
├── config.yaml           # Конфигурация сервиса
└── README.md             # Документация
Компоненты
Шина событий subpub
Основная шина событий, обеспечивающая обмен сообщениями между издателями и подписчиками. Основные характеристики:

Возможность подписки и отписки множества клиентов на один subject
Асинхронная обработка сообщений, предотвращающая блокировку быстрых подписчиков медленными
Сохранение порядка сообщений с использованием FIFO-очереди
Корректная обработка контекста при закрытии
Отсутствие утечек горутин

gRPC-сервис PubSub
API-сервис, предоставляющий доступ к шине событий через gRPC:

Subscribe - потоковый метод для получения событий по ключу
Publish - метод для публикации событий по ключу

Использованные паттерны

Dependency Injection

Шина событий subpub передается в сервис PubSub через конструктор
Логгер также передается через конструктор для гибкой настройки логирования


Graceful Shutdown

Корректное завершение работы при получении сигналов SIGINT и SIGTERM
Освобождение ресурсов и завершение текущих операций


Конфигурируемость

Использование библиотеки Viper для чтения конфигурации из файла
Гибкая настройка параметров сервиса (порт, логирование)



Сборка и запуск
Предварительные требования

Go 1.23.6 или выше
Protocol Buffers компилятор
gRPC инструменты для Go

Генерация gRPC кода
bashprotoc --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    pb/pubsub.proto
Сборка
bash: go build -o pubsub_service
docker: docker build -t pubsub-service .
Запуск
bash: ./pubsub_service
docker: docker run -p 50051:50051 pubsub-service
Конфигурация
Сервис настраивается через файл config.yaml:
yamlport: 50051                 # Порт для gRPC сервера
log_file: "pubsub.log"          # Файл для логов
log_to_console: true            # Вывод логов в консоль
Пример использования (клиентский код)
Подписка на события
```
// Создание клиента
conn, err := grpc.Dial("localhost:50051", grpc.WithInsecure())
client := pb.NewPubSubClient(conn)

// Подписка на события
req := &pb.SubscribeRequest{Key: "my-topic"}
stream, err := client.Subscribe(context.Background(), req)

// Получение событий
for {
    event, err := stream.Recv()
    if err != nil {
        break
    }
    fmt.Printf("Received: %s\n", event.Data)
}
Публикация событий
go// Создание клиента
conn, err := grpc.Dial("localhost:50051", grpc.WithInsecure())
client := pb.NewPubSubClient(conn)

// Публикация события
req := &pb.PublishRequest{
    Key: "my-topic",
    Data: "Hello, world!",
}
client.Publish(context.Background(), req)
```