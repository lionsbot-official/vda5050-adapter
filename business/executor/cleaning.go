package executor

import (
	"errors"
	"fmt"

	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/order"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/state"
	"github.com/lionsbot-official/vda5050-adapter/business/robotbus"
)

// startCleaning dispatches a startCleaning node action upstream and tracks it
// as the in-flight clean; any failure lands in the action's state, never a
// silent drop (a stuck WAITING action wedges isIdle forever).
func (e *Executor) startCleaning(a order.Action, cleaning **order.Action) {
	if *cleaning != nil {
		e.store.SetActionStatus(a.ActionID, state.ActionStatusFailed, "another operation is executing")
		return
	}
	zones, bypass, err := cleaningParams(a)
	if err != nil {
		e.store.SetActionStatus(a.ActionID, state.ActionStatusFailed, err.Error())
		return
	}
	if err := e.cmd.StartCleaning(zones, bypass); err != nil {
		e.store.SetActionStatus(a.ActionID, state.ActionStatusFailed, "clean dispatch failed: "+err.Error())
		return
	}
	e.store.SetActionStatus(a.ActionID, state.ActionStatusRunning, "")
	ac := a
	*cleaning = &ac
}

// completeCleaning resolves the in-flight clean from its end event; events
// with no matching in-flight clean are stale and ignored.
func (e *Executor) completeCleaning(done *robotbus.CleanCompleted, cleaning **order.Action) {
	if *cleaning == nil {
		return
	}
	if done.Success {
		e.store.SetActionStatus((*cleaning).ActionID, state.ActionStatusFinished, "")
	} else {
		e.store.SetActionStatus((*cleaning).ActionID, state.ActionStatusFailed, done.Reason)
	}
	*cleaning = nil
}

// cleaningParams extracts the startCleaning arguments from the action's
// parameters as they arrive off the wire. homePoi is deliberately ignored:
// the robot stays put after cleaning.
func cleaningParams(a order.Action) ([]robotbus.ZoneRef, bool, error) {
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
				if mode, ok := obj["cleaningMode"].(string); ok {
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
