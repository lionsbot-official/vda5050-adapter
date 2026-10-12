package masterbus

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/common"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/order"
	vda5050 "github.com/lionsbot-official/vda5050-adapter/reference/VDA5050-3.0.0"
	"github.com/santhosh-tekuri/jsonschema/v5"
)

var orderSchema = jsonschema.MustCompileString("order.json", vda5050.OrderSchema)
var instantActionsSchema = jsonschema.MustCompileString("instantActions.json", vda5050.InstantActionsSchema)

func validateMessage(msg []byte, schema *jsonschema.Schema, dst any) error {
	var doc any
	if err := json.Unmarshal(msg, &doc); err != nil {
		return err
	}
	if err := schema.Validate(doc); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(msg))
	decoder.DisallowUnknownFields()
	return decoder.Decode(dst)
}

func (b *Bus) validateHeader(h common.Header) error {
	if h.Version != b.version {
		return fmt.Errorf("version %q is not supported; expected %s", h.Version, b.version)
	}
	if h.Manufacturer != b.manufacturer || h.SerialNumber != b.serialNumber {
		return fmt.Errorf("message identity does not match this vehicle")
	}
	return nil
}
func (b *Bus) unsupportedOrderField(o order.Order) string {
	active := b.robot.store.Snapshot().MobileRobotPosition
	for _, n := range o.Nodes {
		if n.SequenceID > 0 && n.NodePosition == nil {
			return "nodePosition (required for navigation)"
		}
		if active != nil && n.NodePosition != nil && n.NodePosition.MapID != active.MapID {
			return "nodePosition.mapId"
		}
		for _, a := range n.Actions {
			if a.Retriable != nil && *a.Retriable {
				return "nodes.actions.retriable"
			}
			if supportedActions[a.ActionType].blocking != "" && (len(o.Nodes) != 1 || len(n.Actions) != 1) {
				return "nodes.actions (requires a single-node, single-action order)"
			}
		}
		if p := n.NodePosition; p != nil {
			if p.AllowedDeviationTheta != nil {
				return "nodePosition.allowedDeviationTheta"
			}
			if d := p.AllowedDeviationXY; d != nil && (d.A <= 0 || d.A != d.B) {
				return "nodePosition.allowedDeviationXY (only positive circular tolerances are supported)"
			}
		}
	}
	for _, e := range o.Edges {
		switch {
		case e.MaximumSpeed != nil:
			return "maxSpeed"
		case e.MaximumMobileRobotHeight != nil:
			return "maxHeight"
		case e.MinimumLoadHandlingDeviceHeight != nil:
			return "minHeight"
		case e.Orientation != nil || e.OrientationType != "":
			return "orientation"
		case e.Direction != "":
			return "direction"
		case e.ReachOrientationBeforeEntering:
			return "reachOrientationBeforeEntering"
		case e.MaxRotationSpeed != nil:
			return "maxRotationSpeed"
		case e.Trajectory != nil:
			return "trajectory"
		case e.Length != nil:
			return "length"
		case e.Corridor != nil:
			return "corridor"
		case len(e.Actions) > 0:
			return "edges.actions"
		}
	}
	return ""
}

func validateTopology(o order.Order) error {
	if o.Nodes[0].SequenceID%2 != 0 {
		return fmt.Errorf("node sequenceId must be even")
	}
	horizon := false
	ids := map[string]bool{}
	for i, n := range o.Nodes {
		if uint64(n.SequenceID) != uint64(o.Nodes[0].SequenceID)+uint64(i)*2 {
			return fmt.Errorf("nodes must be in consecutive sequence order")
		}
		if horizon && n.Released {
			return fmt.Errorf("released node follows the horizon")
		}
		if !n.Released {
			horizon = true
		}
		for _, a := range n.Actions {
			if ids[a.ActionID] {
				return fmt.Errorf("duplicate actionId %q", a.ActionID)
			}
			ids[a.ActionID] = true
		}
		if i == 0 {
			continue
		}
		e := o.Edges[i-1]
		if e.SequenceID != n.SequenceID-1 {
			return fmt.Errorf("edge %q must connect its adjacent nodes with the intermediate sequenceId", e.EdgeID)
		}
		if e.Released != n.Released {
			return fmt.Errorf("edge and destination node must have matching release status")
		}
	}
	return nil
}
