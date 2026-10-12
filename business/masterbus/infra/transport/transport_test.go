package transport

import (
	"testing"

	"github.com/lionsbot-official/vda5050-adapter/foundation/env"
)

func TestNew_CredentialsOptional(t *testing.T) {
	var cfg env.Configs
	cfg.MasterConfig.Host = "localhost"
	cfg.MasterConfig.Port = 1883
	// Username/Password intentionally empty: an unauthenticated broker needs no creds,
	// and an authenticated one rejects at connect — never a config-time gate.

	if _, err := New(cfg, "R3-0001", "will/topic", nil); err != nil {
		t.Fatalf("New with empty credentials: %v", err)
	}
}

func TestDeriveClientID_UniquePerRobot(t *testing.T) {
	a := deriveClientID("lionsbot-adapter", "R3-0001")
	b := deriveClientID("lionsbot-adapter", "R3-0002")
	if a == b {
		t.Fatalf("two robots share client ID %q — instances will disconnect each other", a)
	}
	if a != "lionsbot-adapter-R3-0001" {
		t.Errorf("clientID = %q, want base-serial", a)
	}
}

func TestDeriveClientID_SerialFallbackWhenNoBase(t *testing.T) {
	if got := deriveClientID("", "R3-0001"); got != "R3-0001" {
		t.Errorf("clientID = %q, want the serial alone when no base is set", got)
	}
}
