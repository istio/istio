// Copyright Istio Authors.
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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
	appsv1 "k8s.io/api/apps/v1"
	apierror "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"istio.io/api/annotation"
	"istio.io/istio/istioctl/pkg/util/handlers"
	"istio.io/istio/pkg/config/analysis/analyzers/util"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/pkg/log"
)

func removeFromMeshCmd() *cobra.Command {
	removeFromMeshCmd := &cobra.Command{
		Use:     "remove-from-mesh",
		Aliases: []string{"rm"},
		Short:   "Remove workloads from Istio service mesh",
		Long: `'istioctl experimental remove-from-mesh' restarts pods without an Istio sidecar or removes external service access configuration.
Use 'remove-from-mesh' to quickly test uninjected behavior as part of compatibility troubleshooting.
The 'add-to-mesh' command can be used to add or restore the sidecar.`,
		Example: `  # Restart all productpage pods without an Istio sidecar
  istioctl experimental remove-from-mesh service productpage

  # Restart all details-v1 pods without an Istio sidecar
  istioctl x rm service details-v1

  # Restart all ratings-v1 pods without an Istio sidecar
  istioctl x rm deploy ratings-v1`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("unknown resource type %q", args[0])
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.HelpFunc()(cmd, args)
			return nil
		},
	}
	removeFromMeshCmd.AddCommand(svcUnMeshifyCmd())
	removeFromMeshCmd.AddCommand(deploymentUnMeshifyCmd())
	removeFromMeshCmd.AddCommand(externalSvcUnMeshifyCmd())
	removeFromMeshCmd.Long += "\n\n" + ExperimentalMsg
	return removeFromMeshCmd
}

func deploymentUnMeshifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "deployment <deployment>",
		Aliases: []string{"deploy", "dep"},
		Short:   "Remove deployment from Istio service mesh",
		Long: `'istioctl experimental remove-from-mesh deployment' restarts pods with the Istio sidecar un-injected.
'remove-from-mesh' is a compatibility troubleshooting tool.`,
		Example: `  # Restart all productpage-v1 pods without an Istio sidecar
  istioctl experimental remove-from-mesh deployment productpage-v1

  # Restart all details-v1 pods without an Istio sidecar
  istioctl x remove-from-mesh deploy details-v1

  # Restart all ratings-v1 pods without an Istio sidecar
  istioctl x rm dep ratings-v1`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("expecting deployment name")
			}
			ns := handlers.HandleNamespace(namespace, defaultNamespace)
			if util.IsSystemNamespace(resource.Namespace(ns)) || ns == istioNamespace {
				return fmt.Errorf("namespace %s is a system namespace and has no Istio sidecar injected", ns)
			}
			client, err := interfaceFactory(kubeconfig)
			if err != nil {
				return err
			}
			dep, err := client.AppsV1().Deployments(ns).Get(context.TODO(), args[0], metav1.GetOptions{})
			if err != nil {
				if apierror.IsNotFound(err) {
					return fmt.Errorf("deployment %q does not exist", args[0])
				}
				return err
			}
			writer := cmd.OutOrStdout()
			deps := []appsv1.Deployment{}
			deps = append(deps, *dep)
			return unInjectSideCarFromDeployment(client, deps, args[0], ns, writer)
		},
	}
	cmd.Long += "\n\n" + ExperimentalMsg
	return cmd
}

func svcUnMeshifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "service <service>",
		Aliases: []string{"svc"},
		Short:   "Remove Service from Istio service mesh",
		Long: `'istioctl experimental remove-from-mesh service' restarts pods with the Istio sidecar un-injected.
'remove-from-mesh' is a compatibility troubleshooting tool.`,
		Example: `  # Restart all productpage pods without an Istio sidecar
  istioctl experimental remove-from-mesh service productpage

  # Restart all details-v1 pods without an Istio sidecar
  istioctl x remove-from-mesh svc details-v1

  # Restart all ratings-v1 pods without an Istio sidecar
  istioctl x rm svc ratings-v1`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("expecting service name")
			}
			ns := handlers.HandleNamespace(namespace, defaultNamespace)
			if util.IsSystemNamespace(resource.Namespace(ns)) || ns == istioNamespace {
				return fmt.Errorf("namespace %s is a system namespace and has no Istio sidecar injected", ns)
			}
			client, err := interfaceFactory(kubeconfig)
			if err != nil {
				return err
			}
			_, err = client.CoreV1().Services(ns).Get(context.TODO(), args[0], metav1.GetOptions{})
			if err != nil {
				if apierror.IsNotFound(err) {
					return fmt.Errorf("service %q does not exist, skip", args[0])
				}
				return err
			}
			matchingDeployments, err := findDeploymentsForSvc(client, ns, args[0])
			if err != nil {
				return err
			}
			writer := cmd.OutOrStdout()
			if len(matchingDeployments) == 0 {
				fmt.Fprintf(writer, "No deployments found for service %s.%s\n", args[0], ns)
				return nil
			}
			return unInjectSideCarFromDeployment(client, matchingDeployments, args[0], ns, writer)
		},
	}
	cmd.Long += "\n\n" + ExperimentalMsg
	return cmd
}

func externalSvcUnMeshifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "external-service <svcname>",
		Aliases: []string{"es"},
		Short:   "Remove Service Entry and Kubernetes Service for the external service from Istio service mesh",
		Long: `'istioctl experimental remove-from-mesh external-service' removes the ServiceEntry and
the Kubernetes Service for the specified external service (e.g. services running on a VM) from Istio service mesh.
The typical usage scenario is Mesh Expansion on VMs.`,
		Example: `  # Remove "vmhttp" service entry rules
  istioctl experimental remove-from-mesh external-service vmhttp

  # Remove "vmhttp" service entry rules
  istioctl x remove-from-mesh es vmhttp

  # Remove "vmhttp" service entry rules
  istioctl x rm es vmhttp`,
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
			_, err = client.CoreV1().Services(ns).Get(context.TODO(), args[0], metav1.GetOptions{})
			if err == nil {
				return removeServiceOnVMFromMesh(seClient, client, ns, args[0], writer)
			}
			if apierror.IsNotFound(err) {
				return fmt.Errorf("service %q does not exist, skip", args[0])
			}
			return err
		},
	}
	cmd.Long += "\n\n" + ExperimentalMsg
	return cmd
}

func unInjectSideCarFromDeployment(client kubernetes.Interface, deps []appsv1.Deployment,
	svcName, svcNamespace string, writer io.Writer,
) error {
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

		var appProbe inject.KubeAppProbers
		appProbeStr := retrieveAppProbe(podSpec.Containers)
		if appProbeStr != "" {
			err := json.Unmarshal([]byte(appProbeStr), &appProbe)
			errs = multierror.Append(errs, err)
		}
		if appProbe != nil {
			podSpec.Containers = restoreAppProbes(podSpec.Containers, appProbe)
		}

		podSpec.InitContainers = removeInjectedContainers(podSpec.InitContainers, initContainerName)
		podSpec.InitContainers = removeInjectedContainers(podSpec.InitContainers, initValidationContainerName)
		podSpec.InitContainers = removeInjectedContainers(podSpec.InitContainers, enableCoreDumpContainerName)
		podSpec.Containers = removeInjectedContainers(podSpec.Containers, proxyContainerName)
		podSpec.Volumes = removeInjectedVolumes(podSpec.Volumes, certVolumeName)
		podSpec.Volumes = removeInjectedVolumes(podSpec.Volumes, dataVolumeName)
		podSpec.Volumes = removeInjectedVolumes(podSpec.Volumes, envoyVolumeName)
		podSpec.Volumes = removeInjectedVolumes(podSpec.Volumes, jwtTokenVolumeName)
		podSpec.Volumes = removeInjectedVolumes(podSpec.Volumes, pilotCertVolumeName)
		podSpec.Volumes = removeInjectedVolumes(podSpec.Volumes, podInfoVolumeName)
		removeDNSConfig(podSpec.DNSConfig)
		res.Spec.Template.Spec = *podSpec
		// If we are in an auto-inject namespace, removing the sidecar isn't enough, we
		// must prevent injection
		if res.Spec.Template.Annotations == nil {
			res.Spec.Template.Annotations = make(map[string]string)
		}
		res.Spec.Template.Annotations[annotation.SidecarInject.Name] = "false"
		if _, err := client.AppsV1().Deployments(svcNamespace).Update(context.TODO(), res, metav1.UpdateOptions{}); err != nil {
			errs = multierror.Append(errs, fmt.Errorf("failed to update deployment %q for service %q due to %v", depName, name, err))
			continue
		}
		d := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:            dep.Name,
				Namespace:       dep.Namespace,
				UID:             dep.UID,
				OwnerReferences: dep.OwnerReferences,
			},
		}
		if _, err := client.AppsV1().Deployments(svcNamespace).UpdateStatus(context.TODO(), d, metav1.UpdateOptions{}); err != nil {
			errs = multierror.Append(errs, fmt.Errorf("failed to update deployment status %q for service %q due to %v", depName, name, err))
			continue
		}
		fmt.Fprintf(writer, "deployment %q updated successfully with Istio sidecar un-injected.\n", depName)
	}
	return errs
}

// removeServiceOnVMFromMesh removes the Service Entry and K8s service for the specified external service
func removeServiceOnVMFromMesh(dynamicClient dynamic.Interface, client kubernetes.Interface, ns string,
	svcName string, writer io.Writer,
) error {
	// Pre-check Kubernetes service and service entry does not exist.
	_, err := client.CoreV1().Services(ns).Get(context.TODO(), svcName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("service %q does not exist, skip", svcName)
	}
	serviceEntryGVR := schema.GroupVersionResource{
		Group:    "networking.istio.io",
		Version:  collections.IstioNetworkingV1Alpha3Serviceentries.Resource().Version(),
		Resource: "serviceentries",
	}
	_, err = dynamicClient.Resource(serviceEntryGVR).Namespace(ns).Get(context.TODO(), resourceName(svcName), metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("service entry %q does not exist, skip", resourceName(svcName))
	}
	err = client.CoreV1().Services(ns).Delete(context.TODO(), svcName, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete Kubernetes service %q due to %v", svcName, err)
	}
	name := strings.Join([]string{svcName, ns}, ".")
	fmt.Fprintf(writer, "Kubernetes Service %q has been deleted for external service %q\n", name, svcName)
	err = dynamicClient.Resource(serviceEntryGVR).Namespace(ns).Delete(context.TODO(), resourceName(svcName), metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete service entry %q due to %v", resourceName(svcName), err)
	}
	fmt.Fprintf(writer, "Service Entry %q has been deleted for external service %q\n", resourceName(svcName), svcName)
	return nil
}
