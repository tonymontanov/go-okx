package rest

import (
	"net/http"
	"testing"
	"time"
)

// The REST transport checks HTTP/2 connection health: a connection that
// went silent is closed after an unanswered PING instead of swallowing every
// following request until each one times out.
func TestNewClient_HTTP2HealthCheck(t *testing.T) {
	var c *Client = NewClient("https://example.com", nil, Config{RequestTimeout: time.Second}, "test", nil)
	var transport, ok = c.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport is %T, want *http.Transport", c.httpClient.Transport)
	}
	if !transport.ForceAttemptHTTP2 {
		t.Fatal("HTTP/2 is not attempted")
	}
	if transport.HTTP2 == nil {
		t.Fatal("no HTTP/2 config: dead connections are never detected")
	}
	if transport.HTTP2.SendPingTimeout != http2PingAfterIdle || transport.HTTP2.PingTimeout != http2PingTimeout {
		t.Fatalf("HTTP/2 health check = ping after %v, timeout %v; want %v, %v",
			transport.HTTP2.SendPingTimeout, transport.HTTP2.PingTimeout, http2PingAfterIdle, http2PingTimeout)
	}
}
