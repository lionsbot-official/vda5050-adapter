package topichelper

import (
	"fmt"

	versionhelper "github.com/lionsbot-official/vda5050-adapter/business/common/version"
)

const master = "vda5050"
const topicTpl = "%s/%s/%s/%s/%s" // master/version/company/serialNumber/connection

type Topic string

const (
	TopicConnection     Topic = "connection"
	TopicState          Topic = "state"
	TopicOrder          Topic = "order"
	TopicInstantActions Topic = "instantActions"
	TopicFactsheet      Topic = "factsheet"
	// TopicCleaningCatalog is a LionsBot vendor extension, not a VDA5050 topic.
	TopicCleaningCatalog Topic = "cleaningcatalog"
)

func ToTopic(endpoint Topic, version, manufacturer, serialNumber string) (string, error) {
	v, err := versionhelper.Parse(version)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(topicTpl, master, v, manufacturer, serialNumber, endpoint), nil
}
