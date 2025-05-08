package subpub

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestSubscribeAndPublish(t *testing.T) {
	sp := NewSubPub()

	var wg sync.WaitGroup
	wg.Add(1)

	msgReceived := false
	_, err := sp.Subscribe("test", func(msg interface{}) {
		defer wg.Done()
		if msg == "hello" {
			msgReceived = true
		}
	})
	if err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}

	if err := sp.Publish("test", "hello"); err != nil {
		t.Fatalf("publish failed: %v", err)
	}

	wg.Wait()

	if !msgReceived {
		t.Error("message was not received by the subscriber")
	}
}

func TestPublishBeforeSubscribe(t *testing.T) {
	sp := NewSubPub()

	// Publish message before subscribing
	if err := sp.Publish("pre_sub", "early"); err != nil {
		t.Fatalf("publish before subscribe failed: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)

	msgReceived := false
	_, err := sp.Subscribe("pre_sub", func(msg interface{}) {
		defer wg.Done()
		if msg == "early" {
			msgReceived = true
		}
	})
	if err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}

	wg.Wait()

	if !msgReceived {
		t.Error("subscriber did not receive pre-published message")
	}
}

func TestUnsubscribe(t *testing.T) {
	sp := NewSubPub()

	var wg sync.WaitGroup
	wg.Add(1)

	count := 0
	sub, err := sp.Subscribe("once", func(msg interface{}) {
		count++
		wg.Done()
	})
	if err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}

	if err := sp.Publish("once", "msg1"); err != nil {
		t.Fatalf("publish failed: %v", err)
	}
	wg.Wait()

	sub.Unsubscribe()

	// This message should not be received
	if err := sp.Publish("once", "msg2"); err != nil {
		t.Fatalf("publish failed: %v", err)
	}

	// Give some time for goroutines to run (should not increment `count`)
	time.Sleep(100 * time.Millisecond)

	if count != 1 {
		t.Errorf("expected 1 message received, got %d", count)
	}
}

func TestClose(t *testing.T) {
	sp := NewSubPub()

	var wg sync.WaitGroup
	wg.Add(1)

	_, err := sp.Subscribe("shutdown", func(msg interface{}) {
		defer wg.Done()
		time.Sleep(50 * time.Millisecond)
	})
	if err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}

	if err := sp.Publish("shutdown", "msg"); err != nil {
		t.Fatalf("publish failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	if err := sp.Close(ctx); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	wg.Wait()
}

func TestSubscribeNilHandler(t *testing.T) {
	sp := NewSubPub()
	_, err := sp.Subscribe("bad", nil)
	if err == nil {
		t.Error("expected error when subscribing with nil handler")
	}
}

func TestPublishAfterClose(t *testing.T) {
	sp := NewSubPub()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	if err := sp.Close(ctx); err != nil {
		t.Fatalf("unexpected close error: %v", err)
	}

	err := sp.Publish("after", "msg")
	if err == nil {
		t.Error("expected error publishing after close")
	}
}
