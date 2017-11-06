package environment

import (
	"log"

	"istio.io/istio/tests/integration/framework/component"
	fortioServer "istio.io/istio/tests/integration/framework/component/fortio_server"
	mixer "istio.io/istio/tests/integration/framework/component/mixer"
	proxy "istio.io/istio/tests/integration/framework/component/proxy"
)

type MixerEnvoyEnv struct {
	TestEnv
	EnvID string
}

func NewMixerEnvoyEnv(id string) *MixerEnvoyEnv {
	return &MixerEnvoyEnv{
		EnvID: id,
	}
}

func (mixerEnvoyEnv *MixerEnvoyEnv) GetName() string {
	return mixerEnvoyEnv.EnvID
}

func (mixerEnvoyEnv *MixerEnvoyEnv) Bringup() {
}

func (mixerEnvoyEnv *MixerEnvoyEnv) GetComponents() []component.Component {
	logDir := "/tmp"
	comps := []component.Component{}
	comps = append(comps, fortioServer.NewFortioServerComp("my_fortio_server", logDir))
	comps = append(comps, mixer.NewMixerComponent("my_local_mixer", logDir, "/home/yutongz/go/src/istio.io"))
	comps = append(comps, proxy.NewProxyComponent("my_local_envory", logDir))

	return comps
}

func (mixerEnvoyEnv *MixerEnvoyEnv) Cleanup() {
	log.Printf("Cleaning up %s", mixerEnvoyEnv.EnvID)
}
