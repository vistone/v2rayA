package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/devfeel/mapper"
	pb "github.com/v2fly/v2ray-core/v5/app/observatory/command"
	"github.com/v2rayA/v2rayA/conf"
	"github.com/v2rayA/v2rayA/db"
	"github.com/v2rayA/v2rayA/db/configure"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// obscheck queries the v2ray-core observatory gRPC service directly and
// compares the returned outbound tags against the connected-servers index
// mapping that the ObservatoryProducer used historically (for diagnosing why
// the GUI was not receiving latency updates).
//
// Usage:
//
//	go run ./cmd/obscheck -conf /path/to/run -api 127.0.0.1:45433
func main() {
	confDir := flag.String("conf", "", "v2rayA configuration directory (where v2raya.db lives)")
	apiAddr := flag.String("api", "127.0.0.1:45433", "v2ray-core gRPC API address")
	iterations := flag.Int("n", 3, "number of polling iterations")
	flag.Parse()

	if *confDir == "" {
		fmt.Println("-conf is required")
		return
	}
	conf.SetConfig(conf.Params{Config: *confDir})
	if err := db.Open(); err != nil {
		fmt.Println("db.Open err:", err)
		return
	}
	mapper.Register(&struct{}{})

	outbounds := configure.GetOutbounds()
	fmt.Println("outbounds:", outbounds)

	css := configure.GetConnectedServers()
	fmt.Printf("GetConnectedServers: nil=%v len=%d\n", css == nil, css.Len())
	tag2WhichIndex := make(map[string]int)
	if css != nil {
		for i, w := range css.Get() {
			if sr, err := w.LocateServerRaw(); err == nil && sr != nil {
				tag2WhichIndex["『"+sr.ServerObj.GetName()+"』"] = i
				fmt.Printf("  css[%d] outbound=%q node=%q\n", i, w.Outbound, sr.ServerObj.GetName())
			}
		}
	}
	fmt.Println("tag2WhichIndex:", tag2WhichIndex)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, *apiAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		fmt.Println("dial err:", err)
		return
	}
	defer conn.Close()
	c := pb.NewObservatoryServiceClient(conn)

	for iter := 0; iter < *iterations; iter++ {
		time.Sleep(time.Second)
		fmt.Printf("=== iteration %d ===\n", iter)
		for _, tag := range outbounds {
			ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
			resp, err := c.GetOutboundStatus(ctx2, &pb.GetOutboundStatusRequest{Tag: tag})
			cancel2()
			if err != nil {
				fmt.Printf("GetOutboundStatus(%q) err: %v\n", tag, err)
				continue
			}
			sts := resp.GetStatus().GetStatus()
			cssLen := 0
			if css != nil {
				cssLen = css.Len()
			}
			fmt.Printf("GetOutboundStatus(%q) count=%d\n", tag, len(sts))
			for _, s := range sts {
				idx, ok := tag2WhichIndex[s.GetOutboundTag()]
				skip := !ok || idx >= cssLen
				fmt.Printf("  tag=%q idx=%d ok=%v cssLen=%d -> %s\n",
					s.GetOutboundTag(), idx, ok, cssLen, map[bool]string{true: "SKIP", false: "ok"}[skip])
			}
		}
	}
}
