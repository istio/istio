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

package charts

import (
	"istio.io/istio/pkg/test/impl/helm"
)

// Istio is the chart for a default Istio configuration without mTLS.
var Istio = helm.LoadChart("istio").WithValuesFromFile("values-istio")

// IstioAll is the chart with all components/features enabled (including mTLS)
var IstioAll = helm.LoadChart("istio").WithValuesFromFile("values-istio-all")

// IstioAuth is the chart for a default Istio configuration with mTLS.
var IstioAuth = helm.LoadChart("istio").WithValuesFromFile("values-istio-auth")

// IstioOneNamespace is the chart configured for a single namespace, and without mTLS.
var IstioOneNamespace = helm.LoadChart("istio").WithValuesFromFile("values-istio-one-namespace")

// IstioOneNamespaceAuth is the chart configured for a single namespace with mTLS.
var IstioOneNamespaceAuth = helm.LoadChart("istio").WithValuesFromFile("values-istio-auth")
