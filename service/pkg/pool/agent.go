package pool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultAgentTimeout = 10 * time.Second
	statusPath          = "/v1/status"
	controlPath         = "/v1/control"
)

type TrafficStatus struct {
	UsedBytes  int64      `json:"usedBytes"`
	QuotaBytes int64      `json:"quotaBytes"`
	UsedPct    float64    `json:"usedPct"`
	WarnPct    float64    `json:"warnPct"`
	StopPct    float64    `json:"stopPct"`
	NextReset  string     `json:"nextReset"`
	Tripped    bool       `json:"tripped"`
	TrippedAt  *time.Time `json:"trippedAt"`
	CheckSec   int        `json:"checkSec"`
}

type AgentStatus struct {
	Node struct {
		ID       string `json:"id"`
		Protocol string `json:"protocol"`
		Version  string `json:"version"`
	} `json:"node"`
	System struct {
		Load1             float64 `json:"load1"`
		CPUPct            float64 `json:"cpuPct"`
		MemUsedPct        float64 `json:"memUsedPct"`
		ActiveConnections int     `json:"activeConnections"`
	} `json:"system"`
	Latency struct {
		Target string  `json:"target"`
		Ms     float64 `json:"ms"`
	} `json:"latency"`
	Traffic TrafficStatus `json:"traffic"`
	Mesh    struct {
		Role      string `json:"role"`
		PeerCount int    `json:"peerCount"`
	} `json:"mesh"`
	ReportedAt time.Time `json:"reportedAt"`
}

type ControlAction string

const (
	ControlTrip             ControlAction = "trip"
	ControlResume           ControlAction = "resume"
	ControlSetThresholds    ControlAction = "set-thresholds"
	ControlSetCheckInterval ControlAction = "set-check-interval"
)

func joinURL(base, path string) string {
	return strings.TrimRight(base, "/") + path
}

func newRequest(ctx context.Context, method, url, token string, body any) (*http.Request, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func do(ctx context.Context, method, url, token string, body any, out any) error {
	req, err := newRequest(ctx, method, url, token, body)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: defaultAgentTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("agent %s %s: status %d: %s", method, url, resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// FetchStatus 拉取某台 VPS agent 的实时状态。
func FetchStatus(ctx context.Context, agentURL, token string) (*AgentStatus, error) {
	var st AgentStatus
	if err := do(ctx, http.MethodGet, joinURL(agentURL, statusPath), token, nil, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

// SendControl 向 agent 发送控制指令。
func SendControl(ctx context.Context, agentURL, token string, action ControlAction, params map[string]any) error {
	body := map[string]any{"action": string(action)}
	for k, v := range params {
		body[k] = v
	}
	return do(ctx, http.MethodPost, joinURL(agentURL, controlPath), token, body, nil)
}
