// Package cleaningcatalog is the vendor cleaning-metadata message published
// (retained) on the cleaningcatalog topic: the active map's cleanable zones
// with their equalizer modes/configs and the map's docks. It is a LionsBot
// extension, not a VDA5050 message — VDA5050 has no notion of cleaning zones.
package cleaningcatalog

type Catalog struct {
	MapID string   `json:"mapId"`
	Zones []Zone   `json:"zones"`
	Docks []string `json:"docks"`
}

type Zone struct {
	Name         string `json:"name"`
	SelectedMode string `json:"selectedMode"`
	Modes        []Mode `json:"modes"`
}

type Mode struct {
	Name    string   `json:"name"`
	Configs []Config `json:"configs"`
}

// Config is one equalizer setting with the bounds a slider needs.
type Config struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
	Min   float64 `json:"min"`
	Max   float64 `json:"max"`
	Step  float64 `json:"step"`
}
