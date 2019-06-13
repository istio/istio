package fakes

import (
	"github.com/lukechampine/freeze"

	"istio.io/istio/pilot/pkg/model"
)

type frozenIstioConfigStore struct {
	*IstioConfigStore
}

func (frozen *frozenIstioConfigStore) List(typ string, namespace string) ([]model.Config, error) {
	configs, err := frozen.IstioConfigStore.List(typ, namespace)
	if err != nil {
		return nil, err
	}
	return freeze.Slice(configs).([]model.Config), nil
}

func (fake *IstioConfigStore) Freeze() model.IstioConfigStore {
	return &frozenIstioConfigStore{IstioConfigStore: fake}
}
