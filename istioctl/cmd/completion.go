// Copyright Istio Authors
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
	"strings"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/istioctl/pkg/util/handlers"
)

func getPodsNameInDefaultNamespace(toComplete string) ([]string, error) {
	kubeClient, err := kubeClient(kubeconfig, configContext)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	ns := handlers.HandleNamespace(namespace, defaultNamespace)
	podList, err := kubeClient.Kube().CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	var podsName []string
	for _, pod := range podList.Items {
		if toComplete == "" || strings.HasPrefix(pod.Name, toComplete) {
			podsName = append(podsName, pod.Name)
		}
	}

	return podsName, nil
}

func validPodsNameArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) != 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	podsName, err := getPodsNameInDefaultNamespace(toComplete)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return podsName, cobra.ShellCompDirectiveNoFileComp
}

func getServicesName(toComplete string) ([]string, error) {
	kubeClient, err := kubeClient(kubeconfig, configContext)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	ns := handlers.HandleNamespace(namespace, defaultNamespace)
	serviceList, err := kubeClient.Kube().CoreV1().Services(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	var serviceNameList []string
	for _, service := range serviceList.Items {
		if toComplete == "" || strings.HasPrefix(service.Name, toComplete) {
			serviceNameList = append(serviceNameList, service.Name)
		}
	}

	return serviceNameList, nil
}

func validServiceArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) != 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	servicesName, err := getServicesName(toComplete)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return servicesName, cobra.ShellCompDirectiveNoFileComp
}

func getNamespacesName(toComplete string) ([]string, error) {
	kubeClient, err := kubeClient(kubeconfig, configContext)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	nsList, err := getNamespaces(ctx, kubeClient)
	if err != nil {
		return nil, err
	}

	var nsNameList []string
	for _, ns := range nsList {
		if toComplete == "" || strings.HasPrefix(ns.Name, toComplete) {
			nsNameList = append(nsNameList, ns.Name)
		}
	}

	return nsNameList, nil
}

func validNamespaceArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) != 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	nsName, err := getNamespacesName(toComplete)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return nsName, cobra.ShellCompDirectiveNoFileComp
}
