//  Copyright Istio Authors
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

package istio

import (
	"flag"
)

// init registers the command-line flags that we can exposed for "go test".
func init() {
	flag.StringVar(&settingsFromCommandline.SystemNamespace, "istio.test.kube.systemNamespace", settingsFromCommandline.SystemNamespace,
		"Deprecated, specifies the namespace where the Istio components (<=1.1) reside in a typical deployment.")
	flag.StringVar(&settingsFromCommandline.TelemetryNamespace, "istio.test.kube.telemetryNamespace", settingsFromCommandline.TelemetryNamespace,
		"Specifies the namespace in which kiali, tracing providers, graphana, prometheus are deployed.")
	flag.BoolVar(&settingsFromCommandline.DeployIstio, "istio.test.kube.deploy", settingsFromCommandline.DeployIstio,
		"Deploy Istio into the target Kubernetes environment.")
	flag.StringVar(&settingsFromCommandline.PrimaryClusterIOPFile, "istio.test.kube.helm.iopFile", settingsFromCommandline.PrimaryClusterIOPFile,
		"IstioOperator spec file. This can be an absolute path or relative to repository root.")
	flag.StringVar(&helmValues, "istio.test.kube.helm.values", helmValues,
		"Manual overrides for Helm values file. Only valid when deploying Istio.")
	flag.BoolVar(&settingsFromCommandline.DeployEastWestGW, "istio.test.kube.deployEastWestGW", settingsFromCommandline.DeployEastWestGW,
		"Deploy Istio east west gateway into the target Kubernetes environment.")
	flag.BoolVar(&settingsFromCommandline.DumpKubernetesManifests, "istio.test.istio.dumpManifests", settingsFromCommandline.DumpKubernetesManifests,
		"Dump generated Istio install manifests in the artifacts directory.")
	flag.BoolVar(&settingsFromCommandline.IstiodlessRemotes, "istio.test.istio.istiodlessRemotes", settingsFromCommandline.IstiodlessRemotes,
		"Remote clusters run without istiod, using webhooks/ca from the primary cluster.")
	flag.StringVar(&operatorOptions, "istio.test.istio.operatorOptions", operatorOptions,
		`Comma separated operator configuration in addition to the default operator configuration.
		e.g. components.cni.enabled=true,components.cni.namespace=kube-system`)
	flag.BoolVar(&settingsFromCommandline.EnableCNI, "istio.test.istio.enableCNI", settingsFromCommandline.EnableCNI,
		"Deploy Istio with CNI enabled.")
}
