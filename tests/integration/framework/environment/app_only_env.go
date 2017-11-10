package environment

import (
	"log"

	"istio.io/istio/tests/integration/framework"
	fortioServer "istio.io/istio/tests/integration/framework/component/fortio_server"
)

type AppOnlyEnv struct {
	framework.TestEnv
	EnvID    string
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

func (appOnlyEnv *AppOnlyEnv) GetComponents() []framework.Component {
	comps := []framework.Component{}
	comps = append(comps, fortioServer.NewFortioServerComp("my_fortio_server", "/tmp"))

	return comps
}

func (appOnlyEnv *AppOnlyEnv) Cleanup() {
	log.Printf("Cleaning up %s", appOnlyEnv.EnvID)
}
