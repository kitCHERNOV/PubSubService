package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "vk/pb"
)

var (
	serverAddr = flag.String("server", "localhost:8080", "The server address in the format of host:port")
	mode       = flag.String("mode", "subscribe", "Mode: 'subscribe' or 'publish'")
	key        = flag.String("key", "default-topic", "Topic key to subscribe or publish to")
	data       = flag.String("data", "", "Data to publish (only used in publish mode)")
	interval   = flag.Int("interval", 0, "Interval between publish events in seconds (only used in publish mode)")
)

func main() {
	flag.Parse()

	conn, err := grpc.NewClient(*serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect to server: %v", err)
	}
	defer conn.Close()

	client := pb.NewPubSubClient(conn)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	switch strings.ToLower(*mode) {
	case "subscribe":
		subscribeMode(client, sigCh)
	case "publish":
		publishMode(client, sigCh)
	default:
		log.Fatalf("Unknown mode: %s. Use 'subscribe' or 'publish'", *mode)
	}
}

func subscribeMode(client pb.PubSubClient, sigCh chan os.Signal) {
	log.Printf("Starting subscriber for key: %s", *key)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := &pb.SubscribeRequest{
		Key: *key,
	}

	stream, err := client.Subscribe(ctx, req)
	if err != nil {
		log.Fatalf("Failed to subscribe: %v", err)
	}

	done := make(chan struct{})

	go func() {
		defer close(done)
		for {
			event, err := stream.Recv()
			if err == io.EOF {
				log.Println("Stream closed by server")
				return
			}
			if err != nil {
				log.Printf("Error receiving event: %v", err)
				return
			}
			log.Printf("Received event: %s", event.Data)
		}
	}()

	select {
	case sig := <-sigCh:
		log.Printf("Signal received: %v. Cancelling subscription...", sig)
		cancel()
	case <-done:
		log.Println("Subscription ended")
	}

	time.Sleep(time.Second)
}

func publishMode(client pb.PubSubClient, sigCh chan os.Signal) {
	log.Printf("Starting publisher for key: %s", *key)

	if *interval > 0 {
		ticker := time.NewTicker(time.Duration(*interval) * time.Second)
		defer ticker.Stop()

		counter := 1
		for {
			select {
			case <-ticker.C:
				publishEvent(client, counter)
				counter++
			case sig := <-sigCh:
				log.Printf("Signal received: %v. Stopping publisher...", sig)
				return
			}
		}
	} else {
		publishEvent(client, 0)
	}
}

func publishEvent(client pb.PubSubClient, counter int) {
	eventData := *data
	if eventData == "" {
		if counter > 0 {
			eventData = fmt.Sprintf("Event #%d at %s", counter, time.Now().Format(time.RFC3339))
		} else {
			eventData = fmt.Sprintf("Single event at %s", time.Now().Format(time.RFC3339))
		}
	}

	req := &pb.PublishRequest{
		Key:  *key,
		Data: eventData,
	}

	_, err := client.Publish(context.Background(), req)
	if err != nil {
		log.Printf("Error publishing event: %v", err)
		return
	}

	log.Printf("Published event with key '%s': %s", *key, eventData)
}
