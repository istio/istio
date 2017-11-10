package environment

import (
	"io/ioutil"
	"log"
	"path/filepath"

	"istio.io/istio/tests/integration/framework"
	fortioServer "istio.io/istio/tests/integration/framework/component/fortio_server"
	mixer "istio.io/istio/tests/integration/framework/component/mixer"
	proxy "istio.io/istio/tests/integration/framework/component/proxy"
)

type MixerEnvoyEnv struct {
	framework.TestEnv
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

func (mixerEnvoyEnv *MixerEnvoyEnv) GetComponents() []framework.Component {
	logDir, err := ioutil.TempDir("", mixerEnvoyEnv.GetName())
	if err != nil {
		log.Printf("Failed to get a temp dir: %v", err)
		return nil
	}
	configDir := filepath.Join(logDir, "config")

	comps := []framework.Component{}
	comps = append(comps, fortioServer.NewFortioServerComp("my_fortio_server", logDir))
	comps = append(comps, mixer.NewMixerComponent("my_local_mixer", logDir, configDir))
	comps = append(comps, proxy.NewProxyComponent("my_local_envory", logDir))

	return comps
}

func (mixerEnvoyEnv *MixerEnvoyEnv) Cleanup() {
	log.Printf("Cleaning up %s", mixerEnvoyEnv.EnvID)
}
