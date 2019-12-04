// Copyright 2019 Istio Authors
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

package model

import (
	"fmt"
	"reflect"
	"testing"

	"istio.io/istio/pkg/config/mesh"

	meshconfig "istio.io/api/mesh/v1alpha1"
	securityBeta "istio.io/api/security/v1beta1"
	selectorpb "istio.io/api/type/v1beta1"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schemas"
)

const (
	rootNamespace = "istio-config"
)

func TestGetJwtPoliciesForWorkload(t *testing.T) {
	policies := getTestAuthenticationPolicies(createTestConfigs(), t)

	cases := []struct {
		name              string
		workloadNamespace string
		workloadLabels    labels.Collection
		want              []*Config
	}{
		{
			name:              "Empty workload labels in foo",
			workloadNamespace: "foo",
			workloadLabels:    labels.Collection{},
			want: []*Config{
				{
					ConfigMeta: ConfigMeta{
						Type:      "request-authentication",
						Name:      "default",
						Namespace: "foo",
					},
					Spec: &securityBeta.RequestAuthentication{},
				},
				{
					ConfigMeta: ConfigMeta{
						Type:      "request-authentication",
						Name:      "default",
						Namespace: "istio-config",
					},
					Spec: &securityBeta.RequestAuthentication{},
				},
			},
		},
		{
			name:              "Empty workload labels in bar",
			workloadNamespace: "bar",
			workloadLabels:    labels.Collection{},
			want: []*Config{
				{
					ConfigMeta: ConfigMeta{
						Type:      "request-authentication",
						Name:      "default",
						Namespace: "bar",
					},
					Spec: &securityBeta.RequestAuthentication{},
				},
				{
					ConfigMeta: ConfigMeta{
						Type:      "request-authentication",
						Name:      "default",
						Namespace: "istio-config",
					},
					Spec: &securityBeta.RequestAuthentication{},
				},
			},
		},
		{
			name:              "Empty workload labels in baz",
			workloadNamespace: "baz",
			workloadLabels:    labels.Collection{},
			want: []*Config{
				{
					ConfigMeta: ConfigMeta{
						Type:      "request-authentication",
						Name:      "default",
						Namespace: "istio-config",
					},
					Spec: &securityBeta.RequestAuthentication{},
				},
			},
		},
		{
			name:              "Match workload labels in foo",
			workloadNamespace: "foo",
			workloadLabels:    labels.Collection{{"app": "httpbin", "version": "v1", "other": "labels"}},
			want: []*Config{
				{
					ConfigMeta: ConfigMeta{
						Type:      "request-authentication",
						Name:      "default",
						Namespace: "foo",
					},
					Spec: &securityBeta.RequestAuthentication{},
				},
				{
					ConfigMeta: ConfigMeta{
						Type:      "request-authentication",
						Name:      "with-selector",
						Namespace: "foo",
					},
					Spec: &securityBeta.RequestAuthentication{
						Selector: &selectorpb.WorkloadSelector{
							MatchLabels: map[string]string{
								"app":     "httpbin",
								"version": "v1",
							},
						},
					},
				},
				{
					ConfigMeta: ConfigMeta{
						Type:      "request-authentication",
						Name:      "default",
						Namespace: "istio-config",
					},
					Spec: &securityBeta.RequestAuthentication{},
				},
			},
		},
		{
			name:              "Match workload labels in bar",
			workloadNamespace: "bar",
			workloadLabels:    labels.Collection{{"app": "httpbin", "version": "v1"}},
			want: []*Config{
				{
					ConfigMeta: ConfigMeta{
						Type:      "request-authentication",
						Name:      "default",
						Namespace: "bar",
					},
					Spec: &securityBeta.RequestAuthentication{},
				},
				{
					ConfigMeta: ConfigMeta{
						Type:      "request-authentication",
						Name:      "default",
						Namespace: "istio-config",
					},
					Spec: &securityBeta.RequestAuthentication{},
				},
			},
		},
		{
			name:              "Paritial match workload labels in foo",
			workloadNamespace: "foo",
			workloadLabels:    labels.Collection{{"app": "httpbin"}},
			want: []*Config{
				{
					ConfigMeta: ConfigMeta{
						Type:      "request-authentication",
						Name:      "default",
						Namespace: "foo",
					},
					Spec: &securityBeta.RequestAuthentication{},
				},
				{
					ConfigMeta: ConfigMeta{
						Type:      "request-authentication",
						Name:      "default",
						Namespace: "istio-config",
					},
					Spec: &securityBeta.RequestAuthentication{},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := policies.GetJwtPoliciesForWorkload(tc.workloadNamespace, tc.workloadLabels); !reflect.DeepEqual(tc.want, got) {
				t.Errorf("want %+v\n, but got %+v\n", printConfigs(tc.want), printConfigs(got))
			}
		})
	}
}

func getTestAuthenticationPolicies(configs []*Config, t *testing.T) *AuthenticationPolicies {
	configStore := newFakeStore()
	for _, cfg := range configs {
		log.Infof("add config %s\n", cfg.Name)
		if _, err := configStore.Create(*cfg); err != nil {
			t.Fatalf("getTestAuthenticationPolicies %v", err)
		}
	}
	environment := &Environment{
		IstioConfigStore: MakeIstioStore(configStore),
		Watcher:          mesh.NewFixedWatcher(&meshconfig.MeshConfig{RootNamespace: rootNamespace}),
	}
	return initAuthenticationPolicies(environment)
}

func createTestConfig(name string, namespace string, selector *selectorpb.WorkloadSelector) *Config {
	return &Config{
		ConfigMeta: ConfigMeta{
			Type: schemas.RequestAuthentication.Type, Name: name, Namespace: namespace},
		Spec: &securityBeta.RequestAuthentication{
			Selector: selector,
		},
	}
}

func createTestConfigs() []*Config {
	configs := make([]*Config, 0)

	selector := &selectorpb.WorkloadSelector{
		MatchLabels: map[string]string{
			"app":     "httpbin",
			"version": "v1",
		},
	}
	configs = append(configs, createTestConfig("default", rootNamespace, nil),
		createTestConfig("should-be-ignored", rootNamespace, nil),
		createTestConfig("default", "foo", nil),
		createTestConfig("default", "bar", nil),
		createTestConfig("with-selector", "foo", selector))

	return configs
}

func printConfigs(configs []*Config) string {
	s := "[\n"
	for _, c := range configs {
		s += fmt.Sprintf("%+v\n", c)
	}
	return s + "]"
}
