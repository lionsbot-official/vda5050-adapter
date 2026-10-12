package lionsbot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func newTestTokens(t *testing.T, token string) *TokenProvider {
	t.Helper()
	login := fakeLogin(t, token)
	t.Cleanup(login.Close)
	p := NewTokenProvider(login.URL, Credentials{Email: "e@x.io", Password: "pw"}, login.Client())
	if _, _, err := p.Login(context.Background()); err != nil {
		t.Fatalf("login: %v", err)
	}
	return p
}

func TestMapsClient_ResolveUUID(t *testing.T) {
	const uuid = "248df4b7-aa70-47b8-a036-33ac447e668d"
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"` + uuid + `","robotEncodingId":"robot-001","robotFamily":"R3"}`))
	}))
	defer srv.Close()

	m := newMapsClient(srv.URL, newTestTokens(t, "tok"), srv.Client())
	got, err := m.resolveUUID(context.Background(), "robot-001")
	if err != nil {
		t.Fatalf("resolveUUID: %v", err)
	}
	if got != uuid {
		t.Errorf("uuid = %q, want %q", got, uuid)
	}
	if want := "/openapi/v2/robot/by-encoding-id/robot-001"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("Authorization = %q, want Bearer tok", gotAuth)
	}
}

func TestMapsClient_ResolveUUIDErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"MissingRobotByEncodingId"}`))
	}))
	defer srv.Close()

	m := newMapsClient(srv.URL, newTestTokens(t, "tok"), srv.Client())
	if _, err := m.resolveUUID(context.Background(), "nope"); err == nil {
		t.Fatal("want error on 400")
	}
}

// mapsFixture serves /map + a relative info file, counting info-file hits.
type mapsFixture struct {
	srv       *httptest.Server
	mapBody   atomic.Value // string
	infoBody  atomic.Value // string
	infoHits  atomic.Int32
	infoAuths chan string
}

func newMapsFixture(t *testing.T) *mapsFixture {
	t.Helper()
	f := &mapsFixture{infoAuths: make(chan string, 8)}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/info/map-a.json":
			f.infoHits.Add(1)
			f.infoAuths <- r.Header.Get("Authorization")
			_, _ = w.Write([]byte(f.infoBody.Load().(string)))
		default:
			_, _ = w.Write([]byte(f.mapBody.Load().(string)))
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

const infoJSON = `{"meta_data":{"height":5402,"width":6537,"ox":-165.4,"oy":-158.07,"resolution":0.05}}`

func TestMapsClient_CurrentFrame(t *testing.T) {
	f := newMapsFixture(t)
	f.mapBody.Store(`{"currentMapId":"map-a","workSiteMaps":[
		{"id":"map-b","jsonLink":"/info/map-b.json","lastModifiedDate":"2026-01-01T00:00:00Z","level":"1"},
		{"id":"map-a","jsonLink":"/info/map-a.json","lastModifiedDate":1700000000,"level":2}
	]}`)
	f.infoBody.Store(infoJSON)

	m := newMapsClient(f.srv.URL, newTestTokens(t, "tok"), f.srv.Client())
	frame, _, ok, err := m.currentFrame(context.Background(), "uuid-1")
	if err != nil || !ok {
		t.Fatalf("currentFrame: ok=%v err=%v", ok, err)
	}
	want := MapFrame{MapID: "map-a", Resolution: 0.05, OriginX: -165.4, OriginY: -158.07, Height: 5402}
	if frame != want {
		t.Errorf("frame = %+v, want %+v", frame, want)
	}
	if auth := <-f.infoAuths; auth != "Bearer tok" {
		t.Errorf("relative info link Authorization = %q, want Bearer tok", auth)
	}
}

func TestMapsClient_AbsoluteLinkNoAuth(t *testing.T) {
	var gotAuth atomic.Value
	info := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth.Store(r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(infoJSON))
	}))
	defer info.Close()

	f := newMapsFixture(t)
	f.mapBody.Store(`{"currentMapId":"map-a","workSiteMaps":[
		{"id":"map-a","jsonLink":"` + info.URL + `/presigned.json","lastModifiedDate":"v1"}
	]}`)

	m := newMapsClient(f.srv.URL, newTestTokens(t, "tok"), f.srv.Client())
	_, _, ok, err := m.currentFrame(context.Background(), "uuid-1")
	if err != nil || !ok {
		t.Fatalf("currentFrame: ok=%v err=%v", ok, err)
	}
	if a := gotAuth.Load().(string); a != "" {
		t.Errorf("absolute link Authorization = %q, want none (presigned)", a)
	}
}

func TestMapsClient_CacheByModDate(t *testing.T) {
	f := newMapsFixture(t)
	f.mapBody.Store(`{"currentMapId":"map-a","workSiteMaps":[{"id":"map-a","jsonLink":"/info/map-a.json","lastModifiedDate":"v1"}]}`)
	f.infoBody.Store(infoJSON)

	m := newMapsClient(f.srv.URL, newTestTokens(t, "tok"), f.srv.Client())
	for range 2 {
		if _, _, ok, err := m.currentFrame(context.Background(), "uuid-1"); err != nil || !ok {
			t.Fatalf("currentFrame: ok=%v err=%v", ok, err)
		}
	}
	if n := f.infoHits.Load(); n != 1 {
		t.Errorf("info fetches = %d, want 1 (cached on unchanged lastModifiedDate)", n)
	}

	f.mapBody.Store(`{"currentMapId":"map-a","workSiteMaps":[{"id":"map-a","jsonLink":"/info/map-a.json","lastModifiedDate":"v2"}]}`)
	if _, _, ok, err := m.currentFrame(context.Background(), "uuid-1"); err != nil || !ok {
		t.Fatalf("currentFrame after change: ok=%v err=%v", ok, err)
	}
	if n := f.infoHits.Load(); n != 2 {
		t.Errorf("info fetches = %d, want 2 (changed lastModifiedDate refetches)", n)
	}
}

func TestMapsClient_NoFrame(t *testing.T) {
	cases := []struct {
		name, mapBody, infoBody string
	}{
		{"current map not in list", `{"currentMapId":"map-x","workSiteMaps":[{"id":"map-a","jsonLink":"/info/map-a.json","lastModifiedDate":"v1"}]}`, infoJSON},
		{"no jsonLink", `{"currentMapId":"map-a","workSiteMaps":[{"id":"map-a","lastModifiedDate":"v1"}]}`, infoJSON},
		{"zero resolution", `{"currentMapId":"map-a","workSiteMaps":[{"id":"map-a","jsonLink":"/info/map-a.json","lastModifiedDate":"v1"}]}`, `{"meta_data":{"height":100,"width":100,"ox":0,"oy":0,"resolution":0}}`},
		{"missing meta_data", `{"currentMapId":"map-a","workSiteMaps":[{"id":"map-a","jsonLink":"/info/map-a.json","lastModifiedDate":"v1"}]}`, `{"zones":{}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newMapsFixture(t)
			f.mapBody.Store(tc.mapBody)
			f.infoBody.Store(tc.infoBody)
			m := newMapsClient(f.srv.URL, newTestTokens(t, "tok"), f.srv.Client())
			_, _, ok, err := m.currentFrame(context.Background(), "uuid-1")
			if err != nil {
				t.Fatalf("want no error (best-effort), got %v", err)
			}
			if ok {
				t.Error("ok = true, want false (no usable frame)")
			}
		})
	}
}

func TestMapsClient_CurrentFrameParsesDockMarkers(t *testing.T) {
	var gotQuery atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/info/") {
			_, _ = w.Write([]byte(infoJSON))
			return
		}
		gotQuery.Store(r.URL.RawQuery)
		_, _ = w.Write([]byte(`{"currentMapId":"map-a","workSiteMaps":[{"id":"map-a","jsonLink":"/info/map-a.json","lastModifiedDate":"v1",
			"markers":[
				{"name":"charging_742","type":"dock","angle":2.73,"coordinates":"3142 2311"},
				{"name":"poi_7","type":"poi","angle":0,"coordinates":"10 20"},
				{"name":"broken","type":"dock","angle":1,"coordinates":""}
			]}]}`))
	}))
	defer srv.Close()

	m := newMapsClient(srv.URL, newTestTokens(t, "tok"), srv.Client())
	_, docks, ok, err := m.currentFrame(context.Background(), "uuid-1")
	if err != nil || !ok {
		t.Fatalf("currentFrame: ok=%v err=%v", ok, err)
	}
	want := []DockMarker{{Name: "charging_742", X: 3142, Y: 2311, Heading: 2.73}}
	if !reflect.DeepEqual(docks, want) {
		t.Errorf("docks = %+v, want %+v", docks, want)
	}
	if q, _ := gotQuery.Load().(string); !strings.Contains(q, "requireMarkers=true") {
		t.Errorf("map request query = %q, want requireMarkers=true", q)
	}
}
