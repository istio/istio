// Copyright 2017 Istio Authors
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

package envoy

import (
	"reflect"
	"testing"

	"istio.io/pilot/proxy"
	"istio.io/pilot/test/mock"
)

func TestHandlers(t *testing.T) {
	controller := mockController{}
	mesh := proxy.DefaultMeshConfig()
	context := proxy.Context{
		Discovery:  mock.Discovery,
		Accounts:   mock.Discovery,
		Config:     mock.MakeRegistry(),
		MeshConfig: &mesh,
	}
	_, err := NewWatcher(&controller, &context)
	if err != nil {
		t.Errorf("failed creating watcher %v", err)
	}
	if controller.handlers != 4 {
		t.Errorf("expected handlers for services, instances, route rules, and destinations, got %d", controller.handlers)
	}
}

func TestEnvoyArgs(t *testing.T) {
	mesh := proxy.DefaultMeshConfig()
	got := envoyArgs("test.json", 5, &mesh, "my-proxy")
	want := []string{
		"-c", "test.json",
		"--restart-epoch", "5",
		"--drain-time-s", "2",
		"--parent-shutdown-time-s", "3",
		"--service-cluster", mesh.IstioServiceCluster,
		"--service-node", "my-proxy",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("envoyArgs() => got %v, want %v", got, want)
	}
}
