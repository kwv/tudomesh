package mesh

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Run with -race: readers and writers of a shared CalibrationData must not race.
func TestCalibrationDataConcurrentAccess(t *testing.T) {
	cal := &CalibrationData{Vacuums: map[string]VacuumCalibration{}}
	path := filepath.Join(t.TempDir(), "cal.json")

	var wg sync.WaitGroup
	stop := make(chan struct{})
	worker := func(f func(i int)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
					f(i)
				}
			}
		}()
	}
	worker(func(i int) {
		cal.UpdateVacuumCalibration("v", VacuumCalibration{Transform: Translation(float64(i), 0), LastUpdated: int64(i)})
	})
	worker(func(int) { cal.SetReference("ref") })
	worker(func(int) { _ = cal.GetTransform("v") })
	worker(func(int) { _ = cal.ShouldRecalibrate("v", 1, time.Minute) })
	worker(func(int) { _ = cal.Transforms(); _ = cal.Reference() })
	worker(func(int) { _ = cal.GetStatus([]string{"v"}) })
	worker(func(int) { _ = SaveCalibration(path, cal) })

	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()

	loaded, err := LoadCalibration(path)
	if err != nil || loaded == nil || loaded.Reference() != "ref" {
		t.Fatalf("saved cache unreadable: %v %+v", err, loaded)
	}
}

// robotAPI serves validMap; if gate is non-nil it waits for it to close first.
func robotAPI(t *testing.T, hits *int32, started chan<- struct{}, gate <-chan struct{}) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(hits, 1)
		if started != nil {
			select {
			case started <- struct{}{}:
			default:
			}
		}
		if gate != nil {
			<-gate
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(validMap(1500))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestOnDockingEvent_SlowRobotDoesNotBlockOthers(t *testing.T) {
	var slowHits, fastHits int32
	started := make(chan struct{}, 1)
	gate := make(chan struct{})
	slow := robotAPI(t, &slowHits, started, gate)
	fast := robotAPI(t, &fastHits, nil, nil)

	slowURL, fastURL := slow.URL, fast.URL
	cfg := &Config{
		Reference: "ref",
		Vacuums: []VacuumConfig{
			{ID: "ref"},
			{ID: "slow", ApiURL: &slowURL},
			{ID: "fast", ApiURL: &fastURL},
		},
	}
	st := NewStateTracker()
	st.UpdateMap("ref", validMap(2000))
	cache := &CalibrationData{ReferenceVacuum: "ref", Vacuums: map[string]VacuumCalibration{}}
	ac := NewAutoCalibrator(cfg, cache, filepath.Join(t.TempDir(), "cal.json"), "", st)

	slowDone := make(chan struct{})
	go func() {
		ac.OnDockingEvent("slow")
		close(slowDone)
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("slow robot was never contacted")
	}

	// A second event for the same robot while it is in flight is skipped.
	ac.OnDockingEvent("slow")
	if n := atomic.LoadInt32(&slowHits); n != 1 {
		t.Errorf("slow robot fetched %d times, want 1 while in flight", n)
	}

	// Another robot calibrates while the slow fetch is still blocked.
	fastDone := make(chan struct{})
	go func() {
		ac.OnDockingEvent("fast")
		close(fastDone)
	}()
	select {
	case <-fastDone:
	case <-time.After(5 * time.Second):
		t.Fatal("fast robot blocked behind the slow one")
	}
	if cache.GetVacuumCalibration("fast") == nil {
		t.Error("fast robot was not calibrated")
	}

	close(gate)
	select {
	case <-slowDone:
	case <-time.After(5 * time.Second):
		t.Fatal("slow robot never finished")
	}
	if cache.GetVacuumCalibration("slow") == nil {
		t.Error("slow robot was not calibrated after its fetch completed")
	}
}
