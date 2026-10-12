package lionsbot

import (
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/lionsbot-official/vda5050-adapter/business/common/vehiclestate"
	"github.com/lionsbot-official/vda5050-adapter/business/robotbus"
)

func fakeLogin(t *testing.T, token string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"` + token + `","tokenExpiryIsoUtcTime":"2026-07-16T10:30:00Z"}`))
	}))
}

func wsURL(s *httptest.Server) string {
	return "ws" + strings.TrimPrefix(s.URL, "http")
}

// fakeFeed upgrades, reads the subscribe frame, sends one frame, then holds the
// connection open until the client disconnects.
func fakeFeed(t *testing.T, frame string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgradeEcho(w, r)
		if err != nil {
			return
		}
		defer conn.Close()
		var sub map[string]any
		if err := conn.ReadJSON(&sub); err != nil {
			return
		}
		_ = conn.WriteMessage(websocket.TextMessage, []byte(frame))
		_, _, _ = conn.ReadMessage()
	}))
}

// fakeRest serves the robot resolve plus a /map response with no usable frame.
func fakeRest(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "by-encoding-id") {
			_, _ = w.Write([]byte(`{"id":"uuid-1"}`))
			return
		}
		_, _ = w.Write([]byte(`{"currentMapId":"","workSiteMaps":[]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

var upgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

func upgradeEcho(w http.ResponseWriter, r *http.Request) (*websocket.Conn, error) {
	var respHeader http.Header
	if p := r.Header.Get("Sec-Websocket-Protocol"); p != "" {
		respHeader = http.Header{"Sec-Websocket-Protocol": {p}}
	}
	return upgrader.Upgrade(w, r, respHeader)
}

func TestClient_DialsSubscribesAndTranslates(t *testing.T) {
	const token = "access-jwt-xyz"
	login := fakeLogin(t, token)
	defer login.Close()

	type handshake struct{ subproto, authz, beToken string }
	var hs handshake
	var sub map[string]any
	subGot := make(chan struct{})

	ws := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hs = handshake{
			subproto: r.Header.Get("Sec-Websocket-Protocol"),
			authz:    r.Header.Get("Authorization"),
			beToken:  r.Header.Get("X-WSS-AUTH-BE-TOKEN"),
		}
		conn, err := upgradeEcho(w, r)
		if err != nil {
			return
		}
		defer conn.Close()
		if err := conn.ReadJSON(&sub); err != nil {
			return
		}
		close(subGot)
		_ = conn.WriteMessage(websocket.TextMessage,
			[]byte(`{"operation_fb":"touchscreen_robot_status","content":{"battery_info":{"soc":42.5}}}`))
		_, _, _ = conn.ReadMessage() // keep the conn open until the client disconnects
	}))
	defer ws.Close()

	cfg := Config{
		LoginURL: login.URL, StatusURL: wsURL(ws), APIBase: fakeRest(t).URL,
		RobotID: "robot-001", RobotType: "R3",
		version: codeV1, Frame: MapFrame{MapID: "map-1", Resolution: 0.05},
	}
	client := New(cfg)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Disconnect()

	out, err := client.Telemetry()
	if err != nil {
		t.Fatalf("Telemetry: %v", err)
	}

	// subscribe frame received (also a sync point for the shared capture vars)
	select {
	case <-subGot:
	case <-time.After(2 * time.Second):
		t.Fatal("no subscribe frame received")
	}
	if hs.subproto != token {
		t.Errorf("Sec-Websocket-Protocol = %q, want %q", hs.subproto, token)
	}
	if hs.authz != "Bearer "+token {
		t.Errorf("Authorization = %q, want %q", hs.authz, "Bearer "+token)
	}
	if hs.beToken != token {
		t.Errorf("X-WSS-AUTH-BE-TOKEN = %q, want %q", hs.beToken, token)
	}
	if sub["operation_cmd"] != "subscribe" {
		t.Errorf("operation_cmd = %v, want subscribe", sub["operation_cmd"])
	}
	if sub["robot_id"] != "robot-001" {
		t.Errorf("robot_id = %v, want robot-001", sub["robot_id"])
	}
	if sub["robot_type"] != "R3" {
		t.Errorf("robot_type = %v, want R3", sub["robot_type"])
	}

	select {
	case got := <-out:
		if got.BatteryState == nil || got.BatteryState.BatteryCharge != 42.5 {
			t.Errorf("SOC = %+v, want 42.5", got.BatteryState)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no telemetry received")
	}
}

func TestClient_TwoSocketsFanInAndAggregate(t *testing.T) {
	login := fakeLogin(t, "tok")
	defer login.Close()

	statusWS := fakeFeed(t, `{"operation_fb":"touchscreen_robot_status","content":{"battery_info":{"soc":55},"localised":true,"state":2,"status":"Moving"}}`)
	defer statusWS.Close()
	poseWS := fakeFeed(t, `{"operation_fb":"robot_pose","content":{"x":0,"y":5402,"heading":0}}`)
	defer poseWS.Close()

	cfg := Config{
		LoginURL: login.URL, StatusURL: wsURL(statusWS), PoseURL: wsURL(poseWS), APIBase: fakeRest(t).URL,
		RobotID: "robot-001", RobotType: "R3",
		version: codeV1,
		Frame:   MapFrame{MapID: "map-1", Resolution: 0.05, OriginX: -165.4, OriginY: -158.07, Height: 5402},
	}
	client := New(cfg)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Disconnect()

	out, err := client.Telemetry()
	if err != nil {
		t.Fatalf("Telemetry: %v", err)
	}

	// Each emission is a full snapshot of the aggregated last-known state; read
	// until one carries BOTH the status-derived and pose-derived fields, proving
	// both sockets fanned into the one translator (order-independent).
	var got vehiclestate.Telemetry
	timeout := time.After(3 * time.Second)
	for got.BatteryState == nil || got.AGVPosition == nil {
		select {
		case got = <-out:
		case <-timeout:
			t.Fatalf("no aggregated snapshot (both feeds); last = %+v", got)
		}
	}

	if got.BatteryState.BatteryCharge != 55 {
		t.Errorf("SOC = %v, want 55 (from status feed)", got.BatteryState.BatteryCharge)
	}
	if got.AGVPosition.X != -165.4 || got.AGVPosition.Y != -158.07 {
		t.Errorf("position = (%v,%v), want (-165.4,-158.07) (from pose feed via lbmap transform)",
			got.AGVPosition.X, got.AGVPosition.Y)
	}
	if !got.AGVPosition.PositionInitialized {
		t.Error("PositionInitialized = false, want true (crossed from the status feed)")
	}
}

func TestClient_ReconnectsAndResubscribes(t *testing.T) {
	login := fakeLogin(t, "tok")
	defer login.Close()

	var subs atomic.Int32
	ws := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgradeEcho(w, r)
		if err != nil {
			return
		}
		defer conn.Close()
		var sub map[string]any
		if err := conn.ReadJSON(&sub); err != nil {
			return
		}
		n := subs.Add(1)
		_ = conn.WriteMessage(websocket.TextMessage,
			[]byte(`{"operation_fb":"touchscreen_robot_status","content":{"battery_info":{"soc":42.5}}}`))
		if n == 1 {
			return // drop the first connection to force a reconnect
		}
		_, _, _ = conn.ReadMessage() // hold later connections open
	}))
	defer ws.Close()

	cfg := Config{
		LoginURL: login.URL, StatusURL: wsURL(ws), APIBase: fakeRest(t).URL,
		RobotID: "robot-001", RobotType: "R3",
		version: codeV1, Frame: MapFrame{Resolution: 0.05},
	}
	client := New(cfg)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Disconnect()

	out, err := client.Telemetry()
	if err != nil {
		t.Fatalf("Telemetry: %v", err)
	}

	// One frame per connection; the first connection drops, so receiving two
	// frames proves a reconnect happened. The channel must not close meanwhile.
	got := 0
	timeout := time.After(3 * time.Second)
	for got < 2 {
		select {
		case _, ok := <-out:
			if !ok {
				t.Fatalf("telemetry channel closed after %d frame(s) — driver did not reconnect", got)
			}
			got++
		case <-timeout:
			t.Fatalf("received %d frame(s), want 2 (subscribes=%d) — reconnect/resubscribe failed", got, subs.Load())
		}
	}
	if n := subs.Load(); n < 2 {
		t.Errorf("subscribe count = %d, want >= 2 (resubscribe on every connect)", n)
	}
}

func TestClient_ConnectResolveFailureIsFatal(t *testing.T) {
	login := fakeLogin(t, "tok")
	defer login.Close()
	rest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"MissingRobotByEncodingId"}`))
	}))
	defer rest.Close()

	cfg := Config{
		LoginURL: login.URL, StatusURL: "wss://unused", APIBase: rest.URL,
		RobotID: "robot-001", version: codeV1,
	}
	if err := New(cfg).Connect(); err == nil {
		t.Fatal("Connect = nil, want fatal error when the robot id cannot be resolved")
	}
}

func TestClient_FrameFetchedAtConnectTransformsPose(t *testing.T) {
	login := fakeLogin(t, "tok")
	defer login.Close()

	rest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "by-encoding-id"):
			_, _ = w.Write([]byte(`{"id":"uuid-1"}`))
		case strings.HasSuffix(r.URL.Path, "/map"):
			_, _ = w.Write([]byte(`{"currentMapId":"map-a","workSiteMaps":[{"id":"map-a","jsonLink":"/info/map-a.json","lastModifiedDate":"v1"}]}`))
		default:
			_, _ = w.Write([]byte(infoJSON))
		}
	}))
	defer rest.Close()

	poseWS := fakeFeed(t, `{"operation_fb":"robot_pose","content":{"x":0,"y":5402,"heading":0}}`)
	defer poseWS.Close()

	// No static frame: map params must come from the fetch at Connect.
	cfg := Config{
		LoginURL: login.URL, PoseURL: wsURL(poseWS), APIBase: rest.URL,
		RobotID: "robot-001", RobotType: "R3", version: codeV1,
	}
	client := New(cfg)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Disconnect()
	out, err := client.Telemetry()
	if err != nil {
		t.Fatalf("Telemetry: %v", err)
	}

	const eps = 1e-6
	timeout := time.After(3 * time.Second)
	for {
		select {
		case got := <-out:
			p := got.AGVPosition
			if p == nil {
				continue // pre-frame snapshot; position omitted
			}
			if math.Abs(p.X-(-165.4)) > eps || math.Abs(p.Y-(-158.07)) > eps || p.MapID != "map-a" {
				t.Fatalf("position = (%v,%v,%q), want (-165.4,-158.07,map-a) via fetched frame", p.X, p.Y, p.MapID)
			}
			return
		case <-timeout:
			t.Fatal("no transformed position — fetched frame never reached the translator")
		}
	}
}

// fakeClock routes waits by length: the token refresh waits ~years (expiry vs
// the pinned Now), the map poll waits seconds — so long and short waits get
// separate trigger channels and tests fire exactly the loop they mean to.
// fakeCommandWS upgrades, reads the subscribe frame (signalled on subscribed),
// then forwards every later frame — the command channel under test.
func fakeCommandWS(t *testing.T) (*httptest.Server, chan struct{}, chan map[string]any) {
	t.Helper()
	subscribed := make(chan struct{})
	cmds := make(chan map[string]any, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgradeEcho(w, r)
		if err != nil {
			return
		}
		defer conn.Close()
		var sub map[string]any
		if err := conn.ReadJSON(&sub); err != nil {
			return
		}
		close(subscribed)
		for {
			var m map[string]any
			if err := conn.ReadJSON(&m); err != nil {
				return
			}
			if m["operation_cmd"] == "request_path" {
				continue // connect handshake, not a command under test
			}
			cmds <- m
		}
	}))
	t.Cleanup(srv.Close)
	return srv, subscribed, cmds
}

func TestClient_MoveToSendsP2PStart(t *testing.T) {
	login := fakeLogin(t, "tok")
	defer login.Close()
	ws, subscribed, cmds := fakeCommandWS(t)

	cfg := Config{
		LoginURL: login.URL, StatusURL: wsURL(ws), APIBase: fakeRest(t).URL,
		RobotID: "robot-001", RobotType: "R3", version: codeV1,
		Frame: MapFrame{MapID: "map-1", Resolution: 0.05, OriginX: -165.4, OriginY: -158.07, Height: 5402},
	}
	client := New(cfg)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Disconnect()
	if _, err := client.Telemetry(); err != nil {
		t.Fatalf("Telemetry: %v", err)
	}
	select {
	case <-subscribed:
	case <-time.After(2 * time.Second):
		t.Fatal("no subscribe")
	}

	// world origin metres -> pixel (0,5402) under the live-validated frame
	if err := client.MoveTo(-165.4, -158.07, 1.5); err != nil {
		t.Fatalf("MoveTo: %v", err)
	}

	select {
	case m := <-cmds:
		if m["operation_cmd"] != "p2p_start" {
			t.Errorf("operation_cmd = %v, want p2p_start", m["operation_cmd"])
		}
		if m["robot_id"] != "robot-001" || m["robot_type"] != "R3" {
			t.Errorf("identity = %v/%v, want robot-001/R3", m["robot_id"], m["robot_type"])
		}
		if _, ok := m["time_stamp"].(float64); !ok {
			t.Errorf("time_stamp missing or not a number: %v", m["time_stamp"])
		}
		content, _ := m["content"].(map[string]any)
		if content == nil {
			t.Fatal("content missing")
		}
		const eps = 1e-6
		if x := content["x"].(float64); math.Abs(x-0) > eps {
			t.Errorf("content.x = %v, want 0 (pixels)", x)
		}
		if y := content["y"].(float64); math.Abs(y-5402) > eps {
			t.Errorf("content.y = %v, want 5402 (pixels)", y)
		}
		if h := content["heading"].(float64); math.Abs(h-1.5) > eps {
			t.Errorf("content.heading = %v, want 1.5 rad", h)
		}
		if content["name"] != "Custom" {
			t.Errorf("content.name = %v, want Custom", content["name"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no command frame received")
	}
}

func TestClient_RelocalizeSendsHotLocalize(t *testing.T) {
	login := fakeLogin(t, "tok")
	defer login.Close()
	ws, subscribed, cmds := fakeCommandWS(t)

	cfg := Config{
		LoginURL: login.URL, StatusURL: wsURL(ws), APIBase: fakeRest(t).URL,
		RobotID: "robot-001", RobotType: "R3", version: codeV1,
		Frame: MapFrame{MapID: "map-1", Resolution: 0.05, OriginX: -165.4, OriginY: -158.07, Height: 5402},
	}
	client := New(cfg)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Disconnect()
	if _, err := client.Telemetry(); err != nil {
		t.Fatalf("Telemetry: %v", err)
	}
	select {
	case <-subscribed:
	case <-time.After(2 * time.Second):
		t.Fatal("no subscribe")
	}

	// same world->pixel path as a move: origin metres -> pixel (0,5402)
	if err := client.Relocalize(-165.4, -158.07, 1.5); err != nil {
		t.Fatalf("Relocalize: %v", err)
	}

	select {
	case m := <-cmds:
		if m["operation_cmd"] != "hot_localize" {
			t.Errorf("operation_cmd = %v, want hot_localize", m["operation_cmd"])
		}
		content, _ := m["content"].(map[string]any)
		if content == nil {
			t.Fatal("content missing")
		}
		const eps = 1e-6
		if x := content["x"].(float64); math.Abs(x-0) > eps {
			t.Errorf("content.x = %v, want 0 (pixels)", x)
		}
		if y := content["y"].(float64); math.Abs(y-5402) > eps {
			t.Errorf("content.y = %v, want 5402 (pixels)", y)
		}
		if h := content["heading"].(float64); math.Abs(h-1.5) > eps {
			t.Errorf("content.heading = %v, want 1.5 rad", h)
		}
		if _, hasMap := content["mapId"]; hasMap {
			t.Error("hot_localize content must not carry a mapId (the robot cannot switch maps)")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no command frame received")
	}
}

func TestClient_RelocalizeWithoutFrameErrors(t *testing.T) {
	login := fakeLogin(t, "tok")
	defer login.Close()
	ws, subscribed, _ := fakeCommandWS(t)

	// no usable frame: Relocalize must refuse rather than emit garbage pixels
	cfg := Config{
		LoginURL: login.URL, StatusURL: wsURL(ws), APIBase: fakeRest(t).URL,
		RobotID: "robot-001", version: codeV1,
	}
	client := New(cfg)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Disconnect()
	if _, err := client.Telemetry(); err != nil {
		t.Fatalf("Telemetry: %v", err)
	}
	select {
	case <-subscribed:
	case <-time.After(2 * time.Second):
		t.Fatal("no subscribe")
	}

	if err := client.Relocalize(1, 2, 3); err == nil {
		t.Fatal("Relocalize without a usable frame must error")
	}
}

func TestClient_StopMovementSendsP2PStop(t *testing.T) {
	login := fakeLogin(t, "tok")
	defer login.Close()
	ws, subscribed, cmds := fakeCommandWS(t)

	cfg := Config{
		LoginURL: login.URL, StatusURL: wsURL(ws), APIBase: fakeRest(t).URL,
		RobotID: "robot-001", RobotType: "R3", version: codeV1,
		Frame: MapFrame{Resolution: 0.05},
	}
	client := New(cfg)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Disconnect()
	if _, err := client.Telemetry(); err != nil {
		t.Fatalf("Telemetry: %v", err)
	}
	select {
	case <-subscribed:
	case <-time.After(2 * time.Second):
		t.Fatal("no subscribe")
	}

	if err := client.StopMovement(); err != nil {
		t.Fatalf("StopMovement: %v", err)
	}
	select {
	case m := <-cmds:
		if m["operation_cmd"] != "p2p_stop" {
			t.Errorf("operation_cmd = %v, want p2p_stop", m["operation_cmd"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no command frame received")
	}
}

func TestClient_MoveToWithoutFrameOrConnErrors(t *testing.T) {
	login := fakeLogin(t, "tok")
	defer login.Close()
	ws, subscribed, _ := fakeCommandWS(t)

	// no usable frame: MoveTo must refuse rather than emit garbage pixels
	cfg := Config{
		LoginURL: login.URL, StatusURL: wsURL(ws), APIBase: fakeRest(t).URL,
		RobotID: "robot-001", version: codeV1,
	}
	client := New(cfg)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Disconnect()
	if _, err := client.Telemetry(); err != nil {
		t.Fatalf("Telemetry: %v", err)
	}
	select {
	case <-subscribed:
	case <-time.After(2 * time.Second):
		t.Fatal("no subscribe")
	}
	if err := client.MoveTo(1, 2, 0); err == nil {
		t.Error("MoveTo with no map frame: want error")
	}

	// no live conn: never connected client must refuse
	offline := New(Config{
		LoginURL: login.URL, StatusURL: "wss://unused", APIBase: fakeRest(t).URL,
		RobotID: "robot-001", version: codeV1, Frame: MapFrame{Resolution: 0.05},
	})
	if err := offline.MoveTo(1, 2, 0); err == nil {
		t.Error("MoveTo without a live conn: want error")
	}
}

func TestClient_P2PEndStatusEmitsMoveCompleted(t *testing.T) {
	login := fakeLogin(t, "tok")
	defer login.Close()

	send := make(chan string, 4)
	ws := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgradeEcho(w, r)
		if err != nil {
			return
		}
		defer conn.Close()
		var sub map[string]any
		if err := conn.ReadJSON(&sub); err != nil {
			return
		}
		for frame := range send {
			if err := conn.WriteMessage(websocket.TextMessage, []byte(frame)); err != nil {
				return
			}
		}
		_, _, _ = conn.ReadMessage()
	}))
	defer ws.Close()
	defer close(send)

	cfg := Config{
		LoginURL: login.URL, StatusURL: wsURL(ws), APIBase: fakeRest(t).URL,
		RobotID: "robot-001", RobotType: "R3", version: codeV1,
		Frame: MapFrame{Resolution: 0.05},
	}
	client := New(cfg)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Disconnect()
	out, err := client.Telemetry()
	if err != nil {
		t.Fatalf("Telemetry: %v", err)
	}
	events, err := client.Events()
	if err != nil {
		t.Fatalf("Events: %v", err)
	}

	// success end-status -> MoveCompleted success
	send <- `{"operation_fb":"p2p_end_status","content":{"status":true}}`
	select {
	case ev := <-events:
		if ev.MoveCompleted == nil || !ev.MoveCompleted.Success {
			t.Errorf("event = %+v, want MoveCompleted success", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no execution event for p2p_end_status")
	}

	// failure end-status -> failure with a reason
	send <- `{"operation_fb":"p2p_end_status","content":{"status":false,"reason":"blocked"}}`
	select {
	case ev := <-events:
		if ev.MoveCompleted == nil || ev.MoveCompleted.Success {
			t.Errorf("event = %+v, want MoveCompleted failure", ev)
		}
		if ev.MoveCompleted.Reason == "" {
			t.Error("failure event has empty Reason")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no execution event for failed p2p_end_status")
	}

	// telemetry unaffected: a status frame still translates and flows
	send <- `{"operation_fb":"touchscreen_robot_status","content":{"battery_info":{"soc":42.5}}}`
	timeout := time.After(2 * time.Second)
	for {
		select {
		case got := <-out:
			if got.BatteryState != nil && got.BatteryState.BatteryCharge == 42.5 {
				return
			}
		case <-timeout:
			t.Fatal("telemetry stopped flowing after end-status frames")
		}
	}
}

func TestClient_DisconnectClosesEvents(t *testing.T) {
	login := fakeLogin(t, "tok")
	defer login.Close()
	ws, subscribed, _ := fakeCommandWS(t)

	client := New(Config{
		LoginURL: login.URL, StatusURL: wsURL(ws), APIBase: fakeRest(t).URL,
		RobotID: "robot-001", version: codeV1, Frame: MapFrame{Resolution: 0.05},
	})
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if _, err := client.Telemetry(); err != nil {
		t.Fatalf("Telemetry: %v", err)
	}
	events, err := client.Events()
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	select {
	case <-subscribed:
	case <-time.After(2 * time.Second):
		t.Fatal("no subscribe")
	}

	client.Disconnect()
	select {
	case _, ok := <-events:
		if ok {
			t.Error("got an event after Disconnect, want closed channel")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("events channel not closed after Disconnect")
	}
}

type fakeClock struct{ fireLong, fireShort chan time.Time }

func (f *fakeClock) Now() time.Time { return time.Unix(1000, 0) }
func (f *fakeClock) After(d time.Duration) <-chan time.Time {
	if d > time.Hour {
		return f.fireLong
	}
	return f.fireShort
}

func TestClient_RefreshReLoginsAndReconnects(t *testing.T) {
	var logins atomic.Int32
	login := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := logins.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"token":"token-%d","tokenExpiryIsoUtcTime":"2026-07-16T10:30:00Z"}`, n)
	}))
	defer login.Close()

	// The subprotocol carries the token; record it per connection (after the
	// subscribe read, so the connection is registered before the test drops it).
	tokens := make(chan string, 4)
	ws := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgradeEcho(w, r)
		if err != nil {
			return
		}
		defer conn.Close()
		var sub map[string]any
		if err := conn.ReadJSON(&sub); err != nil {
			return
		}
		tokens <- r.Header.Get("Sec-Websocket-Protocol")
		_, _, _ = conn.ReadMessage() // hold open until dropped/disconnected
	}))
	defer ws.Close()

	fc := &fakeClock{fireLong: make(chan time.Time), fireShort: make(chan time.Time)}
	cfg := Config{
		LoginURL: login.URL, StatusURL: wsURL(ws), APIBase: fakeRest(t).URL,
		RobotID: "robot-001", RobotType: "R3",
		version: codeV1, Frame: MapFrame{Resolution: 0.05},
	}
	client := New(cfg)
	client.clock = fc
	client.backoffBase = 10 * time.Millisecond
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Disconnect()
	if _, err := client.Telemetry(); err != nil {
		t.Fatalf("Telemetry: %v", err)
	}

	select {
	case tk := <-tokens:
		if tk != "token-1" {
			t.Fatalf("first connect token = %q, want token-1", tk)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no first connection")
	}

	// Fire the refresh: re-login mints token-2, the socket is dropped, and the
	// reconnect must carry the new token as subprotocol.
	select {
	case fc.fireLong <- time.Time{}:
	case <-time.After(2 * time.Second):
		t.Fatal("refresh loop never waited on the clock")
	}

	select {
	case tk := <-tokens:
		if tk != "token-2" {
			t.Errorf("reconnect token = %q, want token-2 (refreshed via re-login)", tk)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no reconnect after refresh")
	}
}

func TestClient_PollSwapsFrameOnMapChange(t *testing.T) {
	login := fakeLogin(t, "tok")
	defer login.Close()

	const (
		mapA  = `{"currentMapId":"map-a","workSiteMaps":[{"id":"map-a","jsonLink":"/info/map-a.json","lastModifiedDate":"v1"}]}`
		mapB  = `{"currentMapId":"map-b","workSiteMaps":[{"id":"map-a","jsonLink":"/info/map-a.json","lastModifiedDate":"v1"},{"id":"map-b","jsonLink":"/info/map-b.json","lastModifiedDate":"v1"}]}`
		infoB = `{"meta_data":{"height":5402,"width":6537,"ox":-100,"oy":-50,"resolution":0.05}}`
	)
	var mapBody atomic.Value
	mapBody.Store(mapA)
	var infoAHits atomic.Int32
	rest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "by-encoding-id"):
			_, _ = w.Write([]byte(`{"id":"uuid-1"}`))
		case strings.HasSuffix(r.URL.Path, "/map"):
			_, _ = w.Write([]byte(mapBody.Load().(string)))
		case strings.HasSuffix(r.URL.Path, "map-a.json"):
			infoAHits.Add(1)
			_, _ = w.Write([]byte(infoJSON))
		default:
			_, _ = w.Write([]byte(infoB))
		}
	}))
	defer rest.Close()

	poseWS := fakeFeed(t, `{"operation_fb":"robot_pose","content":{"x":0,"y":5402,"heading":0}}`)
	defer poseWS.Close()

	fc := &fakeClock{fireLong: make(chan time.Time), fireShort: make(chan time.Time)}
	cfg := Config{
		LoginURL: login.URL, PoseURL: wsURL(poseWS), APIBase: rest.URL,
		RobotID: "robot-001", version: codeV1, MapPollSeconds: 60,
	}
	client := New(cfg)
	client.clock = fc
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Disconnect()
	out, err := client.Telemetry()
	if err != nil {
		t.Fatalf("Telemetry: %v", err)
	}

	waitPosition := func(wantMap string, wantX float64) {
		t.Helper()
		const eps = 1e-6
		timeout := time.After(3 * time.Second)
		for {
			select {
			case got := <-out:
				p := got.AGVPosition
				if p == nil || p.MapID != wantMap {
					continue
				}
				if math.Abs(p.X-wantX) > eps {
					t.Fatalf("X = %v on %s, want %v", p.X, wantMap, wantX)
				}
				return
			case <-timeout:
				t.Fatalf("no position on map %s", wantMap)
			}
		}
	}
	fireTick := func() {
		t.Helper()
		select {
		case fc.fireShort <- time.Time{}:
		case <-time.After(2 * time.Second):
			t.Fatal("poll loop never waited on the clock")
		}
	}

	waitPosition("map-a", -165.4) // frame fetched at connect

	fireTick() // unchanged map: cache hit, no new info fetch, frame stays map-a
	mapBody.Store(mapB)
	fireTick() // changed currentMapId: fetch map-b info, swap the frame

	waitPosition("map-b", -100)
	if n := infoAHits.Load(); n != 1 {
		t.Errorf("map-a info fetches = %d, want 1 (unchanged tick must hit the cache)", n)
	}
}

func TestClient_DisconnectNoGoroutineLeak(t *testing.T) {
	const token = "tok"
	login := fakeLogin(t, token)
	defer login.Close()

	sent := make(chan struct{})
	ws := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgradeEcho(w, r)
		if err != nil {
			return
		}
		defer conn.Close()
		var sub map[string]any
		if err := conn.ReadJSON(&sub); err != nil {
			return
		}
		_ = conn.WriteMessage(websocket.TextMessage,
			[]byte(`{"operation_fb":"touchscreen_robot_status","content":{"battery_info":{"soc":42.5}}}`))
		close(sent)
		_, _, _ = conn.ReadMessage()
	}))
	defer ws.Close()

	cfg := Config{
		LoginURL: login.URL, StatusURL: wsURL(ws), APIBase: fakeRest(t).URL,
		RobotID: "robot-001", RobotType: "R3",
		version: codeV1, Frame: MapFrame{Resolution: 0.05},
	}
	client := New(cfg)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if _, err := client.Telemetry(); err != nil {
		t.Fatalf("Telemetry: %v", err)
	}
	// deliberately never read the telemetry channel, so the forwarder is left
	// blocked on a pending send when the context is cancelled.

	<-sent
	time.Sleep(100 * time.Millisecond) // let the frame reach the blocked forward send

	done := make(chan struct{})
	go func() {
		client.Disconnect()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Disconnect did not return within 2s — goroutine leak (forward stuck on send)")
	}
}

func seedDocks(c *Client, docks ...DockMarker) {
	c.frameMu.Lock()
	c.cmdDocks = docks
	c.frameMu.Unlock()
}

func TestClient_StartChargingSendsDockStart(t *testing.T) {
	login := fakeLogin(t, "tok")
	defer login.Close()
	ws, subscribed, cmds := fakeCommandWS(t)

	cfg := Config{
		LoginURL: login.URL, StatusURL: wsURL(ws), APIBase: fakeRest(t).URL,
		RobotID: "robot-001", RobotType: "R3", version: codeV1,
		Frame: MapFrame{MapID: "map-1", Resolution: 0.05, OriginX: -165.8, OriginY: -158.05, Height: 5402},
	}
	client := New(cfg)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Disconnect()
	if _, err := client.Telemetry(); err != nil {
		t.Fatalf("Telemetry: %v", err)
	}
	select {
	case <-subscribed:
	case <-time.After(2 * time.Second):
		t.Fatal("no subscribe")
	}
	seedDocks(client, DockMarker{Name: "charging_742", X: 3142, Y: 2311, Heading: 2.73})

	if err := client.StartCharging(); err != nil {
		t.Fatalf("StartCharging: %v", err)
	}

	select {
	case m := <-cmds:
		if m["operation_cmd"] != "dock_start" {
			t.Errorf("operation_cmd = %v, want dock_start", m["operation_cmd"])
		}
		content, _ := m["content"].(map[string]any)
		if content == nil {
			t.Fatal("content missing")
		}
		if content["name"] != "charging_742" {
			t.Errorf("content.name = %v, want charging_742", content["name"])
		}
		const eps = 1e-6
		if x := content["x"].(float64); math.Abs(x-3142) > eps {
			t.Errorf("content.x = %v, want 3142 (pixels)", x)
		}
		if y := content["y"].(float64); math.Abs(y-2311) > eps {
			t.Errorf("content.y = %v, want 2311 (pixels)", y)
		}
		if h := content["heading"].(float64); math.Abs(h-2.73) > eps {
			t.Errorf("content.heading = %v, want 2.73", h)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no command frame received")
	}
}

func TestClient_StartChargingPicksNearestDock(t *testing.T) {
	login := fakeLogin(t, "tok")
	defer login.Close()

	send := make(chan string, 4)
	cmds := make(chan map[string]any, 4)
	ws := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgradeEcho(w, r)
		if err != nil {
			return
		}
		defer conn.Close()
		var sub map[string]any
		if err := conn.ReadJSON(&sub); err != nil {
			return
		}
		go func() {
			for frame := range send {
				if err := conn.WriteMessage(websocket.TextMessage, []byte(frame)); err != nil {
					return
				}
			}
		}()
		for {
			var m map[string]any
			if err := conn.ReadJSON(&m); err != nil {
				return
			}
			if m["operation_cmd"] == "request_path" {
				continue // connect handshake, not a command under test
			}
			cmds <- m
		}
	}))
	defer ws.Close()
	defer close(send)

	cfg := Config{
		LoginURL: login.URL, StatusURL: wsURL(ws), APIBase: fakeRest(t).URL,
		RobotID: "robot-001", RobotType: "R3", version: codeV1,
		Frame: MapFrame{MapID: "map-1", Resolution: 0.05, Height: 5402},
	}
	client := New(cfg)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Disconnect()
	out, err := client.Telemetry()
	if err != nil {
		t.Fatalf("Telemetry: %v", err)
	}
	seedDocks(client,
		DockMarker{Name: "far", X: 5000, Y: 5000, Heading: 0},
		DockMarker{Name: "near", X: 100, Y: 100, Heading: 1},
	)

	// pose flows through the forward loop; once it emerges translated, the
	// stashed pixel pose is guaranteed set
	send <- `{"operation_fb":"robot_pose","content":{"x":120,"y":90,"heading":0}}`
	timeout := time.After(2 * time.Second)
poseWait:
	for {
		select {
		case got := <-out:
			if got.AGVPosition != nil {
				break poseWait
			}
		case <-timeout:
			t.Fatal("pose never flowed through")
		}
	}

	if err := client.StartCharging(); err != nil {
		t.Fatalf("StartCharging: %v", err)
	}
	select {
	case m := <-cmds:
		content, _ := m["content"].(map[string]any)
		if content == nil {
			t.Fatal("content missing")
		}
		if content["name"] != "near" {
			t.Errorf("content.name = %v, want near (nearest to pose)", content["name"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no command frame received")
	}
}

func TestClient_StopChargingSendsUndockStart(t *testing.T) {
	login := fakeLogin(t, "tok")
	defer login.Close()
	ws, subscribed, cmds := fakeCommandWS(t)

	cfg := Config{
		LoginURL: login.URL, StatusURL: wsURL(ws), APIBase: fakeRest(t).URL,
		RobotID: "robot-001", RobotType: "R3", version: codeV1,
		Frame: MapFrame{Resolution: 0.05},
	}
	client := New(cfg)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Disconnect()
	if _, err := client.Telemetry(); err != nil {
		t.Fatalf("Telemetry: %v", err)
	}
	select {
	case <-subscribed:
	case <-time.After(2 * time.Second):
		t.Fatal("no subscribe")
	}

	if err := client.StopCharging(); err != nil {
		t.Fatalf("StopCharging: %v", err)
	}

	select {
	case m := <-cmds:
		if m["operation_cmd"] != "undock_start" {
			t.Errorf("operation_cmd = %v, want undock_start", m["operation_cmd"])
		}
		content, _ := m["content"].(map[string]any)
		if content["status"] != true {
			t.Errorf("content = %v, want status true", content)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no command frame received")
	}
}

func TestClient_ChargingRefusalPaths(t *testing.T) {
	login := fakeLogin(t, "tok")
	defer login.Close()

	// never connected: no live command socket
	client := New(Config{
		LoginURL: login.URL, APIBase: fakeRest(t).URL,
		RobotID: "robot-001", version: codeV1, Frame: MapFrame{Resolution: 0.05},
	})
	seedDocks(client, DockMarker{Name: "d", X: 1, Y: 1})
	if err := client.StartCharging(); err == nil {
		t.Error("StartCharging without a live conn: want error")
	}
	if err := client.StopCharging(); err == nil {
		t.Error("StopCharging without a live conn: want error")
	}

	// no dock marker on the active map
	seedDocks(client)
	if err := client.StartCharging(); err == nil {
		t.Error("StartCharging without a dock marker: want error")
	}
}

func TestClient_ChargingEndStatusEmitsEvents(t *testing.T) {
	login := fakeLogin(t, "tok")
	defer login.Close()

	send := make(chan string, 4)
	ws := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgradeEcho(w, r)
		if err != nil {
			return
		}
		defer conn.Close()
		var sub map[string]any
		if err := conn.ReadJSON(&sub); err != nil {
			return
		}
		for frame := range send {
			if err := conn.WriteMessage(websocket.TextMessage, []byte(frame)); err != nil {
				return
			}
		}
		_, _, _ = conn.ReadMessage()
	}))
	defer ws.Close()
	defer close(send)

	cfg := Config{
		LoginURL: login.URL, StatusURL: wsURL(ws), APIBase: fakeRest(t).URL,
		RobotID: "robot-001", version: codeV1, Frame: MapFrame{Resolution: 0.05},
	}
	client := New(cfg)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Disconnect()
	if _, err := client.Telemetry(); err != nil {
		t.Fatalf("Telemetry: %v", err)
	}
	events, err := client.Events()
	if err != nil {
		t.Fatalf("Events: %v", err)
	}

	send <- `{"operation_fb":"dock_end_status","content":{"status":true}}`
	select {
	case ev := <-events:
		if ev.StartChargingCompleted == nil || !ev.StartChargingCompleted.Success {
			t.Errorf("event = %+v, want StartChargingCompleted success", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no execution event for dock_end_status")
	}

	send <- `{"operation_fb":"undock_end_status","content":{"status":false,"reason":"stuck"}}`
	select {
	case ev := <-events:
		if ev.StopChargingCompleted == nil || ev.StopChargingCompleted.Success {
			t.Errorf("event = %+v, want StopChargingCompleted failure", ev)
		}
		if ev.StopChargingCompleted.Reason != "stuck" {
			t.Errorf("Reason = %q, want stuck", ev.StopChargingCompleted.Reason)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no execution event for undock_end_status")
	}
}

// verbatim frames captured from a live robot (robot_id anonymized):
// content carries mission_id + status only — no reason field exists upstream.
func TestExecutionEvent_DecodesCapturedFrames(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		check   func(ev robotbus.ExecutionEvent) bool
		wantMsg string
	}{
		{
			name: "undock end success",
			raw:  `{"robot_id":"R3-0001","operation_fb":"undock_end_status","content":{"mission_id":"862af644-7e9b-400d-8192-bdfdfd9aa7fb","status":true},"timestamp":1784287842593}`,
			check: func(ev robotbus.ExecutionEvent) bool {
				return ev.StopChargingCompleted != nil && ev.StopChargingCompleted.Success
			},
			wantMsg: "StopChargingCompleted success",
		},
		{
			name: "dock end success",
			raw:  `{"robot_id":"R3-0001","operation_fb":"dock_end_status","content":{"mission_id":"7188a544-873d-42b5-a40e-30f18c101ef5","status":true},"timestamp":1784287883880}`,
			check: func(ev robotbus.ExecutionEvent) bool {
				return ev.StartChargingCompleted != nil && ev.StartChargingCompleted.Success
			},
			wantMsg: "StartChargingCompleted success",
		},
		{
			name:    "p2p end success",
			raw:     `{"robot_id":"R3-0001","operation_fb":"p2p_end_status","content":{"mission_id":"0e623438-14c2-4b3c-a984-d6629f66a69d","status":true},"timestamp":1784287934494}`,
			check:   func(ev robotbus.ExecutionEvent) bool { return ev.MoveCompleted != nil && ev.MoveCompleted.Success },
			wantMsg: "MoveCompleted success",
		},
		{
			name: "p2p end failure carries a default reason (upstream sends none)",
			raw:  `{"robot_id":"R3-0001","operation_fb":"p2p_end_status","content":{"mission_id":"5d9fee67-e192-49d7-8fc8-84d6d49b8501","status":false},"timestamp":1784287980694}`,
			check: func(ev robotbus.ExecutionEvent) bool {
				return ev.MoveCompleted != nil && !ev.MoveCompleted.Success && ev.MoveCompleted.Reason != ""
			},
			wantMsg: "MoveCompleted failure with non-empty reason",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev, ok := executionEvent([]byte(tt.raw))
			if !ok {
				t.Fatal("captured frame not recognized as an execution event")
			}
			if !tt.check(ev) {
				t.Fatalf("event = %+v, want %s", ev, tt.wantMsg)
			}
		})
	}
}

func TestExecutionEvent_DecodesHotLocalize(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		success bool
		score   float64
	}{
		{
			name:    "success carries the localization score",
			raw:     `{"robot_id":"R3-0001","operation_fb":"hot_localize","content":{"percentage":0.97,"success":true},"timestamp":1784287842593}`,
			success: true, score: 0.97,
		},
		{
			name:    "failure carries the localization score",
			raw:     `{"robot_id":"R3-0001","operation_fb":"hot_localize","content":{"percentage":0.20,"success":false},"timestamp":1784287842593}`,
			success: false, score: 0.20,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev, ok := executionEvent([]byte(tt.raw))
			if !ok {
				t.Fatal("hot_localize frame not recognized as an execution event")
			}
			if ev.RelocalizeCompleted == nil {
				t.Fatalf("event = %+v, want RelocalizeCompleted", ev)
			}
			if ev.RelocalizeCompleted.Success != tt.success {
				t.Errorf("Success = %v, want %v", ev.RelocalizeCompleted.Success, tt.success)
			}
			if math.Abs(ev.RelocalizeCompleted.Score-tt.score) > 1e-9 {
				t.Errorf("Score = %v, want %v", ev.RelocalizeCompleted.Score, tt.score)
			}
		})
	}
}

func TestClient_RequestsPathOnConnect(t *testing.T) {
	login := fakeLogin(t, "tok")
	defer login.Close()

	frames := make(chan map[string]any, 4)
	ws := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgradeEcho(w, r)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			var m map[string]any
			if err := conn.ReadJSON(&m); err != nil {
				return
			}
			frames <- m
		}
	}))
	defer ws.Close()

	client := New(Config{
		LoginURL: login.URL, StatusURL: wsURL(ws), APIBase: fakeRest(t).URL,
		RobotID: "robot-001", RobotType: "R3", version: codeV1, Frame: MapFrame{Resolution: 0.05},
	})
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Disconnect()
	if _, err := client.Telemetry(); err != nil {
		t.Fatalf("Telemetry: %v", err)
	}

	want := []string{"subscribe", "request_path"}
	for _, cmd := range want {
		select {
		case m := <-frames:
			if m["operation_cmd"] != cmd {
				t.Fatalf("operation_cmd = %v, want %s", m["operation_cmd"], cmd)
			}
			if cmd == "request_path" {
				if content, _ := m["content"].(map[string]any); content["status"] != true {
					t.Errorf("request_path content = %v, want {status:true}", m["content"])
				}
				if m["robot_id"] != "robot-001" {
					t.Errorf("robot_id = %v, want robot-001", m["robot_id"])
				}
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("no %s frame received", cmd)
		}
	}
}

func TestClient_CleaningCommandFrames(t *testing.T) {
	login := fakeLogin(t, "tok")
	defer login.Close()
	ws, subscribed, cmds := fakeCommandWS(t)

	client := New(Config{
		LoginURL: login.URL, StatusURL: wsURL(ws), APIBase: fakeRest(t).URL,
		RobotID: "robot-001", RobotType: "R3", version: codeV1, Frame: MapFrame{Resolution: 0.05},
	})
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Disconnect()
	if _, err := client.Telemetry(); err != nil {
		t.Fatalf("Telemetry: %v", err)
	}
	select {
	case <-subscribed:
	case <-time.After(2 * time.Second):
		t.Fatal("no subscribe")
	}

	// seed the active map's equalizer catalog (what fetchFrame caches live)
	client.frameMu.Lock()
	client.cmdZones = map[string]zoneEqualizer{
		"Zone A": {Name: "Zone A", SelectedModeName: "Standard", Modes: []equalizerMode{
			{Name: "Eco", Configs: []equalizerConfig{{ConfigName: "water", Value: 1}}},
			{Name: "Standard", Configs: []equalizerConfig{{ConfigName: "water", Value: 3}}},
		}},
		"Zone B": {Name: "Zone B", SelectedModeName: "Deep", Modes: []equalizerMode{
			{Name: "Deep", Configs: []equalizerConfig{{ConfigName: "water", Value: 5}}},
		}},
	}
	client.frameMu.Unlock()

	// Zone A defaults from the catalog; Zone B carries a master override
	zones := []robotbus.ZoneRef{
		{Name: "Zone A"},
		{Name: "Zone B", CleaningMode: "Boost", Configs: map[string]float64{"water": 9}},
	}
	if err := client.StartCleaning(zones, true); err != nil {
		t.Fatalf("StartCleaning: %v", err)
	}
	select {
	case m := <-cmds:
		if m["operation_cmd"] != "clean_start" {
			t.Fatalf("operation_cmd = %v, want clean_start", m["operation_cmd"])
		}
		content, _ := m["content"].(map[string]any)
		if content["mode"] != float64(0) {
			t.Errorf("mode = %v, want 0", content["mode"])
		}
		if content["bypass_blackout"] != true {
			t.Errorf("bypass_blackout = %v, want true", content["bypass_blackout"])
		}
		if _, present := content["home_poi"]; present {
			t.Error("home_poi present, must be omitted (robot stays put after cleaning)")
		}
		got, _ := content["zones"].([]any)
		if len(got) != 2 {
			t.Fatalf("zones = %v, want 2 entries", content["zones"])
		}
		wantMode := map[string]string{"Zone A": "Standard", "Zone B": "Boost"}
		wantWater := map[string]float64{"Zone A": 3, "Zone B": 9}
		for i, name := range []string{"Zone A", "Zone B"} {
			z, _ := got[i].(map[string]any)
			if z["name"] != name {
				t.Errorf("zones[%d].name = %v, want %s", i, z["name"], name)
			}
			if z["path_id"] != float64(0) {
				t.Errorf("zones[%d].path_id = %v, want 0", i, z["path_id"])
			}
			cfg, ok := z["configs"].(map[string]any)
			if !ok || cfg["water"] != wantWater[name] {
				t.Errorf("zones[%d].configs = %v, want water=%v", i, z["configs"], wantWater[name])
			}
			if z["cleaning_mode"] != wantMode[name] {
				t.Errorf("zones[%d].cleaning_mode = %v, want %s", i, z["cleaning_mode"], wantMode[name])
			}
			if z["boundary_mode"] != false {
				t.Errorf("zones[%d].boundary_mode = %v, want false", i, z["boundary_mode"])
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no clean_start frame")
	}

	if err := client.StopCleaning(); err != nil {
		t.Fatalf("StopCleaning: %v", err)
	}
	select {
	case m := <-cmds:
		if m["operation_cmd"] != "clean_stop" {
			t.Errorf("operation_cmd = %v, want clean_stop", m["operation_cmd"])
		}
		if content, _ := m["content"].(map[string]any); content["status"] != true {
			t.Errorf("content = %v, want status true", content)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no clean_stop frame")
	}
}

func TestClient_CleaningPauseContinueFrames(t *testing.T) {
	login := fakeLogin(t, "tok")
	defer login.Close()
	ws, subscribed, cmds := fakeCommandWS(t)

	client := New(Config{
		LoginURL: login.URL, StatusURL: wsURL(ws), APIBase: fakeRest(t).URL,
		RobotID: "robot-001", RobotType: "R3", version: codeV1, Frame: MapFrame{Resolution: 0.05},
	})
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Disconnect()
	if _, err := client.Telemetry(); err != nil {
		t.Fatalf("Telemetry: %v", err)
	}
	select {
	case <-subscribed:
	case <-time.After(2 * time.Second):
		t.Fatal("no subscribe")
	}

	if err := client.PauseCleaning(); err != nil {
		t.Fatalf("PauseCleaning: %v", err)
	}
	select {
	case m := <-cmds:
		if m["operation_cmd"] != "clean_pause" {
			t.Errorf("operation_cmd = %v, want clean_pause", m["operation_cmd"])
		}
		if content, _ := m["content"].(map[string]any); content["status"] != true {
			t.Errorf("content = %v, want status true", content)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no clean_pause frame")
	}

	// clean_continue carries the original clean's bypass_blackout, else a resume
	// during a blackout period is blocked upstream
	if err := client.ResumeCleaning(true); err != nil {
		t.Fatalf("ResumeCleaning: %v", err)
	}
	select {
	case m := <-cmds:
		if m["operation_cmd"] != "clean_continue" {
			t.Errorf("operation_cmd = %v, want clean_continue", m["operation_cmd"])
		}
		content, _ := m["content"].(map[string]any)
		if content["status"] != true {
			t.Errorf("content status = %v, want true", content["status"])
		}
		if content["bypass_blackout"] != true {
			t.Errorf("content bypass_blackout = %v, want true", content["bypass_blackout"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no clean_continue frame")
	}
}

func TestClient_DockPauseContinueFrames(t *testing.T) {
	login := fakeLogin(t, "tok")
	defer login.Close()
	ws, subscribed, cmds := fakeCommandWS(t)

	client := New(Config{
		LoginURL: login.URL, StatusURL: wsURL(ws), APIBase: fakeRest(t).URL,
		RobotID: "robot-001", RobotType: "R3", version: codeV1, Frame: MapFrame{Resolution: 0.05},
	})
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Disconnect()
	if _, err := client.Telemetry(); err != nil {
		t.Fatalf("Telemetry: %v", err)
	}
	select {
	case <-subscribed:
	case <-time.After(2 * time.Second):
		t.Fatal("no subscribe")
	}

	if err := client.PauseDocking(); err != nil {
		t.Fatalf("PauseDocking: %v", err)
	}
	select {
	case m := <-cmds:
		if m["operation_cmd"] != "dock_pause" {
			t.Errorf("operation_cmd = %v, want dock_pause", m["operation_cmd"])
		}
		if content, _ := m["content"].(map[string]any); content["status"] != true {
			t.Errorf("content = %v, want status true", content)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no dock_pause frame")
	}

	if err := client.ResumeDocking(); err != nil {
		t.Fatalf("ResumeDocking: %v", err)
	}
	select {
	case m := <-cmds:
		if m["operation_cmd"] != "dock_continue" {
			t.Errorf("operation_cmd = %v, want dock_continue", m["operation_cmd"])
		}
		if content, _ := m["content"].(map[string]any); content["status"] != true {
			t.Errorf("content = %v, want status true", content)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no dock_continue frame")
	}
}

func TestClient_AbortDockingSendsDockStop(t *testing.T) {
	login := fakeLogin(t, "tok")
	defer login.Close()
	ws, subscribed, cmds := fakeCommandWS(t)

	client := New(Config{
		LoginURL: login.URL, StatusURL: wsURL(ws), APIBase: fakeRest(t).URL,
		RobotID: "robot-001", RobotType: "R3", version: codeV1, Frame: MapFrame{Resolution: 0.05},
	})
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Disconnect()
	if _, err := client.Telemetry(); err != nil {
		t.Fatalf("Telemetry: %v", err)
	}
	select {
	case <-subscribed:
	case <-time.After(2 * time.Second):
		t.Fatal("no subscribe")
	}

	// abort is dock_stop — NOT undock_start (the inverse op)
	if err := client.AbortDocking(); err != nil {
		t.Fatalf("AbortDocking: %v", err)
	}
	select {
	case m := <-cmds:
		if m["operation_cmd"] != "dock_stop" {
			t.Errorf("operation_cmd = %v, want dock_stop", m["operation_cmd"])
		}
		if content, _ := m["content"].(map[string]any); content["status"] != true {
			t.Errorf("content = %v, want status true", content)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no dock_stop frame")
	}
}

// frame shapes inferred, not captured from hardware; verify against a real robot.
func TestExecutionEvent_DecodesCleanEndStatus(t *testing.T) {
	t.Run("end of mission success", func(t *testing.T) {
		raw := `{"robot_id":"robot-001","operation_fb":"clean_end_status","content":{"mission_id":"m-1","status":true,"percentage":100,"avoidance_count":0,"time":734,"cleaning_efficiency":1.2},"timestamp":1784287842593}`
		ev, ok := executionEvent([]byte(raw))
		if !ok || ev.CleanCompleted == nil || !ev.CleanCompleted.Success {
			t.Fatalf("event = %+v (ok=%v), want CleanCompleted success", ev, ok)
		}
	})
	t.Run("end of mission failure carries a default reason", func(t *testing.T) {
		raw := `{"robot_id":"robot-001","operation_fb":"clean_end_status","content":{"mission_id":"m-2","status":false,"percentage":40,"time":120},"timestamp":1784287842593}`
		ev, ok := executionEvent([]byte(raw))
		if !ok || ev.CleanCompleted == nil || ev.CleanCompleted.Success || ev.CleanCompleted.Reason == "" {
			t.Fatalf("event = %+v (ok=%v), want CleanCompleted failure with non-empty reason", ev, ok)
		}
	})
	t.Run("bare status false is a zone-boundary marker, not an event", func(t *testing.T) {
		raw := `{"robot_id":"robot-001","operation_fb":"clean_end_status","content":{"status":false},"timestamp":1784287842593}`
		if ev, ok := executionEvent([]byte(raw)); ok {
			t.Fatalf("event = %+v, bare {status:false} must be ignored (mid-mission marker)", ev)
		}
	})
}

func TestClient_PauseAndResumeMovementFrames(t *testing.T) {
	login := fakeLogin(t, "tok")
	defer login.Close()
	ws, subscribed, cmds := fakeCommandWS(t)

	client := New(Config{
		LoginURL: login.URL, StatusURL: wsURL(ws), APIBase: fakeRest(t).URL,
		RobotID: "robot-001", RobotType: "R3", version: codeV1, Frame: MapFrame{Resolution: 0.05},
	})
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Disconnect()
	if _, err := client.Telemetry(); err != nil {
		t.Fatalf("Telemetry: %v", err)
	}
	select {
	case <-subscribed:
	case <-time.After(2 * time.Second):
		t.Fatal("no subscribe")
	}

	if err := client.PauseMovement(); err != nil {
		t.Fatalf("PauseMovement: %v", err)
	}
	select {
	case m := <-cmds:
		if m["operation_cmd"] != "p2p_pause" {
			t.Errorf("operation_cmd = %v, want p2p_pause", m["operation_cmd"])
		}
		if content, _ := m["content"].(map[string]any); content["status"] != true {
			t.Errorf("content = %v, want status true", content)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no pause frame")
	}

	if err := client.ResumeMovement(); err != nil {
		t.Fatalf("ResumeMovement: %v", err)
	}
	select {
	case m := <-cmds:
		if m["operation_cmd"] != "p2p_continue" {
			t.Errorf("operation_cmd = %v, want p2p_continue", m["operation_cmd"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no continue frame")
	}
}
