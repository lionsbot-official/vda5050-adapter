package env

import (
	"log"

	"github.com/kelseyhightower/envconfig"
)

type Configs struct {
	MasterConfig MasterConfig
	RobotConfig  RobotConfig
}

type MasterConfig struct {
	Version      string `default:"2.1.0"`
	Host         string `required:"true"`
	Port         int    `required:"true"`
	CleanSession bool   `split_words:"true" default:"false"`
	ClientID     string `split_words:"true" required:"true"`
	Password     string `required:"true"`
	Username     string `required:"true"`
}

type RobotConfig struct {
	Region string

	// Explicit endpoint overrides; when empty they are derived from Region.
	StatusURL string `split_words:"true"`
	PoseURL   string `split_words:"true"`
	LoginURL  string `split_words:"true"`
	APIBase   string `split_words:"true"`

	Email      string
	Password   string
	AppName    string `split_words:"true"`
	AppVersion string `split_words:"true"`

	Model     string
	RobotID   string `split_words:"true"`
	RobotType string `split_words:"true"`

	ThetaOffsetDeg float64 `split_words:"true"`
	MapPollSeconds int     `split_words:"true" default:"60"`
}

func InitConfigs() (c Configs) {
	if err := envconfig.Process("master", &c.MasterConfig); err != nil {
		log.Fatalf("failed to process env master: %s", err)
	}
	if err := envconfig.Process("robot", &c.RobotConfig); err != nil {
		log.Fatalf("failed to process env robot: %s", err)
	}
	return c
}
