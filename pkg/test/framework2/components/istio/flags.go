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
		"The namespace where the Istio components reside in a typical deployment (default: 'istio-system').")
	flag.StringVar(&settingsFromCommandline.SuiteNamespace, "istio.test.kube.suiteNamespace", settingsFromCommandline.SuiteNamespace,
		"The namespace in which non-system components with suite scope are deployed. If not specified, a new namespace "+
			"will be generated with a UUID once per run.")
	flag.StringVar(&settingsFromCommandline.TestNamespace, "istio.test.kube.testNamespace", settingsFromCommandline.TestNamespace,
		"The namespace in which non-system components with test scope are deployed. If not specified, the namespaces "+
			"are created when an environment is acquired in a test, and the previous one gets deleted. This ensures that "+
			"during a single test run, there is only one test namespace in the system.")
	flag.BoolVar(&settingsFromCommandline.DeployIstio, "istio.test.kube.deploy", settingsFromCommandline.DeployIstio,
		"Deploy Istio into the target Kubernetes environment.")
	flag.DurationVar(&settingsFromCommandline.DeployTimeout, "istio.test.kube.deployTimeout", settingsFromCommandline.DeployTimeout,
		"Timeout applied to deploying Istio into the target Kubernetes environment. Only applies if DeployIstio=true.")
	flag.DurationVar(&settingsFromCommandline.UndeployTimeout, "istio.test.kube.undeployTimeout", settingsFromCommandline.UndeployTimeout,
		"Timeout applied to undeploying Istio from the target Kubernetes environment. Only applies if DeployIstio=true.")
	flag.BoolVar(&settingsFromCommandline.MinikubeIngress, "istio.test.kube.minikubeingress", settingsFromCommandline.MinikubeIngress,
		"Configure the Ingress component so that it gets the IP address from Node, when Minikube is used..")
	flag.StringVar(&settingsFromCommandline.ChartDir, "istio.test.kube.helm.chartDir", settingsFromCommandline.ChartDir,
		"Helm chart dir for Istio. Only valid when deploying Istio.")
	flag.StringVar(&settingsFromCommandline.ValuesFile, "istio.test.kube.helm.valuesFile", settingsFromCommandline.ValuesFile,
		"Helm values file. This can be an absolute path or relative to chartDir. Only valid when deploying Istio.")
	flag.StringVar(&helmValues, "istio.test.kube.helm.values", helmValues,
		"Manual overrides for Helm values file. Only valid when deploying Istio.")
}
