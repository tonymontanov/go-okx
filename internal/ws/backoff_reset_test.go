package ws

import (
	"testing"
	"time"
)

// A connection that lived healthyConnectionSpan or longer resets the
// reconnect backoff; a short-lived one keeps climbing the ladder.
func TestConnectionWasHealthy(t *testing.T) {
	if connectionWasHealthy(healthyConnectionSpan - time.Millisecond) {
		t.Fatal("a connection shorter than the healthy span reset the backoff")
	}
	if !connectionWasHealthy(healthyConnectionSpan) {
		t.Fatal("a connection that lived the healthy span did not reset the backoff")
	}
}
