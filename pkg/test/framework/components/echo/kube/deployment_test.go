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
package kube

import (
	"testing"

	testutil "istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/cluster/clusterboot"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common"
	"istio.io/istio/pkg/test/framework/image"
	"istio.io/istio/pkg/test/framework/resource"
)

var settings = &image.Settings{
	Hub:             "testing.hub",
	Tag:             "latest",
	PullPolicy:      "Always",
	ImagePullSecret: "testdata/secret.yaml",
}

func TestDeploymentYAML(t *testing.T) {
	testCase := []struct {
		name         string
		wantFilePath string
		config       echo.Config
		revVerMap    resource.RevVerMap
	}{
		{
			name:         "basic",
			wantFilePath: "testdata/basic.yaml",
			config: echo.Config{
				Service: "foo",
				Version: "bar",
				Ports: []echo.Port{
					{
						Name:         "http",
						Protocol:     protocol.HTTP,
						InstancePort: 8090,
						ServicePort:  8090,
					},
				},
			},
		},
		{
			name:         "two-workloads-one-nosidecar",
			wantFilePath: "testdata/two-workloads-one-nosidecar.yaml",
			config: echo.Config{
				Service: "foo",
				Ports: []echo.Port{
					{
						Name:         "http",
						Protocol:     protocol.HTTP,
						InstancePort: 8090,
						ServicePort:  8090,
					},
				},
				Subsets: []echo.SubsetConfig{
					{
						Version: "v1",
					},
					{
						Version:     "nosidecar",
						Annotations: echo.NewAnnotations().SetBool(echo.SidecarInject, false),
					},
				},
			},
		},
		{
			name:         "healthcheck-rewrite",
			wantFilePath: "testdata/healthcheck-rewrite.yaml",
			config: echo.Config{
				Service: "healthcheck",
				Ports: []echo.Port{{
					Name:         "http-8080",
					Protocol:     protocol.HTTP,
					ServicePort:  8080,
					InstancePort: 8080,
				}},
				Subsets: []echo.SubsetConfig{
					{
						Annotations: echo.NewAnnotations().SetBool(echo.SidecarRewriteAppHTTPProbers, true),
					},
				},
			},
		},
		{
			name:         "multiversion",
			wantFilePath: "testdata/multiversion.yaml",
			config: echo.Config{
				Service: "multiversion",
				Subsets: []echo.SubsetConfig{
					{
						Version: "v-istio",
					},
					{
						Version:     "v-legacy",
						Annotations: echo.NewAnnotations().SetBool(echo.SidecarInject, false),
					},
				},
				Ports: []echo.Port{
					{
						Name:         "http",
						Protocol:     protocol.HTTP,
						InstancePort: 8090,
						ServicePort:  8090,
					},
					{
						Name:         "tcp",
						Protocol:     protocol.TCP,
						InstancePort: 9000,
						ServicePort:  9000,
					},
					{
						Name:         "grpc",
						Protocol:     protocol.GRPC,
						InstancePort: 9090,
						ServicePort:  9090,
					},
				},
			},
		},
		{
			name:         "multiple-istio-versions",
			wantFilePath: "testdata/multiple-istio-versions.yaml",
			config: echo.Config{
				Service: "foo",
				Version: "bar",
				Ports: []echo.Port{
					{
						Name:         "http",
						Protocol:     protocol.HTTP,
						InstancePort: 8090,
						ServicePort:  8090,
					},
				},
			},
			revVerMap: resource.RevVerMap{
				"rev-a": resource.IstioVersion("1.8.2"),
				"rev-b": resource.IstioVersion("1.9.0"),
			},
		},
	}
	for _, tc := range testCase {
		t.Run(tc.name, func(t *testing.T) {
			clusters, err := clusterboot.NewFactory().With(cluster.Config{
				Kind: cluster.Fake, Name: "cluster-0",
				Meta: cluster.ConfigMeta{"majorVersion": 1, "minorVersion": 16},
			}).Build()
			if err != nil {
				t.Fatal(err)
			}
			tc.config.Cluster = clusters[0]
			if err := common.FillInDefaults(nil, &tc.config); err != nil {
				t.Errorf("failed filling in defaults: %v", err)
			}
			serviceYAML, err := GenerateService(tc.config)
			if err != nil {
				t.Errorf("failed to generate service %v", err)
			}
			deploymentYAML, err := generateDeploymentYAML(tc.config, settings, tc.revVerMap)
			if err != nil {
				t.Errorf("failed to generate deployment %v", err)
			}
			gotBytes := []byte(serviceYAML + "---" + deploymentYAML)
			wantedBytes := testutil.ReadGoldenFile(gotBytes, tc.wantFilePath, t)

			wantBytes := testutil.StripVersion(wantedBytes)
			gotBytes = testutil.StripVersion(gotBytes)

			testutil.RefreshGoldenFile(gotBytes, tc.wantFilePath, t)

			testutil.CompareBytes(gotBytes, wantBytes, tc.wantFilePath, t)
		})
	}
}
