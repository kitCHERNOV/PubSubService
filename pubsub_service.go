package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/ilyakaznacheev/cleanenv"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	pb "vk/pb"
	"vk/subpub"
)

type PubSubServer struct {
	pb.UnimplementedPubSubServer
	sp          subpub.SubPub
	mu          sync.RWMutex
	logger      *log.Logger
	subscribers map[string]map[chan string]struct{}
}

func NewPubSubServer(sp subpub.SubPub, logger *log.Logger) *PubSubServer {
	return &PubSubServer{
		sp:          sp,
		logger:      logger,
		subscribers: make(map[string]map[chan string]struct{}),
	}
}

func (s *PubSubServer) Subscribe(req *pb.SubscribeRequest, stream pb.PubSub_SubscribeServer) error {
	if req.Key == "" {
		return status.Errorf(codes.InvalidArgument, "key cannot be empty")
	}

	s.logger.Printf("New subscription request for key: %s", req.Key)

	eventCh := make(chan string, 100)

	s.mu.Lock()
	if s.subscribers[req.Key] == nil {
		s.subscribers[req.Key] = make(map[chan string]struct{})
	}
	s.subscribers[req.Key][eventCh] = struct{}{}
	s.mu.Unlock()

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

	subscription, err := s.sp.Subscribe(req.Key, func(msg interface{}) {
		if data, ok := msg.(string); ok {
			select {
			case eventCh <- data:
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

func (s *PubSubServer) Publish(ctx context.Context, req *pb.PublishRequest) (*emptypb.Empty, error) {
	if req.Key == "" {
		return nil, status.Errorf(codes.InvalidArgument, "key cannot be empty")
	}

	s.logger.Printf("Publishing event for key: %s, data: %s", req.Key, req.Data)

	if err := s.sp.Publish(req.Key, req.Data); err != nil {
		s.logger.Printf("Error publishing event: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to publish event: %v", err)
	}

	return &emptypb.Empty{}, nil
}

type Config struct {
	Port         int  `yaml:"port"`
	LogToConsole bool `yaml:"log_to_console"`
}

func LoadConfig() (*Config, error) {
	var cfg Config

	err := cleanenv.ReadConfig("config.yaml", &cfg)
	if err != nil {
		return nil, errors.New("failed to load config file")
	}

	return &cfg, nil
}

func setupLogger(config *Config) *log.Logger {

	if config.LogToConsole {
		return log.New(os.Stderr, "", log.LstdFlags)
	}

	return log.New(os.Stderr, "", log.LstdFlags)
}

func main() {
	config, err := LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	logger := setupLogger(config)
	logger.Printf("Starting PubSub service on port %d", config.Port)

	sp := subpub.NewSubPub()

	server := NewPubSubServer(sp, logger)

	grpcServer := grpc.NewServer()
	pb.RegisterPubSubServer(grpcServer, server)

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", config.Port))
	if err != nil {
		logger.Fatalf("Failed to listen on port %d: %v", config.Port, err)
	}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh

		logger.Printf("Received signal: %v", sig)

		grpcServer.GracefulStop()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := sp.Close(ctx); err != nil {
			logger.Printf("Error closing subpub: %v", err)
		}

		logger.Println("Service stopped")
	}()

	logger.Println("PubSub service is running")
	if err := grpcServer.Serve(lis); err != nil {
		logger.Fatalf("Failed to serve: %v", err)
	}
}
