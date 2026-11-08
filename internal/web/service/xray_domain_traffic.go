package service

import "github.com/mhsanaei/3x-ui/v3/internal/xray"

func (s *XrayService) GetDomainTraffic(ackCore, ackSequence string) (*xray.DomainTrafficBatch, error) {
	process := currentXrayProcess()
	if process == nil || !process.IsRunning() {
		return nil, ErrXrayNotRunning
	}
	api := new(xray.XrayAPI)
	if err := api.Init(process.GetAPIPort()); err != nil {
		return nil, err
	}
	defer api.Close()
	return api.GetDomainTraffic(ackCore, ackSequence)
}
