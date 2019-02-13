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

package descriptors

import (
	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/api/ids"
)

var (
	// NativeEnvironment component
	NativeEnvironment = component.Descriptor{
		Key:               component.Key{ID: ids.Environment, Variant: "native"},
		IsSystemComponent: true,
	}

	// KubernetesEnvironment component
	KubernetesEnvironment = component.Descriptor{
		Key:               component.Key{ID: ids.Environment, Variant: "kubernetes"},
		IsSystemComponent: true,
	}

	// Pilot component
	Pilot = component.Descriptor{
		Key:               ids.Pilot.GetKey(),
		IsSystemComponent: true,
		Requires: []component.Requirement{
			&ids.Mixer,
			&ids.Environment,
		},
	}

	// Mixer component
	Mixer = component.Descriptor{
		Key:               ids.Mixer.GetKey(),
		IsSystemComponent: true,
		Requires: []component.Requirement{
			&ids.Environment,
		},
	}

	// Galley component
	Galley = component.Descriptor{
		Key:               ids.Galley.GetKey(),
		IsSystemComponent: true,
		Requires: []component.Requirement{
			&ids.Environment,
		},
	}

	// Citadel component
	Citadel = component.Descriptor{
		Key:               ids.Citadel.GetKey(),
		IsSystemComponent: true,
		Requires: []component.Requirement{
			&ids.Environment,
		},
	}

	// Ingress component
	Ingress = component.Descriptor{
		Key:               ids.Ingress.GetKey(),
		IsSystemComponent: true,
		Requires: []component.Requirement{
			&ids.Environment,
		},
	}

	// Prometheus component
	Prometheus = component.Descriptor{
		Key:               ids.Prometheus.GetKey(),
		IsSystemComponent: true,
		Requires: []component.Requirement{
			&ids.Environment,
		},
	}

	// PolicyBackend component
	PolicyBackend = component.Descriptor{
		Key:               ids.PolicyBackend.GetKey(),
		IsSystemComponent: true,
		Requires: []component.Requirement{
			&ids.Mixer,
			&ids.Environment,
		},
	}

	// Echo component. Multiple of these may be created.
	Echo = component.Descriptor{
		Key:               ids.Echo.GetKey(),
		IsSystemComponent: false,
		Requires: []component.Requirement{
			&ids.Environment,
		},
	}

	// Apps component
	Apps = component.Descriptor{
		Key:               ids.Apps.GetKey(),
		IsSystemComponent: false,
		Requires: []component.Requirement{
			&ids.Pilot,
			&ids.Environment,
		},
	}

	// BookInfo component
	BookInfo = component.Descriptor{
		Key:               ids.BookInfo.GetKey(),
		IsSystemComponent: false,
		Requires: []component.Requirement{
			&ids.Pilot,
			&ids.Mixer,
			&ids.Environment,
		},
	}
)
