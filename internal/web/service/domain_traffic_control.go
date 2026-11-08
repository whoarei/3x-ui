package service

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

type DomainTrafficControl struct {
	Configured bool   `json:"configured" example:"false"`
	Running    bool   `json:"running" example:"true"`
	Supported  bool   `json:"supported" example:"true"`
	Enabled    bool   `json:"enabled" example:"false"`
	Error      string `json:"error" example:""`
}

func (s *XrayService) configureDomainTraffic(cfg *xray.Config) error {
	enabled, err := s.settingService.GetDomainTrafficEnabled()
	if err != nil {
		return err
	}
	env := map[string]string{}
	if len(cfg.Env) > 0 && string(cfg.Env) != "null" {
		if err := json.Unmarshal(cfg.Env, &env); err != nil {
			return fmt.Errorf("invalid Xray environment: %w", err)
		}
	}
	if env == nil {
		env = map[string]string{}
	}
	env["XRAY_DOMAIN_STATS"] = "0"
	if enabled {
		env["XRAY_DOMAIN_STATS"] = "1"
	}
	cfg.Env, err = json.Marshal(env)
	return err
}

func (s *XrayService) GetDomainTrafficControl() (*DomainTrafficControl, error) {
	enabled, err := s.settingService.GetDomainTrafficEnabled()
	if err != nil {
		return nil, err
	}
	state := &DomainTrafficControl{Configured: enabled, Running: s.IsXrayRunning()}
	if !state.Running {
		state.Error = ErrXrayNotRunning.Error()
		return state, nil
	}
	batch, err := s.GetDomainTraffic("", "0")
	if err != nil {
		state.Error = err.Error()
		return state, nil
	}
	state.Supported, state.Enabled = true, batch.Enabled
	return state, nil
}

func (s *XrayService) SetDomainTrafficEnabled(enabled bool) (*DomainTrafficControl, error) {
	lock.Lock()
	defer lock.Unlock()
	state, err := s.GetDomainTrafficControl()
	if err != nil {
		return nil, err
	}
	if state.Configured == enabled && state.Running && state.Enabled == enabled && (state.Supported || !enabled) {
		return state, nil
	}
	if enabled && state.Running && !state.Supported {
		return state, fmt.Errorf("custom Xray domain API is not available: %s", state.Error)
	}
	if err := s.settingService.setBool("domainTrafficEnabled", enabled); err != nil {
		return state, err
	}
	if err := s.restartXrayLocked(true); err != nil {
		state, _ = s.GetDomainTrafficControl()
		return state, fmt.Errorf("setting saved, but Xray restart failed: %w", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		state, err = s.GetDomainTrafficControl()
		if err != nil {
			return nil, err
		}
		if state.Running && state.Supported && state.Enabled == enabled {
			return state, nil
		}
		if !state.Running || time.Now().After(deadline) {
			return state, fmt.Errorf("setting saved, but Xray mode was not confirmed: %s", state.Error)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
