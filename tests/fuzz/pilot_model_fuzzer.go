// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// nolint: golint
package fuzz

import (
	fuzz "github.com/AdaLogics/go-fuzz-headers"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh"
)

func NewSI(f *fuzz.ConsumeFuzzer) (*model.ServiceInstance, error) {
	si := &model.ServiceInstance{}
	err := f.GenerateStruct(si)
	if err != nil {
		return si, err
	}
	err = si.Validate()
	if err != nil {
		return si, err
	}
	return si, nil
}

func NewS(f *fuzz.ConsumeFuzzer) (*model.Service, error) {
	s := &model.Service{}
	err := f.GenerateStruct(s)
	if err != nil {
		return s, err
	}
	err = s.Validate()
	if err != nil {
		return s, err
	}
	return s, nil
}

func FuzzInitContext(data []byte) int {
	f := fuzz.NewConsumer(data)
	configString, err := f.GetString()
	if err != nil {
		return 0
	}
	m, err := mesh.ApplyMeshConfigDefaults(configString)
	if err != nil {
		return 0
	}

	// Create service instances
	serviceInstances := make([]*model.ServiceInstance, 0)
	number, err := f.GetInt()
	if err != nil {
		return 0
	}
	// We allow a maximum of 20 service instances
	numberOfS := number % 20
	for i := 0; i < numberOfS; i++ {
		si, err := NewSI(f)
		if err != nil {
			return 0
		}
		serviceInstances = append(serviceInstances, si)
	}

	// Create services
	services := make([]*model.Service, 0)
	number, err = f.GetInt()
	if err != nil {
		return 0
	}
	// We allow a maximum of 20 services
	numberOfS = number % 20
	for i := 0; i < numberOfS; i++ {
		s, err := NewS(f)
		if err != nil {
			return 0
		}
		services = append(services, s)
	}

	env := &model.Environment{}
	store := model.NewFakeStore()

	env.IstioConfigStore = model.MakeIstioStore(store)
	env.ServiceDiscovery = &localServiceDiscovery{
		services:         services,
		serviceInstances: serviceInstances,
	}

	env.Watcher = mesh.NewFixedWatcher(m)
	env.Init()
	pc := model.NewPushContext()
	_ = pc.InitContext(env, nil, nil)

	return 1
}

// The following code is taken as-is
// from istio/pilot/pkg/model/push_context_test.go:

var _ model.ServiceDiscovery = &localServiceDiscovery{}

// MockDiscovery is an in-memory ServiceDiscover with mock services
type localServiceDiscovery struct {
	services         []*model.Service
	serviceInstances []*model.ServiceInstance
}

var _ model.ServiceDiscovery = &localServiceDiscovery{}

func (l *localServiceDiscovery) Services() ([]*model.Service, error) {
	return l.services, nil
}

func (l *localServiceDiscovery) GetService(hostname host.Name) (*model.Service, error) {
	panic("implement me")
}

func (l *localServiceDiscovery) InstancesByPort(svc *model.Service, servicePort int, labels labels.Collection) []*model.ServiceInstance {
	return l.serviceInstances
}

func (l *localServiceDiscovery) GetProxyServiceInstances(proxy *model.Proxy) []*model.ServiceInstance {
	panic("implement me")
}

func (l *localServiceDiscovery) GetProxyWorkloadLabels(proxy *model.Proxy) labels.Collection {
	panic("implement me")
}

func (l *localServiceDiscovery) GetIstioServiceAccounts(svc *model.Service, ports []int) []string {
	return nil
}

func (l *localServiceDiscovery) NetworkGateways() []*model.NetworkGateway {
	// TODO implement fromRegistry logic from kube controller if needed
	return nil
}
