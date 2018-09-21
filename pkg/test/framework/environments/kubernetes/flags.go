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

package kubernetes

import (
	"flag"
)

// init registers the command-line flags that we can exposed for "go test".
func init() {
	flag.StringVar(&globalSettings.KubeConfig, "istio.test.kube.config", globalSettings.KubeConfig,
		"The path to the kube config file for cluster environments")
	flag.StringVar(&globalSettings.Hub, "istio.test.kube.hub", globalSettings.Hub, "The hub for docker images")
	flag.StringVar(&globalSettings.Tag, "istio.test.kube.tag", globalSettings.Tag, "The tag for docker images.")
	flag.StringVar(&globalSettings.IstioSystemNamespace, "istio.test.kube.systemNamespace", globalSettings.IstioSystemNamespace,
		"The namespace where the Istio components reside in a typical deployment (typically 'istio-system'). "+
			"If not specified, a new namespace will be generated with a UUID.")
	flag.StringVar(&globalSettings.DependencyNamespace, "istio.test.kube.dependencyNamespace", globalSettings.DependencyNamespace,
		"The namespace in which dependency components are deployed. If not specified, a new namespace will be generated "+
			"with a UUID once per run. Test framework dependencies can deploy components here when they get initialized. "+
			"They will get deployed only once.")
	flag.StringVar(&globalSettings.TestNamespace, "istio.test.kube.testNamespace", globalSettings.TestNamespace,
		"The namespace for each individual test. If not specified, the namespaces are created when an environment "+
			"is acquired in a test, and the previous one gets deleted. This ensures that during a single test run, there is only "+
			"one test namespace in the system.")
	flag.BoolVar(&globalSettings.DeployIstio, "istio.test.kube.deploy", globalSettings.DeployIstio,
		"Deploy Istio into the target Kubernetes environment.")
}
