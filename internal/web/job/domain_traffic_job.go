package job

import (
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

type DomainTrafficJob struct {
	mu        sync.Mutex
	xray      service.XrayService
	history   service.DomainTrafficService
	settings  service.SettingService
	cleaned   time.Time
	lastError string
}

func NewDomainTrafficJob() *DomainTrafficJob { return new(DomainTrafficJob) }
func (j *DomainTrafficJob) Run() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if !j.xray.IsXrayRunning() {
		return
	}
	core, sequence, err := j.history.Acknowledgement()
	if err != nil {
		logger.Warning("domain traffic checkpoint: ", err)
		return
	}
	batch, err := j.xray.GetDomainTraffic(core, sequence)
	if err != nil {
		message := err.Error()
		if message != j.lastError {
			logger.Warning("domain traffic unavailable: ", err)
			_ = j.history.SetError(message)
			j.lastError = message
		}
		return
	}
	location, err := j.settings.GetTimeLocation()
	if err != nil {
		logger.Warning("domain traffic time zone: ", err)
		return
	}
	if err := j.history.Commit(batch, location); err != nil {
		logger.Warning("domain traffic commit: ", err)
		return
	}
	j.lastError = ""
	if time.Since(j.cleaned) > 10*time.Minute {
		if err := j.history.Cleanup(); err != nil {
			logger.Warning("domain traffic cleanup: ", err)
		} else {
			j.cleaned = time.Now()
		}
	}
}
