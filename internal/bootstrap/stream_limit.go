package bootstrap

import (
	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/op"
	"github.com/alist-org/alist/v3/internal/setting"
	"github.com/alist-org/alist/v3/internal/stream"
	"golang.org/x/time/rate"
)

func filterNegative(limit int) (rate.Limit, int) {
	if limit < 0 {
		return rate.Inf, 0
	}
	return rate.Limit(limit), limit
}

func initLimiter(limiter **rate.Limiter, s string) {
	clientDownLimit, burst := filterNegative(setting.GetInt(s, -1))
	*limiter = rate.NewLimiter(clientDownLimit, burst)
	op.RegisterSettingChangingCallback(func() {
		newLimit, newBurst := filterNegative(setting.GetInt(s, -1))
		(*limiter).SetLimit(newLimit)
		(*limiter).SetBurst(newBurst)
	})
}

func InitStreamLimit() {
	initLimiter(&stream.ClientDownloadLimit, conf.StreamMaxClientDownloadSpeed)
	initLimiter(&stream.ClientUploadLimit, conf.StreamMaxClientUploadSpeed)
	initLimiter(&stream.ServerDownloadLimit, conf.StreamMaxServerDownloadSpeed)
	initLimiter(&stream.ServerUploadLimit, conf.StreamMaxServerUploadSpeed)
}
