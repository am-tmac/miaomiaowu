package handler

import (
	"context"
	"fmt"
	"miaomiaowu/internal/storage"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCFSMRejectsInvalidReports(t *testing.T) {
	cases := []struct {
		name, body string
		status     int
	}{
		{"missing server", `{"servers":[]}`, 200},
		{"missing month no lifetime fallback", fmt.Sprintf(`{"servers":[{"id":%q,"net_rx":100,"net_tx":200}]}`, cfsmTestID), 200},
		{"null month", fmt.Sprintf(`{"servers":[{"id":%q,"net_rx_monthly":null,"net_tx_monthly":0}]}`, cfsmTestID), 200},
		{"negative", fmt.Sprintf(`{"servers":[{"id":%q,"net_rx_monthly":-1,"net_tx_monthly":0}]}`, cfsmTestID), 200},
		{"nonfinite", fmt.Sprintf(`{"servers":[{"id":%q,"net_rx_monthly":"NaN","net_tx_monthly":0}]}`, cfsmTestID), 200},
		{"infinite", fmt.Sprintf(`{"servers":[{"id":%q,"net_rx_monthly":"Infinity","net_tx_monthly":0}]}`, cfsmTestID), 200},
		{"overflow", fmt.Sprintf(`{"servers":[{"id":%q,"net_rx_monthly":9223372036854775807,"net_tx_monthly":1}]}`, cfsmTestID), 200},
		{"malformed", `{"servers":`, 200}, {"trailing", `{"servers":[]} {}`, 200}, {"wrong envelope", `{}`, 200},
		{"http error", `{"servers":[]}`, 503},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) }))
			defer upstream.Close()
			h := newTrafficSummaryHandler(upstream.Client(), nil)
			cfg := storage.ProbeConfig{ProbeType: "cfsm", Address: upstream.URL, Servers: []storage.ProbeServer{{ServerID: cfsmTestID, TrafficMethod: "both", MonthlyTrafficBytes: 100}}}
			if _, _, _, err := h.fetchCFSMTotals(context.Background(), cfg); err == nil {
				t.Fatal("expected explicit monthly report error")
			}
			if _, err := h.fetchPerServerTraffic(context.Background(), cfg); err == nil {
				t.Fatal("expected per-server error")
			}
		})
	}
}

func TestCFSMUnavailableReportPropagates(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"servers":[]}`) }))
	defer upstream.Close()
	repo, err := storage.NewTrafficRepository(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	_, err = repo.UpsertProbeConfig(context.Background(), storage.ProbeConfig{ProbeType: "cfsm", Address: upstream.URL, Servers: []storage.ProbeServer{{ServerID: cfsmTestID, Name: "host", TrafficMethod: "both", MonthlyTrafficBytes: 100}}})
	if err != nil {
		t.Fatal(err)
	}
	h := newTrafficSummaryHandler(upstream.Client(), repo)
	if _, _, _, _, err := h.FetchTrafficSummaryForNotify(context.Background()); err == nil {
		t.Error("notifications must not fabricate zero when monthly report is unavailable")
	}
	w := httptest.NewRecorder()
	h.HandleSubscribeTraffic(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 502 {
		t.Errorf("subscribe traffic must report error, got %d: %s", w.Code, w.Body)
	}
}

func TestCFSMProductionClientsBlockSSRF(t *testing.T) {
	hits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++; fmt.Fprint(w, `{"servers":[]}`) }))
	defer upstream.Close()
	repo, err := storage.NewTrafficRepository(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	sync := NewProbeSyncHandler(repo).(*probeSyncHandler)
	if _, err := sync.fetchCFSMInventory(context.Background(), upstream.URL); err == nil {
		t.Fatal("sync SSRF allowed loopback")
	}
	h := NewTrafficSummaryHandler(repo)
	if _, err := h.cfsmUsage(context.Background(), storage.ProbeConfig{Address: upstream.URL}); err == nil {
		t.Fatal("traffic SSRF allowed loopback")
	}
	if hits != 0 {
		t.Fatalf("SSRF reached upstream %d times", hits)
	}
}

func TestCFSMAdminConfigRoundTrip(t *testing.T) {
	repo, err := storage.NewTrafficRepository(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	h := NewProbeConfigHandler(repo)
	body := fmt.Sprintf(`{"probe_type":"cfsm","address":"https://status.example","servers":[{"server_id":%q,"name":"host","traffic_method":"down","monthly_traffic_gb":123}]}`, cfsmTestID)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("PUT", "/", strings.NewReader(body)))
	if w.Code != 200 {
		t.Fatalf("admin config: %d %s", w.Code, w.Body)
	}
	cfg, err := repo.GetProbeConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProbeType != "cfsm" || cfg.Servers[0].ServerID != cfsmTestID || cfg.Servers[0].MonthlyTrafficBytes != 123*1073741824 {
		t.Fatalf("round trip: %+v", cfg)
	}
}
