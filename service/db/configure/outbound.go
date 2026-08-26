package configure

type ObservatoryType string

func (t ObservatoryType) String() string {
	return string(t)
}

const (
	LeastPing  ObservatoryType = "leastping"  // 每轮探测选延迟最低的成员（一次只走一个）
	Random     ObservatoryType = "random"     // 并发连接随机分发到各成员（并行出口）
	RoundRobin ObservatoryType = "roundrobin" // 并发连接轮询分发到各成员（并行出口）
)

// Valid 报告该出站策略是否被 v2raya_core（xray）支持。
func (t ObservatoryType) Valid() bool {
	switch t {
	case LeastPing, Random, RoundRobin:
		return true
	}
	return false
}

type OutboundSetting struct {
	ProbeURL      string          `json:"probeURL"`
	ProbeInterval string          `json:"probeInterval"`
	Type          ObservatoryType `json:"type"`
}

// DefaultOutboundSetting returns an OutboundSetting with default values.
func DefaultOutboundSetting() OutboundSetting {
	return OutboundSetting{
		ProbeURL:      DefaultProbeURL,
		ProbeInterval: DefaultProbeInterval,
		Type:          ObservatoryType(DefaultOutboundType),
	}
}
