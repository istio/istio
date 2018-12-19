//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package native

import (
	"bytes"
	"fmt"
	"testing"
	"text/template"

	authn "istio.io/api/authentication/v1alpha1"
	meshConfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/api/context"
	"istio.io/istio/pkg/test/framework/api/descriptors"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
	"istio.io/istio/pkg/test/framework/runtime/api"
	"istio.io/istio/pkg/test/framework/runtime/components/environment/native/service"
)

var (
	_ api.Environment = &Environment{}
)

// Environment for testing natively on the host machine. It implements api.Environment, and also
// hosts publicly accessible methods that are specific to local environment.
type Environment struct {
	scope lifecycle.Scope

	Namespace string

	// Mesh for configuring pilot.
	Mesh *meshConfig.MeshConfig

	// ServiceManager for all deployed services.
	ServiceManager *service.Manager
}

// NewEnvironment factory function for the component
func NewEnvironment() (api.Component, error) {
	mesh := model.DefaultMeshConfig()
	env := &Environment{
		Namespace:      service.Namespace,
		Mesh:           &mesh,
		ServiceManager: service.NewManager(),
	}
	_, err := env.ServiceManager.ConfigStore.Create(
		model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:      model.AuthenticationPolicy.Type,
				Name:      "default",
				Namespace: "istio-system",
			},
			Spec: &authn.Policy{
				// TODO: investigate why the policy can't work return err when target is specified.
				// Targets: []*authn.TargetSelector{
				// 	{
				// 		Name: "a",
				// 	},
				// },
				Peers: []*authn.PeerAuthenticationMethod{{
					Params: &authn.PeerAuthenticationMethod_Mtls{
						Mtls: &authn.MutualTls{
							Mode: authn.MutualTls_PERMISSIVE,
						},
					},
				}},
			},
		},
	)
	// for i := 0; i < 10; i++ {
	// 	specs, err := env.ServiceManager.ConfigStore.List(model.AuthenticationPolicy.Type, "default")
	// 	fmt.Println("jianfeih debug list specs, ", specs, env.ServiceManager.ConfigStore, "default", err)
	// 	time.Sleep(5 * time.Second)
	// }
	return env, err
}

// GetEnvironment from the repository.
func GetEnvironment(r component.Repository) (*Environment, error) {
	e := api.GetEnvironment(r)
	if e == nil {
		return nil, fmt.Errorf("environment has not been created")
	}

	ne, ok := e.(*Environment)
	if !ok {
		return nil, fmt.Errorf("unsupported environment: %q", e.Descriptor().Variant)
	}
	return ne, nil
}

// GetEnvironmentOrFail from the repository.
func GetEnvironmentOrFail(r component.Repository, t testing.TB) *Environment {
	e, err := GetEnvironment(r)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// Scope for this component.
func (p *Environment) Scope() lifecycle.Scope {
	return p.scope
}

// Descriptor for this component.
func (p *Environment) Descriptor() component.Descriptor {
	return descriptors.NativeEnvironment
}

// Start implements the api.Environment interface
func (p *Environment) Start(ctx context.Instance, scope lifecycle.Scope) error {
	p.scope = scope
	mesh := model.DefaultMeshConfig()
	p.Mesh = &mesh
	return nil
}

// Evaluate implements the api.Environment interface
func (p *Environment) Evaluate(tpl string) (string, error) {
	t := template.New("test template")

	t2, err := t.Parse(tpl)
	if err != nil {
		return "", err
	}

	var b bytes.Buffer
	if err = t2.Execute(&b, map[string]string{
		"SystemNamespace": p.Namespace,
		"SuiteNamespace":  p.Namespace,
		"TestNamespace":   p.Namespace,
	}); err != nil {
		return "", err
	}

	str := b.String()
	fmt.Println("NM: " + str)
	return b.String(), nil
}

// DumpState implements the api.Environment interface
func (p *Environment) DumpState(context string) {
	// Nothing to do for in-process environment.
}
