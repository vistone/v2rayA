package main

import (
	"fmt"
	"os"
	"time"

	"github.com/gorilla/websocket"
)

// wscheck connects to the v2rayA /api/message websocket and prints messages
// for a few seconds.  Usage:
//
//	V2RAYA_TOKEN=<jwt> go run ./cmd/wscheck [ws://127.0.0.1:2018/api/message]
func main() {
	token := os.Getenv("V2RAYA_TOKEN")
	if token == "" {
		fmt.Println("V2RAYA_TOKEN env var is required")
		os.Exit(1)
	}
	url := "ws://127.0.0.1:2018/api/message"
	if len(os.Args) > 1 {
		url = os.Args[1]
	}
	sep := "?"
	if containsQuery(url) {
		sep = "&"
	}
	url = url + sep + "Authorization=" + token

	d := websocket.Dialer{HandshakeTimeout: 3 * time.Second}
	c, resp, err := d.Dial(url, nil)
	if err != nil {
		if resp != nil {
			fmt.Println("dial err:", err, "status:", resp.Status)
		} else {
			fmt.Println("dial err:", err)
		}
		os.Exit(1)
	}
	defer c.Close()
	fmt.Println("CONNECTED, waiting messages...")
	got := 0
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		_ = c.SetReadDeadline(time.Now().Add(1500 * time.Millisecond))
		_, msg, err := c.ReadMessage()
		if err != nil {
			if ne, ok := err.(interface{ Timeout() bool }); ok && ne.Timeout() {
				fmt.Println("tick: no msg")
				continue
			}
			fmt.Println("read err:", err)
			return
		}
		got++
		s := string(msg)
		if len(s) > 1500 {
			s = s[:1500]
		}
		fmt.Printf("MSG %d: %s\n---\n", got, s)
	}
	fmt.Println("total msgs:", got)
}

func containsQuery(u string) bool {
	for i := 0; i < len(u); i++ {
		if u[i] == '?' {
			return true
		}
		if u[i] == '#' {
			return false
		}
	}
	return false
}
