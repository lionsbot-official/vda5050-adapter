package actionparams

import (
	"errors"
	"fmt"
	"math"

	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/order"
	"github.com/lionsbot-official/vda5050-adapter/business/robotbus"
)

// cleaningParams extracts the startCleaning arguments from the action's
// parameters as they arrive off the wire. homePoi is deliberately ignored:
// the robot stays put after cleaning.
func Cleaning(a order.Action) ([]robotbus.ZoneRef, bool, error) {
	if err := keys(a, "zones", "bypassBlackout", "homePoi"); err != nil {
		return nil, false, err
	}
	var zones []robotbus.ZoneRef
	bypass := false
	for _, p := range a.ActionParameters {
		switch p.Key {
		case "zones":
			raw, ok := p.Value.([]any)
			if !ok {
				return nil, false, fmt.Errorf("zones parameter is %T, want array", p.Value)
			}
			for i, z := range raw {
				obj, ok := z.(map[string]any)
				if !ok {
					return nil, false, fmt.Errorf("zones[%d] is %T, want object", i, z)
				}
				name, _ := obj["name"].(string)
				if name == "" {
					return nil, false, fmt.Errorf("zones[%d] has no name", i)
				}
				zone := robotbus.ZoneRef{Name: name}
				for key := range obj {
					if key != "name" && key != "cleaningMode" && key != "configs" {
						return nil, false, fmt.Errorf("unsupported zone field %q", key)
					}
				}
				if raw, present := obj["cleaningMode"]; present {
					mode, ok := raw.(string)
					if !ok {
						return nil, false, fmt.Errorf("cleaningMode must be string")
					}
					zone.CleaningMode = mode
				}
				if rawCfg, present := obj["configs"]; present {
					configs, err := zoneConfigs(rawCfg, i)
					if err != nil {
						return nil, false, err
					}
					if len(configs) > 0 {
						zone.Configs = configs
					}
				}
				zones = append(zones, zone)
			}
		case "homePoi":
			obj, ok := p.Value.(map[string]any)
			if !ok {
				return nil, false, fmt.Errorf("homePoi must be an object")
			}
			name, ok := obj["name"].(string)
			if !ok || name == "" || len(obj) != 1 {
				return nil, false, fmt.Errorf("homePoi requires only a nonempty name")
			}
		case "bypassBlackout":
			b, ok := p.Value.(bool)
			if !ok {
				return nil, false, fmt.Errorf("bypassBlackout parameter is %T, want bool", p.Value)
			}
			bypass = b
		}
	}
	if len(zones) == 0 {
		return nil, false, errors.New("startCleaning requires at least one zone")
	}
	return zones, bypass, nil
}

// zoneConfigs parses a zone's optional configs override: a JSON object of
// numeric equalizer values.
func zoneConfigs(raw any, i int) (map[string]float64, error) {
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("zones[%d].configs is %T, want object", i, raw)
	}
	configs := make(map[string]float64, len(obj))
	for k, v := range obj {
		f, ok := v.(float64)
		if !ok {
			return nil, fmt.Errorf("zones[%d].configs[%q] is %T, want number", i, k, v)
		}
		configs[k] = f
	}
	return configs, nil
}

// Pose is the world-frame pose an initializePosition action carries.
// LastNodeID is committed only after the robot confirms localization.
type Pose struct {
	X, Y, Theta float64
	MapID       string
	LastNodeID  string
}

// Relocalize extracts the initializePosition arguments (VDA5050_EN.md:714:
// x/y/theta metres+radians, mapId, lastNodeId) as they arrive off the wire.
// x/y/theta and a non-empty mapId are required; a zero coordinate is valid.
func Relocalize(a order.Action) (Pose, error) {
	if err := keys(a, "x", "y", "theta", "mapId", "lastNodeId"); err != nil {
		return Pose{}, err
	}
	var pose Pose
	var haveX, haveY, haveTheta, haveLastNode bool
	for _, p := range a.ActionParameters {
		switch p.Key {
		case "x":
			f, ok := p.Value.(float64)
			if !ok {
				return Pose{}, fmt.Errorf("x parameter is %T, want number", p.Value)
			}
			pose.X, haveX = f, true
		case "y":
			f, ok := p.Value.(float64)
			if !ok {
				return Pose{}, fmt.Errorf("y parameter is %T, want number", p.Value)
			}
			pose.Y, haveY = f, true
		case "theta":
			f, ok := p.Value.(float64)
			if !ok {
				return Pose{}, fmt.Errorf("theta parameter is %T, want number", p.Value)
			}
			pose.Theta, haveTheta = f, true
		case "mapId":
			s, ok := p.Value.(string)
			if !ok {
				return Pose{}, fmt.Errorf("mapId parameter is %T, want string", p.Value)
			}
			pose.MapID = s
		case "lastNodeId":
			s, ok := p.Value.(string)
			if !ok {
				return Pose{}, fmt.Errorf("lastNodeId parameter is %T, want string", p.Value)
			}
			pose.LastNodeID, haveLastNode = s, true
		}
	}
	if !haveX || !haveY || !haveTheta {
		return Pose{}, errors.New("initializePosition requires x, y and theta")
	}
	if pose.MapID == "" {
		return Pose{}, errors.New("initializePosition requires mapId")
	}
	if !haveLastNode {
		return Pose{}, errors.New("initializePosition requires lastNodeId")
	}
	if math.IsNaN(pose.X) || math.IsInf(pose.X, 0) || math.IsNaN(pose.Y) || math.IsInf(pose.Y, 0) || math.IsNaN(pose.Theta) || math.IsInf(pose.Theta, 0) || math.Abs(pose.Theta) > math.Pi {
		return Pose{}, errors.New("initializePosition requires finite coordinates and theta in [-pi, pi]")
	}
	return pose, nil
}

// Validate checks all parameters before an action can replace an accepted plan.
func Validate(a order.Action) error {
	switch a.ActionType {
	case "startCleaning":
		_, _, err := Cleaning(a)
		return err
	case "cancelOrder":
		if err := keys(a, "orderId"); err != nil {
			return err
		}
		for _, p := range a.ActionParameters {
			if _, ok := p.Value.(string); !ok {
				return errors.New("orderId must be a string")
			}
		}
		return nil
	case "initializePosition":
		_, err := Relocalize(a)
		return err
	default:
		return keys(a)
	}
}
func keys(a order.Action, allowed ...string) error {
	seen := map[string]bool{}
	for _, p := range a.ActionParameters {
		if seen[p.Key] {
			return fmt.Errorf("duplicate action parameter %q", p.Key)
		}
		seen[p.Key] = true
		supported := false
		for _, key := range allowed {
			if p.Key == key {
				supported = true
				break
			}
		}
		if !supported {
			return fmt.Errorf("unsupported action parameter %q", p.Key)
		}
	}
	return nil
}
