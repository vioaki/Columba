package server

import (
	"context"
	"time"
)

// errorDetailTruncateBytes 是错误详情入库的展示截断上限。
const errorDetailTruncateBytes = 2048

// waitForRetryOrCancel 等待重试间隔；客户端在等待期断开时返回 false
// （调用方负责落库并终止，见 abortRetryOnClientCancel）。
func waitForRetryOrCancel(ctx context.Context, retryIntervalMs int) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(time.Duration(retryIntervalMs) * time.Millisecond):
		return true
	}
}

func truncateForDisplay(value string, limit int) string {
	return truncateUsageBody(value, limit).Content
}
