package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"miaomiaowu/internal/storage"
)

var errCFSMUnavailable = errors.New("CF-Server-Monitor monthly traffic unavailable")

type cfsmServer struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Rx   json.RawMessage `json:"net_rx_monthly"`
	Tx   json.RawMessage `json:"net_tx_monthly"`
}

// CF-Server-Monitor exposes a public, read-only inventory. No credentials are sent.
func fetchCFSMServers(ctx context.Context, client *http.Client, address string) ([]cfsmServer, error) {
	base, err := url.Parse(strings.TrimSpace(address))
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" || base.User != nil {
		return nil, fmt.Errorf("invalid CF-Server-Monitor address")
	}
	target := base.ResolveReference(&url.URL{Path: "/api/servers"})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("CF-Server-Monitor request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("CF-Server-Monitor HTTP status %s", resp.Status)
	}
	var payload struct {
		Servers []cfsmServer `json:"servers"`
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 8<<20))
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("parse CF-Server-Monitor response: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("invalid trailing CF-Server-Monitor response")
	}
	if payload.Servers == nil {
		return nil, fmt.Errorf("CF-Server-Monitor response missing servers")
	}
	seen := map[string]bool{}
	for _, srv := range payload.Servers {
		if strings.TrimSpace(srv.ID) == "" || seen[srv.ID] {
			return nil, fmt.Errorf("CF-Server-Monitor invalid or duplicate server ID")
		}
		seen[srv.ID] = true
	}
	return payload.Servers, nil
}

func (h *probeSyncHandler) fetchCFSMInventory(ctx context.Context, address string) ([]probeSyncServer, error) {
	client := h.cfsmClient
	if client == nil {
		client = h.client
	}
	servers, err := fetchCFSMServers(ctx, client, address)
	if err != nil {
		return nil, err
	}
	result := make([]probeSyncServer, 0, len(servers))
	for _, srv := range servers {
		result = append(result, probeSyncServer{ServerID: srv.ID, Name: srv.Name, TrafficMethod: "both"})
	}
	return result, nil
}

func cfsmMonthlyBytes(raw json.RawMessage) (int64, error) {
	value := string(raw)
	if len(raw) > 0 && raw[0] == '"' {
		if err := json.Unmarshal(raw, &value); err != nil {
			return 0, err
		}
	}
	number, err := strconv.ParseInt(value, 10, 64)
	if err != nil || number < 0 {
		return 0, fmt.Errorf("missing or invalid monthly byte counter %q", value)
	}
	return number, nil
}

// These are host/fleet monthly counters, not individual proxy-node usage.
// Never substitute lifetime net_rx/net_tx or infer a monthly allowance.
func (h *TrafficSummaryHandler) cfsmUsage(ctx context.Context, cfg storage.ProbeConfig) ([]int64, error) {
	client := h.cfsmClient
	if client == nil {
		client = h.client
	}
	servers, err := fetchCFSMServers(ctx, client, cfg.Address)
	if err != nil {
		return nil, err
	}
	observed := make(map[string]cfsmServer, len(servers))
	for _, srv := range servers {
		observed[srv.ID] = srv
	}
	usage := make([]int64, len(cfg.Servers))
	for i, srv := range cfg.Servers {
		report, ok := observed[srv.ServerID]
		if !ok {
			return nil, fmt.Errorf("CF-Server-Monitor server %s: missing monthly report", srv.ServerID)
		}
		rx, err := cfsmMonthlyBytes(report.Rx)
		if err != nil {
			return nil, fmt.Errorf("CF-Server-Monitor server %s net_rx_monthly: %w", srv.ServerID, err)
		}
		tx, err := cfsmMonthlyBytes(report.Tx)
		if err != nil {
			return nil, fmt.Errorf("CF-Server-Monitor server %s net_tx_monthly: %w", srv.ServerID, err)
		}
		switch srv.TrafficMethod {
		case storage.TrafficMethodUp:
			usage[i] = tx
		case storage.TrafficMethodDown:
			usage[i] = rx
		default:
			if rx > math.MaxInt64-tx {
				return nil, fmt.Errorf("CF-Server-Monitor monthly usage overflow")
			}
			usage[i] = rx + tx
		}
	}
	return usage, nil
}

func (h *TrafficSummaryHandler) fetchCFSMTotals(ctx context.Context, cfg storage.ProbeConfig) (limitResult, remainingResult, usedResult int64, resultErr error) {
	defer func() {
		if resultErr != nil {
			resultErr = fmt.Errorf("%w: %v", errCFSMUnavailable, resultErr)
		}
	}()
	usage, err := h.cfsmUsage(ctx, cfg)
	if err != nil {
		return 0, 0, 0, err
	}
	var limit, used int64
	for i, srv := range cfg.Servers {
		if srv.MonthlyTrafficBytes < 0 || limit > math.MaxInt64-srv.MonthlyTrafficBytes || used > math.MaxInt64-usage[i] {
			return 0, 0, 0, fmt.Errorf("CF-Server-Monitor traffic totals overflow")
		}
		limit += srv.MonthlyTrafficBytes
		used += usage[i]
	}
	remaining := limit - used
	if remaining < 0 {
		remaining = 0
	}
	return limit, remaining, used, nil
}

func (h *TrafficSummaryHandler) fetchCFSMPerServer(ctx context.Context, cfg storage.ProbeConfig) ([]ServerTraffic, error) {
	usage, err := h.cfsmUsage(ctx, cfg)
	if err != nil {
		return nil, err
	}
	result := make([]ServerTraffic, 0, len(cfg.Servers))
	for i, srv := range cfg.Servers {
		result = append(result, ServerTraffic{Name: srv.Name, LimitGB: roundUpTwoDecimals(bytesToGigabytes(srv.MonthlyTrafficBytes)), UsedGB: roundUpTwoDecimals(bytesToGigabytes(usage[i]))})
	}
	return result, nil
}
