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
	flag.StringVar(&settingsFromCommandline.IstioNamespace, "istio.test.kube.istioNamespace", settingsFromCommandline.IstioNamespace,
		"Specifies the namespace in which istio ca and cert provisioning components are deployed.")
	flag.StringVar(&settingsFromCommandline.ConfigNamespace, "istio.test.kube.configNamespace", settingsFromCommandline.ConfigNamespace,
		"Specifies the namespace in which config, discovery and auto-injector are deployed.")
	flag.StringVar(&settingsFromCommandline.TelemetryNamespace, "istio.test.kube.telemetryNamespace", settingsFromCommandline.TelemetryNamespace,
		"Specifies the namespace in which kiali, tracing providers, graphana, prometheus are deployed.")
	flag.StringVar(&settingsFromCommandline.PolicyNamespace, "istio.test.kube.policyNamespace", settingsFromCommandline.PolicyNamespace,
		"Specifies the namespace in which istio policy checker is deployed.")
	flag.StringVar(&settingsFromCommandline.IngressNamespace, "istio.test.kube.ingressNamespace", settingsFromCommandline.IngressNamespace,
		"Specifies the namespace in which istio ingressgateway is deployed.")
	flag.StringVar(&settingsFromCommandline.EgressNamespace, "istio.test.kube.egressNamespace", settingsFromCommandline.EgressNamespace,
		"Specifies the namespace in which istio egressgateway is deployed.")
	flag.BoolVar(&settingsFromCommandline.DeployIstio, "istio.test.kube.deploy", settingsFromCommandline.DeployIstio,
		"Deploy Istio into the target Kubernetes environment.")
	flag.DurationVar(&settingsFromCommandline.DeployTimeout, "istio.test.kube.deployTimeout", 0,
		"Timeout applied to deploying Istio into the target Kubernetes environment. Only applies if DeployIstio=true.")
	flag.DurationVar(&settingsFromCommandline.UndeployTimeout, "istio.test.kube.undeployTimeout", 0,
		"Timeout applied to undeploying Istio from the target Kubernetes environment. Only applies if DeployIstio=true.")
	flag.StringVar(&settingsFromCommandline.PrimaryClusterIOPFile, "istio.test.kube.helm.iopFile", settingsFromCommandline.PrimaryClusterIOPFile,
		"IstioOperator spec file. This can be an absolute path or relative to repository root.")
	flag.StringVar(&helmValues, "istio.test.kube.helm.values", helmValues,
		"Manual overrides for Helm values file. Only valid when deploying Istio.")
	flag.BoolVar(&settingsFromCommandline.DeployEastWestGW, "istio.test.kube.deployEastWestGW", settingsFromCommandline.DeployEastWestGW,
		"Deploy Istio east west gateway into the target Kubernetes environment.")
	flag.BoolVar(&settingsFromCommandline.DeployHelm, "istio.test.helm.deploy", settingsFromCommandline.DeployHelm,
		"Deploy Istio into the target Kubernetes environment with Helm.")
	flag.BoolVar(&settingsFromCommandline.DumpKubernetesManifests, "istio.test.istio.dumpManifests", settingsFromCommandline.DumpKubernetesManifests,
		"Dump generated Istio install manifests in the artifacts directory.")
	flag.BoolVar(&settingsFromCommandline.IstiodlessRemotes, "istio.test.istio.istiodlessRemotes", settingsFromCommandline.IstiodlessRemotes,
		"Remote clusters run without istiod, using webhooks/ca from the primary cluster.")
}
