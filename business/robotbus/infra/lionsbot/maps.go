package lionsbot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// mapsClient fetches the robot's identity and its active map's frame from the
// REST API. Best-effort: an unusable map yields (zero, false, nil), not an error.
type mapsClient struct {
	base   string
	tokens *TokenProvider
	client *http.Client

	cachedMapID string
	cachedMod   string
	cachedFrame MapFrame
	cachedDocks []DockMarker

	cachedZonesMapID string
	cachedZones      map[string]zoneEqualizer
}

// DockMarker is a charging dock on the active map, in the vendor pixel frame.
type DockMarker struct {
	Name    string
	X, Y    float64 // pixels
	Heading float64 // radians
}

func newMapsClient(base string, tokens *TokenProvider, client *http.Client) *mapsClient {
	if client == nil {
		client = http.DefaultClient
	}
	return &mapsClient{base: strings.TrimRight(base, "/"), tokens: tokens, client: client}
}

// zoneEqualizers fetches the active map's per-zone cleaning equalizers, keyed
// by zone name for clean_start resolution. Cached per map: equalizers rarely
// change and cleaning is infrequent, so a repeat for the same map is a cache
// hit (a map switch refetches).
func (m *mapsClient) zoneEqualizers(ctx context.Context, mapID, uuid string) (map[string]zoneEqualizer, error) {
	if m.cachedZonesMapID == mapID && m.cachedZones != nil {
		return m.cachedZones, nil
	}
	var resp zoneEqualizersResp
	url := m.base + "/openapi/v2/robot/map/" + mapID + "/equalizer-configs?robotId=" + uuid
	if err := m.getJSON(ctx, url, true, &resp); err != nil {
		return nil, fmt.Errorf("fetch equalizer configs for map %q: %w", mapID, err)
	}
	catalog := make(map[string]zoneEqualizer, len(resp.Zones))
	for _, z := range resp.Zones {
		catalog[z.Name] = z
	}
	m.cachedZonesMapID, m.cachedZones = mapID, catalog
	return catalog, nil
}

func (m *mapsClient) resolveUUID(ctx context.Context, encodingID string) (string, error) {
	var view struct {
		ID string `json:"id"`
	}
	if err := m.getJSON(ctx, m.base+"/openapi/v2/robot/by-encoding-id/"+encodingID, true, &view); err != nil {
		return "", fmt.Errorf("resolve robot %q: %w", encodingID, err)
	}
	if view.ID == "" {
		return "", fmt.Errorf("resolve robot %q: response has no id", encodingID)
	}
	return view.ID, nil
}

type workSiteMap struct {
	ID       string          `json:"id"`
	JSONLink string          `json:"jsonLink"`
	Modified json.RawMessage `json:"lastModifiedDate"` // string or number; raw bytes are the cache key
	Markers  []rawMarker     `json:"markers"`
}

type rawMarker struct {
	Name        string  `json:"name"`
	Type        string  `json:"type"`
	Angle       float64 `json:"angle"`
	Coordinates string  `json:"coordinates"` // whitespace-separated pixel pairs "x y ..."
}

// currentFrame returns the active map's frame and its dock markers. ok=false
// means no usable frame (unknown map, no info link, unusable meta_data) — the
// caller keeps last-known.
func (m *mapsClient) currentFrame(ctx context.Context, uuid string) (MapFrame, []DockMarker, bool, error) {
	var resp struct {
		CurrentMapID string        `json:"currentMapId"`
		WorkSiteMaps []workSiteMap `json:"workSiteMaps"`
	}
	if err := m.getJSON(ctx, m.base+"/openapi/v2/robot/"+uuid+"/map?requireMarkers=true", true, &resp); err != nil {
		return MapFrame{}, nil, false, fmt.Errorf("fetch maps for %q: %w", uuid, err)
	}

	var current *workSiteMap
	for i := range resp.WorkSiteMaps {
		if resp.WorkSiteMaps[i].ID == resp.CurrentMapID {
			current = &resp.WorkSiteMaps[i]
			break
		}
	}
	if current == nil || current.JSONLink == "" {
		return MapFrame{}, nil, false, nil
	}

	mod := string(current.Modified)
	if m.cachedMapID == current.ID && m.cachedMod == mod {
		return m.cachedFrame, m.cachedDocks, true, nil
	}

	link := current.JSONLink
	withAuth := strings.HasPrefix(link, "/")
	if withAuth {
		link = m.base + link
	}
	var info struct {
		MetaData struct {
			Height     float64 `json:"height"`
			OX         float64 `json:"ox"`
			OY         float64 `json:"oy"`
			Resolution float64 `json:"resolution"`
		} `json:"meta_data"`
	}
	if err := m.getJSON(ctx, link, withAuth, &info); err != nil {
		return MapFrame{}, nil, false, fmt.Errorf("fetch map info %q: %w", current.ID, err)
	}
	if info.MetaData.Resolution <= 0 {
		return MapFrame{}, nil, false, nil
	}

	frame := MapFrame{
		MapID:      current.ID,
		Resolution: info.MetaData.Resolution,
		OriginX:    info.MetaData.OX,
		OriginY:    info.MetaData.OY,
		Height:     info.MetaData.Height,
	}
	docks := dockMarkers(current.Markers)
	m.cachedMapID, m.cachedMod, m.cachedFrame, m.cachedDocks = current.ID, mod, frame, docks
	return frame, docks, true, nil
}

func dockMarkers(markers []rawMarker) []DockMarker {
	var docks []DockMarker
	for _, raw := range markers {
		if raw.Type != "dock" {
			continue
		}
		fields := strings.Fields(raw.Coordinates)
		if len(fields) < 2 {
			continue
		}
		x, errX := strconv.ParseFloat(fields[0], 64)
		y, errY := strconv.ParseFloat(fields[1], 64)
		if errX != nil || errY != nil {
			continue
		}
		docks = append(docks, DockMarker{Name: raw.Name, X: x, Y: y, Heading: raw.Angle})
	}
	return docks
}

// getJSON GETs url and decodes the body. withAuth attaches the Bearer token;
// presigned (absolute) links must be fetched without it.
func (m *mapsClient) getJSON(ctx context.Context, url string, withAuth bool, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if withAuth {
		token, _ := m.tokens.Token()
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet(body, 300))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode %s: %w", url, err)
	}
	return nil
}
