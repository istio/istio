// Copyright 2019 Istio Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"istio.io/api/annotation"

	"istio.io/pkg/log"

	"istio.io/istio/istioctl/pkg/util/handlers"
	"istio.io/istio/pkg/config/schema/collections"
)

func removeFromMeshCmd() *cobra.Command {
	removeFromMeshCmd := &cobra.Command{
		Use:     "remove-from-mesh",
		Aliases: []string{"rm"},
		Short:   "Remove workloads from Istio service mesh",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.HelpFunc()(cmd, args)
			if len(args) != 0 {
				return fmt.Errorf("unknown resource type %q", args[0])
			}
			return nil
		},
	}
	removeFromMeshCmd.AddCommand(svcUnMeshifyCmd())
	removeFromMeshCmd.AddCommand(deploymentUnMeshifyCmd())
	removeFromMeshCmd.AddCommand(externalSvcUnMeshifyCmd())
	return removeFromMeshCmd
}

func deploymentUnMeshifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deployment",
		Short: "Remove deployment from Istio service mesh",
		Long: `istioctl experimental remove-from-mesh deployment restarts pods with the Istio sidecar un-injected.
THIS COMMAND IS STILL UNDER ACTIVE DEVELOPMENT AND NOT READY FOR PRODUCTION USE.
`,
		Example: `istioctl experimental remove-from-mesh deployment productpage-v1`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("expecting deployment name")
			}
			client, err := interfaceFactory(kubeconfig)
			if err != nil {
				return err
			}
			ns := handlers.HandleNamespace(namespace, defaultNamespace)
			writer := cmd.OutOrStdout()
			dep, err := client.AppsV1().Deployments(ns).Get(args[0], metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("deployment %q does not exist", args[0])
			}
			deps := []appsv1.Deployment{}
			deps = append(deps, *dep)
			return unInjectSideCarFromDeployment(client, deps, args[0], ns, writer)
		},
	}
	return cmd
}

func svcUnMeshifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Remove Service from Istio service mesh",
		Long: `istioctl experimental remove-from-mesh service restarts pods with the Istio sidecar un-injected.
THIS COMMAND IS STILL UNDER ACTIVE DEVELOPMENT AND NOT READY FOR PRODUCTION USE.
`,
		Example: `istioctl experimental remove-from-mesh service productpage`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("expecting service name")
			}
			client, err := interfaceFactory(kubeconfig)
			if err != nil {
				return err
			}
			ns := handlers.HandleNamespace(namespace, defaultNamespace)
			writer := cmd.OutOrStdout()
			_, err = client.CoreV1().Services(ns).Get(args[0], metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("service %q does not exist, skip", args[0])
			}
			matchingDeployments, err := findDeploymentsForSvc(client, ns, args[0])
			if err != nil {
				return err
			}
			if len(matchingDeployments) == 0 {
				fmt.Fprintf(writer, "No deployments found for service %s.%s\n", args[0], ns)
				return nil
			}
			return unInjectSideCarFromDeployment(client, matchingDeployments, args[0], ns, writer)
		},
	}
	return cmd
}

func externalSvcUnMeshifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "external-service <svcname>",
		Short: "Remove Service Entry and Kubernetes Service for the external service from Istio service mesh",
		Long: `istioctl experimental remove-from-mesh external-service remove the ServiceEntry and\ 
the kubernetes Service for the specified external service(eg:services running on VM) from Istio service mesh.
The typical usage scenario is Mesh Expansion on VMs.
THIS COMMAND IS STILL UNDER ACTIVE DEVELOPMENT AND NOT READY FOR PRODUCTION USE.
`,
		Example: `istioctl experimental remove-from-mesh external-service vmhttp`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("expecting external service name")
			}
			client, err := interfaceFactory(kubeconfig)
			if err != nil {
				return err
			}
			seClient, err := crdFactory(kubeconfig)
			if err != nil {
				return err
			}
			writer := cmd.OutOrStdout()
			ns := handlers.HandleNamespace(namespace, defaultNamespace)
			_, err = client.CoreV1().Services(ns).Get(args[0], metav1.GetOptions{})
			if err == nil {
				return removeServiceOnVMFromMesh(seClient, client, ns, args[0], writer)
			}
			return fmt.Errorf("service %q does not exist, skip", args[0])
		},
	}
	return cmd
}

func unInjectSideCarFromDeployment(client kubernetes.Interface, deps []appsv1.Deployment,
	svcName, svcNamespace string, writer io.Writer) error {
	var errs error
	name := strings.Join([]string{svcName, svcNamespace}, ".")
	for _, dep := range deps {
		log.Debugf("updating deployment %s.%s with Istio sidecar un-injected",
			dep.Name, dep.Namespace)
		res := dep.DeepCopy()
		depName := strings.Join([]string{dep.Name, dep.Namespace}, ".")
		sidecarInjected := false
		podSpec := dep.Spec.Template.Spec.DeepCopy()
		for _, c := range podSpec.Containers {
			if c.Name == proxyContainerName {
				sidecarInjected = true
				break
			}
		}
		if !sidecarInjected {
			// The sidecar wasn't explicitly injected.  (Unless there is annotation it may have been auto injected)
			if val := dep.Spec.Template.Annotations[annotation.SidecarInject.Name]; strings.EqualFold(val, "false") {
				fmt.Fprintf(writer, "deployment %q has no Istio sidecar injected. Skipping.\n", depName)
				continue
			}
		}
		podSpec.InitContainers = removeInjectedContainers(podSpec.InitContainers, initContainerName)
		podSpec.InitContainers = removeInjectedContainers(podSpec.InitContainers, enableCoreDumpContainerName)
		podSpec.Containers = removeInjectedContainers(podSpec.Containers, proxyContainerName)
		podSpec.Volumes = removeInjectedVolumes(podSpec.Volumes, envoyVolumeName)
		podSpec.Volumes = removeInjectedVolumes(podSpec.Volumes, certVolumeName)
		podSpec.Volumes = removeInjectedVolumes(podSpec.Volumes, jwtTokenVolumeName)
		removeDNSConfig(podSpec.DNSConfig)
		res.Spec.Template.Spec = *podSpec
		// If we are in an auto-inject namespace, removing the sidecar isn't enough, we
		// must prevent injection
		if res.Spec.Template.Annotations == nil {
			res.Spec.Template.Annotations = make(map[string]string)
		}
		res.Spec.Template.Annotations[annotation.SidecarInject.Name] = "false"
		if _, err := client.AppsV1().Deployments(svcNamespace).Update(res); err != nil {
			errs = multierror.Append(errs, fmt.Errorf("failed to update deployment %q for service %q due to %v", depName, name, err))
			continue
		}
		d := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dep.Name,
				Namespace: dep.Namespace,
				UID:       dep.UID,
			},
		}
		if _, err := client.AppsV1().Deployments(svcNamespace).UpdateStatus(d); err != nil {
			errs = multierror.Append(errs, fmt.Errorf("failed to update deployment status %q for service %q due to %v", depName, name, err))
			continue
		}
		fmt.Fprintf(writer, "deployment %q updated successfully with Istio sidecar un-injected.\n", depName)
	}
	return errs
}

// removeServiceOnVMFromMesh removes the Service Entry and K8s service for the specified external service
func removeServiceOnVMFromMesh(dynamicClient dynamic.Interface, client kubernetes.Interface, ns string,
	svcName string, writer io.Writer) error {
	// Pre-check Kubernetes service and service entry does not exist.
	_, err := client.CoreV1().Services(ns).Get(svcName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("service %q does not exist, skip", svcName)
	}
	serviceEntryGVR := schema.GroupVersionResource{
		Group:    "networking.istio.io",
		Version:  collections.IstioNetworkingV1Alpha3Serviceentries.Resource().Version(),
		Resource: "serviceentries",
	}
	_, err = dynamicClient.Resource(serviceEntryGVR).Namespace(ns).Get(resourceName(svcName), metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("service entry %q does not exist, skip", resourceName(svcName))
	}
	err = client.CoreV1().Services(ns).Delete(svcName, &metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete Kubernetes service %q due to %v", svcName, err)
	}
	name := strings.Join([]string{svcName, ns}, ".")
	fmt.Fprintf(writer, "Kubernetes Service %q has been deleted for external service %q\n", name, svcName)
	err = dynamicClient.Resource(serviceEntryGVR).Namespace(ns).Delete(resourceName(svcName), &metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete service entry %q due to %v", resourceName(svcName), err)
	}
	fmt.Fprintf(writer, "Service Entry %q has been deleted for external service %q\n", resourceName(svcName), svcName)
	return nil
}
