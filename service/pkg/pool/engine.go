package pool

import (
	"context"
	"reflect"
	"sync"
	"time"

	"github.com/v2rayA/v2rayA/db/configure"
	"github.com/v2rayA/v2rayA/pkg/util/log"
)

const staleAfter = 3 * time.Minute

type PoolMemberState struct {
	Member     configure.PoolMember
	Reachable  bool
	Err        string
	Status     *AgentStatus
	ReportedAt time.Time
	Gate       GateState
}

type PoolState struct {
	Pool     configure.Pool
	Members  []*PoolMemberState
	LastPoll time.Time
}

// Engine 管理所有池的轮询与门槛评估。onChange 在成员集合变化时被调用
// （由宿主注入，用于触发 v2ray 配置重生成）。
type Engine struct {
	mu       sync.Mutex
	states   map[string]*PoolState
	cancel   map[string]context.CancelFunc
	onChange func()
}

func NewEngine(onChange func()) *Engine {
	return &Engine{
		states:   map[string]*PoolState{},
		cancel:   map[string]context.CancelFunc{},
		onChange: onChange,
	}
}

func (e *Engine) Start(pools []configure.Pool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, p := range pools {
		if _, ok := e.cancel[p.Name]; ok {
			continue
		}
		st := &PoolState{Pool: p}
		for _, m := range p.Members {
			st.Members = append(st.Members, &PoolMemberState{Member: m, Gate: GateActive})
		}
		e.states[p.Name] = st
		ctx, cancel := context.WithCancel(context.Background())
		e.cancel[p.Name] = cancel
		go e.pollLoop(ctx, st)
	}
}

func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for name, cancel := range e.cancel {
		cancel()
		delete(e.cancel, name)
	}
	e.states = map[string]*PoolState{}
}

// Sync 以配置为准全量对齐：停止已删除或已变更的池轮询器，再启动新池。
func (e *Engine) Sync(pools []configure.Pool) {
	e.mu.Lock()
	byName := make(map[string]configure.Pool, len(pools))
	for _, p := range pools {
		byName[p.Name] = p
	}
	for name, cancel := range e.cancel {
		p, ok := byName[name]
		if !ok || !reflect.DeepEqual(e.states[name].Pool, p) {
			cancel()
			delete(e.cancel, name)
			delete(e.states, name)
		}
	}
	e.mu.Unlock()
	e.Start(pools)
}

func (e *Engine) State(name string) (*PoolState, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	st, ok := e.states[name]
	if !ok {
		return nil, false
	}
	return clonePoolState(st), true
}

func (e *Engine) AllStates() []*PoolState {
	e.mu.Lock()
	defer e.mu.Unlock()
	res := make([]*PoolState, 0, len(e.states))
	for _, st := range e.states {
		res = append(res, clonePoolState(st))
	}
	return res
}

// clonePoolState 返回状态的快照副本：轮询 goroutine 持续在锁内更新原对象，
// 调用方在锁外读取副本不会产生数据竞争。
func clonePoolState(st *PoolState) *PoolState {
	out := &PoolState{Pool: st.Pool, LastPoll: st.LastPoll}
	out.Members = make([]*PoolMemberState, 0, len(st.Members))
	for _, ms := range st.Members {
		c := *ms
		out.Members = append(out.Members, &c)
	}
	return out
}

// ActiveMembers 返回当前处于 selector 中的成员（enabled 且未因门槛被摘除），
// Which.Outbound 填充为池的出站名。
func (e *Engine) ActiveMembers() []configure.Which {
	e.mu.Lock()
	defer e.mu.Unlock()
	var res []configure.Which
	for _, st := range e.states {
		for _, ms := range st.Members {
			if !ms.Member.Enabled || ms.Gate != GateActive {
				continue
			}
			w := ms.Member.Which
			w.Outbound = st.Pool.OutboundName()
			res = append(res, w)
		}
	}
	return res
}

func (e *Engine) pollLoop(ctx context.Context, st *PoolState) {
	settings := st.Pool.ValidSettings()
	interval, err := time.ParseDuration(settings.PollInterval)
	if err != nil || interval < time.Second {
		interval = 30 * time.Second
	}
	e.pollOnce(st)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.pollOnce(st)
		}
	}
}

func (e *Engine) pollOnce(st *PoolState) {
	var wg sync.WaitGroup
	for _, ms := range st.Members {
		if !ms.Member.Enabled {
			continue
		}
		wg.Add(1)
		go func(ms *PoolMemberState) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), defaultAgentTimeout)
			defer cancel()
			status, err := FetchStatus(ctx, ms.Member.AgentURL, ms.Member.AgentToken)
			e.mu.Lock()
			ms.Status = status
			ms.ReportedAt = time.Now()
			ms.Reachable = err == nil
			ms.Err = ""
			if err != nil {
				ms.Err = err.Error()
			}
			e.mu.Unlock()
		}(ms)
	}
	wg.Wait()

	e.mu.Lock()
	st.LastPoll = time.Now()
	changed := e.evaluateGateLocked(st)
	onChange := e.onChange
	e.mu.Unlock()
	if changed && onChange != nil {
		onChange()
	}
}

func memberEval(ms *PoolMemberState) MemberEval {
	if ms.Reachable && ms.Status != nil && time.Since(ms.ReportedAt) <= staleAfter {
		return MemberEval{
			Reachable: true,
			UsedPct:   ms.Status.Traffic.UsedPct,
			Tripped:   ms.Status.Traffic.Tripped,
		}
	}
	return MemberEval{Reachable: false}
}

func (st *PoolState) enabledMembers() []*PoolMemberState {
	var res []*PoolMemberState
	for _, ms := range st.Members {
		if ms.Member.Enabled {
			res = append(res, ms)
		}
	}
	return res
}

func (st *PoolState) allExcluded() bool {
	enabled := st.enabledMembers()
	if len(enabled) == 0 {
		return false
	}
	for _, ms := range enabled {
		if ms.Gate != GateExcluded {
			return false
		}
	}
	return true
}

// evaluateGateLocked 在持锁状态下评估所有成员的硬门槛，返回成员集合是否变化。
func (e *Engine) evaluateGateLocked(st *PoolState) bool {
	settings := st.Pool.ValidSettings()
	changed := false
	for _, ms := range st.enabledMembers() {
		next, didChange := Evaluate(ms.Gate, memberEval(ms), settings)
		if didChange {
			ms.Gate = next
			changed = true
		}
	}
	if settings.FailOpen != nil && *settings.FailOpen && st.allExcluded() {
		enabled := st.enabledMembers()
		evals := make([]MemberEval, 0, len(enabled))
		for _, ms := range enabled {
			evals = append(evals, memberEval(ms))
		}
		if idx := SelectFailOpenMember(evals); idx >= 0 {
			if enabled[idx].Gate != GateActive {
				enabled[idx].Gate = GateActive
				changed = true
				log.Warn("pool %s: all members excluded by traffic gate; fail-open re-added member %d", st.Pool.Name, idx)
			}
		}
	}
	return changed
}

// ---- 包级单例 ----

var defaultEngine = NewEngine(nil)

// SetOnMembershipChanged 注册成员集合变化回调（由宿主注入，触发 v2ray 配置重生成）。
func SetOnMembershipChanged(f func()) {
	defaultEngine.mu.Lock()
	defaultEngine.onChange = f
	defaultEngine.mu.Unlock()
}

func ensureAllOutboundSettings() {
	for _, p := range configure.GetPools() {
		s := p.ValidSettings()
		_ = configure.SetOutboundSetting(p.OutboundName(), configure.OutboundSetting{
			ProbeURL:      s.ProbeURL,
			ProbeInterval: s.PollInterval,
			Type:          configure.LeastPing,
		})
	}
}

// Start 启动引擎：读取当前池配置并开始轮询；同时写入各池的观测器设置。
func Start() {
	defaultEngine.Start(configure.GetPools())
	ensureAllOutboundSettings()
}

func Stop() {
	defaultEngine.Stop()
}

// SyncPools 在池配置 CRUD 后调用：对齐轮询器与观测器设置。
func SyncPools() {
	defaultEngine.Sync(configure.GetPools())
	ensureAllOutboundSettings()
}

func State(name string) (*PoolState, bool) {
	return defaultEngine.State(name)
}

func AllStates() []*PoolState {
	return defaultEngine.AllStates()
}

func ActiveMembers() []configure.Which {
	return defaultEngine.ActiveMembers()
}
