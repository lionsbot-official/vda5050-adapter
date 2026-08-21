package lionsbot

import (
	"fmt"
	"strings"
	"testing"

	"github.com/lionsbot-official/vda5050-adapter/foundation/env"
)

func TestCodeVersionForModel(t *testing.T) {
	cases := []struct {
		model   string
		want    codeVersion
		wantErr bool
	}{
		{"R3", codeV1, false},
		{"r3", codeV1, false},
		{"Nilfisk", codeV1, false},
		{"R5", codeV2, false},
		{"LeoBot", codeV0, false},
		{"", 0, true},
		{"X9", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			got, err := codeVersionForModel(tc.model)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("model %q: want error, got version %d", tc.model, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("model %q: unexpected error %v", tc.model, err)
			}
			if got != tc.want {
				t.Errorf("model %q: got version %d, want %d", tc.model, got, tc.want)
			}
		})
	}
}

func TestNewConfig_DerivesEndpointsFromRegion(t *testing.T) {
	cases := []struct {
		region   string
		wantHost string
	}{
		{"asia", "api.lionsbot.io"},
		{"europe", "eu-api.lionsbot.io"},
		{"us", "us-api.lionsbot.io"},
		{"australia", "au-api.lionsbot.io"},
		{"Asia", "api.lionsbot.io"}, // case-insensitive
	}
	for _, tc := range cases {
		t.Run(tc.region, func(t *testing.T) {
			got, err := NewConfig(env.RobotConfig{Region: tc.region, Model: "R3", RobotID: "robot-001"})
			if err != nil {
				t.Fatalf("NewConfig: %v", err)
			}
			if want := "https://" + tc.wantHost + "/openapi/v2/edge/auth/login"; got.LoginURL != want {
				t.Errorf("LoginURL = %q, want %q", got.LoginURL, want)
			}
			if want := "https://" + tc.wantHost; got.APIBase != want {
				t.Errorf("APIBase = %q, want %q", got.APIBase, want)
			}
			if want := "wss://" + tc.wantHost + "/openapi/v2/ws/robotstatus"; got.StatusURL != want {
				t.Errorf("StatusURL = %q, want %q", got.StatusURL, want)
			}
			if want := "wss://" + tc.wantHost + "/openapi/v2/ws/robotpose"; got.PoseURL != want {
				t.Errorf("PoseURL = %q, want %q", got.PoseURL, want)
			}
		})
	}
}

func TestNewConfig_ExplicitURLOverridesDerivation(t *testing.T) {
	got, err := NewConfig(env.RobotConfig{
		Region: "asia", Model: "R3", RobotID: "robot-001",
		StatusURL: "wss://staging.example.com/status",
	})
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if got.StatusURL != "wss://staging.example.com/status" {
		t.Errorf("StatusURL = %q, want explicit override", got.StatusURL)
	}
	// other fields still derived
	if want := "wss://api.lionsbot.io/openapi/v2/ws/robotpose"; got.PoseURL != want {
		t.Errorf("PoseURL = %q, want derived %q", got.PoseURL, want)
	}
}

func TestNewConfig_RegionErrors(t *testing.T) {
	if _, err := NewConfig(env.RobotConfig{Region: "mars", Model: "R3"}); err == nil {
		t.Error("unknown region: want error")
	}
	// no region at all -> every URL must be explicit
	if _, err := NewConfig(env.RobotConfig{Model: "R3"}); err == nil {
		t.Error("no region, no URLs: want error")
	}
	if _, err := NewConfig(env.RobotConfig{
		Model:     "R3",
		LoginURL:  "https://x/login",
		APIBase:   "https://x",
		StatusURL: "wss://x/status",
		PoseURL:   "wss://x/pose",
	}); err != nil {
		t.Errorf("all URLs explicit without region: want ok, got %v", err)
	}
}

func TestNewConfig_CarriesIdentityPollAndThetaOffset(t *testing.T) {
	got, err := NewConfig(env.RobotConfig{
		Region: "asia", Model: "R3", RobotID: "robot-001", RobotType: "R3",
		Email: "bot@example.com", Password: "secret", AppName: "adapter", AppVersion: "1.0.0",
		ThetaOffsetDeg: 90, MapPollSeconds: 30,
	})
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if got.RobotID != "robot-001" || got.RobotType != "R3" || got.version != codeV1 {
		t.Errorf("identity = %q/%q/v%d, want robot-001/R3/v1", got.RobotID, got.RobotType, got.version)
	}
	if got.Frame.ThetaOffsetDeg != 90 {
		t.Errorf("Frame.ThetaOffsetDeg = %v, want 90 (map params come from the dynamic lookup)", got.Frame.ThetaOffsetDeg)
	}
	if got.Frame.Resolution != 0 {
		t.Errorf("Frame.Resolution = %v, want 0 (no static map frame)", got.Frame.Resolution)
	}
	if got.MapPollSeconds != 30 {
		t.Errorf("MapPollSeconds = %d, want 30", got.MapPollSeconds)
	}
}

func TestNewConfig_UnknownModelErrors(t *testing.T) {
	if _, err := NewConfig(env.RobotConfig{Region: "asia", Model: "nope"}); err == nil {
		t.Fatal("want error for unknown model")
	}
}

func TestConfig_StringRedactsPassword(t *testing.T) {
	cfg, err := NewConfig(env.RobotConfig{Region: "asia", Model: "R3", Password: "topsecret", RobotID: "robot-001"})
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	for _, s := range []string{cfg.String(), fmt.Sprintf("%v", cfg), fmt.Sprintf("%+v", cfg)} {
		if strings.Contains(s, "topsecret") {
			t.Errorf("password leaked in %q", s)
		}
	}
}
