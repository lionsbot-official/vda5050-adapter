package lionsbot

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/lionsbot-official/vda5050-adapter/business/common/vehiclestate"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/cleaningcatalog"
	"github.com/lionsbot-official/vda5050-adapter/business/robotbus"
)

var (
	_ robotbus.RobotClient    = (*Client)(nil)
	_ robotbus.RobotCommander = (*Client)(nil)
)

const (
	handshakeTimeout   = 15 * time.Second
	readLimit          = 8 << 20
	defaultBackoffBase = 1 * time.Second
	defaultBackoffMax  = 30 * time.Second
	refreshBuffer      = 60 * time.Second
	refreshFloor       = 5 * time.Second
)

// clock is the time source for the token-refresh schedule; injectable for tests.
type clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

type Client struct {
	cfg    Config
	tokens *TokenProvider
	maps   *mapsClient
	tr     translator
	clock  clock

	uuid string

	backoffBase time.Duration
	backoffMax  time.Duration

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	frames       chan []byte
	frameUpdates chan MapFrame
	events       chan robotbus.ExecutionEvent

	connMu  sync.Mutex
	active  map[*websocket.Conn]struct{}
	cmdConn *websocket.Conn // live status-socket conn; commands ride it
	writeMu sync.Mutex      // serializes all conn writes (gorilla: one writer)

	frameMu  sync.Mutex
	cmdFrame MapFrame                 // current frame for metres->pixels command conversion
	cmdDocks []DockMarker             // active map's docks for dock_start targeting
	cmdZones map[string]zoneEqualizer // active map's zone equalizers for clean_start resolution
	lastPX   float64                  // last raw pixel pose, for nearest-dock selection
	lastPY   float64
	havePose bool

	onCatalogChanged func(cleaningcatalog.Catalog) // fires when the active map's cleaning catalog changes
}

// SetCatalogChangedHook registers a callback invoked with the active map's
// cleaning catalog whenever it is (re)resolved; wired to the master bus publish
// from main.
func (c *Client) SetCatalogChangedHook(fn func(cleaningcatalog.Catalog)) {
	c.onCatalogChanged = fn
}

// emitCatalog builds the current cleaning catalog and hands it to the hook.
// No hook or no zones (nothing to show) is a no-op.
func (c *Client) emitCatalog() {
	if c.onCatalogChanged == nil {
		return
	}
	c.frameMu.Lock()
	mapID, zones, docks := c.cmdFrame.MapID, c.cmdZones, c.cmdDocks
	c.frameMu.Unlock()
	if len(zones) == 0 {
		return
	}
	c.onCatalogChanged(buildCatalog(mapID, zones, docks))
}

type clientMessage struct {
	RobotID      string  `json:"robot_id"`
	RobotType    string  `json:"robot_type,omitempty"`
	OperationCmd string  `json:"operation_cmd"`
	Timestamp    float64 `json:"time_stamp"`
	Content      any     `json:"content"`
}

type p2pStartContent struct {
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Heading float64 `json:"heading"`
	Name    string  `json:"name"`
	ID      string  `json:"id"`
}

func New(cfg Config) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	tokens := NewTokenProvider(cfg.LoginURL, Credentials{
		Email:      cfg.Email,
		Password:   cfg.password,
		AppName:    cfg.AppName,
		AppVersion: cfg.AppVersion,
	}, nil)
	return &Client{
		cfg:          cfg,
		tokens:       tokens,
		maps:         newMapsClient(cfg.APIBase, tokens, nil),
		tr:           translator{version: cfg.version, frame: cfg.Frame},
		cmdFrame:     cfg.Frame,
		clock:        realClock{},
		backoffBase:  defaultBackoffBase,
		backoffMax:   defaultBackoffMax,
		ctx:          ctx,
		cancel:       cancel,
		frames:       make(chan []byte),
		frameUpdates: make(chan MapFrame, 1),
		events:       make(chan robotbus.ExecutionEvent, 16),
		active:       make(map[*websocket.Conn]struct{}),
	}
}

// Connect logs in for the initial token, resolves the robot's UUID, and fetches
// the active map's frame. Sockets are dialled by Telemetry, which reconnects
// each independently with backoff, so a transient upstream outage is retried
// rather than fatal; login and robot-resolve failures are config-class → fatal.
func (c *Client) Connect() error {
	if len(c.urls()) == 0 {
		return fmt.Errorf("no upstream socket URLs configured")
	}
	if _, _, err := c.tokens.Login(c.ctx); err != nil {
		return fmt.Errorf("login: %w", err)
	}
	uuid, err := c.maps.resolveUUID(c.ctx, c.cfg.RobotID)
	if err != nil {
		return err
	}
	c.uuid = uuid
	slog.Info("upstream: robot resolved", "robotId", c.cfg.RobotID, "uuid", uuid)
	c.fetchFrame() // best-effort; without a frame, position is omitted
	return nil
}

// fetchFrame fetches the active map's frame and queues it for the forward loop,
// which owns the translator. Errors and unusable maps keep the current frame.
func (c *Client) fetchFrame() {
	frame, docks, ok, err := c.maps.currentFrame(c.ctx, c.uuid)
	if err != nil {
		slog.Warn("upstream: map frame fetch failed; keeping last-known", "err", err)
		return
	}
	if !ok {
		slog.Warn("upstream: no usable map frame; position omitted until one appears")
		return
	}
	// zone equalizers for clean_start resolution; best-effort — cleaning fails
	// with a clear reason if they're unavailable, but position/moves still work
	zones, zerr := c.maps.zoneEqualizers(c.ctx, frame.MapID, c.uuid)
	if zerr != nil {
		slog.Warn("upstream: equalizer configs fetch failed; cleaning zone resolution unavailable", "err", zerr)
	}
	slog.Info("upstream: map frame", "mapId", frame.MapID, "resolution", frame.Resolution, "height", frame.Height, "docks", len(docks), "zones", len(zones))
	frame.ThetaOffsetDeg = c.cfg.Frame.ThetaOffsetDeg
	c.frameMu.Lock()
	c.cmdFrame = frame
	c.cmdDocks = docks
	c.cmdZones = zones // tied to this map; nil (fetch failed) makes clean_start error clearly
	c.frameMu.Unlock()
	c.emitCatalog() // publish the map's cleaning options to the fleet side
	select {
	case c.frameUpdates <- frame:
	default: // an undelivered update is pending; the newest fetch wins next time
	}
}

// MoveTo drives the robot to a world-frame pose: metres/radians in, the
// vendor's pixel-frame point-to-point command out.
func (c *Client) MoveTo(x, y, theta float64) error {
	c.frameMu.Lock()
	frame := c.cmdFrame
	c.frameMu.Unlock()
	if frame.Resolution <= 0 {
		return fmt.Errorf("move: no usable map frame")
	}
	px := (x - frame.OriginX) / frame.Resolution
	py := frame.Height - (y-frame.OriginY)/frame.Resolution
	heading := theta - frame.ThetaOffsetDeg*math.Pi/180
	return c.sendCommand("p2p_start", p2pStartContent{X: px, Y: py, Heading: heading, Name: "Custom"})
}

type hotLocalizeContent struct {
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Heading float64 `json:"heading"`
}

// Relocalize resets the robot's pose on its current map: world-frame metres and
// radians in, the vendor's pixel-frame hot_localize out. Same conversion as a
// move; hot_localize carries no map id (the robot cannot switch maps itself).
func (c *Client) Relocalize(x, y, theta float64) error {
	c.frameMu.Lock()
	frame := c.cmdFrame
	c.frameMu.Unlock()
	if frame.Resolution <= 0 {
		return fmt.Errorf("relocalize: no usable map frame")
	}
	px := (x - frame.OriginX) / frame.Resolution
	py := frame.Height - (y-frame.OriginY)/frame.Resolution
	heading := theta - frame.ThetaOffsetDeg*math.Pi/180
	return c.sendCommand("hot_localize", hotLocalizeContent{X: px, Y: py, Heading: heading})
}

func (c *Client) StopMovement() error {
	return c.sendCommand("p2p_stop", map[string]any{"status": true})
}

func (c *Client) PauseMovement() error {
	return c.sendCommand("p2p_pause", map[string]any{"status": true})
}

func (c *Client) ResumeMovement() error {
	return c.sendCommand("p2p_continue", map[string]any{"status": true})
}

type dockStartContent struct {
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Heading float64 `json:"heading"`
	Name    string  `json:"name"`
}

// StartCharging sends the robot to its charging dock: the dock marker nearest
// to the last-known pose (docks and poses are both vendor pixels).
func (c *Client) StartCharging() error {
	c.frameMu.Lock()
	docks := c.cmdDocks
	px, py, have := c.lastPX, c.lastPY, c.havePose
	c.frameMu.Unlock()
	if len(docks) == 0 {
		return fmt.Errorf("start charging: no dock marker on active map")
	}
	dock := docks[0]
	if have {
		best := math.Inf(1)
		for _, d := range docks {
			if dist := math.Hypot(d.X-px, d.Y-py); dist < best {
				best, dock = dist, d
			}
		}
	}
	return c.sendCommand("dock_start", dockStartContent{X: dock.X, Y: dock.Y, Heading: dock.Heading, Name: dock.Name})
}

func (c *Client) StopCharging() error {
	return c.sendCommand("undock_start", map[string]any{"status": true})
}

func (c *Client) PauseDocking() error {
	return c.sendCommand("dock_pause", map[string]any{"status": true})
}

func (c *Client) ResumeDocking() error {
	return c.sendCommand("dock_continue", map[string]any{"status": true})
}

// AbortDocking stops an in-flight dock (dock_stop). Distinct from StopCharging,
// which undocks (undock_start).
func (c *Client) AbortDocking() error {
	return c.sendCommand("dock_stop", map[string]any{"status": true})
}

type cleanStartZone struct {
	Name         string             `json:"name"`
	PathID       int32              `json:"path_id"`
	Configs      map[string]float64 `json:"configs"`
	CleaningMode string             `json:"cleaning_mode"`
	BoundaryMode bool               `json:"boundary_mode"`
}

type cleanStartContent struct {
	Mode           int              `json:"mode"`
	Zones          []cleanStartZone `json:"zones"`
	BypassBlackout bool             `json:"bypass_blackout"`
	// home_poi deliberately absent: the robot stays put after cleaning
}

// StartCleaning starts a zone clean. Each zone's mode + configs come from a
// master override when present, else the active map's equalizer catalog (cached
// from the poll loop, so no blocking REST on the sequencer); with neither, the
// zone is sent bare and the robot loads its own default. mode 0 pending live
// grounding.
func (c *Client) StartCleaning(zones []robotbus.ZoneRef, bypassBlackout bool) error {
	c.frameMu.Lock()
	catalog := c.cmdZones
	c.frameMu.Unlock()

	zs := resolveCleanZones(catalog, zones)
	return c.sendCommand("clean_start", cleanStartContent{Zones: zs, BypassBlackout: bypassBlackout})
}

func (c *Client) StopCleaning() error {
	return c.sendCommand("clean_stop", map[string]any{"status": true})
}

func (c *Client) PauseCleaning() error {
	return c.sendCommand("clean_pause", map[string]any{"status": true})
}

type cleanContinueContent struct {
	Status         bool `json:"status"`
	BypassBlackout bool `json:"bypass_blackout"`
}

// ResumeCleaning resumes a paused clean. clean_continue carries only status +
// the original bypass_blackout: the robot resumes its own paused task (empty
// configs is a valid resume), and the flat clean_continue cannot carry the
// per-zone configs of a multi-zone clean anyway.
func (c *Client) ResumeCleaning(bypassBlackout bool) error {
	return c.sendCommand("clean_continue", cleanContinueContent{Status: true, BypassBlackout: bypassBlackout})
}

// stashPose keeps the latest raw pixel pose for nearest-dock selection.
func (c *Client) stashPose(raw []byte) {
	var env struct {
		OperationFb string `json:"operation_fb"`
		Content     struct {
			X float64 `json:"x"`
			Y float64 `json:"y"`
		} `json:"content"`
	}
	if json.Unmarshal(raw, &env) != nil || env.OperationFb != "robot_pose" {
		return
	}
	c.frameMu.Lock()
	c.lastPX, c.lastPY, c.havePose = env.Content.X, env.Content.Y, true
	c.frameMu.Unlock()
}

func (c *Client) sendCommand(cmd string, content any) error {
	c.connMu.Lock()
	conn := c.cmdConn
	c.connMu.Unlock()
	if conn == nil {
		return fmt.Errorf("%s: not connected", cmd)
	}
	msg := clientMessage{
		RobotID:      c.cfg.RobotID,
		RobotType:    c.cfg.RobotType,
		OperationCmd: cmd,
		Timestamp:    float64(time.Now().UnixMilli()) / 1000.0,
		Content:      content,
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := conn.WriteJSON(msg); err != nil {
		return fmt.Errorf("%s: %w", cmd, err)
	}
	return nil
}

func (c *Client) urls() []string {
	var urls []string
	if c.cfg.StatusURL != "" {
		urls = append(urls, c.cfg.StatusURL)
	}
	if c.cfg.PoseURL != "" {
		urls = append(urls, c.cfg.PoseURL)
	}
	return urls
}

// Telemetry runs one reconnecting reader per socket plus the token-refresh loop,
// all fanning raw frames into the single translator, and streams the snapshots.
func (c *Client) Telemetry() (<-chan vehiclestate.Telemetry, error) {
	out := make(chan vehiclestate.Telemetry)

	var readers sync.WaitGroup
	for _, url := range c.urls() {
		readers.Add(1)
		c.wg.Go(func() {
			defer readers.Done()
			c.runSocket(url)
		})
	}

	c.wg.Go(c.refreshLoop)
	c.wg.Go(c.pollMapLoop)

	// Close frames once every socket loop has stopped.
	c.wg.Go(func() {
		readers.Wait()
		close(c.frames)
	})

	c.wg.Go(func() {
		c.forward(out)
	})

	return out, nil
}

// runSocket keeps one socket connected: dial, subscribe, read until it drops,
// then reconnect with exponential backoff, until the context is cancelled.
// The status socket doubles as the command channel.
func (c *Client) runSocket(url string) {
	isCommandSocket := url == c.cfg.StatusURL
	backoff := c.backoffBase
	for c.ctx.Err() == nil {
		token, _ := c.tokens.Token()
		conn, err := c.dial(url, token)
		if err == nil {
			c.addActive(conn)
			if isCommandSocket {
				c.setCmdConn(conn)
			}
			if err = c.subscribe(conn); err == nil {
				backoff = c.backoffBase // reset after a clean connect + subscribe
				slog.Info("upstream: connected and subscribed", "url", url, "robotId", c.cfg.RobotID)
				if isCommandSocket {
					// best-effort: telemetry works without the path stream
					if err := c.requestPath(conn); err != nil {
						slog.Warn("upstream: request_path failed", "err", err)
					}
				}
				c.readConn(conn)
			}
			if isCommandSocket {
				c.setCmdConn(nil)
			}
			c.removeActive(conn)
			conn.Close()
		}
		if c.ctx.Err() != nil {
			return
		}
		if err != nil {
			slog.Warn("upstream: socket connect failed", "url", url, "err", err, "retryIn", backoff)
		} else {
			slog.Warn("upstream: socket closed; reconnecting", "url", url, "retryIn", backoff)
		}
		if !c.sleep(backoff) {
			return
		}
		backoff = nextBackoff(backoff, c.backoffMax)
	}
}

// refreshLoop re-logs-in before the token expires and drops the sockets so the
// reconnect carries the fresh token as subprotocol.
func (c *Client) refreshLoop() {
	for {
		_, exp := c.tokens.Token()
		select {
		case <-c.ctx.Done():
			return
		case <-c.clock.After(c.refreshWait(exp)):
		}
		if _, _, err := c.tokens.Login(c.ctx); err != nil {
			continue // keep the current token; retry next cycle
		}
		c.dropConns()
	}
}

// pollMapLoop re-checks the robot's active map on an interval so a map change
// (e.g. a floor switch) swaps the transform frame. Failures keep last-known.
func (c *Client) pollMapLoop() {
	interval := time.Duration(c.cfg.MapPollSeconds) * time.Second
	if interval <= 0 {
		return
	}
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-c.clock.After(interval):
		}
		c.fetchFrame()
	}
}

func (c *Client) refreshWait(exp time.Time) time.Duration {
	if exp.IsZero() {
		return refreshFloor
	}
	if d := exp.Sub(c.clock.Now()) - refreshBuffer; d > refreshFloor {
		return d
	}
	return refreshFloor
}

func (c *Client) setCmdConn(conn *websocket.Conn) {
	c.connMu.Lock()
	c.cmdConn = conn
	c.connMu.Unlock()
}

func (c *Client) addActive(conn *websocket.Conn) {
	c.connMu.Lock()
	c.active[conn] = struct{}{}
	c.connMu.Unlock()
}

func (c *Client) removeActive(conn *websocket.Conn) {
	c.connMu.Lock()
	delete(c.active, conn)
	c.connMu.Unlock()
}

func (c *Client) dropConns() {
	c.connMu.Lock()
	for conn := range c.active {
		conn.Close()
	}
	c.connMu.Unlock()
}

func (c *Client) readConn(conn *websocket.Conn) {
	done := make(chan struct{})
	c.wg.Go(func() {
		select {
		case <-c.ctx.Done():
			conn.Close() // unblock the blocking ReadMessage
		case <-done:
		}
	})
	defer close(done)

	first := true
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if first {
			first = false
			var env struct {
				OperationFb string `json:"operation_fb"`
			}
			_ = json.Unmarshal(data, &env)
			slog.Info("upstream: receiving frames", "firstOperationFb", env.OperationFb)
		}
		select {
		case c.frames <- data:
		case <-c.ctx.Done():
			return
		}
	}
}

// Events streams the outcomes of commanded operations.
func (c *Client) Events() (<-chan robotbus.ExecutionEvent, error) {
	return c.events, nil
}

// executionEvent recognizes end-status feed messages; ok=false means the frame
// is not an execution event and flows to the translator as usual.
func executionEvent(raw []byte) (robotbus.ExecutionEvent, bool) {
	var env struct {
		OperationFb string `json:"operation_fb"`
		Content     struct {
			Status bool   `json:"status"`
			Reason string `json:"reason"`
		} `json:"content"`
	}
	if json.Unmarshal(raw, &env) != nil {
		return robotbus.ExecutionEvent{}, false
	}
	reason := env.Content.Reason
	if !env.Content.Status && reason == "" {
		reason = "operation ended unsuccessfully"
	}
	switch env.OperationFb {
	case "p2p_end_status":
		return robotbus.ExecutionEvent{
			MoveCompleted: &robotbus.MoveCompleted{Success: env.Content.Status, Reason: reason},
		}, true
	case "dock_end_status":
		return robotbus.ExecutionEvent{
			StartChargingCompleted: &robotbus.ChargingCompleted{Success: env.Content.Status, Reason: reason},
		}, true
	case "undock_end_status":
		return robotbus.ExecutionEvent{
			StopChargingCompleted: &robotbus.ChargingCompleted{Success: env.Content.Status, Reason: reason},
		}, true
	case "clean_end_status":
		// bare {status:false} is a mid-mission zone-boundary marker, not an
		// end of mission; real end frames carry report fields
		if !env.Content.Status && bareStatusContent(raw) {
			return robotbus.ExecutionEvent{}, false
		}
		return robotbus.ExecutionEvent{
			CleanCompleted: &robotbus.CleanCompleted{Success: env.Content.Status, Reason: reason},
		}, true
	case "hot_localize":
		// distinct content shape: {percentage = localization score, success}
		var hl struct {
			Content struct {
				Percentage float64 `json:"percentage"`
				Success    bool    `json:"success"`
			} `json:"content"`
		}
		if json.Unmarshal(raw, &hl) != nil {
			return robotbus.ExecutionEvent{}, false
		}
		return robotbus.ExecutionEvent{
			RelocalizeCompleted: &robotbus.RelocalizeCompleted{Success: hl.Content.Success, Score: hl.Content.Percentage},
		}, true
	default:
		return robotbus.ExecutionEvent{}, false
	}
}

func bareStatusContent(raw []byte) bool {
	var probe struct {
		Content map[string]json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &probe) != nil {
		return false
	}
	if len(probe.Content) > 1 {
		return false
	}
	_, onlyStatus := probe.Content["status"]
	return len(probe.Content) == 0 || onlyStatus
}

// forward owns the translator: raw frames and map-frame updates are both applied
// here, on one goroutine, so the translator needs no locks.
func (c *Client) forward(out chan<- vehiclestate.Telemetry) {
	defer close(out)
	defer close(c.events)
	for {
		var t vehiclestate.Telemetry
		select {
		case raw, ok := <-c.frames:
			if !ok {
				return
			}
			if ev, isEvent := executionEvent(raw); isEvent {
				// raw end-status frames are rare and are the ground truth for
				// the decode; log them verbatim
				slog.Info("upstream: execution event", "raw", string(raw))
				c.tr.lastPath = nil // operation over; the projected path is void
				select {
				case c.events <- ev:
				case <-c.ctx.Done():
					return
				}
				continue
			}
			c.stashPose(raw)
			var err error
			t, err = c.tr.translate(raw)
			if err != nil {
				continue
			}
		case frame := <-c.frameUpdates:
			c.tr.frame = frame
			t = c.tr.snapshot() // re-emit so a held pose transforms immediately
		case <-c.ctx.Done():
			return
		}
		select {
		case out <- t:
		case <-c.ctx.Done():
			return
		}
	}
}

func (c *Client) sleep(d time.Duration) bool {
	select {
	case <-c.ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

func nextBackoff(cur, max time.Duration) time.Duration {
	cur *= 2
	if cur > max {
		cur = max
	}
	return cur
}

func (c *Client) dial(url, token string) (*websocket.Conn, error) {
	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = handshakeTimeout
	dialer.Subprotocols = []string{token}
	hdr := http.Header{}
	hdr.Set("Authorization", "Bearer "+token)
	hdr.Set("X-WSS-AUTH-BE-TOKEN", token)
	conn, resp, err := dialer.DialContext(c.ctx, url, hdr)
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("dial %s: %w (http %d)", url, err, resp.StatusCode)
		}
		return nil, fmt.Errorf("dial %s: %w", url, err)
	}
	conn.SetReadLimit(readLimit)
	return conn, nil
}

// requestPath asks the robot to stream its planned path (robot_path frames)
// while moving; sent once per command-socket connect.
func (c *Client) requestPath(conn *websocket.Conn) error {
	msg := clientMessage{
		RobotID:      c.cfg.RobotID,
		RobotType:    c.cfg.RobotType,
		OperationCmd: "request_path",
		Timestamp:    float64(time.Now().UnixMilli()) / 1000.0,
		Content:      map[string]any{"status": true},
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := conn.WriteJSON(msg); err != nil {
		return fmt.Errorf("request_path %s: %w", c.cfg.RobotID, err)
	}
	return nil
}

func (c *Client) subscribe(conn *websocket.Conn) error {
	sub := clientMessage{
		RobotID:      c.cfg.RobotID,
		RobotType:    c.cfg.RobotType,
		OperationCmd: "subscribe",
		Timestamp:    float64(time.Now().UnixMilli()) / 1000.0,
		Content:      map[string]any{},
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := conn.WriteJSON(sub); err != nil {
		return fmt.Errorf("subscribe %s: %w", c.cfg.RobotID, err)
	}
	return nil
}

// Disconnect cancels the internal context and waits for the socket goroutines
// to exit, so a stopped client leaves nothing running.
func (c *Client) Disconnect() {
	c.cancel()
	c.wg.Wait()
}
