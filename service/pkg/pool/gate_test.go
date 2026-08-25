package pool

import (
	"testing"

	"github.com/v2rayA/v2rayA/db/configure"
)

func gateSettings(gate, hys float64) configure.PoolSettings {
	return configure.PoolSettings{TrafficGatePct: gate, HysteresisPct: hys}
}

func TestEvaluateTrafficGate(t *testing.T) {
	s := gateSettings(90, 5)

	// 低于门槛：保持 active
	if next, changed := Evaluate(GateActive, MemberEval{Reachable: true, UsedPct: 50}, s); changed || next != GateActive {
		t.Fatalf("below gate: got %v changed=%v", next, changed)
	}
	// 达到门槛：摘除
	if next, changed := Evaluate(GateActive, MemberEval{Reachable: true, UsedPct: 90}, s); !changed || next != GateExcluded {
		t.Fatalf("at gate: got %v changed=%v", next, changed)
	}
	// 低于门槛-滞后：加回
	if next, changed := Evaluate(GateExcluded, MemberEval{Reachable: true, UsedPct: 84}, s); !changed || next != GateActive {
		t.Fatalf("below gate-hys: got %v changed=%v", next, changed)
	}
	// 在 (gate-hys, gate) 区间内：保持原状（不抖动）
	if next, changed := Evaluate(GateExcluded, MemberEval{Reachable: true, UsedPct: 87}, s); changed || next != GateExcluded {
		t.Fatalf("in hysteresis band: got %v changed=%v", next, changed)
	}
	// 已摘除且仍超门槛：不重复触发
	if next, changed := Evaluate(GateExcluded, MemberEval{Reachable: true, UsedPct: 95}, s); changed || next != GateExcluded {
		t.Fatalf("already excluded at gate: got %v changed=%v", next, changed)
	}
}

func TestEvaluateUnreachableKeepsState(t *testing.T) {
	s := gateSettings(90, 5)
	// 不可达：fail-open，保持现状
	if next, changed := Evaluate(GateActive, MemberEval{Reachable: false}, s); changed || next != GateActive {
		t.Fatalf("unreachable active: got %v changed=%v", next, changed)
	}
	if next, changed := Evaluate(GateExcluded, MemberEval{Reachable: false}, s); changed || next != GateExcluded {
		t.Fatalf("unreachable excluded: got %v changed=%v", next, changed)
	}
}

func TestEvaluateTripped(t *testing.T) {
	s := gateSettings(90, 5)
	// 服务器已熔断停服：摘除
	if next, changed := Evaluate(GateActive, MemberEval{Reachable: true, UsedPct: 30, Tripped: true}, s); !changed || next != GateExcluded {
		t.Fatalf("tripped: got %v changed=%v", next, changed)
	}
}

func TestSelectFailOpenMember(t *testing.T) {
	evals := []MemberEval{
		{Reachable: true, UsedPct: 95},
		{Reachable: true, UsedPct: 91},
		{Reachable: false, UsedPct: 0},
	}
	if idx := SelectFailOpenMember(evals); idx != 1 {
		t.Fatalf("SelectFailOpenMember = %d, want 1", idx)
	}
	// 全部不可达：仍返回一个（最低 usedPct 优先）
	evals = []MemberEval{
		{Reachable: false, UsedPct: 50},
		{Reachable: false, UsedPct: 10},
	}
	if idx := SelectFailOpenMember(evals); idx != 1 {
		t.Fatalf("SelectFailOpenMember all unreachable = %d, want 1", idx)
	}
	// 空列表：返回 -1
	if idx := SelectFailOpenMember(nil); idx != -1 {
		t.Fatalf("SelectFailOpenMember empty = %d, want -1", idx)
	}
}

func TestGateStateString(t *testing.T) {
	if GateActive.String() != "active" || GateExcluded.String() != "excluded" {
		t.Fatal("GateState.String mismatch")
	}
}
