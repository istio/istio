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

package dependency

const (
	// Apps is a dependency on fake networked apps. This can be used to mimic traffic thru a mesh.
	Apps = Instance("apps")

	// Local environment is required for running the test.
	Local = Instance("local")

	// Kubernetes environment is required for running the test.
	Kubernetes = Instance("kubernetes")

	// GKE dependency
	GKE = Instance("gke")

	// FortioApps is a dependency on fake networked apps. This can be used to mimic traffic thru a mesh.
	FortioApps = Instance("fortioApps")

	// Mixer indicates a dependency on Mixer.
	Mixer = Instance("mixer")

	// MTLS indicates a dependency on MTLS being enabled.
	MTLS = Instance("mtls")

	// Pilot indicates a dependency on Pilot.
	Pilot = Instance("pilot")

	// PolicyBackend indicates a dependency on the mock policy backend.
	PolicyBackend = Instance("policyBackend")

	// APIServer indicates that there is a dependency on having an API Server available.
	// In cluster mode, this is satisfied via existing API Server. In local model, this is satisfied
	// via a minikube installation.
	APIServer = Instance("apiserver")

	// Citadel indicates a dependency on Citadel.
	Citadel = Instance("citadel")

	// BookInfo indicates that BookInfo sample should be deployed.
	BookInfo = Instance("bookInfo")

	// Prometheus indicates that a handle to a deployed Prometheus instance in Kubernetes should be returned.
	Prometheus = Instance("prometheus")

	// Ingress indicates a dependency on the ingress gateway.
	Ingress = Instance("ingress")
)
