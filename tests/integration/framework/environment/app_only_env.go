package environment

import (
	"log"

	"istio.io/istio/tests/integration/framework/component"
	fortioServer "istio.io/istio/tests/integration/framework/component/fortio_server"
)

type AppOnlyEnv struct {
	TestEnv
	EnvID string
	EndPoint string
}

func NewAppOnlyEnv(id string) *AppOnlyEnv {
	return &AppOnlyEnv{
		EnvID: id,
	}
}

func (appOnlyEnv *AppOnlyEnv) GetName() string {
	return appOnlyEnv.EnvID
}

func (appOnlyEnv *AppOnlyEnv) Bringup() {
}

func (appOnlyEnv *AppOnlyEnv) GetComponents() []component.Component {
	comps := []component.Component{}
	comps = append(comps, fortioServer.NewFortioServerComp("my_fortio_server", "/tmp"))

	return comps
}

func (appOnlyEnv *AppOnlyEnv) Cleanup() {
	log.Printf("Cleaning up %s", appOnlyEnv.EnvID)
}
