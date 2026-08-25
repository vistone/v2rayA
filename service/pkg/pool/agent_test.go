package pool

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const sampleStatus = `{
  "node": { "id": "tile1.spacexway.com", "protocol": "tuic", "version": "0.2.40" },
  "system": { "load1": 0.35, "cpuPct": 5.2, "memUsedPct": 42.1, "activeConnections": 23 },
  "latency": { "target": "https://www.gstatic.com/generate_204", "ms": 18 },
  "traffic": {
    "usedBytes": 123456789, "quotaBytes": 1099511627776, "usedPct": 11.2,
    "warnPct": 80, "stopPct": 95, "nextReset": "2026-09-01T00:00:00Z",
    "tripped": false, "trippedAt": null, "checkSec": 300
  },
  "mesh": { "role": "master", "peerCount": 4 },
  "reportedAt": "2026-08-25T10:00:00Z"
}`

func newAgentServer(t *testing.T, token string, statusBody string, statusCode int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/v1/status"):
			if statusCode != http.StatusOK {
				w.WriteHeader(statusCode)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(statusBody))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/v1/control"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			action, _ := body["action"].(string)
			if action == "set-thresholds" {
				if _, ok := body["warnPct"]; !ok {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
			}
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestFetchStatus(t *testing.T) {
	srv := newAgentServer(t, "tok", sampleStatus, http.StatusOK)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	st, err := FetchStatus(ctx, srv.URL, "tok")
	if err != nil {
		t.Fatalf("FetchStatus: %v", err)
	}
	if st.Node.ID != "tile1.spacexway.com" {
		t.Fatalf("Node.ID = %q", st.Node.ID)
	}
	if st.Traffic.UsedPct != 11.2 || st.Traffic.Tripped {
		t.Fatalf("Traffic = %+v", st.Traffic)
	}
	if st.System.ActiveConnections != 23 {
		t.Fatalf("ActiveConnections = %d", st.System.ActiveConnections)
	}
}

func TestFetchStatusAuthError(t *testing.T) {
	srv := newAgentServer(t, "tok", sampleStatus, http.StatusOK)
	defer srv.Close()

	ctx := context.Background()
	if _, err := FetchStatus(ctx, srv.URL, "wrong"); err == nil {
		t.Fatal("FetchStatus with wrong token should fail")
	}
}

func TestFetchStatusServerError(t *testing.T) {
	srv := newAgentServer(t, "tok", "", http.StatusServiceUnavailable)
	defer srv.Close()

	ctx := context.Background()
	if _, err := FetchStatus(ctx, srv.URL, "tok"); err == nil {
		t.Fatal("FetchStatus with 503 should fail")
	}
}

func TestSendControl(t *testing.T) {
	srv := newAgentServer(t, "tok", sampleStatus, http.StatusOK)
	defer srv.Close()

	ctx := context.Background()
	if err := SendControl(ctx, srv.URL, "tok", ControlTrip, nil); err != nil {
		t.Fatalf("SendControl trip: %v", err)
	}
	if err := SendControl(ctx, srv.URL, "tok", ControlSetThresholds, map[string]any{
		"warnPct": 80.0, "stopPct": 95.0,
	}); err != nil {
		t.Fatalf("SendControl set-thresholds: %v", err)
	}
	// 校验失败：set-thresholds 缺 warnPct → 400 → 客户端应报错
	if err := SendControl(ctx, srv.URL, "tok", ControlSetThresholds, map[string]any{"stopPct": 95.0}); err == nil {
		t.Fatal("SendControl set-thresholds without warnPct should fail")
	}
}

func TestFetchStatusTrailingSlash(t *testing.T) {
	srv := newAgentServer(t, "tok", sampleStatus, http.StatusOK)
	defer srv.Close()

	ctx := context.Background()
	if _, err := FetchStatus(ctx, srv.URL+"/", "tok"); err != nil {
		t.Fatalf("FetchStatus with trailing slash: %v", err)
	}
}
