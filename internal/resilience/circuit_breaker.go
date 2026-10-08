package resilience

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrOpen = errors.New("circuit breaker is open")

type CircuitBreaker struct {
	mu sync.Mutex

	failureThreshold int
	openTimeout      time.Duration

	failures int
	openedAt time.Time
	state    state
}

type state uint8

const (
	closed state = iota
	open
	halfOpen
)

func NewCircuitBreaker(failureThreshold int, openTimeout time.Duration) (*CircuitBreaker, error) {
	if failureThreshold < 1 {
		return nil, fmt.Errorf("failure threshold must be positive")
	}
	if openTimeout <= 0 {
		return nil, fmt.Errorf("open timeout must be positive")
	}

	return &CircuitBreaker{
		failureThreshold: failureThreshold,
		openTimeout:      openTimeout,
		state:            closed,
	}, nil
}

func (b *CircuitBreaker) Execute(fn func() error) error {
	if err := b.beforeCall(); err != nil {
		return err
	}

	err := fn()
	b.afterCall(err)
	return err
}

func (b *CircuitBreaker) beforeCall() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case closed:
		return nil
	case open:
		if time.Since(b.openedAt) < b.openTimeout {
			return ErrOpen
		}
		b.state = halfOpen
		return nil
	case halfOpen:
		return ErrOpen
	default:
		return ErrOpen
	}
}

func (b *CircuitBreaker) afterCall(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err == nil {
		b.failures = 0
		b.state = closed
		b.openedAt = time.Time{}
		return
	}

	b.failures++
	if b.state == halfOpen || b.failures >= b.failureThreshold {
		b.state = open
		b.openedAt = time.Now()
	}
}

func (b *CircuitBreaker) State() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case open:
		if time.Since(b.openedAt) >= b.openTimeout {
			return "half-open"
		}
		return "open"
	case halfOpen:
		return "half-open"
	default:
		return "closed"
	}
}
