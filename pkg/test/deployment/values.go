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

package deployment

// valuesFile is the name of the values file to use for deployment.
type valuesFile string

const (

	// Istio values file
	Istio valuesFile = "values-istio.yaml"

	// IstioAuth values file
	IstioAuth valuesFile = "values-istio-auth.yaml"

	// IstioAuthMcp values file
	IstioAuthMcp valuesFile = "values-istio-auth-mcp.yaml"

	// IstioAuthMulticluster values file
	IstioAuthMulticluster valuesFile = "values-istio-auth-multicluster.yaml"

	// IstioDemo values file
	IstioDemo valuesFile = "values-istio-demo.yaml"

	// IstioDemoAuth values file
	IstioDemoAuth valuesFile = "values-istio-demo-auth.yaml"

	// IstioGateways values file
	IstioGateways valuesFile = "values-istio-gateways.yaml"

	// IstioMCP values file
	IstioMCP valuesFile = "values-istio-mcp.yaml"

	// IstioMulticluster values file
	IstioMulticluster valuesFile = "values-istio-multicluster.yaml"

	// IstioOneNamespace values file
	IstioOneNamespace valuesFile = "values-istio-one-namespace.yaml"

	// IstioOneNamespaceAuth values file
	IstioOneNamespaceAuth valuesFile = "values-istio-one-namespace-auth.yaml"
)
