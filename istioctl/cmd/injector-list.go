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
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	admit_v1 "k8s.io/api/admissionregistration/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	api_pkg_labels "k8s.io/apimachinery/pkg/labels"

	"istio.io/api/annotation"
	"istio.io/api/label"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/injection"
	analyzer_util "istio.io/istio/galley/pkg/config/analysis/analyzers/util"
	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/kube"
)

type revisionCount struct {
	// pods in a revision
	pods int
	// pods that are disabled from injection
	disabled int
	// pods that are enabled for injection, but whose revision doesn't match their namespace's revision
	needsRestart int
}

func injectorCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "injector",
		Short:   "List sidecar injector and sidecar versions",
		Long:    `List sidecar injector and sidecar versions`,
		Example: `  istioctl experimental injector list`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.HelpFunc()(cmd, args)
			if len(args) != 0 {
				return fmt.Errorf("unknown dashboard %q", args[0])
			}

			return nil
		},
	}

	cmd.AddCommand(injectorListCommand())
	return cmd
}

func injectorListCommand() *cobra.Command {
	var opts clioptions.ControlPlaneOptions
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List sidecar injector and sidecar versions",
		Long:    `List sidecar injector and sidecar versions`,
		Example: `  istioctl experimental injector list`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := kubeClientWithRevision(kubeconfig, configContext, opts.Revision)
			if err != nil {
				return fmt.Errorf("failed to create k8s client: %v", err)
			}
			nslist, err := getNamespaces(client)
			if err != nil {
				return err
			}
			hooks, err := getWebhooks(client)
			if err != nil {
				return err
			}
			pods, err := getPods(client)
			_ = pods
			if err != nil {
				return err
			}
			err = printNS(cmd.OutOrStdout(), nslist, hooks, pods)
			if err != nil {
				return err
			}
			fmt.Println()
			injectedImages, err := getInjectedImages(client)
			if err != nil {
				return err
			}
			return printHooks(cmd.OutOrStdout(), nslist, hooks, injectedImages)
		},
	}

	return cmd
}

func getNamespaces(client kube.ExtendedClient) ([]v1.Namespace, error) {
	nslist, err := client.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return []v1.Namespace{}, err
	}
	return nslist.Items, nil
}

func printNS(writer io.Writer, namespaces []v1.Namespace, hooks []admit_v1.MutatingWebhookConfiguration,
	allPods map[resource.Namespace][]v1.Pod) error {
	if len(namespaces) == 0 {
		_, err := fmt.Fprintf(writer, "No namespaces present.\n")
		return err
	}

	// TODO sort

	w := new(tabwriter.Writer).Init(writer, 0, 8, 1, ' ', 0)
	fmt.Fprintln(w, "NAMESPACE\tISTIO-REVISION\tPOD-REVISIONS")
	for _, namespace := range namespaces {
		if hideFromOutput(resource.Namespace(namespace.Name)) {
			continue
		}

		revision := getInjectedRevision(&namespace, hooks)
		podCount := podCountByRevision(allPods[resource.Namespace(namespace.Name)], revision)
		for injectedRevision, count := range podCount {
			fmt.Fprintf(w, "%s\t%s\t%s\n", namespace.Name, revision,
				fmt.Sprintf("%s: %s", injectedRevision, renderCounts(count)))
		}
	}
	return w.Flush()
}

func getWebhooks(client kube.ExtendedClient) ([]admit_v1.MutatingWebhookConfiguration, error) {
	hooks, err := client.AdmissionregistrationV1().MutatingWebhookConfigurations().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return []admit_v1.MutatingWebhookConfiguration{}, err
	}
	return hooks.Items, nil
}

func printHooks(writer io.Writer, namespaces []v1.Namespace, hooks []admit_v1.MutatingWebhookConfiguration, injectedImages map[string]string) error {
	if len(hooks) == 0 {
		_, err := fmt.Fprintf(writer, "No Istio injection hooks present.\n")
		return err
	}

	// TODO sort

	w := new(tabwriter.Writer).Init(writer, 0, 8, 1, ' ', 0)
	fmt.Fprintln(w, "NAMESPACES\tINJECTOR-HOOK\tISTIO-REVISION\tSIDECAR-IMAGE")
	for _, hook := range hooks {
		revision := hook.ObjectMeta.GetLabels()[label.IstioRev]
		namespaces := getMatchingNamespaces(&hook, namespaces)
		if len(namespaces) == 0 {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", "DOES NOT AUTOINJECT", hook.Name, revision, injectedImages[revision])
			continue
		}
		for _, namespace := range namespaces {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", namespace.Name, hook.Name, revision, injectedImages[revision])
		}
	}
	return w.Flush()
}

func getInjector(namespace *v1.Namespace, hooks []admit_v1.MutatingWebhookConfiguration) *admit_v1.MutatingWebhookConfiguration {
	// find matching hook
	for _, hook := range hooks {
		for _, webhook := range hook.Webhooks {
			nsSelector, err := metav1.LabelSelectorAsSelector(webhook.NamespaceSelector)
			if err != nil {
				continue
			}
			// @@@ nsSelector := api_pkg_labels.SelectorFromSet(webhook.NamespaceSelector)
			if nsSelector.Matches(api_pkg_labels.Set(namespace.ObjectMeta.Labels)) {
				// @@@ TODO verify the meaning of Webhooks is "OR" not "AND"
				return &hook
			}
		}
	}
	return nil
}

func getInjectedRevision(namespace *v1.Namespace, hooks []admit_v1.MutatingWebhookConfiguration) string {
	injector := getInjector(namespace, hooks)
	if injector != nil {
		return injector.ObjectMeta.GetLabels()[label.IstioRev]
	}
	newRev := namespace.ObjectMeta.GetLabels()[label.IstioRev]
	oldLabel, ok := namespace.ObjectMeta.GetLabels()[analyzer_util.InjectionLabelName]
	// If there is no istio-injection=disabled and no istio.io/rev, the namespace isn't injected
	if newRev == "" && (ok && oldLabel == "disabled" || !ok) {
		return ""
	}
	if newRev != "" {
		return fmt.Sprintf("MISSING/%s", newRev)
	}
	return fmt.Sprintf("MISSING/%s", analyzer_util.InjectionLabelName)
}

func getMatchingNamespaces(hook *admit_v1.MutatingWebhookConfiguration, namespaces []v1.Namespace) []v1.Namespace {
	retval := make([]v1.Namespace, 0)
	for _, webhook := range hook.Webhooks {
		nsSelector, err := metav1.LabelSelectorAsSelector(webhook.NamespaceSelector)
		if err != nil {
			return retval
		}

		for _, namespace := range namespaces {
			if nsSelector.Matches(api_pkg_labels.Set(namespace.Labels)) {
				// @@@ TODO Verify "OR" not "AND" on Webhooks
				retval = append(retval, namespace)
			}
		}
	}
	return retval
}

func getPods(client kube.ExtendedClient) (map[resource.Namespace][]v1.Pod, error) {
	retval := map[resource.Namespace][]v1.Pod{}
	// All pods in all namespaces
	pods, err := client.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return retval, err
	}
	for _, pod := range pods.Items {
		podList, ok := retval[resource.Namespace(pod.GetNamespace())]
		if !ok {
			retval[resource.Namespace(pod.GetNamespace())] = []v1.Pod{pod}
		} else {
			retval[resource.Namespace(pod.GetNamespace())] = append(podList, pod)
		}
	}
	return retval, nil
}

// getInjectedImages() returns a map of revision->dockerimage
func getInjectedImages(client kube.ExtendedClient) (map[string]string, error) {
	retval := map[string]string{}

	// All configs in all namespaces that are Istio revisioned
	configMaps, err := client.CoreV1().ConfigMaps("").List(context.TODO(), metav1.ListOptions{LabelSelector: label.IstioRev})
	if err != nil {
		return retval, err
	}

	for _, configMap := range configMaps.Items {
		image := injection.GetIstioProxyImage(&configMap)
		if image != "" {
			retval[configMap.ObjectMeta.GetLabels()[label.IstioRev]] = image
		}
	}

	return retval, nil
}

func podCountByRevision(pods []v1.Pod, expectedRevision string) map[string]revisionCount {
	retval := map[string]revisionCount{}
	for _, pod := range pods {
		revision := pod.ObjectMeta.GetLabels()[label.IstioRev]
		revisionLabel := revision
		if revision == "" {
			revisionLabel = "<non-Istio>"
		}
		counts := retval[revisionLabel]
		counts.pods++
		if injectionDisabled(&pod) {
			counts.disabled++
		} else if revision != expectedRevision {
			counts.needsRestart++
		}
		retval[revisionLabel] = counts
	}
	return retval
}

func hideFromOutput(ns resource.Namespace) bool {
	if analyzer_util.IsSystemNamespace(ns) {
		return true
	}
	if ns == resource.Namespace(istioNamespace) {
		return true
	}
	return false
}

func injectionDisabled(pod *v1.Pod) bool {
	inject := pod.ObjectMeta.GetAnnotations()[annotation.SidecarInject.Name]
	return strings.EqualFold(inject, "false")
}

func renderCounts(counts revisionCount) string {
	retval := strconv.Itoa(counts.pods)
	if counts.disabled > 0 {
		retval += fmt.Sprintf(" (injection disabled: %d)", counts.disabled)
	}
	if counts.needsRestart > 0 {
		retval += fmt.Sprintf(" NEEDS RESTART: %d", counts.needsRestart)
	}
	return retval
}
