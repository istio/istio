package appoptics

import (
	"istio.io/istio/mixer/adapter/appoptics/papertrail"
	"istio.io/istio/mixer/pkg/adapter"
)

type adapterEnvInst struct {
}

func (a *adapterEnvInst) Logger() adapter.Logger {
	return &papertrail.LoggerImpl{}
}

func (a *adapterEnvInst) ScheduleWork(fn adapter.WorkFunc) {
}

func (a *adapterEnvInst) ScheduleDaemon(fn adapter.DaemonFunc) {
}
