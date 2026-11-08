package mesh

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// VacuumIdentity is what tudomesh learns from a robot's Valetudo API.
type VacuumIdentity struct {
	ID          string `json:"id"`               // Valetudo systemId, e.g. "FrugalLameLion"
	Name        string `json:"name"`             // MQTT identifier, e.g. "rockette"
	TopicPrefix string `json:"topicPrefix"`      // MQTT topic prefix, default "valetudo"
	Broker      string `json:"broker,omitempty"` // broker the robot uses, "mqtt://host:port"
}

// MapTopic is the robot's MQTT map-data topic.
func (v VacuumIdentity) MapTopic() string {
	return fmt.Sprintf("%s/%s/MapData/map-data", v.TopicPrefix, v.Name)
}

// apiMapPath is appended to a vacuum's base URL to fetch its full map.
const apiMapPath = "/api/v2/robot/state/map"

// BaseURL returns the robot's Valetudo base URL: URL if set, otherwise
// ApiURL with the API path removed. "" if neither is set.
func (vc *VacuumConfig) BaseURL() string {
	if vc.URL != "" {
		return strings.TrimRight(vc.URL, "/")
	}
	if vc.ApiURL != nil {
		if i := strings.Index(*vc.ApiURL, "/api/"); i > 0 {
			return (*vc.ApiURL)[:i]
		}
	}
	return ""
}

// DiscoverVacuum asks the Valetudo API at baseURL for the robot's identity.
func DiscoverVacuum(ctx context.Context, client *http.Client, baseURL string) (VacuumIdentity, error) {
	get := func(path string, v any) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("GET %s: %s", path, resp.Status)
		}
		return json.NewDecoder(resp.Body).Decode(v)
	}

	var info struct {
		SystemID string `json:"systemId"`
	}
	if err := get("/api/v2/valetudo", &info); err != nil {
		return VacuumIdentity{}, err
	}
	var mqttCfg struct {
		Connection struct {
			Host string `json:"host"`
			Port int    `json:"port"`
		} `json:"connection"`
		Identity struct {
			Identifier string `json:"identifier"`
		} `json:"identity"`
		Customizations struct {
			TopicPrefix string `json:"topicPrefix"`
		} `json:"customizations"`
	}
	if err := get("/api/v2/valetudo/config/interfaces/mqtt", &mqttCfg); err != nil {
		return VacuumIdentity{}, err
	}
	if info.SystemID == "" || mqttCfg.Identity.Identifier == "" {
		return VacuumIdentity{}, fmt.Errorf("%s: Valetudo API returned no systemId or MQTT identifier", baseURL)
	}

	id := VacuumIdentity{
		ID:          info.SystemID,
		Name:        mqttCfg.Identity.Identifier,
		TopicPrefix: mqttCfg.Customizations.TopicPrefix,
	}
	if id.TopicPrefix == "" {
		id.TopicPrefix = "valetudo"
	}
	if h := mqttCfg.Connection.Host; h != "" {
		port := mqttCfg.Connection.Port
		if port == 0 {
			port = 1883
		}
		id.Broker = fmt.Sprintf("mqtt://%s", net.JoinHostPort(h, fmt.Sprint(port)))
	}
	return id, nil
}

// identityFromHost guesses an identity from the URL's first host label
// ("http://rocky6.home" -> "rocky6"), used when the robot is unreachable
// and nothing is cached.
func identityFromHost(baseURL string) (VacuumIdentity, bool) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Hostname() == "" || net.ParseIP(u.Hostname()) != nil {
		return VacuumIdentity{}, false
	}
	name := strings.SplitN(u.Hostname(), ".", 2)[0]
	return VacuumIdentity{ID: name, Name: name, TopicPrefix: "valetudo"}, true
}

// ResolveVacuums completes vacuum entries that only give a URL: ID, Topic
// and ApiURL are filled from each robot's Valetudo API (explicit values in
// config win). If the config has no MQTT broker, the first robot's broker is
// used. Lookups are cached in cachePath (keyed by base URL) so robots that
// are offline at startup still resolve; failing that, the URL's host name is
// used as the robot's name.
func ResolveVacuums(cfg *Config, cachePath string, timeout time.Duration) {
	cache := map[string]VacuumIdentity{}
	if data, err := os.ReadFile(cachePath); err == nil {
		if err := json.Unmarshal(data, &cache); err != nil {
			log.Printf("Warning: ignoring unreadable %s: %v", cachePath, err)
		}
	}

	type result struct {
		id  VacuumIdentity
		err error
	}
	results := make([]result, len(cfg.Vacuums))
	client := &http.Client{Timeout: timeout}
	var wg sync.WaitGroup
	for i := range cfg.Vacuums {
		base := cfg.Vacuums[i].BaseURL()
		if base == "" {
			continue
		}
		wg.Add(1)
		go func(i int, base string) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			id, err := DiscoverVacuum(ctx, client, base)
			results[i] = result{id, err}
		}(i, base)
	}
	wg.Wait()

	changed := false
	for i := range cfg.Vacuums {
		vc := &cfg.Vacuums[i]
		base := vc.BaseURL()
		if base == "" {
			continue
		}
		id, err := results[i].id, results[i].err
		switch {
		case err == nil:
			if cache[base] != id {
				cache[base], changed = id, true
			}
			log.Printf("Discovered %s: id=%s name=%s topic=%s", base, id.ID, id.Name, id.MapTopic())
		case cache[base].ID != "":
			id = cache[base]
			log.Printf("Warning: %s unreachable (%v); using cached identity %s", base, err, id.ID)
		default:
			guess, ok := identityFromHost(base)
			switch {
			case vc.ID != "" && vc.Topic != "":
				log.Printf("Warning: %s unreachable (%v); using id and topic from config", base, err)
				id = VacuumIdentity{}
			case !ok:
				log.Printf("Warning: %s unreachable (%v) and not cached; set id and topic in config", base, err)
				continue
			default:
				id = guess
				log.Printf("Warning: %s unreachable (%v); guessing name %q from the host name", base, err, id.Name)
			}
		}

		if vc.ID == "" {
			vc.ID = id.ID
		}
		if vc.Topic == "" {
			vc.Topic = id.MapTopic()
		}
		if vc.ApiURL == nil {
			api := base + apiMapPath
			vc.ApiURL = &api
		}
		if cfg.MQTT.Broker == "" && id.Broker != "" {
			cfg.MQTT.Broker = id.Broker
			log.Printf("MQTT broker from %s: %s", base, id.Broker)
		}
	}

	// Drop entries that still cannot be addressed.
	kept := cfg.Vacuums[:0]
	for _, vc := range cfg.Vacuums {
		if vc.ID != "" && vc.Topic != "" {
			kept = append(kept, vc)
		}
	}
	cfg.Vacuums = kept

	if changed && cachePath != "" {
		data, _ := json.MarshalIndent(cache, "", "  ")
		if err := os.WriteFile(cachePath, data, 0644); err != nil {
			log.Printf("Warning: could not save %s: %v", cachePath, err)
		}
	}
}
