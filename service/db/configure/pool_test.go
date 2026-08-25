package configure

import (
	"os"
	"testing"

	"github.com/v2rayA/v2rayA/conf"
	"github.com/v2rayA/v2rayA/db"
	"github.com/v2rayA/v2rayA/kernel/serverObj"
)

func TestMain(m *testing.M) {
	dir, _ := os.MkdirTemp("", "v2raya-test")
	defer os.RemoveAll(dir)
	conf.SetConfig(conf.Params{Config: dir})
	if err := db.Open(); err != nil {
		panic(err)
	}
	_ = InitDefaultOutbound()
	os.Exit(m.Run())
}

type ServerObjStub struct{}

func (s *ServerObjStub) Configuration(info serverObj.PriorInfo) (serverObj.Configuration, error) {
	return serverObj.Configuration{}, nil
}
func (s *ServerObjStub) ExportToURL() string  { return "ss://dGVzdA@127.0.0.1:8388" }
func (s *ServerObjStub) NeedPluginPort() bool { return false }
func (s *ServerObjStub) ProtoToShow() string  { return "SS" }
func (s *ServerObjStub) GetProtocol() string  { return "ss" }
func (s *ServerObjStub) GetHostname() string  { return "127.0.0.1" }
func (s *ServerObjStub) GetPort() int         { return 8388 }
func (s *ServerObjStub) GetName() string      { return "stub" }
func (s *ServerObjStub) SetName(name string)  {}

func TestPoolOutboundName(t *testing.T) {
	p := &Pool{Name: "mypool"}
	if got := p.OutboundName(); got != "mypool" {
		t.Fatalf("OutboundName() = %q, want %q", got, "mypool")
	}
	p = &Pool{Name: "mypool", Outbound: "poolA"}
	if got := p.OutboundName(); got != "poolA" {
		t.Fatalf("OutboundName() = %q, want %q", got, "poolA")
	}
}

func TestPoolValidSettings(t *testing.T) {
	p := &Pool{Name: "mypool"}
	s := p.ValidSettings()
	if s.PollInterval != "30s" || s.TrafficGatePct != 90 || s.HysteresisPct != 5 || s.ProbeURL != DefaultProbeURL || s.FailOpen == nil || !*s.FailOpen {
		t.Fatalf("ValidSettings() = %+v, want defaults", s)
	}
	// 显式关闭 fail-open 必须保留
	f := false
	p = &Pool{Name: "mypool", Settings: PoolSettings{FailOpen: &f}}
	if s := p.ValidSettings(); s.FailOpen == nil || *s.FailOpen {
		t.Fatalf("ValidSettings should preserve explicit failOpen=false, got %+v", s)
	}
}

func TestValidatePool(t *testing.T) {
	if err := ValidatePool(nil); err == nil {
		t.Fatal("ValidatePool(nil) should fail")
	}
	// 保留名
	if err := ValidatePool(&Pool{Name: "proxy"}); err == nil {
		t.Fatal("ValidatePool(proxy) should fail")
	}
	// 空成员
	if err := ValidatePool(&Pool{Name: "p1"}); err == nil {
		t.Fatal("ValidatePool(no members) should fail")
	}
	// 非法 gate pct
	p := &Pool{
		Name:     "p1",
		Members:  []PoolMember{{Which: Which{TYPE: ServerType, ID: 1}, AgentURL: "http://127.0.0.1:19528"}},
		Settings: PoolSettings{TrafficGatePct: 150},
	}
	if err := ValidatePool(p); err == nil {
		t.Fatal("ValidatePool(gate 150) should fail")
	}
	// 非法 poll interval
	p = &Pool{
		Name:     "p1",
		Members:  []PoolMember{{Which: Which{TYPE: ServerType, ID: 1}, AgentURL: "http://127.0.0.1:19528"}},
		Settings: PoolSettings{PollInterval: "abc"},
	}
	if err := ValidatePool(p); err == nil {
		t.Fatal("ValidatePool(bad interval) should fail")
	}
}

func TestPoolSetGetRemove(t *testing.T) {
	defer func() {
		_ = RemoveServers([]int{0})
		_ = RemovePool("p1")
	}()
	_ = AppendServers([]*ServerRaw{{ServerObj: &ServerObjStub{}}})

	p := &Pool{
		Name: "p1",
		Members: []PoolMember{{
			Which:    Which{TYPE: ServerType, ID: 1},
			AgentURL: "http://127.0.0.1:19528",
		}},
	}
	if err := SetPool(p); err != nil {
		t.Fatalf("SetPool: %v", err)
	}
	got, err := GetPool("p1")
	if err != nil || got == nil || got.Name != "p1" {
		t.Fatalf("GetPool: %v, %v", got, err)
	}
	pools := GetPools()
	found := false
	for _, pp := range pools {
		if pp.Name == "p1" {
			found = true
		}
	}
	if !found {
		t.Fatal("GetPools should contain p1")
	}
	if err := RemovePool("p1"); err != nil {
		t.Fatalf("RemovePool: %v", err)
	}
	if _, err := GetPool("p1"); err == nil {
		t.Fatal("GetPool(p1) should fail after removal")
	}
}
