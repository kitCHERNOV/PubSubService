package subpub

import (
	"context"
	"errors"
	"sync"
)

type MessageHandler func(msg interface{})

type Subscription interface {
	Unsubscribe()
}

type SubPub interface {
	Subscribe(subject string, cb MessageHandler) (Subscription, error)
	Publish(subject string, msg interface{}) error
	Close(ctx context.Context) error
}

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

type subPub struct {
	mu            sync.RWMutex
	subjects      map[string]map[*subscription]struct{}
	messageQueues map[string][]interface{}
	processingWg  sync.WaitGroup
	closed        bool
}

func NewSubPub() SubPub {
	return &subPub{
		subjects:      make(map[string]map[*subscription]struct{}),
		messageQueues: make(map[string][]interface{}),
	}
}

func (sp *subPub) Subscribe(subject string, cb MessageHandler) (Subscription, error) {
	if cb == nil {
		return nil, errors.New("message handler cannot be nil")
	}

	sp.mu.Lock()
	defer sp.mu.Unlock()

	if sp.closed {
		return nil, errors.New("subpub is closed")
	}

	sub := &subscription{
		subject: subject,
		handler: cb,
		sp:      sp,
		active:  true,
	}

	if sp.subjects[subject] == nil {
		sp.subjects[subject] = make(map[*subscription]struct{})
	}
	sp.subjects[subject][sub] = struct{}{}

	if messages, exists := sp.messageQueues[subject]; exists && len(messages) > 0 {
		for _, msg := range messages {
			sp.deliverMessage(sub, msg)
		}
		//if sp.subjects[subject] == nil {
		sp.messageQueues[subject] = nil
		//}

	}

	return sub, nil
}

func (sp *subPub) Publish(subject string, msg interface{}) error {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	if sp.closed {
		return errors.New("subpub is closed")
	}

	subs, exists := sp.subjects[subject]
	if !exists || len(subs) == 0 {
		sp.messageQueues[subject] = append(sp.messageQueues[subject], msg)
		return nil
	}

	for sub := range subs {
		sp.deliverMessage(sub, msg)
	}

	return nil
}

func (sp *subPub) Close(ctx context.Context) error {
	sp.mu.Lock()
	if sp.closed {
		sp.mu.Unlock()
		return nil
	}
	sp.closed = true
	sp.mu.Unlock()

	done := make(chan struct{})

	go func() {
		sp.processingWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

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

func (sp *subPub) deliverMessage(sub *subscription, msg interface{}) {
	sp.processingWg.Add(1)

	go func() {
		defer sp.processingWg.Done()

		sub.mu.Lock()
		active := sub.active
		sub.mu.Unlock()

		if active {
			sub.handler(msg)
		}
	}()
}
