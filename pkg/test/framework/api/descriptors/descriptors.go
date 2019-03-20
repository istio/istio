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
		ID:                ids.Environment,
		IsSystemComponent: true,
		Variant:           "native",
	}

	// KubernetesEnvironment component
	KubernetesEnvironment = component.Descriptor{
		ID:                ids.Environment,
		IsSystemComponent: true,
		Variant:           "kubernetes",
	}

	// Pilot component
	Pilot = component.Descriptor{
		ID:                ids.Pilot,
		IsSystemComponent: true,
		Requires: []component.Requirement{
			&ids.Mixer,
			&ids.Environment,
		},
	}

	// Mixer component
	Mixer = component.Descriptor{
		ID:                ids.Mixer,
		IsSystemComponent: true,
		Requires: []component.Requirement{
			&ids.Environment,
		},
	}

	// Galley component
	Galley = component.Descriptor{
		ID:                ids.Galley,
		IsSystemComponent: true,
		Requires: []component.Requirement{
			&ids.Environment,
		},
	}

	// Citadel component
	Citadel = component.Descriptor{
		ID:                ids.Citadel,
		IsSystemComponent: true,
		Requires: []component.Requirement{
			&ids.Environment,
		},
	}

	// Ingress component
	Ingress = component.Descriptor{
		ID:                ids.Ingress,
		IsSystemComponent: true,
		Requires: []component.Requirement{
			&ids.Environment,
		},
	}

	// Prometheus component
	Prometheus = component.Descriptor{
		ID:                ids.Prometheus,
		IsSystemComponent: true,
		Requires: []component.Requirement{
			&ids.Environment,
		},
	}

	// PolicyBackend component
	PolicyBackend = component.Descriptor{
		ID:                ids.PolicyBackend,
		IsSystemComponent: true,
		Requires: []component.Requirement{
			&ids.Mixer,
			&ids.Environment,
		},
	}

	// Echo component. Multiple of these may be created.
	Echo = component.Descriptor{
		ID:                ids.Echo,
		IsSystemComponent: false,
		Requires: []component.Requirement{
			&ids.Environment,
		},
	}

	// Apps component
	Apps = component.Descriptor{
		ID:                ids.Apps,
		IsSystemComponent: false,
		Requires: []component.Requirement{
			&ids.Pilot,
			&ids.Environment,
		},
	}

	// BookInfo component
	BookInfo = component.Descriptor{
		ID:                ids.BookInfo,
		IsSystemComponent: false,
		Requires: []component.Requirement{
			&ids.Pilot,
			&ids.Mixer,
			&ids.Environment,
		},
	}
)
