package mesh

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeValetudo serves the two endpoints DiscoverVacuum reads.
func fakeValetudo(t *testing.T, systemID, identifier, prefix string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/valetudo":
			_ = json.NewEncoder(w).Encode(map[string]any{"embedded": true, "systemId": systemID})
		case "/api/v2/valetudo/config/interfaces/mqtt":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"connection":     map[string]any{"host": "mqtt.home", "port": 1883},
				"identity":       map[string]any{"identifier": identifier},
				"customizations": map[string]any{"topicPrefix": prefix},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestResolveVacuums_FromURL(t *testing.T) {
	srv := fakeValetudo(t, "NormalLostHamster", "rocky", "")
	cfg := &Config{Vacuums: []VacuumConfig{{URL: srv.URL + "/", Color: "#123456"}}}
	cachePath := filepath.Join(t.TempDir(), ".vacuums.json")

	ResolveVacuums(cfg, cachePath, time.Second)

	vc := cfg.Vacuums[0]
	if vc.ID != "NormalLostHamster" || vc.Topic != "valetudo/rocky/MapData/map-data" {
		t.Errorf("id/topic = %q/%q", vc.ID, vc.Topic)
	}
	if vc.ApiURL == nil || *vc.ApiURL != srv.URL+"/api/v2/robot/state/map" {
		t.Errorf("apiUrl = %v", vc.ApiURL)
	}
	if cfg.MQTT.Broker != "mqtt://mqtt.home:1883" {
		t.Errorf("broker = %q", cfg.MQTT.Broker)
	}
	if _, err := os.Stat(cachePath); err != nil {
		t.Errorf("identity cache not written: %v", err)
	}
}

func TestResolveVacuums_ExplicitValuesWin(t *testing.T) {
	srv := fakeValetudo(t, "NormalLostHamster", "rocky", "home/valetudo")
	cfg := &Config{
		MQTT:    MQTTConfig{Broker: "mqtt://other:1883"},
		Vacuums: []VacuumConfig{{URL: srv.URL, ID: "basement"}},
	}
	ResolveVacuums(cfg, filepath.Join(t.TempDir(), ".vacuums.json"), time.Second)
	if vc := cfg.Vacuums[0]; vc.ID != "basement" || vc.Topic != "home/valetudo/rocky/MapData/map-data" {
		t.Errorf("id/topic = %q/%q", vc.ID, vc.Topic)
	}
	if cfg.MQTT.Broker != "mqtt://other:1883" {
		t.Errorf("configured broker overwritten: %q", cfg.MQTT.Broker)
	}
}

func TestResolveVacuums_OfflineUsesCacheThenHostName(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, ".vacuums.json")

	// Learn the identity while online.
	srv := fakeValetudo(t, "NormalLostHamster", "rocky", "")
	ResolveVacuums(&Config{Vacuums: []VacuumConfig{{URL: srv.URL}}}, cachePath, time.Second)
	srv.Close()

	// Same URL offline: cached identity.
	cfg := &Config{Vacuums: []VacuumConfig{{URL: srv.URL}}}
	ResolveVacuums(cfg, cachePath, 200*time.Millisecond)
	if cfg.Vacuums[0].ID != "NormalLostHamster" {
		t.Errorf("cached id = %q", cfg.Vacuums[0].ID)
	}

	// Unknown, unreachable host name: guessed from the first label.
	cfg = &Config{Vacuums: []VacuumConfig{{URL: "http://rocky6.invalid"}}}
	ResolveVacuums(cfg, cachePath, 200*time.Millisecond)
	if len(cfg.Vacuums) != 1 || cfg.Vacuums[0].ID != "rocky6" || cfg.Vacuums[0].Topic != "valetudo/rocky6/MapData/map-data" {
		t.Errorf("guessed = %+v", cfg.Vacuums)
	}

	// Unreachable IP with nothing cached cannot be addressed and is dropped.
	cfg = &Config{Vacuums: []VacuumConfig{{URL: "http://127.0.0.1:1"}}}
	ResolveVacuums(cfg, cachePath, 200*time.Millisecond)
	if len(cfg.Vacuums) != 0 {
		t.Errorf("unaddressable vacuum kept: %+v", cfg.Vacuums)
	}
}

func TestVacuumConfigBaseURL(t *testing.T) {
	api := "http://rocky7.home/api/v2/robot/state/map"
	cases := []struct {
		vc   VacuumConfig
		want string
	}{
		{VacuumConfig{URL: "http://rocky.home/"}, "http://rocky.home"},
		{VacuumConfig{ApiURL: &api}, "http://rocky7.home"},
		{VacuumConfig{ID: "x", Topic: "t"}, ""},
	}
	for _, c := range cases {
		if got := c.vc.BaseURL(); got != c.want {
			t.Errorf("BaseURL(%+v) = %q, want %q", c.vc, got, c.want)
		}
	}
}
