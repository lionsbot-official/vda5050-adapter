package lionsbot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/cleaningcatalog"
	"github.com/lionsbot-official/vda5050-adapter/business/robotbus"
)

const equalizerBody = `{
  "isSynced": true,
  "robotId": "uuid-1",
  "zoneAllEqualizerResponse": [
    {"name": "Lobby", "selectedModeName": "Standard", "modes": [
      {"name": "Eco", "configs": [{"configName": "water", "value": 1}]},
      {"name": "Standard", "configs": [{"configName": "water", "value": 3}, {"configName": "brush", "value": 80}]}
    ]}
  ]
}`

func TestBuildCatalog(t *testing.T) {
	zones := map[string]zoneEqualizer{
		"Lobby": {Name: "Lobby", SelectedModeName: "Standard", Modes: []equalizerMode{
			{Name: "Eco", Configs: []equalizerConfig{{ConfigName: "water", Value: 1, Min: 0, Max: 5, Step: 1}}},
			{Name: "Standard", Configs: []equalizerConfig{{ConfigName: "water", Value: 3, Min: 0, Max: 5, Step: 1}}},
		}},
		"Aisle": {Name: "Aisle", SelectedModeName: "Deep", Modes: []equalizerMode{
			{Name: "Deep", Configs: []equalizerConfig{{ConfigName: "brush", Value: 80, Min: 0, Max: 100, Step: 10}}},
		}},
	}
	docks := []DockMarker{{Name: "dock-A"}, {Name: "dock-B"}}

	cat := buildCatalog("map-1", zones, docks)

	if cat.MapID != "map-1" {
		t.Errorf("mapId = %q, want map-1", cat.MapID)
	}
	// zones sorted by name for a stable payload
	if len(cat.Zones) != 2 || cat.Zones[0].Name != "Aisle" || cat.Zones[1].Name != "Lobby" {
		t.Fatalf("zones = %+v, want [Aisle Lobby] sorted", cat.Zones)
	}
	lobby := cat.Zones[1]
	if lobby.SelectedMode != "Standard" || len(lobby.Modes) != 2 {
		t.Fatalf("Lobby = %+v, want Standard selected with 2 modes", lobby)
	}
	std := lobby.Modes[1]
	if std.Name != "Standard" || len(std.Configs) != 1 {
		t.Fatalf("Lobby mode[1] = %+v, want Standard/1 config", std)
	}
	c := std.Configs[0]
	if c.Name != "water" || c.Value != 3 || c.Min != 0 || c.Max != 5 || c.Step != 1 {
		t.Errorf("config = %+v, want water value=3 min=0 max=5 step=1 (slider bounds)", c)
	}
	if !reflect.DeepEqual(cat.Docks, []string{"dock-A", "dock-B"}) {
		t.Errorf("docks = %v, want [dock-A dock-B]", cat.Docks)
	}
}

func TestClient_EmitCatalog(t *testing.T) {
	newClient := func() *Client { return New(Config{version: codeV1}) }

	t.Run("invokes the hook with the built catalog when zones are present", func(t *testing.T) {
		c := newClient()
		c.cmdFrame = MapFrame{MapID: "map-1"}
		c.cmdZones = map[string]zoneEqualizer{
			"Lobby": {Name: "Lobby", SelectedModeName: "Std", Modes: []equalizerMode{
				{Name: "Std", Configs: []equalizerConfig{{ConfigName: "water", Value: 2, Max: 5}}},
			}},
		}
		c.cmdDocks = []DockMarker{{Name: "dock-A"}}

		got := make(chan cleaningcatalog.Catalog, 1)
		c.SetCatalogChangedHook(func(cat cleaningcatalog.Catalog) { got <- cat })
		c.emitCatalog()

		select {
		case cat := <-got:
			if cat.MapID != "map-1" || len(cat.Zones) != 1 || cat.Zones[0].Name != "Lobby" || len(cat.Docks) != 1 {
				t.Fatalf("catalog = %+v, want map-1 / Lobby / one dock", cat)
			}
		default:
			t.Fatal("catalog hook not invoked")
		}
	})

	t.Run("no hook is a no-op", func(t *testing.T) {
		c := newClient()
		c.cmdZones = map[string]zoneEqualizer{"Lobby": {Name: "Lobby"}}
		c.emitCatalog() // must not panic
	})

	t.Run("no zones does not emit (nothing to show)", func(t *testing.T) {
		c := newClient()
		c.cmdFrame = MapFrame{MapID: "map-1"}
		called := false
		c.SetCatalogChangedHook(func(cleaningcatalog.Catalog) { called = true })
		c.emitCatalog()
		if called {
			t.Fatal("hook invoked with no zones; want no emit")
		}
	})
}

func TestMapsClient_ZoneEqualizers(t *testing.T) {
	var gotPath, gotQuery, gotAuth string
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		gotPath, gotQuery, gotAuth = r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(equalizerBody))
	}))
	defer srv.Close()

	m := newMapsClient(srv.URL, newTestTokens(t, "tok"), srv.Client())
	catalog, err := m.zoneEqualizers(context.Background(), "map-1", "uuid-1")
	if err != nil {
		t.Fatalf("zoneEqualizers: %v", err)
	}
	if want := "/openapi/v2/robot/map/map-1/equalizer-configs"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if gotQuery != "robotId=uuid-1" {
		t.Errorf("query = %q, want robotId=uuid-1", gotQuery)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("auth = %q, want Bearer tok", gotAuth)
	}
	z, ok := catalog["Lobby"]
	if !ok || z.SelectedModeName != "Standard" || len(z.Modes) != 2 {
		t.Fatalf("catalog[Lobby] = %+v, want Standard with 2 modes", z)
	}

	// same map: cache hit, no second request
	if _, err := m.zoneEqualizers(context.Background(), "map-1", "uuid-1"); err != nil {
		t.Fatalf("zoneEqualizers (cached): %v", err)
	}
	if hits.Load() != 1 {
		t.Errorf("server hits = %d, want 1 (second call must hit the cache)", hits.Load())
	}
}

func TestResolveCleanZones(t *testing.T) {
	// two zones, each with two modes; the selected mode's configs are the ones
	// that must land in clean_start when the master supplies no override.
	catalog := map[string]zoneEqualizer{
		"Zone A": {
			Name:             "Zone A",
			SelectedModeName: "Standard",
			Modes: []equalizerMode{
				{Name: "Eco", Configs: []equalizerConfig{{ConfigName: "water", Value: 1}}},
				{Name: "Standard", Configs: []equalizerConfig{{ConfigName: "water", Value: 3}, {ConfigName: "brush", Value: 80}}},
			},
		},
		"Zone B": {
			Name:             "Zone B",
			SelectedModeName: "Deep",
			Modes: []equalizerMode{
				{Name: "Deep", Configs: []equalizerConfig{{ConfigName: "water", Value: 5}}},
			},
		},
	}
	ref := func(name, mode string, configs map[string]float64) robotbus.ZoneRef {
		return robotbus.ZoneRef{Name: name, CleaningMode: mode, Configs: configs}
	}

	t.Run("no override resolves the selected mode's configs, in requested order", func(t *testing.T) {
		got := resolveCleanZones(catalog, []robotbus.ZoneRef{ref("Zone A", "", nil), ref("Zone B", "", nil)})
		want := []cleanStartZone{
			{Name: "Zone A", PathID: 0, CleaningMode: "Standard", BoundaryMode: false,
				Configs: map[string]float64{"water": 3, "brush": 80}},
			{Name: "Zone B", PathID: 0, CleaningMode: "Deep", BoundaryMode: false,
				Configs: map[string]float64{"water": 5}},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("resolveCleanZones = %+v, want %+v", got, want)
		}
	})

	t.Run("configs override is used verbatim, mode defaults from catalog", func(t *testing.T) {
		got := resolveCleanZones(catalog, []robotbus.ZoneRef{ref("Zone A", "", map[string]float64{"water": 9})})
		if len(got) != 1 || got[0].CleaningMode != "Standard" ||
			!reflect.DeepEqual(got[0].Configs, map[string]float64{"water": 9}) {
			t.Fatalf("resolveCleanZones = %+v, want water=9 with Standard mode", got)
		}
	})

	t.Run("mode-only override pulls that mode's configs from the catalog", func(t *testing.T) {
		got := resolveCleanZones(catalog, []robotbus.ZoneRef{ref("Zone A", "Eco", nil)})
		if len(got) != 1 || got[0].CleaningMode != "Eco" ||
			!reflect.DeepEqual(got[0].Configs, map[string]float64{"water": 1}) {
			t.Fatalf("resolveCleanZones = %+v, want Eco mode with water=1", got)
		}
	})

	t.Run("no override and no catalog sends a bare zone (robot loads its defaults)", func(t *testing.T) {
		got := resolveCleanZones(nil, []robotbus.ZoneRef{ref("Nowhere", "", nil)})
		if len(got) != 1 || got[0].Name != "Nowhere" || got[0].CleaningMode != "" || len(got[0].Configs) != 0 {
			t.Fatalf("resolveCleanZones = %+v, want a bare Nowhere zone", got)
		}
	})

	t.Run("override survives even without a catalog entry", func(t *testing.T) {
		got := resolveCleanZones(nil, []robotbus.ZoneRef{ref("Nowhere", "Turbo", map[string]float64{"water": 7})})
		if len(got) != 1 || got[0].CleaningMode != "Turbo" ||
			!reflect.DeepEqual(got[0].Configs, map[string]float64{"water": 7}) {
			t.Fatalf("resolveCleanZones = %+v, want Turbo/water=7 kept", got)
		}
	})
}
