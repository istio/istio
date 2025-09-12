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
		"Specifies the namespace where the istiod resides in a typical deployment. Defaults to istio-system")
	flag.StringVar(&settingsFromCommandline.TelemetryNamespace, "istio.test.kube.telemetryNamespace", settingsFromCommandline.TelemetryNamespace,
		"Specifies the namespace in which kiali, tracing providers, graphana, prometheus are deployed.")
	flag.BoolVar(&settingsFromCommandline.DeployIstio, "istio.test.kube.deploy", settingsFromCommandline.DeployIstio,
		"Deploy Istio into the target Kubernetes environment.")
	flag.StringVar(&settingsFromCommandline.BaseIOPFile, "istio.test.kube.helm.baseIopFile", settingsFromCommandline.BaseIOPFile,
		"Base IstioOperator spec file. This can be an absolute path or relative to repository root.")
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
	flag.StringVar(&settingsFromCommandline.IngressGatewayServiceName, "istio.test.kube.ingressGatewayServiceName",
		settingsFromCommandline.IngressGatewayServiceName,
		`Specifies the name of the ingressgateway service to use when running tests in a preinstalled istio installation.
		Should only be set when istio.test.kube.deploy=false`)
	flag.StringVar(&settingsFromCommandline.IngressGatewayServiceNamespace, "istio.test.kube.ingressGatewayServiceNamespace",
		settingsFromCommandline.IngressGatewayServiceNamespace,
		`Specifies the namespace of the ingressgateway service to use when running tests in a preinstalled istio installation.
		Should only be set when istio.test.kube.deploy=false`)
	flag.StringVar(&settingsFromCommandline.IngressGatewayIstioLabel, "istio.test.kube.ingressGatewayIstioLabel",
		settingsFromCommandline.IngressGatewayIstioLabel,
		`Specifies the istio label of the ingressgateway to search for when running tests in a preinstalled istio installation.
		Should only be set when istio.test.kube.deploy=false`)
	flag.StringVar(&settingsFromCommandline.EgressGatewayServiceName, "istio.test.kube.egressGatewayServiceName",
		settingsFromCommandline.EgressGatewayServiceName,
		`Specifies the name of the egressgateway service to use when running tests in a preinstalled istio installation.
		Should only be set when istio.test.kube.deploy=false`)
	flag.StringVar(&settingsFromCommandline.EgressGatewayServiceNamespace, "istio.test.kube.egressGatewayServiceNamespace",
		settingsFromCommandline.EgressGatewayServiceNamespace,
		`Specifies the namespace of the egressgateway service to use when running tests in a preinstalled istio installation.
		Should only be set when istio.test.kube.deploy=false`)
	flag.StringVar(&settingsFromCommandline.EgressGatewayIstioLabel, "istio.test.kube.egressGatewayIstioLabel",
		settingsFromCommandline.EgressGatewayIstioLabel,
		`Specifies the istio label of the egressgateway to search for when running tests in a preinstalled istio installation.
		Should only be set when istio.test.kube.deploy=false`)
	flag.StringVar(&settingsFromCommandline.SharedMeshConfigName, "istio.test.kube.sharedMeshConfigName",
		settingsFromCommandline.SharedMeshConfigName,
		`Specifies the name of the SHARED_MESH_CONFIG defined and created by the user upon installing Istio.
		Should only be set when istio.test.kube.userSharedMeshConfig=true and istio.test.kube.deploy=false.`)
	flag.StringVar(&settingsFromCommandline.ControlPlaneInstaller, "istio.test.kube.controlPlaneInstaller",
		settingsFromCommandline.ControlPlaneInstaller,
		`Specifies the external script to install external control plane at run time.
		Should only be set when istio.test.kube.deploy=false.`)
	flag.BoolVar(&settingsFromCommandline.DeployGatewayAPI, "istio.test.kube.deployGatewayAPI",
		settingsFromCommandline.DeployGatewayAPI,
		"Deploy Gateway API into the target Kubernetes environment.")
}
