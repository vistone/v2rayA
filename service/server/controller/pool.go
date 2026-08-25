package controller

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/v2rayA/v2rayA/common"
	"github.com/v2rayA/v2rayA/db/configure"
	"github.com/v2rayA/v2rayA/kernel/v2ray"
	pool "github.com/v2rayA/v2rayA/pkg/pool"
)

type memberStateResp struct {
	Which      configure.Which   `json:"which"`
	AgentURL   string            `json:"agentURL"`
	AgentToken string            `json:"agentToken"`
	Enabled    bool              `json:"enabled"`
	Reachable  bool              `json:"reachable"`
	Err        string            `json:"err,omitempty"`
	Gate       string            `json:"gate"`
	Status     *pool.AgentStatus `json:"status,omitempty"`
	ReportedAt time.Time         `json:"reportedAt"`
}

type poolResp struct {
	Name     string                 `json:"name"`
	Outbound string                 `json:"outbound"`
	Settings configure.PoolSettings `json:"settings"`
	Members  []memberStateResp      `json:"members"`
	LastPoll time.Time              `json:"lastPoll"`
}

func poolToResp(st *pool.PoolState) poolResp {
	resp := poolResp{
		Name:     st.Pool.Name,
		Outbound: st.Pool.Outbound,
		Settings: st.Pool.ValidSettings(),
		LastPoll: st.LastPoll,
		Members:  make([]memberStateResp, 0, len(st.Members)),
	}
	for _, ms := range st.Members {
		resp.Members = append(resp.Members, memberStateResp{
			Which:      ms.Member.Which,
			AgentURL:   ms.Member.AgentURL,
			AgentToken: ms.Member.AgentToken,
			Enabled:    ms.Member.Enabled,
			Reachable:  ms.Reachable,
			Err:        ms.Err,
			Gate:       ms.Gate.String(),
			Status:     ms.Status,
			ReportedAt: ms.ReportedAt,
		})
	}
	return resp
}

func GetPools(ctx *gin.Context) {
	resp := make([]poolResp, 0)
	for _, st := range pool.AllStates() {
		resp = append(resp, poolToResp(st))
	}
	common.ResponseSuccess(ctx, gin.H{"pools": resp})
}

func GetPoolStatus(ctx *gin.Context) {
	name := ctx.Param("name")
	st, ok := pool.State(name)
	if !ok {
		common.ResponseError(ctx, logError(fmt.Errorf("pool %q not found", name)))
		return
	}
	common.ResponseSuccess(ctx, gin.H{"pool": poolToResp(st)})
}

func PostPool(ctx *gin.Context) {
	updatingMu.Lock()
	if updating {
		common.ResponseError(ctx, processingErr)
		updatingMu.Unlock()
		return
	}
	updating = true
	updatingMu.Unlock()
	defer func() {
		updatingMu.Lock()
		updating = false
		updatingMu.Unlock()
	}()

	var data configure.Pool
	if err := ctx.ShouldBindJSON(&data); err != nil {
		common.ResponseError(ctx, logError("bad request"))
		return
	}
	if existing, _ := configure.GetPool(data.Name); existing != nil {
		common.ResponseError(ctx, logError(fmt.Errorf("pool %q already exists", data.Name)))
		return
	}
	if err := configure.ValidatePool(&data); err != nil {
		common.ResponseError(ctx, logError(err))
		return
	}
	if err := configure.SetPool(&data); err != nil {
		common.ResponseError(ctx, logError(err))
		return
	}
	pool.SyncPools()
	if v2ray.ProcessManager.Running() {
		if err := v2ray.UpdateV2RayConfig(); err != nil {
			common.ResponseError(ctx, fmt.Errorf("invalid config: %w", err))
			return
		}
	}
	GetPools(ctx)
}

func PutPool(ctx *gin.Context) {
	updatingMu.Lock()
	if updating {
		common.ResponseError(ctx, processingErr)
		updatingMu.Unlock()
		return
	}
	updating = true
	updatingMu.Unlock()
	defer func() {
		updatingMu.Lock()
		updating = false
		updatingMu.Unlock()
	}()

	name := ctx.Param("name")
	var data configure.Pool
	if err := ctx.ShouldBindJSON(&data); err != nil {
		common.ResponseError(ctx, logError("bad request"))
		return
	}
	if data.Name != name {
		common.ResponseError(ctx, logError(fmt.Errorf("pool name in body (%q) does not match path (%q); rename is not supported", data.Name, name)))
		return
	}
	if _, err := configure.GetPool(name); err != nil {
		common.ResponseError(ctx, logError(fmt.Errorf("pool %q not found", name)))
		return
	}
	if err := configure.ValidatePool(&data); err != nil {
		common.ResponseError(ctx, logError(err))
		return
	}
	// 保护：未提供新 token（前端留空）时保留旧成员的 token，避免误清空
	if existing, err := configure.GetPool(name); err == nil {
		for i := range data.Members {
			if data.Members[i].AgentToken != "" {
				continue
			}
			for j := range existing.Members {
				if existing.Members[j].Which.EqualTo(data.Members[i].Which) {
					data.Members[i].AgentToken = existing.Members[j].AgentToken
					break
				}
			}
		}
	}
	if err := configure.SetPool(&data); err != nil {
		common.ResponseError(ctx, logError(err))
		return
	}
	pool.SyncPools()
	if v2ray.ProcessManager.Running() {
		if err := v2ray.UpdateV2RayConfig(); err != nil {
			common.ResponseError(ctx, fmt.Errorf("invalid config: %w", err))
			return
		}
	}
	GetPools(ctx)
}

func DeletePool(ctx *gin.Context) {
	updatingMu.Lock()
	if updating {
		common.ResponseError(ctx, processingErr)
		updatingMu.Unlock()
		return
	}
	updating = true
	updatingMu.Unlock()
	defer func() {
		updatingMu.Lock()
		updating = false
		updatingMu.Unlock()
	}()

	name := ctx.Param("name")
	if _, err := configure.GetPool(name); err != nil {
		common.ResponseError(ctx, logError(fmt.Errorf("pool %q not found", name)))
		return
	}
	if err := configure.RemovePool(name); err != nil {
		common.ResponseError(ctx, logError(err))
		return
	}
	pool.SyncPools()
	if v2ray.ProcessManager.Running() {
		if err := v2ray.UpdateV2RayConfig(); err != nil {
			common.ResponseError(ctx, fmt.Errorf("invalid config: %w", err))
			return
		}
	}
	GetPools(ctx)
}

func PostPoolControl(ctx *gin.Context) {
	name := ctx.Param("name")
	var data struct {
		Member  configure.Which `json:"member"`
		Action  string          `json:"action"`
		WarnPct *float64        `json:"warnPct"`
		StopPct *float64        `json:"stopPct"`
		Seconds *int            `json:"seconds"`
	}
	if err := ctx.ShouldBindJSON(&data); err != nil {
		common.ResponseError(ctx, logError("bad request"))
		return
	}
	st, ok := pool.State(name)
	if !ok {
		common.ResponseError(ctx, logError(fmt.Errorf("pool %q not found", name)))
		return
	}
	var member *configure.PoolMember
	for i := range st.Pool.Members {
		if st.Pool.Members[i].Which.EqualTo(data.Member) {
			member = &st.Pool.Members[i]
			break
		}
	}
	if member == nil {
		common.ResponseError(ctx, logError(fmt.Errorf("member not found in pool %q", name)))
		return
	}
	params := map[string]any{}
	switch pool.ControlAction(data.Action) {
	case pool.ControlTrip, pool.ControlResume:
	case pool.ControlSetThresholds:
		if data.WarnPct == nil || data.StopPct == nil {
			common.ResponseError(ctx, logError("set-thresholds requires warnPct and stopPct"))
			return
		}
		params["warnPct"] = *data.WarnPct
		params["stopPct"] = *data.StopPct
	case pool.ControlSetCheckInterval:
		if data.Seconds == nil || *data.Seconds < 60 {
			common.ResponseError(ctx, logError("set-check-interval requires seconds >= 60"))
			return
		}
		params["seconds"] = *data.Seconds
	default:
		common.ResponseError(ctx, logError(fmt.Errorf("unknown action %q", data.Action)))
		return
	}
	if err := pool.SendControl(ctx.Request.Context(), member.AgentURL, member.AgentToken, pool.ControlAction(data.Action), params); err != nil {
		common.ResponseError(ctx, logError(err))
		return
	}
	common.ResponseSuccess(ctx, nil)
}
