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

package istio

import (
	"flag"
)

// init registers the command-line flags that we can exposed for "go test".
func init() {
	flag.StringVar(&settingsFromCommandline.SystemNamespace, "istio.test.kube.systemNamespace", settingsFromCommandline.SystemNamespace,
		"The namespace where the Istio components reside in a typical deployment.")
	flag.BoolVar(&settingsFromCommandline.DeployIstio, "istio.test.kube.deploy", settingsFromCommandline.DeployIstio,
		"Deploy Istio into the target Kubernetes environment.")
	flag.DurationVar(&settingsFromCommandline.DeployTimeout, "istio.test.kube.deployTimeout", 0,
		"Timeout applied to deploying Istio into the target Kubernetes environment. Only applies if DeployIstio=true.")
	flag.DurationVar(&settingsFromCommandline.UndeployTimeout, "istio.test.kube.undeployTimeout", 0,
		"Timeout applied to undeploying Istio from the target Kubernetes environment. Only applies if DeployIstio=true.")
	flag.StringVar(&settingsFromCommandline.ChartDir, "istio.test.kube.helm.chartDir", settingsFromCommandline.ChartDir,
		"Helm chart dir for Istio. Only valid when deploying Istio.")
	flag.StringVar(&settingsFromCommandline.ValuesFile, "istio.test.kube.helm.valuesFile", settingsFromCommandline.ValuesFile,
		"Helm values file. This can be an absolute path or relative to chartDir. Only valid when deploying Istio.")
	flag.StringVar(&helmValues, "istio.test.kube.helm.values", helmValues,
		"Manual overrides for Helm values file. Only valid when deploying Istio.")
}
