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
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/environment"
	"istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
)

// Environment is the implementation of a kubernetes environment. It implements environment.Environment,
// and also hosts publicly accessible methods that are specific to cluster environment.
type Environment struct {
	id resource.ID

	ctx          resource.Context
	KubeClusters []Cluster
	s            *Settings
}

var _ resource.Environment = &Environment{}

// New returns a new Kubernetes environment
func New(ctx resource.Context) (resource.Environment, error) {
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

	e.KubeClusters = make([]Cluster, 0, len(s.KubeConfig))
	for i := range s.KubeConfig {
		a, err := kube.NewAccessor(s.KubeConfig[i], workDir)
		if err != nil {
			return nil, err
		}
		clusterIndex := resource.ClusterIndex(i)
		e.KubeClusters = append(e.KubeClusters, Cluster{
			filename: s.KubeConfig[i],
			index:    clusterIndex,
			Accessor: a,
		})
	}

	return e, nil
}

func (e *Environment) EnvironmentName() environment.Name {
	return environment.Kube
}

func (e *Environment) IsMulticluster() bool {
	return len(e.KubeClusters) > 1
}

func (e *Environment) Clusters() []resource.Cluster {
	out := make([]resource.Cluster, 0, len(e.KubeClusters))
	for _, c := range e.KubeClusters {
		out = append(out, c)
	}
	return out
}

func (e *Environment) ControlPlaneClusters() []Cluster {
	out := make([]Cluster, 0, len(e.KubeClusters))
	for _, c := range e.KubeClusters {
		if e.IsControlPlaneCluster(c) {
			out = append(out, c)
		}
	}
	return out
}

// IsControlPlaneCluster returns true if the cluster uses its own control plane in the ControlPlaneTopology.
// We return if there is no mapping for the cluster, similar to the behavior of the istio.test.kube.controlPlaneTopology.
func (e *Environment) IsControlPlaneCluster(cluster resource.Cluster) bool {
	if controlPlaneIndex, ok := e.Settings().ControlPlaneTopology[cluster.Index()]; ok {
		return controlPlaneIndex == cluster.Index()
	}
	return true
}

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
