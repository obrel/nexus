package circuitbreaker

import (
	"errors"
	"sync"
	"time"
)

// ErrCircuitOpen is returned when the circuit breaker is in the open state.
var ErrCircuitOpen = errors.New("circuit breaker is open")

// State represents the circuit breaker state.
type State int

const (
	StateClosed   State = iota // normal operation — requests pass through
	StateOpen                  // failures exceeded threshold — requests are rejected
	StateHalfOpen              // testing recovery — limited requests pass through
)

// Breaker implements a simple circuit breaker with three states: closed, open, half-open.
//
// In the closed state, all requests pass through. If consecutive failures reach the
// threshold, the breaker transitions to open. After the reset timeout, it transitions
// to half-open and allows a single probe request. If that succeeds, the breaker closes;
// if it fails, the breaker reopens.
type Breaker struct {
	mu               sync.Mutex
	state            State
	failureCount     int
	failureThreshold int
	resetTimeout     time.Duration
	lastFailureTime  time.Time
}

// New creates a Breaker that opens after failureThreshold consecutive errors
// and resets after resetTimeout.
func New(failureThreshold int, resetTimeout time.Duration) *Breaker {
	return &Breaker{
		failureThreshold: failureThreshold,
		resetTimeout:     resetTimeout,
	}
}

// Execute runs fn if the circuit allows it. It records success or failure and
// transitions state accordingly.
func (b *Breaker) Execute(fn func() error) error {
	if !b.allowRequest() {
		return ErrCircuitOpen
	}

	err := fn()

	b.mu.Lock()
	defer b.mu.Unlock()

	if err != nil {
		b.failureCount++
		b.lastFailureTime = time.Now()
		if b.failureCount >= b.failureThreshold {
			b.state = StateOpen
		}
		return err
	}

	// Success — reset to closed.
	b.failureCount = 0
	b.state = StateClosed
	return nil
}

// allowRequest returns true if the breaker allows a request to pass through.
func (b *Breaker) allowRequest() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case StateClosed:
		return true
	case StateOpen:
		if time.Since(b.lastFailureTime) > b.resetTimeout {
			b.state = StateHalfOpen
			return true
		}
		return false
	case StateHalfOpen:
		// Only one probe at a time — transition back to open until result is known.
		b.state = StateOpen
		b.lastFailureTime = time.Now()
		return true
	default:
		return true
	}
}

// State returns the current circuit breaker state.
func (b *Breaker) CurrentState() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

// Reset manually resets the breaker to the closed state.
func (b *Breaker) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.state = StateClosed
	b.failureCount = 0
}
