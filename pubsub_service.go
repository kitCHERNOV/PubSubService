package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/viper"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	pb "vk/pb"
	"vk/subpub"
)

// PubSubServer реализует интерфейс gRPC-сервиса PubSub
type PubSubServer struct {
	pb.UnimplementedPubSubServer
	sp         subpub.SubPub
	mu         sync.RWMutex
	logger     *log.Logger
	subscribers map[string]map[chan string]struct{}
}

// NewPubSubServer создает новый экземпляр PubSubServer
func NewPubSubServer(sp subpub.SubPub, logger *log.Logger) *PubSubServer {
	return &PubSubServer{
		sp:         sp,
		logger:     logger,
		subscribers: make(map[string]map[chan string]struct{}),
	}
}

// Subscribe обрабатывает запросы на подписку
func (s *PubSubServer) Subscribe(req *pb.SubscribeRequest, stream pb.PubSub_SubscribeServer) error {
	if req.Key == "" {
		return status.Errorf(codes.InvalidArgument, "key cannot be empty")
	}

	s.logger.Printf("New subscription request for key: %s", req.Key)

	// Создаем канал для получения событий
	eventCh := make(chan string, 100)
	
	// Регистрируем канал в списке подписчиков
	s.mu.Lock()
	if s.subscribers[req.Key] == nil {
		s.subscribers[req.Key] = make(map[chan string]struct{})
	}
	s.subscribers[req.Key][eventCh] = struct{}{}
	s.mu.Unlock()

	// Отписываемся при завершении
	defer func() {
		s.mu.Lock()
		delete(s.subscribers[req.Key], eventCh)
		if len(s.subscribers[req.Key]) == 0 {
			delete(s.subscribers, req.Key)
		}
		s.mu.Unlock()
		close(eventCh)
		s.logger.Printf("Subscription closed for key: %s", req.Key)
	}()

	// Подписываемся на шину событий
	subscription, err := s.sp.Subscribe(req.Key, func(msg interface{}) {
		if data, ok := msg.(string); ok {
			select {
			case eventCh <- data:
				// Сообщение успешно отправлено
			default:
				// Буфер заполнен, пропускаем сообщение
				s.logger.Printf("Buffer full for subscriber on key: %s, message dropped", req.Key)
			}
		}
	})
	
	if err != nil {
		return status.Errorf(codes.Internal, "failed to subscribe: %v", err)
	}
	
	defer subscription.Unsubscribe()

	// Отправляем сообщения клиенту
	for {
		select {
		case data := <-eventCh:
			event := &pb.Event{
				Data: data,
			}
			if err := stream.Send(event); err != nil {
				s.logger.Printf("Error sending event to client: %v", err)
				return status.Errorf(codes.Internal, "failed to send event: %v", err)
			}
		case <-stream.Context().Done():
			return status.Errorf(codes.Canceled, "client canceled subscription")
		}
	}
}

// Publish публикует события для всех подписчиков по ключу
func (s *PubSubServer) Publish(ctx context.Context, req *pb.PublishRequest) (*emptypb.Empty, error) {
	if req.Key == "" {
		return nil, status.Errorf(codes.InvalidArgument, "key cannot be empty")
	}
	
	s.logger.Printf("Publishing event for key: %s, data: %s", req.Key, req.Data)

	// Публикуем событие в шину
	if err := s.sp.Publish(req.Key, req.Data); err != nil {
		s.logger.Printf("Error publishing event: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to publish event: %v", err)
	}

	return &emptypb.Empty{}, nil
}

// Config содержит конфигурацию сервиса
type Config struct {
	Port         int    `mapstructure:"port"`
	LogFile      string `mapstructure:"log_file"`
	LogToConsole bool   `mapstructure:"log_to_console"`
}

// LoadConfig загружает конфигурацию из файла
func LoadConfig(path string) (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(path)
	viper.SetDefault("port", 50051)
	viper.SetDefault("log_to_console", true)

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &config, nil
}

// setupLogger настраивает логирование
func setupLogger(config *Config) *log.Logger {
	var logOutput *os.File
	var err error

	if config.LogFile != "" {
		logOutput, err = os.OpenFile(config.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Fatalf("Failed to open log file: %v", err)
		}
	}

	if config.LogToConsole {
		if logOutput != nil {
			return log.New(os.Stderr, "", log.LstdFlags)
		}
		return log.New(os.Stderr, "", log.LstdFlags)
	}

	if logOutput != nil {
		return log.New(logOutput, "", log.LstdFlags)
	}

	// Если ничего не задано, используем stderr
	return log.New(os.Stderr, "", log.LstdFlags)
}

func main() {
	// Загружаем конфигурацию
	config, err := LoadConfig(".")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Настраиваем логирование
	logger := setupLogger(config)
	logger.Printf("Starting PubSub service on port %d", config.Port)

	// Создаем экземпляр subpub
	sp := subpub.NewSubPub()
	
	// Создаем сервер
	server := NewPubSubServer(sp, logger)
	
	// Создаем gRPC-сервер
	grpcServer := grpc.NewServer()
	pb.RegisterPubSubServer(grpcServer, server)
	
	// Запускаем прослушивание
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", config.Port))
	if err != nil {
		logger.Fatalf("Failed to listen on port %d: %v", config.Port, err)
	}
	
	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		
		logger.Printf("Received signal: %v", sig)
		
		// Останавливаем gRPC-сервер
		grpcServer.GracefulStop()
		
		// Закрываем subpub с таймаутом
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		
		if err := sp.Close(ctx); err != nil {
			logger.Printf("Error closing subpub: %v", err)
		}
		
		logger.Println("Service stopped")
	}()
	
	// Запускаем сервер
	logger.Println("PubSub service is running")
	if err := grpcServer.Serve(lis); err != nil {
		logger.Fatalf("Failed to serve: %v", err)
	}
}