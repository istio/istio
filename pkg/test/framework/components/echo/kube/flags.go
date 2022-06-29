package kube

import "flag"

var (
	serviceTemplateFile      = "service.yaml"
	deploymentTemplateFile   = "deployment.yaml"
	vmDeploymentTemplateFile = "vm_deployment.yaml"
)

func init() {
	flag.StringVar(&serviceTemplateFile, "istio.test.echo.kube.template.service", serviceTemplateFile,
		"Specifies the default template file to be used when generating the Kubernetes Service for an instance of the echo application. "+
			"Can be either an absolute path or relative to the templates directory under the echo test component.")
	flag.StringVar(&deploymentTemplateFile, "istio.test.echo.kube.template.deployment", deploymentTemplateFile,
		"Specifies the default template file to be used when generating the Kubernetes Deployment to create an instance of echo application. "+
			"Can be either an absolute path or relative to the templates directory under the echo test component.")
	flag.StringVar(&vmDeploymentTemplateFile, "istio.test.echo.kube.template.deployment.vm", vmDeploymentTemplateFile,
		"Specifies the default template file to be used when generating the Kubernetes Deployment to simulate an instance of echo application in a VM. "+
			"Can be either an absolute path or relative to the templates directory under the echo test component.")
}
