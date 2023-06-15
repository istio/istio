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

package completion

import (
	"context"
	"strings"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/pkg/kube"
)

func getPodsNameInDefaultNamespace(ctx cli.Context, toComplete string) ([]string, error) {
	client, err := ctx.CLIClient()
	if err != nil {
		return nil, err
	}
	ns := ctx.NamespaceOrDefault(ctx.Namespace())
	podList, err := client.Kube().CoreV1().Pods(ns).List(context.Background(), metav1.ListOptions{})
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

func ValidPodsNameArgs(_ *cobra.Command, ctx cli.Context, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) != 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	podsName, err := getPodsNameInDefaultNamespace(ctx, toComplete)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return podsName, cobra.ShellCompDirectiveNoFileComp
}

func getServicesName(ctx cli.Context, toComplete string) ([]string, error) {
	client, err := ctx.CLIClient()
	if err != nil {
		return nil, err
	}
	ns := ctx.NamespaceOrDefault(ctx.Namespace())
	serviceList, err := client.Kube().CoreV1().Services(ns).List(context.Background(), metav1.ListOptions{})
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

func ValidServiceArgs(_ *cobra.Command, ctx cli.Context, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) != 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	servicesName, err := getServicesName(ctx, toComplete)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return servicesName, cobra.ShellCompDirectiveNoFileComp
}

func getNamespacesName(kubeClient kube.CLIClient, toComplete string) ([]string, error) {
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

func getNamespaces(ctx context.Context, client kube.CLIClient) ([]corev1.Namespace, error) {
	nslist, err := client.Kube().CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return []corev1.Namespace{}, err
	}
	return nslist.Items, nil
}

func ValidNamespaceArgs(_ *cobra.Command, ctx cli.Context, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) != 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	client, err := ctx.CLIClient()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	nsName, err := getNamespacesName(client, toComplete)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return nsName, cobra.ShellCompDirectiveNoFileComp
}
