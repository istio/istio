//go:build gofuzz
// +build gofuzz

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
package envoyfilter

import (
	fuzz "github.com/AdaLogics/go-fuzz-headers"
	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/memory"
	"istio.io/istio/pkg/config/host"
)

func InternalFuzzApplyClusterMerge(data []byte) int {
	f := fuzz.NewConsumer(data)

	// create config patches
	number, err := f.GetInt()
	if err != nil {
		return 0
	}
	numberOfPatches := number % 30
	configPatches := make([]*networking.EnvoyFilter_EnvoyConfigObjectPatch, numberOfPatches)
	for i := 0; i < numberOfPatches; i++ {
		patch := &networking.EnvoyFilter_EnvoyConfigObjectPatch{}
		err = f.GenerateStruct(patch)
		if err != nil {
			return 0
		}
		configPatches = append(configPatches, patch)
	}

	// create proxy
	proxy := &model.Proxy{}
	err = f.GenerateStruct(proxy)
	if err != nil {
		return 0
	}

	// crete mesh config
	testMesh := meshconfig.MeshConfig{}
	err = f.GenerateStruct(&testMesh)
	if err != nil {
		return 0
	}

	// create host
	fuzz_host, err := f.GetString()
	if err != nil {
		return 0
	}

	c := &cluster.Cluster{}
	err = f.GenerateStruct(c)
	if err != nil {
		return 0
	}

	serviceDiscovery := memory.NewServiceDiscovery()
	env := newTestEnvironment(serviceDiscovery, testMesh, buildEnvoyFilterConfigStore(configPatches))
	push := model.NewPushContext()
	push.InitContext(env, nil, nil)
	efw := push.EnvoyFilters(proxy)
	_ = ApplyClusterMerge(networking.EnvoyFilter_GATEWAY, efw, c, []host.Name{host.Name(fuzz_host)})
	return 1
}
