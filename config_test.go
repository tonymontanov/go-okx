/*
FILE: config_test.go

DESCRIPTION:
Unit tests for Config.withDefaults(). The main focus is Demo flag behavior:
when Demo=true WS endpoints are automatically switched to wspap.okx.com,
except when the user explicitly set a URL.
*/

package okx

import "testing"

func TestWithDefaults_DemoSwitchesWsEndpoints(t *testing.T) {
	var cfg Config = Config{Demo: true}.withDefaults()
	if cfg.WS.PublicURL != DemoWsPublicURL {
		t.Fatalf("PublicURL: got %q, want %q", cfg.WS.PublicURL, DemoWsPublicURL)
	}
	if cfg.WS.PrivateURL != DemoWsPrivateURL {
		t.Fatalf("PrivateURL: got %q, want %q", cfg.WS.PrivateURL, DemoWsPrivateURL)
	}
	if cfg.WS.BusinessURL != DemoWsBusinessURL {
		t.Fatalf("BusinessURL: got %q, want %q", cfg.WS.BusinessURL, DemoWsBusinessURL)
	}
}

func TestWithDefaults_ProdEndpointsByDefault(t *testing.T) {
	var cfg Config = Config{}.withDefaults()
	if cfg.WS.PublicURL != DefaultWsPublicURL {
		t.Fatalf("PublicURL: got %q, want %q", cfg.WS.PublicURL, DefaultWsPublicURL)
	}
	if cfg.WS.PrivateURL != DefaultWsPrivateURL {
		t.Fatalf("PrivateURL: got %q, want %q", cfg.WS.PrivateURL, DefaultWsPrivateURL)
	}
}

func TestWithDefaults_ExplicitWsUrlsOverrideDemo(t *testing.T) {
	const custom string = "wss://custom.test/ws/v5/public"
	var cfg Config = Config{
		Demo: true,
		WS:   WsConfig{PublicURL: custom},
	}.withDefaults()
	if cfg.WS.PublicURL != custom {
		t.Fatalf("explicit PublicURL must be preserved, got %q", cfg.WS.PublicURL)
	}
	// private should still fall back to Demo
	if cfg.WS.PrivateURL != DemoWsPrivateURL {
		t.Fatalf("PrivateURL should fall back to demo, got %q", cfg.WS.PrivateURL)
	}
}
