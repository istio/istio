// Copyright 2018 Istio Authors
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

package server

import (
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"istio.io/istio/galley/pkg/meshconfig"
	"istio.io/istio/galley/pkg/runtime"
	"istio.io/istio/galley/pkg/source/kube/client"
	"istio.io/istio/galley/pkg/source/kube/dynamic/converter"
	"istio.io/istio/galley/pkg/source/kube/schema"
	"istio.io/istio/galley/pkg/testing/mock"
	"istio.io/istio/pkg/mcp/monitoring"
	mcptestmon "istio.io/istio/pkg/mcp/testing/monitoring"
)

func TestNewServer_Errors(t *testing.T) {

loop:
	for i := 0; ; i++ {
		p := defaultPatchTable()
		mk := mock.NewKube()
		p.newKubeFromConfigFile = func(string) (client.Interfaces, error) { return mk, nil }
		p.newSource = func(client.Interfaces, time.Duration, *schema.Instance, *converter.Config) (runtime.Source, error) {
			return runtime.NewInMemorySource(), nil
		}
		p.newMeshConfigCache = func(path string) (meshconfig.Cache, error) { return meshconfig.NewInMemory(), nil }
		p.fsNew = func(string, *schema.Instance, *converter.Config) (runtime.Source, error) {
			return runtime.NewInMemorySource(), nil
		}
		p.mcpMetricReporter = func(string) monitoring.Reporter {
			return nil
		}

		e := errors.New("err")

		args := DefaultArgs()
		args.APIAddress = "tcp://0.0.0.0:0"
		args.Insecure = true

		switch i {
		case 0:
			p.newKubeFromConfigFile = func(string) (client.Interfaces, error) { return nil, e }
		case 1:
			p.newSource = func(client.Interfaces, time.Duration, *schema.Instance, *converter.Config) (runtime.Source, error) {
				return nil, e
			}
		case 2:
			p.netListen = func(network, address string) (net.Listener, error) { return nil, e }
		case 3:
			p.newMeshConfigCache = func(path string) (meshconfig.Cache, error) { return nil, e }
		case 4:
			args.ConfigPath = "aaa"
			p.fsNew = func(string, *schema.Instance, *converter.Config) (runtime.Source, error) { return nil, e }
		default:
			break loop
		}

		_, err := newServer(args, p)
		if err == nil {
			t.Fatalf("Expected error not found for i=%d", i)
		}
	}
}

func TestNewServer(t *testing.T) {
	args := DefaultArgs()
	args.APIAddress = "tcp://0.0.0.0:0"
	args.Insecure = true

	// filter out schemas for excluded kinds
	sourceResourceSchemas := getSourceSchema(args).All()

	p := defaultPatchTable()
	mk := mock.NewKube()
	p.newKubeFromConfigFile = func(string) (client.Interfaces, error) { return mk, nil }

	var gotSourceSchemas []schema.ResourceSpec
	p.newSource = func(_ client.Interfaces, _ time.Duration, si *schema.Instance, _ *converter.Config) (runtime.Source, error) {
		gotSourceSchemas = si.All()
		return runtime.NewInMemorySource(), nil
	}
	p.mcpMetricReporter = func(s string) monitoring.Reporter {
		return mcptestmon.NewInMemoryStatsContext()
	}
	p.newMeshConfigCache = func(path string) (meshconfig.Cache, error) { return meshconfig.NewInMemory(), nil }
	p.fsNew = func(string, *schema.Instance, *converter.Config) (runtime.Source, error) {
		return runtime.NewInMemorySource(), nil
	}
	p.verifyResourceTypesPresence = func(_ client.Interfaces, specs []schema.ResourceSpec) ([]schema.ResourceSpec, error) {
		return specs, nil
	}

	partialResourceList := sourceResourceSchemas[:len(sourceResourceSchemas)/2]
	p.findSupportedResources = func(interfaces client.Interfaces, _ []schema.ResourceSpec) ([]schema.ResourceSpec, error) {
		return partialResourceList, nil
	}

	tests := []struct {
		name                      string
		wantSourceSchemas         []schema.ResourceSpec
		disableResourceReadyCheck bool
	}{
		{
			name:              "Simple",
			wantSourceSchemas: sourceResourceSchemas,
		},
		{
			name:                      "DisableResourceReadyCheck",
			disableResourceReadyCheck: true,
			wantSourceSchemas:         partialResourceList,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(tt *testing.T) {
			args.DisableResourceReadyCheck = test.disableResourceReadyCheck
			s, err := newServer(args, p)
			if err != nil {
				tt.Fatalf("Unexpected error creating service: %v", err)
			}
			_ = s.Close()

			if len(gotSourceSchemas) != len(test.wantSourceSchemas) {
				tt.Fatalf("wrong number of resource sourceResourceSchemas found: got %v want %v \n gotSchemas %v\nwantSchemas %v",
					len(gotSourceSchemas), len(test.wantSourceSchemas), gotSourceSchemas, test.wantSourceSchemas)
			}

			for j := range gotSourceSchemas {
				if gotSourceSchemas[j].CanonicalResourceName() != test.wantSourceSchemas[j].CanonicalResourceName() {
					tt.Fatalf("wrong resource found: got %v want %v", gotSourceSchemas[j], test.wantSourceSchemas[j])
				}
			}
		})
	}
}

func TestServer_Basic(t *testing.T) {
	p := defaultPatchTable()
	mk := mock.NewKube()
	p.newKubeFromConfigFile = func(string) (client.Interfaces, error) { return mk, nil }
	p.newSource = func(client.Interfaces, time.Duration, *schema.Instance, *converter.Config) (runtime.Source, error) {
		return runtime.NewInMemorySource(), nil
	}
	p.mcpMetricReporter = func(s string) monitoring.Reporter {
		return mcptestmon.NewInMemoryStatsContext()
	}
	p.newMeshConfigCache = func(path string) (meshconfig.Cache, error) { return meshconfig.NewInMemory(), nil }
	p.verifyResourceTypesPresence = func(_ client.Interfaces, specs []schema.ResourceSpec) ([]schema.ResourceSpec, error) {
		return specs, nil
	}

	args := DefaultArgs()
	args.APIAddress = "tcp://0.0.0.0:0"
	args.Insecure = true
	s, err := newServer(args, p)
	if err != nil {
		t.Fatalf("Unexpected error creating service: %v", err)
	}

	// ensure that it does not crash
	addr := s.Address()
	if addr == nil {
		t.Fatalf("expected address not found")
	}

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		s.Run()
	}()

	wg.Wait()
	_ = s.Close()

	// ensure that it does not crash
	addr = s.Address()
	if addr != nil {
		t.Fatalf("unexpected address found")
	}
}
