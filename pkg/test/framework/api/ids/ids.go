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

package ids

import (
	"istio.io/istio/pkg/test/framework/api/component"
)

var (
	// Environment component
	Environment = component.ID("environment")

	// Apps component
	Apps = component.ID("apps")

	// BookInfo component
	BookInfo = component.ID("bookinfo")

	// Citadel component
	Citadel = component.ID("citadel")

	// Ingress component
	Ingress = component.ID("ingress")

	// Mixer component
	Mixer = component.ID("mixer")

	// Galley component
	Galley = component.ID("galley")

	// Pilot component
	Pilot = component.ID("pilot")

	// PolicyBackend component
	PolicyBackend = component.ID("policybackend")

	// Prometheus component
	Prometheus = component.ID("prometheus")
)
