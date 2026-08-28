package controller

import (
	"bufio"
	"errors"
	"io"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/v2rayA/v2rayA/common"
	"github.com/v2rayA/v2rayA/conf"
)

type getLogQuery struct {
	Skip int64 `json:"skip" form:"skip"`
}

// maxLogRead 限制单次 /api/logger 响应大小：trace 级别 + 核心日志可让日志文件膨胀到
// 数百 MB，前端按字节偏移增量拉取时首次请求会把整个文件拉进浏览器导致白屏/超时。
// 保留 [skip, EOF) 语义（前端以返回字节数累加偏移），仅在单次读取时截断。
const maxLogRead = 8 << 20 // 8 MiB

func GetLogger(ctx *gin.Context) {
	config := conf.GetEnvironmentConfig()
	query := getLogQuery{}
	if ctx.ShouldBindQuery(&query) != nil {
		common.ResponseError(ctx, errors.New("invalid query"))
		return
	}
	if config.LogFile == "" {
		if query.Skip == 0 {
			ctx.String(200, "log printed to console, please see log in console.")
		} else {
			ctx.String(200, "")
		}
		return
	}
	f, err := os.Open(config.LogFile)
	if err != nil {
		common.ResponseError(ctx, logError(err))
		return
	}
	defer f.Close()
	_, err = f.Seek(query.Skip, io.SeekStart)
	if err != nil {
		common.ResponseError(ctx, logError(err))
		return
	}
	data, err := io.ReadAll(io.LimitReader(bufio.NewReader(f), maxLogRead))
	if err != nil {
		common.ResponseError(ctx, logError(err))
		return
	}
	ctx.String(200, string(data))
}
