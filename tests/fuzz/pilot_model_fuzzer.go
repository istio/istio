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
	"errors"

	fuzz "github.com/AdaLogics/go-fuzz-headers"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/memory"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/protocol"
)

var protocols = []protocol.Instance{
	protocol.TCP,
	protocol.UDP,
	protocol.GRPC,
	protocol.GRPCWeb,
	protocol.HTTP,
	protocol.HTTP_PROXY,
	protocol.HTTP2,
	protocol.HTTPS,
	protocol.TLS,
	protocol.Mongo,
	protocol.Redis,
	protocol.MySQL,
}

// Creates a new fuzzed ServiceInstance
func NewSI(f *fuzz.ConsumeFuzzer) (*model.ServiceInstance, error) {
	si := &model.ServiceInstance{}
	err := f.GenerateStruct(si)
	if err != nil {
		return si, err
	}
	s, err := NewS(f)
	if err != nil {
		return si, err
	}
	p, err := createPort(f)
	if err != nil {
		return si, err
	}
	s.Ports = append(s.Ports, p)
	si.ServicePort = p
	si.Service = s
	err = si.Validate()
	if err != nil {
		return si, err
	}
	return si, nil
}

// Gets a protocol from global var protocols
func getProtocolInstance(f *fuzz.ConsumeFuzzer) (protocol.Instance, error) {
	pIndex, err := f.GetInt()
	if err != nil {
		return protocol.Unsupported, errors.New("could not create protocolInstance")
	}
	i := protocols[pIndex%len(protocols)]
	return i, nil
}

// Creates a new fuzzed Port
func createPort(f *fuzz.ConsumeFuzzer) (*model.Port, error) {
	p := &model.Port{}
	name, err := f.GetString()
	if err != nil {
		return p, err
	}
	port, err := f.GetInt()
	if err != nil {
		return p, err
	}
	protocolinstance, err := getProtocolInstance(f)
	if err != nil {
		return p, err
	}
	p.Name = name
	p.Port = port
	p.Protocol = protocolinstance
	return p, nil
}

// Creates a new fuzzed Port slice
func createPorts(f *fuzz.ConsumeFuzzer) ([]*model.Port, error) {
	ports := make([]*model.Port, 0, 20)
	numberOfPorts, err := f.GetInt()
	if err != nil {
		return ports, err
	}
	// Maximum 20 ports:
	maxPorts := numberOfPorts % 20
	if maxPorts == 0 {
		maxPorts = 1
	}
	for i := 0; i < maxPorts; i++ {
		port, err := createPort(f)
		if err != nil {
			return ports, err
		}
		ports = append(ports, port)
	}
	return ports, nil
}

// Creates a new fuzzed Service
func NewS(f *fuzz.ConsumeFuzzer) (*model.Service, error) {
	s := &model.Service{}
	err := f.GenerateStruct(s)
	if err != nil {
		return s, err
	}
	ports, err := createPorts(f)
	if err != nil {
		return s, err
	}
	s.Ports = ports
	hostname, err := f.GetString()
	if err != nil {
		return s, err
	}
	s.Hostname = host.Name(hostname)
	err = s.Validate()
	if err != nil {
		return s, err
	}
	return s, nil
}

// Creates an Environment with fuzzed values
// and passes that to InitContext
func FuzzInitContext(data []byte) int {
	f := fuzz.NewConsumer(data)

	// Create service instances
	serviceInstances := make([]*model.ServiceInstance, 0, 20)
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
	services := make([]*model.Service, 0, 20)
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

	configString, err := f.GetString()
	if err != nil {
		return 0
	}
	m, err := mesh.ApplyMeshConfigDefaults(configString)
	if err != nil {
		return 0
	}

	env := &model.Environment{}
	store := model.NewFakeStore()

	env.IstioConfigStore = model.MakeIstioStore(store)
	sd := memory.NewServiceDiscovery(services...)
	sd.WantGetProxyServiceInstances = serviceInstances
	env.ServiceDiscovery = sd

	env.Watcher = mesh.NewFixedWatcher(m)
	env.Init()
	pc := model.NewPushContext()
	_ = pc.InitContext(env, nil, nil)
	return 1
}

func FuzzBNMUnmarshalJSON(data []byte) int {
	var bnm model.BootstrapNodeMetadata
	_ = bnm.UnmarshalJSON(data)
	return 1
}
