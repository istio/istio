/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package internal

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission/types"
)

// Manager is a dummy manager that does nothing.
type Manager struct{}

var _ manager.Manager = &Manager{}

// Add will set reqeusted dependencies on the component, and cause the component to be
// started when Start is called.  Add will inject any dependencies for which the argument
// implements the inject interface - e.g. inject.Client
func (m *Manager) Add(manager.Runnable) error { return nil }

// SetFields will set any dependencies on an object for which the object has implemented the inject
// interface - e.g. inject.Client.
func (m *Manager) SetFields(interface{}) error { return nil }

// Start starts all registered Controllers and blocks until the Stop channel is closed.
// Returns an error if there is an error starting any controller.
func (m *Manager) Start(<-chan struct{}) error { return nil }

// GetConfig returns an initialized Config
func (m *Manager) GetConfig() *rest.Config { return nil }

// GetScheme returns and initialized Scheme
func (m *Manager) GetScheme() *runtime.Scheme { return nil }

// GetAdmissionDecoder returns the runtime.Decoder based on the scheme.
func (m *Manager) GetAdmissionDecoder() types.Decoder { return nil }

// GetClient returns a client configured with the Config
func (m *Manager) GetClient() client.Client { return nil }

// GetFieldIndexer returns a client.FieldIndexer configured with the client
func (m *Manager) GetFieldIndexer() client.FieldIndexer { return nil }

// GetCache returns a cache.Cache
func (m *Manager) GetCache() cache.Cache { return nil }

// GetRecorder returns a new EventRecorder for the provided name
func (m *Manager) GetRecorder(name string) record.EventRecorder { return nil }

// GetRESTMapper returns a RESTMapper
func (m *Manager) GetRESTMapper() meta.RESTMapper { return nil }
