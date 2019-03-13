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
	"fmt"
	"testing"

	"github.com/google/uuid"

	"istio.io/istio/pkg/test/framework2/components/environment/native/service"
	"istio.io/istio/pkg/test/framework2/core"

	meshConfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
)

// Environment for testing natively on the host machine. It implements api.Environment, and also
// hosts publicly accessible methods that are specific to local environment.
type Environment struct {
	id  core.ResourceID
	ctx core.Context

	// Mesh for configuring pilot.
	Mesh *meshConfig.MeshConfig

	// ServiceManager for all deployed services.
	ServiceManager *service.Manager
}

var _ core.Environment = &Environment{}

// New returns a new native environment.
func New(ctx core.Context) (core.Environment, error) {
	mesh := model.DefaultMeshConfig()
	e := &Environment{
		ctx:            ctx,
		Mesh:           &mesh,
		ServiceManager: service.NewManager(),
	}
	ctx.TrackResource(e)

	return e, nil
}

// Type implements environment.Instance
func (e *Environment) EnvironmentName() core.EnvironmentName {
	return core.Native
}

// ID implements resource.Instance
func (e *Environment) ID() core.ResourceID {
	return e.id
}

// AllocateNamespace allocates a new testing namespace.
func (e *Environment) AllocateNamespace(prefix string, inject bool) (core.Namespace, error) {
	ns := fmt.Sprintf("%s-%s", prefix, uuid.New().String())

	n := &nativeNamespace{name: ns}
	id := e.ctx.TrackResource(n)
	n.id = id

	return n, nil
}

// AllocateNamespace allocates a new testing namespace.
func (e *Environment) AllocateNamespaceOrFail(t *testing.T, prefix string, inject bool) core.Namespace {
	t.Helper()

	ns, err := e.AllocateNamespace(prefix, inject)
	if err != nil {
		t.Fatalf("Environment.AllocateNamespaceOrFail: %v", err)
	}

	return ns
}
