package masterbus

import "github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/order"

type actionScope struct {
	order    bool
	instant  bool
	blocking order.BlockingType
}

// supportedActions is the vehicle's full action vocabulary; anything absent
// rejects (INVALID_ORDER_ACTION / INVALID_INSTANT_ACTION) so an inert action
// can never wedge isIdle.
var supportedActions = map[string]actionScope{
	"startCleaning": {order: true, blocking: order.BlockingTypeHard},
	// Dock/undock: instant for a quick dock, node-scoped as an order step.
	"startCharging": {order: true, instant: true, blocking: order.BlockingTypeHard},
	"stopCharging":  {order: true, instant: true, blocking: order.BlockingTypeHard},
	"cancelOrder":   {instant: true},
	"startPause":    {instant: true},
	"stopPause":     {instant: true},
	// Standard VDA action; forwarded to the executor (translated to hot_localize).
	"initPosition": {instant: true, blocking: order.BlockingTypeHard},
	// Served in masterbus, not forwarded to the executor (see handleInstantActions).
	"factsheetRequest": {instant: true},
}

func unsupportedOrderAction(o order.Order) (string, bool) {
	for _, n := range o.Nodes {
		for _, a := range n.Actions {
			if !supportedActions[a.ActionType].order || (supportedActions[a.ActionType].blocking != "" && a.BlockingType != supportedActions[a.ActionType].blocking) {
				return a.ActionType, true
			}
		}
	}
	for _, e := range o.Edges {
		for _, a := range e.Actions {
			if !supportedActions[a.ActionType].order {
				return a.ActionType, true
			}
		}
	}
	return "", false
}
