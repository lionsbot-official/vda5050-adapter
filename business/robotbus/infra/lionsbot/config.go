package lionsbot

import (
	"fmt"
	"strings"

	"github.com/lionsbot-official/vda5050-adapter/foundation/env"
)

// Config is the resolved upstream input for one robot: endpoints, credentials,
// identity, the decoded state-code version, and the map-frame settings.
type Config struct {
	StatusURL string
	PoseURL   string
	LoginURL  string
	APIBase   string

	Email      string
	password   string // unexported + omitted from String so it can't leak
	AppName    string
	AppVersion string

	RobotID   string
	RobotType string

	version        codeVersion
	Frame          MapFrame // initial frame; map params arrive via dynamic lookup
	MapPollSeconds int
}

var regionHosts = map[string]string{
	"asia":      "api.lionsbot.io",
	"europe":    "eu-api.lionsbot.io",
	"us":        "us-api.lionsbot.io",
	"australia": "au-api.lionsbot.io",
	"edge":      "edge.lionsbot.io",
}

var modelCodeVersions = map[string]codeVersion{
	"R3":      codeV1,
	"NILFISK": codeV1,
	"R5":      codeV2,
	"LEOBOT":  codeV0,
}

func codeVersionForModel(model string) (codeVersion, error) {
	v, ok := modelCodeVersions[strings.ToUpper(strings.TrimSpace(model))]
	if !ok {
		return 0, fmt.Errorf("unsupported robot model %q", model)
	}
	return v, nil
}

// endpoints derives the four upstream URLs from the region, letting explicit
// values override per-field. Every URL must end up non-empty.
func endpoints(c env.RobotConfig) (login, apiBase, status, pose string, err error) {
	login, apiBase, status, pose = c.LoginURL, c.APIBase, c.StatusURL, c.PoseURL
	if region := strings.ToLower(strings.TrimSpace(c.Region)); region != "" {
		host, ok := regionHosts[region]
		if !ok {
			return "", "", "", "", fmt.Errorf("unknown region %q", c.Region)
		}
		if login == "" {
			login = "https://" + host + "/openapi/v2/edge/auth/login"
		}
		if apiBase == "" {
			apiBase = "https://" + host
		}
		if status == "" {
			status = "wss://" + host + "/openapi/v2/ws/robotstatus"
		}
		if pose == "" {
			pose = "wss://" + host + "/openapi/v2/ws/robotpose"
		}
	}
	if login == "" || apiBase == "" || status == "" || pose == "" {
		return "", "", "", "", fmt.Errorf("missing upstream endpoints: set a region or explicit URLs")
	}
	return login, apiBase, status, pose, nil
}

func NewConfig(c env.RobotConfig) (Config, error) {
	version, err := codeVersionForModel(c.Model)
	if err != nil {
		return Config{}, err
	}
	login, apiBase, status, pose, err := endpoints(c)
	if err != nil {
		return Config{}, err
	}
	return Config{
		StatusURL:      status,
		PoseURL:        pose,
		LoginURL:       login,
		APIBase:        apiBase,
		Email:          c.Email,
		password:       c.Password,
		AppName:        c.AppName,
		AppVersion:     c.AppVersion,
		RobotID:        c.RobotID,
		RobotType:      c.RobotType,
		version:        version,
		Frame:          MapFrame{ThetaOffsetDeg: c.ThetaOffsetDeg},
		MapPollSeconds: c.MapPollSeconds,
	}, nil
}

func (c Config) String() string {
	return fmt.Sprintf("Config{RobotID:%q Type:%q Version:v%d StatusURL:%q PoseURL:%q LoginURL:%q APIBase:%q Frame:%+v}",
		c.RobotID, c.RobotType, c.version, c.StatusURL, c.PoseURL, c.LoginURL, c.APIBase, c.Frame)
}
