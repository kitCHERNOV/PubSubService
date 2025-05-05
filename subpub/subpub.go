package subpub

import (
	"context"
	"errors"
	"sync"
)

// MessageHandler is a callback function that processes messages delivered to subscribers
type MessageHandler func(msg interface{})

type Subscription interface {
	// Unsubscribe will remove interest in the current subject subscription
	Unsubscribe()
}

type SubPub interface {
	// Subscribe creates an asynchronous queue subscriber on the given subject.
	Subscribe(subject string, cb MessageHandler) (Subscription, error)

	// Publish publishes the msg argument to the given subject.
	Publish(subject string, msg interface{}) error

	// Close will shutdown sub-pub system.
	// May be blocked by data delivery until the context is canceled.
	Close(ctx context.Context) error
}

// subscription implements the Subscription interface
type subscription struct {
	subject string
	handler MessageHandler
	sp      *subPub
	mu      sync.Mutex
	active  bool
}

func (s *subscription) Unsubscribe() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.active {
		return
	}

	s.active = false
	s.sp.removeSubscription(s.subject, s)
}

// subPub implements the SubPub interface
type subPub struct {
	mu            sync.RWMutex
	subjects      map[string]map[*subscription]struct{}
	messageQueues map[string][]interface{}
	processingWg  sync.WaitGroup
	closed        bool
}

// NewSubPub creates a new SubPub instance
func NewSubPub() SubPub {
	return &subPub{
		subjects:      make(map[string]map[*subscription]struct{}),
		messageQueues: make(map[string][]interface{}),
	}
}

// Subscribe adds a new subscription for the given subject
func (sp *subPub) Subscribe(subject string, cb MessageHandler) (Subscription, error) {
	if cb == nil {
		return nil, errors.New("message handler cannot be nil")
	}

	sp.mu.Lock()
	defer sp.mu.Unlock()

	if sp.closed {
		return nil, errors.New("subpub is closed")
	}

	// Create new subscription
	sub := &subscription{
		subject: subject,
		handler: cb,
		sp:      sp,
		active:  true,
	}

	// Add subscription to the list for this subject
	if sp.subjects[subject] == nil {
		sp.subjects[subject] = make(map[*subscription]struct{})
	}
	sp.subjects[subject][sub] = struct{}{}

	// Process any queued messages for this subject
	if messages, exists := sp.messageQueues[subject]; exists && len(messages) > 0 {
		// Process queued messages asynchronously to not block other operations
		for _, msg := range messages {
			sp.deliverMessage(sub, msg)
		}
		// Clear the queue after processing all messages
		sp.messageQueues[subject] = nil
	}

	return sub, nil
}

// Publish sends a message to all subscribers of the given subject
func (sp *subPub) Publish(subject string, msg interface{}) error {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	if sp.closed {
		return errors.New("subpub is closed")
	}

	subs, exists := sp.subjects[subject]
	if !exists || len(subs) == 0 {
		// Queue the message for future subscribers
		sp.messageQueues[subject] = append(sp.messageQueues[subject], msg)
		return nil
	}

	// Deliver message to all active subscribers
	for sub := range subs {
		sp.deliverMessage(sub, msg)
	}

	return nil
}

// Close shuts down the subpub system and waits for all deliveries to complete
// unless the context is canceled
func (sp *subPub) Close(ctx context.Context) error {
	sp.mu.Lock()
	if sp.closed {
		sp.mu.Unlock()
		return nil
	}
	sp.closed = true
	sp.mu.Unlock()

	// Create a channel to signal when all deliveries are complete
	done := make(chan struct{})
	
	go func() {
		// Wait for all ongoing message deliveries to complete
		sp.processingWg.Wait()
		close(done)
	}()

	// Wait for either completion or context cancellation
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// removeSubscription removes a subscription from the subject map
func (sp *subPub) removeSubscription(subject string, sub *subscription) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	if subs, exists := sp.subjects[subject]; exists {
		delete(subs, sub)
		if len(subs) == 0 {
			delete(sp.subjects, subject)
		}
	}
}

// deliverMessage asynchronously delivers a message to a subscription
func (sp *subPub) deliverMessage(sub *subscription, msg interface{}) {
	sp.processingWg.Add(1)
	
	// Use goroutine to ensure slow handlers don't block others
	go func() {
		defer sp.processingWg.Done()
		
		// Check if subscription is still active before delivery
		sub.mu.Lock()
		active := sub.active
		sub.mu.Unlock()
		
		if active {
			// Call the handler with the message
			sub.handler(msg)
		}
	}()
}