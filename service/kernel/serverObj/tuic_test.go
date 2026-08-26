package serverObj

import "testing"

// geoproxy-server 生成的 tuic 分享链接使用驼峰 allowInsecure=1（部分客户端用
// insecure=1 / allow_insecure=1）。v2rayA 必须兼容三种拼写，否则自签证书节点
// 会被错误地要求 TLS 校验，导致握手失败。
const geoproxyTuicLink = "tuic://5d47d8c4-e7ad-4d89-8e9c-117f226973d9:5d47d8c4-e7ad-4d89-8e9c-117f226973d9@65.49.192.85:51646/?alpn=h3&insecure=1&allowInsecure=1&congestion_control=bbr&udp_relay_mode=native#tile3.zeromaps.cn"

func TestParseTuicURL_GeoproxyLink(t *testing.T) {
	tuic, err := ParseTuicURL(geoproxyTuicLink)
	if err != nil {
		t.Fatalf("ParseTuicURL: %v", err)
	}
	if tuic.Server != "65.49.192.85" || tuic.Port != 51646 {
		t.Errorf("server/port = %s:%d, want 65.49.192.85:51646", tuic.Server, tuic.Port)
	}
	if tuic.Name != "tile3.zeromaps.cn" {
		t.Errorf("name = %q, want tile3.zeromaps.cn", tuic.Name)
	}
	if !tuic.AllowInsecure {
		t.Error("AllowInsecure = false, want true (allowInsecure=1 in link)")
	}
	if tuic.Alpn != "h3" || tuic.CongestionControl != "bbr" || tuic.UdpRelayMode != "native" {
		t.Errorf("alpn/congestion/udp = %q/%q/%q", tuic.Alpn, tuic.CongestionControl, tuic.UdpRelayMode)
	}
}

func TestParseTuicURL_AllowInsecureSpellings(t *testing.T) {
	cases := []struct {
		query string
		want  bool
	}{
		{"allowInsecure=1", true},  // 驼峰（geoproxy 等 tuic 客户端标准）
		{"allow_insecure=1", true}, // 下划线（部分客户端）
		{"allow_insecure=true", true},
		{"insecure=1", true}, // 历史兼容写法
		{"", false},
		{"alpn=h3", false},
	}
	for _, c := range cases {
		link := "tuic://u:p@1.2.3.4:443/?" + c.query
		tuic, err := ParseTuicURL(link)
		if err != nil {
			t.Fatalf("ParseTuicURL(%q): %v", link, err)
		}
		if got := tuic.AllowInsecure; got != c.want {
			t.Errorf("query %q: AllowInsecure = %v, want %v", c.query, got, c.want)
		}
	}
}
