package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"miaomiaowu/internal/storage"
)

const cfsmTestID = "550e8400-e29b-41d4-a716-446655440001"

func TestCFSMSyncInventory(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/servers" || r.UserAgent() != "Mozilla/5.0" {
			t.Errorf("unexpected request: %s %s UA=%s", r.Method, r.URL, r.UserAgent())
		}
		fmt.Fprintf(w, `{"servers":[{"id":%q,"name":"CF host","traffic_limit":999,"net_rx_monthly":12,"net_tx_monthly":34}]}`, cfsmTestID)
	}))
	defer upstream.Close()
	h := &probeSyncHandler{client: upstream.Client()}
	body := fmt.Sprintf(`{"probe_type":"cfsm","address":%q}`, upstream.URL)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("want sync 200, got %d: %s", w.Code, w.Body)
	}
	var got probeSyncResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Servers) != 1 || got.Servers[0].ServerID != cfsmTestID || got.Servers[0].Name != "CF host" || got.Servers[0].MonthlyTrafficGB != 0 || got.Servers[0].TrafficMethod != "both" {
		t.Fatalf("inventory/UUID/manual allowance: %+v", got)
	}
}

func TestCFSMMonthlyTrafficAllPaths(t *testing.T) {
	for _, tc := range []struct {
		mode string
		want int64
	}{{"up", 34}, {"down", 12}, {"both", 46}} {
		t.Run(tc.mode, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"servers":[{"id":%q,"name":"CF host","net_rx":999999,"net_tx":888888,"net_rx_monthly":"12","net_tx_monthly":34}]}`, cfsmTestID)
			}))
			defer upstream.Close()
			repo, err := storage.NewTrafficRepository(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer repo.Close()
			cfg, err := repo.UpsertProbeConfig(context.Background(), storage.ProbeConfig{ProbeType: "cfsm", Address: upstream.URL, Servers: []storage.ProbeServer{{ServerID: cfsmTestID, Name: "CF host", TrafficMethod: tc.mode, MonthlyTrafficBytes: 100}}})
			if err != nil {
				t.Fatal(err)
			}
			h := newTrafficSummaryHandler(upstream.Client(), repo)
			for name, fetch := range map[string]func() (int64, int64, int64, error){
				"fleet": func() (int64, int64, int64, error) { return h.fetchTotals(context.Background(), "", nil) },
				"uuid filter": func() (int64, int64, int64, error) {
					return h.fetchTotalsByServerIDs(context.Background(), []string{cfsmTestID})
				},
				"name filter": func() (int64, int64, int64, error) {
					return h.fetchTotals(context.Background(), "", map[string]struct{}{"CF host": {}})
				},
			} {
				limit, remaining, used, err := fetch()
				if err != nil || limit != 100 || remaining != 100-tc.want || used != tc.want {
					t.Fatalf("%s: %d %d %d %v", name, limit, remaining, used, err)
				}
			}
			per, err := h.fetchPerServerTraffic(context.Background(), cfg)
			if err != nil || len(per) != 1 || per[0].UsedGB != roundUpTwoDecimals(bytesToGigabytes(tc.want)) {
				t.Fatalf("per server: %+v %v", per, err)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
			if w.Code != 200 {
				t.Fatalf("summary HTTP: %d %s", w.Code, w.Body)
			}
			w = httptest.NewRecorder()
			h.HandleSubscribeTraffic(w, httptest.NewRequest("GET", "/", nil))
			if w.Code != 200 {
				t.Fatalf("subscribe HTTP: %d %s", w.Code, w.Body)
			}
			if _, _, servers, _, err := h.FetchTrafficSummaryForNotify(context.Background()); err != nil || len(servers) != 1 {
				t.Fatalf("notify: %+v %v", servers, err)
			}
			if err := h.RecordDailyUsage(context.Background()); err != nil {
				t.Fatalf("daily snapshot: %v", err)
			}
		})
	}
}
