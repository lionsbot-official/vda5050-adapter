package instantactions

import (
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/common"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/order"
)

type InstantActions struct {
	common.Header
	Actions []order.Action `json:"actions"`
}
