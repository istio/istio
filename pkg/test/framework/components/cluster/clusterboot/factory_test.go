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

package clusterboot

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/config"
)

func TestBuild(t *testing.T) {
	tests := []struct {
		config  cluster.Config
		cluster cluster.FakeCluster
	}{
		{
			config: cluster.Config{Kind: cluster.Fake, Name: "auto-fill-primary", Network: "network-0"},
			cluster: cluster.FakeCluster{
				ExtendedClient: kube.MockClient{},
				Topology: cluster.Topology{
					ClusterName: "auto-fill-primary",
					ClusterKind: cluster.Fake,
					// The primary and config clusters should match the cluster name when not specified
					PrimaryClusterName: "auto-fill-primary",
					ConfigClusterName:  "auto-fill-primary",
					Network:            "network-0",
					ConfigMetadata:     config.Map{},
				},
			},
		},
		{
			config: cluster.Config{Kind: cluster.Fake, Name: "auto-fill-remote", PrimaryClusterName: "auto-fill-primary"},
			cluster: cluster.FakeCluster{
				ExtendedClient: kube.MockClient{},
				Topology: cluster.Topology{
					ClusterName:        "auto-fill-remote",
					ClusterKind:        cluster.Fake,
					PrimaryClusterName: "auto-fill-primary",
					// The config cluster should match the primary cluster when not specified
					ConfigClusterName: "auto-fill-primary",
					Index:             1,
					ConfigMetadata:    config.Map{},
				},
			},
		},
		{
			config: cluster.Config{Kind: cluster.Fake, Name: "external-istiod", ConfigClusterName: "remote-config"},
			cluster: cluster.FakeCluster{
				ExtendedClient: kube.MockClient{},
				Topology: cluster.Topology{
					ClusterName:        "external-istiod",
					ClusterKind:        cluster.Fake,
					PrimaryClusterName: "external-istiod",
					ConfigClusterName:  "remote-config",
					Index:              2,
					ConfigMetadata:     config.Map{},
				},
			},
		},
		{
			config: cluster.Config{
				Name:               "remote-config",
				Kind:               cluster.Fake,
				PrimaryClusterName: "external-istiod",
				ConfigClusterName:  "remote-config",
			},
			cluster: cluster.FakeCluster{
				ExtendedClient: kube.MockClient{},
				Topology: cluster.Topology{
					ClusterName: "remote-config",
					ClusterKind: cluster.Fake,
					// Explicitly specified in config, should be copied exactly
					PrimaryClusterName: "external-istiod",
					ConfigClusterName:  "remote-config",
					Index:              3,
					ConfigMetadata:     config.Map{},
				},
			},
		},
	}
	var clusters cluster.Clusters
	t.Run("build", func(t *testing.T) {
		factory := NewFactory()
		for _, tc := range tests {
			factory = factory.With(tc.config)
		}
		var err error
		clusters, err = factory.Build()
		if err != nil {
			t.Fatal(err)
		}
		if len(clusters) != len(tests) {
			t.Errorf("expcted %d clusters but built %d", len(tests), len(clusters))
		}
	})
	for _, tc := range tests {
		t.Run("built "+tc.config.Name, func(t *testing.T) {
			built := *clusters.GetByName(tc.config.Name).(*cluster.FakeCluster)
			// don't compare these
			built.AllClusters = nil
			built.Version = nil
			if diff := cmp.Diff(built, tc.cluster); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

func TestValidation(t *testing.T) {
	tests := map[string][]cluster.Config{
		"empty kind": {
			{Name: "no-kind"},
		},
		"empty name": {
			{Kind: cluster.Fake},
		},
		"duplicate name": {
			{Kind: cluster.Fake, Name: "dupe"},
			{Kind: cluster.Fake, Name: "dupe"},
		},
		"non-existent primary": {
			{Kind: cluster.Fake, Name: "no-primary", PrimaryClusterName: "does-not-exist"},
		},
		"non-existent config": {
			{Kind: cluster.Fake, Name: "no-primary", ConfigClusterName: "does-not-exist"},
		},
		"vm without kube primary": {
			{Kind: cluster.StaticVM, Name: "vm", Meta: config.Map{"deployments": []any{
				config.Map{
					"service": "vm", "namespace": "echo", "instances": []config.Map{{"ip": "1.2.3.4"}},
				},
			}}},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := NewFactory().With(tc...).Build()
			if err == nil {
				t.Fatal("expected err but got nil")
			}
		})
	}
}
