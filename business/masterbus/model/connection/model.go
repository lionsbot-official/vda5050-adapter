package connection

import (
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/common"
)

type Connection struct {
	common.Header
	ConnectionState ConnectionState `json:"connectionState"`
}
