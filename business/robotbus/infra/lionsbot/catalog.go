package lionsbot

import (
	"sort"

	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/cleaningcatalog"
	"github.com/lionsbot-official/vda5050-adapter/business/robotbus"
)

// buildCatalog projects the active map's equalizer zones and docks into the
// vendor cleaning-catalog message the dashboard consumes. Zones are sorted by
// name for a stable retained payload.
func buildCatalog(mapID string, zones map[string]zoneEqualizer, docks []DockMarker) cleaningcatalog.Catalog {
	names := make([]string, 0, len(zones))
	for name := range zones {
		names = append(names, name)
	}
	sort.Strings(names)

	cat := cleaningcatalog.Catalog{MapID: mapID, Zones: make([]cleaningcatalog.Zone, 0, len(names))}
	for _, name := range names {
		z := zones[name]
		modes := make([]cleaningcatalog.Mode, 0, len(z.Modes))
		for _, m := range z.Modes {
			configs := make([]cleaningcatalog.Config, 0, len(m.Configs))
			for _, c := range m.Configs {
				configs = append(configs, cleaningcatalog.Config{
					Name: c.ConfigName, Value: c.Value, Min: c.Min, Max: c.Max, Step: c.Step,
				})
			}
			modes = append(modes, cleaningcatalog.Mode{Name: m.Name, Configs: configs})
		}
		cat.Zones = append(cat.Zones, cleaningcatalog.Zone{Name: z.Name, SelectedMode: z.SelectedModeName, Modes: modes})
	}

	cat.Docks = make([]string, 0, len(docks))
	for _, d := range docks {
		cat.Docks = append(cat.Docks, d.Name)
	}
	return cat
}

// equalizer-configs response (open-access RobotZoneEqualizersDetails): per-zone
// the available cleaning modes and which one is selected. Configs of the
// selected mode become the clean_start zone configs.
type zoneEqualizersResp struct {
	Zones []zoneEqualizer `json:"zoneAllEqualizerResponse"`
}

type zoneEqualizer struct {
	Name             string          `json:"name"`
	SelectedModeName string          `json:"selectedModeName"`
	Modes            []equalizerMode `json:"modes"`
}

type equalizerMode struct {
	Name    string            `json:"name"`
	Configs []equalizerConfig `json:"configs"`
}

type equalizerConfig struct {
	ConfigName string  `json:"configName"`
	Value      float64 `json:"value"`
	Min        float64 `json:"minimumValue"`
	Max        float64 `json:"maximumValue"`
	Step       float64 `json:"incrementalValue"`
}

// resolveCleanZones builds clean_start zone entries. A master-supplied
// CleaningMode/Configs override wins; otherwise the zone's default is resolved
// from the equalizer catalog. When neither an override nor a catalog entry is
// available the zone is sent bare (empty mode/configs) and the robot loads its
// own default — resolution is best-effort, never a reason to fail a clean.
// path_id 0 / boundary_mode false are the vendor defaults.
func resolveCleanZones(catalog map[string]zoneEqualizer, zones []robotbus.ZoneRef) []cleanStartZone {
	out := make([]cleanStartZone, 0, len(zones))
	for _, z := range zones {
		mode := z.CleaningMode
		configs := z.Configs
		if cat, ok := catalog[z.Name]; ok {
			if mode == "" {
				mode = cat.SelectedModeName
			}
			if configs == nil {
				configs = modeConfigs(cat, mode)
			}
		}
		if configs == nil {
			configs = map[string]float64{}
		}
		out = append(out, cleanStartZone{
			Name:         z.Name,
			PathID:       0,
			Configs:      configs,
			CleaningMode: mode,
			BoundaryMode: false,
		})
	}
	return out
}

// modeConfigs returns the named mode's config values from a zone's catalog
// entry, or an empty map if the mode is absent (stale/partial catalog).
func modeConfigs(cat zoneEqualizer, mode string) map[string]float64 {
	configs := map[string]float64{}
	for _, m := range cat.Modes {
		if m.Name == mode {
			for _, c := range m.Configs {
				configs[c.ConfigName] = c.Value
			}
		}
	}
	return configs
}
