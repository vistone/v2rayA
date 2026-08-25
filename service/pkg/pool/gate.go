package pool

import "github.com/v2rayA/v2rayA/db/configure"

type GateState int

const (
	GateActive   GateState = iota // 在 balancer selector 中
	GateExcluded                  // 因流量硬门槛被摘除
)

func (s GateState) String() string {
	if s == GateExcluded {
		return "excluded"
	}
	return "active"
}

type MemberEval struct {
	Reachable bool
	UsedPct   float64
	Tripped   bool
}

// Evaluate 依据最新上报与硬门槛决定成员的下一状态。
// 不可达（fail-open）保持现状，不产生变化；Tripped（服务器已停服）直接摘除。
func Evaluate(current GateState, eval MemberEval, settings configure.PoolSettings) (next GateState, changed bool) {
	if !eval.Reachable {
		return current, false
	}
	if eval.Tripped {
		if current != GateExcluded {
			return GateExcluded, true
		}
		return current, false
	}
	gate := settings.TrafficGatePct
	if gate <= 0 {
		gate = 90
	}
	hys := settings.HysteresisPct
	if hys < 0 {
		hys = 5
	}
	switch {
	case eval.UsedPct >= gate:
		if current != GateExcluded {
			return GateExcluded, true
		}
	case eval.UsedPct < gate-hys:
		if current != GateActive {
			return GateActive, true
		}
	}
	return current, false
}

// SelectFailOpenMember 返回应被放回的成员下标：优先可达且 usedPct 最低；
// 全部不可达时选 usedPct 最低者；空列表返回 -1。
func SelectFailOpenMember(evals []MemberEval) int {
	best := -1
	for i := range evals {
		if best == -1 {
			best = i
			continue
		}
		b, c := evals[best], evals[i]
		if c.Reachable && !b.Reachable {
			best = i
			continue
		}
		if c.Reachable == b.Reachable && c.UsedPct < b.UsedPct {
			best = i
		}
	}
	return best
}
