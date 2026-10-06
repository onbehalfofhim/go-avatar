package resilience

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCircuitBreakerOpensAfterThreshold(t *testing.T) {
	breaker, err := NewCircuitBreaker(2, time.Minute)
	require.NoError(t, err)

	failure := errors.New("dependency unavailable")
	require.Equal(t, failure, breaker.Execute(func() error { return failure }))
	require.Equal(t, failure, breaker.Execute(func() error { return failure }))
	require.ErrorIs(t, breaker.Execute(func() error { return nil }), ErrOpen)
	require.Equal(t, "open", breaker.State())
}

func TestCircuitBreakerRecoversAfterTimeout(t *testing.T) {
	breaker, err := NewCircuitBreaker(1, 10*time.Millisecond)
	require.NoError(t, err)

	require.Error(t, breaker.Execute(func() error { return errors.New("failure") }))
	require.ErrorIs(t, breaker.Execute(func() error { return nil }), ErrOpen)

	time.Sleep(15 * time.Millisecond)
	require.NoError(t, breaker.Execute(func() error { return nil }))
	require.Equal(t, "closed", breaker.State())
}
