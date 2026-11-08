package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func TestDomainSwitchOverridesInheritedEnvironment(t *testing.T) {
	binary := os.Getenv("XRAY_DOMAIN_E2E_BINARY")
	if binary == "" {
		t.Skip("set XRAY_DOMAIN_E2E_BINARY to the custom domain kernel")
	}
	initTrafficTestDB(t)
	dir := t.TempDir()
	t.Setenv("XUI_BIN_FOLDER", dir)
	t.Setenv("XUI_LOG_FOLDER", filepath.Join(dir, "log"))
	t.Setenv("XRAY_DOMAIN_STATS", "1")
	if err := os.Symlink(binary, filepath.Join(dir, xray.GetBinaryName())); err != nil {
		t.Fatal(err)
	}
	apiPort := freeLoopbackPort(t)
	template := string(mustJSON(t, map[string]any{
		"log": map[string]any{"loglevel": "warning"}, "stats": map[string]any{},
		"api":       map[string]any{"tag": "api", "services": []string{"StatsService"}},
		"env":       map[string]string{"XRAY_DOMAIN_STATS": "1", "DOMAIN_SWITCH_KEEP": "preserved"},
		"inbounds":  []any{map[string]any{"tag": "api", "listen": "127.0.0.1", "port": apiPort, "protocol": "dokodemo-door", "settings": map[string]any{"address": "127.0.0.1"}}},
		"outbounds": []any{map[string]any{"tag": "direct", "protocol": "freedom"}},
		"routing":   map[string]any{"rules": []any{map[string]any{"type": "field", "inboundTag": []string{"api"}, "outboundTag": "api"}}},
	}))
	settings := SettingService{}
	if err := settings.saveSetting("xrayTemplateConfig", template); err != nil {
		t.Fatal(err)
	}
	if err := settings.saveSetting("domainTrafficEnabled", "false"); err != nil {
		t.Fatal(err)
	}
	previous, result := xrayState.snapshot()
	wasStopped := isManuallyStopped.Load()
	t.Cleanup(func() {
		if process := currentXrayProcess(); process != nil && process != previous {
			_ = process.Stop()
		}
		xrayState.replace(previous)
		xrayState.storeResult(previous, result)
		isManuallyStopped.Store(wasStopped)
	})
	svc := XrayService{}
	waitReady := func() *DomainTrafficControl {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for {
			state, err := svc.GetDomainTrafficControl()
			if err == nil && state.Running && state.Supported {
				return state
			}
			if time.Now().After(deadline) {
				t.Fatalf("domain RPC did not become ready: %+v, %v", state, err)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	if err := svc.RestartXray(true); err != nil {
		t.Fatal(err)
	}
	if waitReady().Enabled {
		t.Fatal("saved OFF was overridden by the parent environment or template; the live kernel still collects domain traffic")
	}
	initial := currentXrayProcess()
	var env map[string]string
	if err := json.Unmarshal(initial.GetConfig().Env, &env); err != nil || env["DOMAIN_SWITCH_KEEP"] != "preserved" || env["XRAY_DOMAIN_STATS"] != "0" {
		t.Fatalf("saved OFF did not preserve the other environment values: %v, %v", env, err)
	}
	state, err := svc.SetDomainTrafficEnabled(true)
	if err != nil || !state.Configured || !state.Enabled || !state.Running || !state.Supported {
		t.Fatalf("ON was not applied to the live core: %+v, %v", state, err)
	}
	if currentXrayProcess() == initial || initial.IsRunning() {
		t.Fatal("mode change did not replace and stop the previous core")
	}
	on := currentXrayProcess()
	if _, err := svc.SetDomainTrafficEnabled(true); err != nil || currentXrayProcess() != on {
		t.Fatalf("an already applied setting unnecessarily restarted the core: %v", err)
	}
	if err := (&XrayService{}).RestartXray(true); err != nil {
		t.Fatal(err)
	}
	state = waitReady()
	if !state.Configured || !state.Enabled {
		t.Fatalf("a later process restart forgot the saved ON setting: %+v", state)
	}
	state, err = svc.SetDomainTrafficEnabled(false)
	if err != nil || state.Configured || state.Enabled || !state.Running || !state.Supported {
		t.Fatalf("OFF was not applied despite inherited and template ON: %+v, %v", state, err)
	}
	off := currentXrayProcess()
	if err := settings.saveSetting("xrayTemplateConfig", "{ invalid"); err != nil {
		t.Fatal(err)
	}
	state, err = svc.SetDomainTrafficEnabled(true)
	if err == nil || !strings.Contains(err.Error(), "restart failed") || !state.Configured || state.Enabled || !state.Running || currentXrayProcess() != off {
		t.Fatalf("failed apply hid the saved/live mismatch or stopped the working core: %+v, %v", state, err)
	}
	if err := settings.saveSetting("xrayTemplateConfig", template); err != nil {
		t.Fatal(err)
	}
	state, err = svc.SetDomainTrafficEnabled(true)
	if err != nil || !state.Configured || !state.Enabled || !state.Running {
		t.Fatalf("retry of the saved but unapplied setting did not recover: %+v, %v", state, err)
	}
}
