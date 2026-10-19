package handler

import (
	"context"
	"miaomiaowu/internal/storage"
	"os"
	"testing"
	"time"
)

// Opt-in public, anonymous smoke test. Never uses a production database.
func TestCFSMPublicAPI(t *testing.T) {
	address := os.Getenv("CFSM_PUBLIC_TEST_URL")
	if address == "" {
		t.Skip("set CFSM_PUBLIC_TEST_URL for anonymous public API smoke test")
	}
	client := newSSRFSafeHTTPClient(20 * time.Second)
	servers, err := fetchCFSMServers(context.Background(), client, address)
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) == 0 {
		t.Fatal("public inventory empty")
	}
	cfg := storage.ProbeConfig{ProbeType: "cfsm", Address: address}
	for _, srv := range servers {
		cfg.Servers = append(cfg.Servers, storage.ProbeServer{ServerID: srv.ID, Name: srv.Name, TrafficMethod: "both"})
	}
	h := newTrafficSummaryHandler(client, nil)
	_, _, used, err := h.fetchCFSMTotals(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("anonymous CFSM API: %d UUID servers, combined monthly usage %d bytes; allowance not inferred", len(servers), used)
}
