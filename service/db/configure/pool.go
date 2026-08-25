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
