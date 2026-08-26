package configure

import (
	"os"
	"testing"

	"github.com/v2rayA/v2rayA/conf"
	"github.com/v2rayA/v2rayA/db"
	"github.com/v2rayA/v2rayA/kernel/serverObj"
)

func TestMain(m *testing.M) {
	// gonfig 会解析 os.Args：清洗掉 go test 传入的 -test.* 参数，避免 log.Fatal
	origArgs := os.Args
	os.Args = os.Args[:1]
	dir, err := os.MkdirTemp("", "v2raya-test")
	if err != nil {
		panic(err)
	}
	conf.SetConfig(conf.Params{Config: dir})
	if err := db.Open(); err != nil {
		panic(err)
	}
	_ = InitDefaultOutbound()
	// gonfig 已在上面消费完清洗后的参数；恢复原参数，让 testing 框架的 flag.Parse
	// （M.Run 内部）能识别 -test.run/-test.v 等，保持正常过滤与冗长输出。
	os.Args = origArgs
	code := m.Run()
	_ = db.Close()
	_ = os.RemoveAll(dir)
	os.Exit(code)
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

func TestPoolSettingsStrategyDefaultsToRandom(t *testing.T) {
	// 新增节点是为了并行增大出口流量：默认出站策略必须 random（并发分发），
	// 而不是 leastping（一次只走一个节点，加节点反而可能降速）
	p := &Pool{Name: "mypool"}
	if s := p.ValidSettings(); s.Strategy != string(Random) {
		t.Fatalf("default strategy = %q, want %q", s.Strategy, Random)
	}
	// 显式指定必须保留
	p = &Pool{Name: "mypool", Settings: PoolSettings{Strategy: string(LeastPing)}}
	if s := p.ValidSettings(); s.Strategy != string(LeastPing) {
		t.Fatalf("explicit strategy = %q, want leastping", s.Strategy)
	}
	p = &Pool{Name: "mypool", Settings: PoolSettings{Strategy: string(RoundRobin)}}
	if s := p.ValidSettings(); s.Strategy != string(RoundRobin) {
		t.Fatalf("explicit strategy = %q, want roundrobin", s.Strategy)
	}
}

func TestValidatePoolRejectsBadStrategy(t *testing.T) {
	defer func() {
		_ = RemoveServers([]int{0})
	}()
	_ = AppendServers([]*ServerRaw{{ServerObj: &ServerObjStub{}}})
	p := &Pool{
		Name:     "p1",
		Members:  []PoolMember{{Which: Which{TYPE: ServerType, ID: 1}, AgentURL: "http://127.0.0.1:19528"}},
		Settings: PoolSettings{Strategy: "bogus-strategy"},
	}
	if err := ValidatePool(p); err == nil {
		t.Fatal("ValidatePool(bogus strategy) should fail")
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
	// Outbound 保留名
	if err := ValidatePool(&Pool{Name: "p1", Outbound: "direct"}); err == nil {
		t.Fatal("ValidatePool(outbound=direct) should fail")
	}
	// hys >= gate
	p = &Pool{
		Name:     "p1",
		Members:  []PoolMember{{Which: Which{TYPE: ServerType, ID: 1}, AgentURL: "http://127.0.0.1:19528"}},
		Settings: PoolSettings{TrafficGatePct: 90, HysteresisPct: 95},
	}
	if err := ValidatePool(p); err == nil {
		t.Fatal("ValidatePool(hys >= gate) should fail")
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

func TestAdjustPoolMembersAfterServerDelete(t *testing.T) {
	defer func() {
		for i := 0; i < 3; i++ {
			_ = RemoveServers([]int{0})
		}
		_ = RemovePool("p1")
	}()
	// 3 台服务器（下标 1、2、3）
	for i := 0; i < 3; i++ {
		_ = AppendServers([]*ServerRaw{{ServerObj: &ServerObjStub{}}})
	}
	p := &Pool{
		Name: "p1",
		Members: []PoolMember{
			{Which: Which{TYPE: ServerType, ID: 1}, AgentURL: "http://127.0.0.1:19528"},
			{Which: Which{TYPE: ServerType, ID: 2}, AgentURL: "http://127.0.0.1:19528"},
			{Which: Which{TYPE: ServerType, ID: 3}, AgentURL: "http://127.0.0.1:19528"},
		},
	}
	if err := SetPool(p); err != nil {
		t.Fatalf("SetPool: %v", err)
	}
	// 删除第 2 台服务器：成员应变为 [1,2]（原 1、3），不再错位
	if err := AdjustPoolMembersAfterServerDelete(2); err != nil {
		t.Fatalf("AdjustPoolMembersAfterServerDelete: %v", err)
	}
	got, err := GetPool("p1")
	if err != nil {
		t.Fatalf("GetPool: %v", err)
	}
	if len(got.Members) != 2 {
		t.Fatalf("members = %d, want 2", len(got.Members))
	}
	if got.Members[0].Which.ID != 1 || got.Members[1].Which.ID != 2 {
		t.Fatalf("member IDs = %d,%d, want 1,2", got.Members[0].Which.ID, got.Members[1].Which.ID)
	}
	// 再次删除第 1 台（原第 3 台）
	if err := AdjustPoolMembersAfterServerDelete(1); err != nil {
		t.Fatalf("Adjust(1): %v", err)
	}
	got, _ = GetPool("p1")
	if len(got.Members) != 1 || got.Members[0].Which.ID != 1 {
		t.Fatalf("members after second delete = %+v, want single ID 1", got.Members)
	}
}

func TestAdjustPoolMembersAfterServerDeleteRemovesEmptyPool(t *testing.T) {
	defer func() {
		_ = RemoveServers([]int{0})
		_ = RemovePool("gone")
	}()
	_ = AppendServers([]*ServerRaw{{ServerObj: &ServerObjStub{}}})
	p := &Pool{
		Name:    "gone",
		Members: []PoolMember{{Which: Which{TYPE: ServerType, ID: 1}, AgentURL: "http://127.0.0.1:19528"}},
	}
	if err := SetPool(p); err != nil {
		t.Fatalf("SetPool: %v", err)
	}
	if err := AdjustPoolMembersAfterServerDelete(1); err != nil {
		t.Fatalf("Adjust: %v", err)
	}
	if _, err := GetPool("gone"); err == nil {
		t.Fatal("pool with no remaining members should be removed")
	}
}
