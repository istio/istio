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

package components

import (
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc/metadata"

	"istio.io/istio/galley/pkg/meshconfig"
	"istio.io/istio/galley/pkg/runtime"
	"istio.io/istio/galley/pkg/server/settings"
	"istio.io/istio/galley/pkg/source/kube/client"
	"istio.io/istio/galley/pkg/source/kube/dynamic/converter"
	"istio.io/istio/galley/pkg/source/kube/schema"
	sourceSchema "istio.io/istio/galley/pkg/source/kube/schema"
	"istio.io/istio/galley/pkg/testing/mock"
	"istio.io/istio/pkg/mcp/monitoring"
	mcptestmon "istio.io/istio/pkg/mcp/testing/monitoring"
)

func TestProcessing_StartErrors(t *testing.T) {
	g := NewGomegaWithT(t)
	defer resetPatchTable()
loop:
	for i := 0; ; i++ {
		resetPatchTable()
		mk := mock.NewKube()
		newKubeFromConfigFile = func(string) (client.Interfaces, error) { return mk, nil }
		newSource = func(client.Interfaces, time.Duration, *schema.Instance, *converter.Config) (runtime.Source, error) {
			return runtime.NewInMemorySource(), nil
		}
		newMeshConfigCache = func(path string) (meshconfig.Cache, error) { return meshconfig.NewInMemory(), nil }
		fsNew = func(string, *schema.Instance, *converter.Config, bool) (runtime.Source, error) {
			return runtime.NewInMemorySource(), nil
		}
		mcpMetricReporter = func(string) monitoring.Reporter {
			return nil
		}
		verifyResourceTypesPresence = func(k client.Interfaces, specs []sourceSchema.ResourceSpec) ([]sourceSchema.ResourceSpec, error) {
			return specs, nil
		}

		e := fmt.Errorf("err%d", i)

		args := settings.DefaultArgs()
		args.APIAddress = "tcp://0.0.0.0:0"
		args.Insecure = true

		switch i {
		case 0:
			newKubeFromConfigFile = func(string) (client.Interfaces, error) { return nil, e }
		case 1:
			newSource = func(client.Interfaces, time.Duration, *schema.Instance, *converter.Config) (runtime.Source, error) {
				return nil, e
			}
		case 2:
			netListen = func(network, address string) (net.Listener, error) { return nil, e }
		case 3:
			newMeshConfigCache = func(path string) (meshconfig.Cache, error) { return nil, e }
		case 4:
			args.ConfigPath = "aaa"
			fsNew = func(string, *schema.Instance, *converter.Config, bool) (runtime.Source, error) { return nil, e }
		case 5:
			args.DisableResourceReadyCheck = true
			findSupportedResources = func(k client.Interfaces, specs []sourceSchema.ResourceSpec) ([]sourceSchema.ResourceSpec, error) {
				return nil, e
			}
		case 6:
			args.Insecure = false
			args.AccessListFile = os.TempDir()
		case 7:
			args.Insecure = false
			args.AccessListFile = "invalid file"
		default:
			break loop
		}

		p := NewProcessing(args)
		err := p.Start()
		g.Expect(err).NotTo(BeNil())
		t.Logf("%d) err: %v", i, err)
		p.Stop()
	}
}

func TestServer_Basic(t *testing.T) {
	g := NewGomegaWithT(t)
	resetPatchTable()
	defer resetPatchTable()

	mk := mock.NewKube()
	newKubeFromConfigFile = func(string) (client.Interfaces, error) { return mk, nil }
	newSource = func(client.Interfaces, time.Duration, *schema.Instance, *converter.Config) (runtime.Source, error) {
		return runtime.NewInMemorySource(), nil
	}
	mcpMetricReporter = func(s string) monitoring.Reporter {
		return mcptestmon.NewInMemoryStatsContext()
	}
	newMeshConfigCache = func(path string) (meshconfig.Cache, error) { return meshconfig.NewInMemory(), nil }
	verifyResourceTypesPresence = func(_ client.Interfaces, specs []schema.ResourceSpec) ([]schema.ResourceSpec, error) {
		return specs, nil
	}

	args := settings.DefaultArgs()
	args.APIAddress = "tcp://0.0.0.0:0"
	args.Insecure = true

	p := NewProcessing(args)
	err := p.Start()
	g.Expect(err).To(BeNil())

	g.Expect(p.Address()).NotTo(BeNil())

	p.Stop()

	g.Expect(p.Address()).To(BeNil())
}

func TestParseSinkMeta(t *testing.T) {
	tests := []struct {
		name    string
		arg     []string
		want    metadata.MD
		wantErr bool
	}{
		{
			name: "Simple",
			arg:  []string{"foo=bar"},
			want: metadata.MD{
				"foo": []string{"bar"},
			},
		},
		{
			name: "MultipleValues",
			arg:  []string{"foo=bar1", "foo=bar2"},
			want: metadata.MD{
				"foo": []string{"bar1", "bar2"},
			},
		},
		{
			name: "MultipleKeys",
			arg:  []string{"foo1=bar", "foo2=bar"},
			want: metadata.MD{
				"foo1": []string{"bar"},
				"foo2": []string{"bar"},
			},
		},
		{
			name:    "NoSeparator",
			arg:     []string{"foo"},
			wantErr: true,
		},
		{
			name:    "NoValue",
			arg:     []string{"foo="},
			wantErr: true,
		},
		{
			name:    "NoKey",
			arg:     []string{"=foo"},
			wantErr: true,
		},
		{
			name:    "JustSeparator",
			arg:     []string{"="},
			wantErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			md := make(metadata.MD)
			err := parseSinkMeta(test.arg, md)
			if test.wantErr {
				if err == nil {
					t.Fatalf("Expected to fail but succeeded with: %v", md)
				}
				t.Logf("Failed as expected: %v", err)
				return
			}
			if got := md; !cmp.Equal(got, test.want) {
				t.Errorf("Wrong final metadata (-want, +got):\n%s", cmp.Diff(test.want, got))
			}
		})
	}
}
