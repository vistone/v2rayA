package pool

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/v2rayA/v2rayA/db/configure"
)

// statusJSON 构造一个给定 usedPct / tripped 的状态响应。
func statusJSON(usedPct float64, tripped bool) string {
	b, _ := json.Marshal(map[string]any{
		"node":   map[string]any{"id": "vps", "protocol": "tuic", "version": "0.2.40"},
		"system": map[string]any{"load1": 0.1, "cpuPct": 1.0, "memUsedPct": 10.0, "activeConnections": 3},
		"traffic": map[string]any{
			"usedBytes": int64(usedPct * 1e9), "quotaBytes": int64(1e11), "usedPct": usedPct,
			"warnPct": 80.0, "stopPct": 95.0, "nextReset": "", "tripped": tripped, "checkSec": 300,
		},
		"reportedAt": time.Now().Format(time.RFC3339),
	})
	return string(b)
}

func newStatusServer(t *testing.T, usedPct float64, tripped bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/v1/status") {
			_, _ = w.Write([]byte(statusJSON(usedPct, tripped)))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func testPool(name, outbound string, members []configure.PoolMember, s configure.PoolSettings) configure.Pool {
	return configure.Pool{
		Name:     name,
		Outbound: outbound,
		Members:  members,
		Settings: s,
	}
}

func boolPtr(b bool) *bool { return &b }

func TestEngineExcludesAtGateAndFiresOnce(t *testing.T) {
	srv := newStatusServer(t, 95, false) // usedPct 95 ≥ gate 90
	defer srv.Close()

	var changed int32
	e := NewEngine(func() { atomic.AddInt32(&changed, 1) })

	p := testPool("poolA", "", []configure.PoolMember{
		{Which: configure.Which{TYPE: configure.ServerType, ID: 1}, AgentURL: srv.URL, AgentToken: "tok", Enabled: true},
	}, configure.PoolSettings{TrafficGatePct: 90, HysteresisPct: 5, PollInterval: "200ms", FailOpen: boolPtr(false)})
	e.Start([]configure.Pool{p})
	defer e.Stop()

	// 等待首次轮询完成
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		st, ok := e.State("poolA")
		if ok && st.LastPoll.After(time.Time{}) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	st, ok := e.State("poolA")
	if !ok || len(st.Members) != 1 {
		t.Fatalf("state not ready: %+v", st)
	}
	ms := st.Members[0]
	if !ms.Reachable {
		t.Fatalf("member should be reachable, err=%q", ms.Err)
	}
	if ms.Gate != GateExcluded {
		t.Fatalf("gate = %v, want excluded", ms.Gate)
	}
	// 成员集合只变化一次（首次轮询），后续轮询不应重复触发
	if got := atomic.LoadInt32(&changed); got != 1 {
		t.Fatalf("onChange called %d times, want 1", got)
	}
	// ActiveMembers 应为空
	if got := e.ActiveMembers(); len(got) != 0 {
		t.Fatalf("ActiveMembers = %v, want empty", got)
	}
}

func TestEngineReaddsBelowGate(t *testing.T) {
	var used atomic.Value
	used.Store(95.0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/v1/status") {
			_, _ = w.Write([]byte(statusJSON(used.Load().(float64), false)))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	var changed int32
	e := NewEngine(func() { atomic.AddInt32(&changed, 1) })
	p := testPool("poolA", "poolA", []configure.PoolMember{
		{Which: configure.Which{TYPE: configure.ServerType, ID: 1}, AgentURL: srv.URL, AgentToken: "tok", Enabled: true},
		{Which: configure.Which{TYPE: configure.ServerType, ID: 2}, AgentURL: srv.URL, AgentToken: "tok", Enabled: true},
	}, configure.PoolSettings{TrafficGatePct: 90, HysteresisPct: 5, PollInterval: "1s"})
	e.Start([]configure.Pool{p})
	defer e.Stop()

	waitActive := func(want int) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			st, _ := e.State("poolA")
			if st == nil {
				time.Sleep(20 * time.Millisecond)
				continue
			}
			active := 0
			for _, ms := range st.Members {
				if ms.Gate == GateActive {
					active++
				}
			}
			if active == want {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		st, _ := e.State("poolA")
		t.Fatalf("timed out waiting for %d active member(s): %+v", want, st)
	}

	// fail-open（默认开）：全部被摘除后放回一个（usedPct 相同取第一个）
	waitActive(1)

	// 用量回落到门槛以下：全部加回
	used.Store(50.0)
	waitActive(2)
	got := e.ActiveMembers()
	if len(got) != 2 {
		t.Fatalf("ActiveMembers = %d, want 2", len(got))
	}
	for _, w := range got {
		if w.Outbound != "poolA" {
			t.Fatalf("member outbound = %q, want poolA", w.Outbound)
		}
	}
}

func TestEngineDisabledMemberIgnored(t *testing.T) {
	srv := newStatusServer(t, 95, false)
	defer srv.Close()

	var changed int32
	e := NewEngine(func() { atomic.AddInt32(&changed, 1) })
	p := testPool("poolA", "", []configure.PoolMember{
		{Which: configure.Which{TYPE: configure.ServerType, ID: 1}, AgentURL: srv.URL, AgentToken: "tok", Enabled: false},
	}, configure.PoolSettings{TrafficGatePct: 90, HysteresisPct: 5, PollInterval: "200ms"})
	e.Start([]configure.Pool{p})
	defer e.Stop()

	time.Sleep(500 * time.Millisecond)
	if got := atomic.LoadInt32(&changed); got != 0 {
		t.Fatalf("disabled member should not trigger onChange, got %d", got)
	}
	if got := e.ActiveMembers(); len(got) != 0 {
		t.Fatalf("disabled member should not be active, got %v", got)
	}
}

func TestEngineSyncStopsRemovedPool(t *testing.T) {
	srv := newStatusServer(t, 10, false)
	defer srv.Close()

	e := NewEngine(nil)
	p := testPool("poolA", "", []configure.PoolMember{
		{Which: configure.Which{TYPE: configure.ServerType, ID: 1}, AgentURL: srv.URL, AgentToken: "tok", Enabled: true},
	}, configure.PoolSettings{PollInterval: "100ms"})
	e.Start([]configure.Pool{p})

	time.Sleep(300 * time.Millisecond)
	if _, ok := e.State("poolA"); !ok {
		t.Fatal("poolA should exist")
	}
	e.Sync(nil)
	if _, ok := e.State("poolA"); ok {
		t.Fatal("poolA should be removed after Sync(nil)")
	}
}

func TestEngineNoOscillationWithFailOpen(t *testing.T) {
	// 全部成员超门槛 + fail-open：净变化检测必须保证 onChange 只触发一次（首次），
	// 不允许每轮"摘除→放回"反复触发（配置风暴）。
	// 注：pollLoop 将 <1s 的间隔钳制为 30s，故用精确的 1s 以覆盖多个轮询周期。
	srv := newStatusServer(t, 95, false)
	defer srv.Close()

	var changed int32
	e := NewEngine(func() { atomic.AddInt32(&changed, 1) })
	p := testPool("poolA", "", []configure.PoolMember{
		{Which: configure.Which{TYPE: configure.ServerType, ID: 1}, AgentURL: srv.URL, AgentToken: "tok", Enabled: true},
		{Which: configure.Which{TYPE: configure.ServerType, ID: 2}, AgentURL: srv.URL, AgentToken: "tok", Enabled: true},
	}, configure.PoolSettings{TrafficGatePct: 90, HysteresisPct: 5, PollInterval: "1s"})
	e.Start([]configure.Pool{p})
	defer e.Stop()

	time.Sleep(2500 * time.Millisecond) // 覆盖多个轮询周期（1s 下限）
	if got := atomic.LoadInt32(&changed); got != 1 {
		t.Fatalf("fail-open oscillation: onChange called %d times, want 1", got)
	}
}
