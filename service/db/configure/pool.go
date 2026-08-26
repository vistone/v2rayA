package configure

import (
	"fmt"
	"net/url"
	"time"

	"github.com/v2rayA/v2rayA/db"
)

// Pool 是一个显式的节点池：成员引用 configure.Which（服务器或订阅服务器），
// 池级配置包含 Agent 端点、流量硬门槛与轮询参数。
type Pool struct {
	Name     string       `json:"name"`
	Outbound string       `json:"outbound"`
	Members  []PoolMember `json:"members"`
	Settings PoolSettings `json:"settings"`
}

type PoolMember struct {
	Which      Which  `json:"which"`
	AgentURL   string `json:"agentURL"`
	AgentToken string `json:"agentToken"`
	Enabled    bool   `json:"enabled"`
}

type PoolSettings struct {
	PollInterval   string  `json:"pollInterval"`
	TrafficGatePct float64 `json:"trafficGatePct"`
	HysteresisPct  float64 `json:"hysteresisPct"`
	ProbeURL       string  `json:"probeURL"`
	FailOpen       *bool   `json:"failOpen"` // 指针：区分"未设置"（默认 true）与显式 false
}

func DefaultPoolSettings() PoolSettings {
	failOpen := true
	return PoolSettings{
		PollInterval:   "30s",
		TrafficGatePct: 90,
		HysteresisPct:  5,
		ProbeURL:       DefaultProbeURL,
		FailOpen:       &failOpen,
	}
}

// OutboundName 返回池的出站/balancer 名，未显式指定时使用池名。
func (p *Pool) OutboundName() string {
	if p.Outbound == "" {
		return p.Name
	}
	return p.Outbound
}

// ValidSettings 返回带默认值的设置副本。
func (p *Pool) ValidSettings() PoolSettings {
	s := p.Settings
	if s.PollInterval == "" {
		s.PollInterval = "30s"
	}
	if s.TrafficGatePct <= 0 {
		s.TrafficGatePct = 90
	}
	if s.HysteresisPct <= 0 {
		s.HysteresisPct = 5
	}
	if s.ProbeURL == "" {
		s.ProbeURL = DefaultProbeURL
	}
	if s.FailOpen == nil {
		t := true
		s.FailOpen = &t
	}
	return s
}

// ValidatePool 校验池配置的合法性，供控制器创建/更新时调用。
func ValidatePool(p *Pool) error {
	if p == nil {
		return fmt.Errorf("pool is nil")
	}
	if p.Name == "" {
		return fmt.Errorf("pool name is required")
	}
	if p.Name == "proxy" || p.Name == "direct" || p.Name == "block" {
		return fmt.Errorf("pool name %q is reserved", p.Name)
	}
	if p.Outbound == "proxy" || p.Outbound == "direct" || p.Outbound == "block" {
		return fmt.Errorf("pool outbound name %q is reserved", p.Outbound)
	}
	outbound := p.OutboundName()
	for _, o := range GetOutbounds() {
		if o == outbound {
			return fmt.Errorf("outbound name %q is already used by an outbound group", outbound)
		}
	}
	s := p.ValidSettings()
	if s.TrafficGatePct <= 0 || s.TrafficGatePct >= 100 {
		return fmt.Errorf("trafficGatePct must be in (0, 100), got %v", s.TrafficGatePct)
	}
	if s.HysteresisPct < 0 || s.HysteresisPct >= 100 {
		return fmt.Errorf("hysteresisPct must be in [0, 100), got %v", s.HysteresisPct)
	}
	if s.HysteresisPct >= s.TrafficGatePct {
		return fmt.Errorf("hysteresisPct (%v) must be less than trafficGatePct (%v)", s.HysteresisPct, s.TrafficGatePct)
	}
	if _, err := time.ParseDuration(s.PollInterval); err != nil {
		return fmt.Errorf("pollInterval: %v", err)
	}
	if len(p.Members) == 0 {
		return fmt.Errorf("pool needs at least one member")
	}
	seen := make(map[string]bool)
	for i := range p.Members {
		m := &p.Members[i]
		if m.AgentURL == "" {
			return fmt.Errorf("member %d: agent URL is required", i)
		}
		u, err := url.Parse(m.AgentURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("member %d: invalid agent URL %q", i, m.AgentURL)
		}
		if _, err := m.Which.LocateServerRaw(); err != nil {
			return fmt.Errorf("member %d: cannot resolve node: %v", i, err)
		}
		key := fmt.Sprintf("%s:%d:%d", m.Which.TYPE, m.Which.ID, m.Which.Sub)
		if seen[key] {
			return fmt.Errorf("member %d: duplicate node", i)
		}
		seen[key] = true
	}
	return nil
}

func GetPools() (pools []Pool) {
	keys, err := db.GetBucketKeys("pools")
	if err != nil {
		return nil
	}
	for _, k := range keys {
		var p Pool
		if err := db.Get("pools", k, &p); err == nil {
			pools = append(pools, p)
		}
	}
	return pools
}

func GetPool(name string) (*Pool, error) {
	var p Pool
	if err := db.Get("pools", name, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func SetPool(p *Pool) error {
	if err := ValidatePool(p); err != nil {
		return err
	}
	return db.Set("pools", p.Name, p)
}

func RemovePool(name string) error {
	return db.Delete("pools", name)
}

// AdjustPoolMembersAfterServerDelete 删除某台服务器后调整所有池成员引用。
// configure.Which.ID 是 1-based 服务器列表下标：不调整的话，残留成员会错位
// 解析成其它节点（LocateServerRaw 用 ID-1 当切片下标）。
// 引用被删服务器的成员被移除；引用更大下标的成员前移一位；
// 成员被删光的池直接删除。
func AdjustPoolMembersAfterServerDelete(deletedID int) error {
	return adjustPoolMembers(func(m *PoolMember) bool {
		if m.Which.TYPE != ServerType {
			return false
		}
		switch {
		case m.Which.ID == deletedID:
			return true // 移除
		case m.Which.ID > deletedID:
			m.Which.ID--
		}
		return false
	})
}

// AdjustPoolMembersAfterSubscriptionDelete 删除某个订阅后调整池内订阅服务器成员的 Sub 下标。
func AdjustPoolMembersAfterSubscriptionDelete(deletedSub int) error {
	return adjustPoolMembers(func(m *PoolMember) bool {
		if m.Which.TYPE != SubscriptionServerType {
			return false
		}
		switch {
		case m.Which.Sub == deletedSub:
			return true // 移除
		case m.Which.Sub > deletedSub:
			m.Which.Sub--
		}
		return false
	})
}

// adjustPoolMembers 遍历所有池，按 remove 判定过滤成员并保存变更。
// remove 返回 true 表示移除该成员；可顺便就地修改其 Which 下标。
func adjustPoolMembers(remove func(m *PoolMember) bool) error {
	pools := GetPools()
	for _, p := range pools {
		members := make([]PoolMember, 0, len(p.Members))
		changed := false
		for _, m := range p.Members {
			mm := m
			if remove(&mm) {
				changed = true
				continue
			}
			members = append(members, mm)
		}
		if !changed {
			continue
		}
		if len(members) == 0 {
			if err := RemovePool(p.Name); err != nil {
				return err
			}
			continue
		}
		p.Members = members
		if err := SetPool(&p); err != nil {
			return err
		}
	}
	return nil
}
