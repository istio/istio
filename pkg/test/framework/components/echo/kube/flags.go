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

package kube

import "flag"

var (
	serviceTemplateFile      = "service.yaml"
	deploymentTemplateFile   = "deployment.yaml"
	vmDeploymentTemplateFile = "vm_deployment.yaml"

	appContainerName = "app"
)

func init() {
	flag.StringVar(&serviceTemplateFile, "istio.test.echo.kube.template.service", serviceTemplateFile,
		"Specifies the default template file to be used when generating the Kubernetes Service for an instance of the echo application. "+
			"Can be either an absolute path or relative to the templates directory under the echo test component. A default will be selected if not specified.")
	flag.StringVar(&deploymentTemplateFile, "istio.test.echo.kube.template.deployment", deploymentTemplateFile,
		"Specifies the default template file to be used when generating the Kubernetes Deployment to create an instance of echo application. "+
			"Can be either an absolute path or relative to the templates directory under the echo test component. A default will be selected if not specified.")
	flag.StringVar(&vmDeploymentTemplateFile, "istio.test.echo.kube.template.deployment.vm", vmDeploymentTemplateFile,
		"Specifies the default template file to be used when generating the Kubernetes Deployment to simulate an instance of echo application in a VM. "+
			"Can be either an absolute path or relative to the templates directory under the echo test component. A default will be selected if not specified.")
	flag.StringVar(&appContainerName, "istio.test.echo.kube.container", appContainerName,
		"Specifies the default container name to be used for the echo application. A default will be selected if not specified.")
}
