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

package kube

import (
	"istio.io/istio/pkg/test/deployment"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/environment/api"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
)

// Environment is the implementation of a kubernetes environment. It implements environment.Environment,
// and also hosts publicly accessible methods that are specific to cluster environment.
type Environment struct {
	id resource.ID

	ctx       api.Context
	Accessors []*kube.Accessor // first entry is an alias for the embedded type
	s         *Settings
}

var _ resource.Environment = &Environment{}

// New returns a new Kubernetes environment
func New(ctx api.Context) (resource.Environment, error) {
	s, err := newSettingsFromCommandline()
	if err != nil {
		return nil, err
	}

	scopes.CI.Infof("Test Framework Kubernetes environment Settings:\n%s", s)

	workDir, err := ctx.CreateTmpDirectory("env-kube")
	if err != nil {
		return nil, err
	}

	e := &Environment{
		ctx: ctx,
		s:   s,
	}
	e.id = ctx.TrackResource(e)

	e.Accessors = make([]*kube.Accessor, len(s.KubeConfig))
	for i := range s.KubeConfig {
		if e.Accessors[i], err = kube.NewAccessor(s.KubeConfig[i], workDir); err != nil {
			return nil, err
		}
	}

	return e, nil
}

// EnvironmentName implements environment.Instance
func (e *Environment) EnvironmentName() environment.Name {
	return environment.Kube
}

// EnvironmentName implements environment.Instance
func (e *Environment) Case(name environment.Name, fn func()) {
	if name == e.EnvironmentName() {
		fn()
	}
}

// ID implements resource.Instance
func (e *Environment) ID() resource.ID {
	return e.id
}

func (e *Environment) Settings() *Settings {
	return e.s.clone()
}

// ApplyContents applies the given yaml contents to the namespace.
func (e *Environment) ApplyContents(namespace, yml string) error {
	return e.ApplyContentsIndex(0, namespace, yml)
}

// ApplyContentsDryRun applies the given yaml contents to the namespace in DryRun mode.
func (e *Environment) ApplyContentsDryRun(namespace, yml string) error {
	return e.ApplyContentsDryRunIndex(0, namespace, yml)
}

// Apply applies the config in the given filename to the namespace.
func (e *Environment) Apply(namespace, ymlFile string) error {
	return e.ApplyIndex(0, namespace, ymlFile)
}

// ApplyDryRun applies the config in the given filename to the namespace in DryRun mode.
func (e *Environment) ApplyDryRun(namespace, ymlFile string) error {
	return e.ApplyContentsDryRunIndex(0, namespace, ymlFile)
}

// Deletes the given yaml contents from the namespace.
func (e *Environment) DeleteContents(namespace, yml string) error {
	return e.DeleteContentsIndex(0, namespace, yml)
}

// Deletes the config in the given filename from the namespace.
func (e *Environment) Delete(namespace, ymlFile string) error {
	return e.DeleteIndex(0, namespace, ymlFile)
}

func (e *Environment) DeployYaml(namespace, yamlFile string) (*deployment.Instance, error) {
	return e.DeployYamlIndex(0, namespace, yamlFile)
}

// ApplyContentsIndex applies the given yaml contents to the namespace.
func (e *Environment) ApplyContentsIndex(index int, namespace, yml string) error {
	_, err := e.Accessors[index].ApplyContents(namespace, yml)
	return err
}

// ApplyContentsIndex applies the given yaml contents to the namespace.
func (e *Environment) ApplyContentsDryRunIndex(index int, namespace, yml string) error {
	_, err := e.Accessors[index].ApplyContentsDryRun(namespace, yml)
	return err
}

// Applies the config in the given filename to the namespace.
func (e *Environment) ApplyIndex(index int, namespace, ymlFile string) error {
	return e.Accessors[index].Apply(namespace, ymlFile)
}

// Deletes the given yaml contents from the namespace.
func (e *Environment) DeleteContentsIndex(index int, namespace, yml string) error {
	return e.Accessors[index].DeleteContents(namespace, yml)
}

// Deletes the config in the given filename from the namespace.
func (e *Environment) DeleteIndex(index int, namespace, ymlFile string) error {
	return e.Accessors[index].Delete(namespace, ymlFile)
}

func (e *Environment) DeployYamlIndex(index int, namespace, yamlFile string) (*deployment.Instance, error) {
	i := deployment.NewYamlDeployment(namespace, yamlFile)

	err := i.Deploy(e.Accessors[index], true)
	if err != nil {
		return nil, err
	}
	return i, nil
}
