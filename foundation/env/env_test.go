package env

import (
	"testing"

	"github.com/kelseyhightower/envconfig"
)

func TestRobotConfig_Process(t *testing.T) {
	t.Setenv("ROBOT_REGION", "asia")
	t.Setenv("ROBOT_STATUS_URL", "wss://x/status")
	t.Setenv("ROBOT_POSE_URL", "wss://x/pose")
	t.Setenv("ROBOT_LOGIN_URL", "https://x/login")
	t.Setenv("ROBOT_API_BASE", "https://x")
	t.Setenv("ROBOT_EMAIL", "bot@example.com")
	t.Setenv("ROBOT_PASSWORD", "secret")
	t.Setenv("ROBOT_APP_NAME", "adapter")
	t.Setenv("ROBOT_APP_VERSION", "1.0.0")
	t.Setenv("ROBOT_MODEL", "R3")
	t.Setenv("ROBOT_ROBOT_ID", "robot-001")
	t.Setenv("ROBOT_ROBOT_TYPE", "R3")
	t.Setenv("ROBOT_THETA_OFFSET_DEG", "90")
	t.Setenv("ROBOT_MAP_POLL_SECONDS", "30")

	var got RobotConfig
	if err := envconfig.Process("robot", &got); err != nil {
		t.Fatalf("process: %v", err)
	}
	want := RobotConfig{
		Region:    "asia",
		StatusURL: "wss://x/status", PoseURL: "wss://x/pose", LoginURL: "https://x/login", APIBase: "https://x",
		Email: "bot@example.com", Password: "secret", AppName: "adapter", AppVersion: "1.0.0",
		Model: "R3", RobotID: "robot-001", RobotType: "R3",
		ThetaOffsetDeg: 90, MapPollSeconds: 30,
	}
	if got != want {
		t.Errorf("RobotConfig = %+v, want %+v", got, want)
	}
}

func TestRobotConfig_MapPollDefault(t *testing.T) {
	var got RobotConfig
	if err := envconfig.Process("robot", &got); err != nil {
		t.Fatalf("process: %v", err)
	}
	if got.MapPollSeconds != 60 {
		t.Errorf("MapPollSeconds default = %d, want 60", got.MapPollSeconds)
	}
}
