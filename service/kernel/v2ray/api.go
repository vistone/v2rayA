package v2ray

import (
	"context"
	"net"
	"strconv"
	"time"

	"github.com/devfeel/mapper"
	"github.com/gin-gonic/gin"
	"github.com/v2fly/v2ray-core/v5/app/observatory"
	pb "github.com/v2fly/v2ray-core/v5/app/observatory/command"
	"github.com/v2rayA/v2rayA/db/configure"
	"github.com/v2rayA/v2rayA/pkg/pool"
	"github.com/v2rayA/v2rayA/pkg/util/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	ApiProducts = []string{
		"observatory",
		"running_state",
	}
	ApiFeed *Feed
)

const (
	ApiFeedBoxSize  = 10
	ApiFeedInterval = 1 * time.Second
)

type OutboundStatus struct {
	Alive bool  `json:"alive"`
	Delay int64 `json:"delay"`
	//LastErrorReason string           `json:"last_error_reason"`
	OutboundTag  string           `json:"outbound_tag"`
	Which        *configure.Which `json:"which"`
	LastSeenTime int64            `json:"last_seen_time"`
	LastTryTime  int64            `json:"last_try_time"`
}

func init() {
	mapper.Register(&observatory.OutboundStatus{})

	ApiFeed = NewSubscriptions(ApiFeedBoxSize)
	for _, product := range ApiProducts {
		ApiFeed.RegisterProduct(product)
	}
}

type ObservatoryResp struct {
	OutboundName string
	Resp         *pb.GetOutboundStatusResponse
}

func getObservatoryResponses(conn *grpc.ClientConn, observatoryTags []string) (r []ObservatoryResp, err error) {
	c := pb.NewObservatoryServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if len(observatoryTags) == 0 {
		observatoryTags = append(observatoryTags, "")
	}
	for _, tag := range observatoryTags {
		resp, err := c.GetOutboundStatus(ctx, &pb.GetOutboundStatusRequest{
			Tag: tag,
		})
		if err != nil {
			return nil, err
		}
		r = append(r, ObservatoryResp{OutboundName: tag, Resp: resp})
	}
	return r, nil
}

// groupTag2WhichMap builds a map from outbound tag (『name』) to the Which of a
// member of the given group.  The group's connected servers come first; node-pool
// active members are appended afterwards (the pool engine persists members in
// configure, not in the connected-servers list).
func groupTag2WhichMap(group string) map[string]*configure.Which {
	m := make(map[string]*configure.Which)
	if css := configure.GetConnectedServersByOutbound(group); css != nil {
		for _, w := range css.Get() {
			if sr, err := w.LocateServerRaw(); err == nil && sr != nil {
				m[GroupWrapper(sr.ServerObj.GetName())] = w
			}
		}
	}
	for _, w := range pool.ActiveMembers() {
		if w.Outbound != group {
			continue
		}
		if sr, err := w.LocateServerRaw(); err == nil && sr != nil {
			ww := w
			m[GroupWrapper(sr.ServerObj.GetName())] = &ww
		}
	}
	return m
}

// ObservatoryProducer monitors outbound status via gRPC API and publishes to ApiFeed.
// This function is only compatible with v2ray-core v5+ API structure.
// For xray-core, this function should not be called as it uses different gRPC services.
func ObservatoryProducer(apiPort int, observatoryTags []string) (closeFunc func()) {
	closed := make(chan struct{})
	go func() {
		const product = "observatory"
		var conn *grpc.ClientConn
		for {
			select {
			case <-closed:
				return
			default:
			}
			if ProcessManager.Process() == nil {
				time.Sleep(ApiFeedInterval)
				continue
			}
			// Set up a connection to the server.
			if conn == nil {
				ctx, cancel := context.WithTimeout(context.Background(), ApiFeedInterval)
				c, err := grpc.DialContext(
					ctx,
					net.JoinHostPort("127.0.0.1", strconv.Itoa(apiPort)),
					grpc.WithInsecure(),
					grpc.WithBlock(),
				)
				cancel()
				if err != nil {
					log.Warn("ObservatoryProducer: did not connect: %v", err)
					time.Sleep(ApiFeedInterval)
					continue
				}
				conn = c
				log.Info("ObservatoryProducer: connected to core API at 127.0.0.1:%v", apiPort)
			}
			resps, err := getObservatoryResponses(conn, observatoryTags)
			if err != nil {
				if status.Code(err) == codes.Unavailable {
					// the connection is unreliable, close it and reconnect
					_ = conn.Close()
					conn = nil
					time.Sleep(ApiFeedInterval)
					continue
				}
				log.Warn("ObservatoryProducer: %v", err)
				time.Sleep(ApiFeedInterval)
				continue
			}
			for _, r := range resps {
				outboundStatus := r.Resp.GetStatus().GetStatus()
				tag2Which := groupTag2WhichMap(r.OutboundName)
				os := make([]OutboundStatus, 0, len(outboundStatus))
				for i := range outboundStatus {
					var st OutboundStatus
					_ = mapper.AutoMapper(outboundStatus[i], &st)
					w, ok := tag2Which[st.OutboundTag]
					if !ok {
						// The tag does not belong to a member of this group (e.g. a
						// pool member that is not connected, or a stale observer).
						// Skip just this status instead of dropping the whole batch.
						continue
					}
					st.Which = w
					os = append(os, st)
				}
				if len(os) == 0 {
					continue
				}
				msg := gin.H{
					"outboundName":   r.OutboundName,
					"outboundStatus": os,
				}
				ApiFeed.ProductMessage(product, msg)
			}
			time.Sleep(ApiFeedInterval)
		}
	}()
	return func() {
		close(closed)
	}
}
