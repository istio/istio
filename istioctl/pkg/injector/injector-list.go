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

package injector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	admitv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	api_pkg_labels "k8s.io/apimachinery/pkg/labels"

	"istio.io/api/annotation"
	"istio.io/api/label"
	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/pkg/config/analysis/analyzers/injection"
	analyzer_util "istio.io/istio/pkg/config/analysis/analyzers/util"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/inject"
)

type revisionCount struct {
	// pods in a revision
	pods int
	// pods that are disabled from injection
	disabled int
	// pods that are enabled for injection, but whose revision doesn't match their namespace's revision
	needsRestart int
}

func Cmd(cliContext cli.Context) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "injector",
		Short:   "List sidecar injector and sidecar versions",
		Long:    `List sidecar injector and sidecar versions`,
		Example: `  istioctl experimental injector list`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("unknown subcommand %q", args[0])
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.HelpFunc()(cmd, args)
			return nil
		},
	}

	cmd.AddCommand(injectorListCommand(cliContext))
	return cmd
}

func injectorListCommand(ctx cli.Context) *cobra.Command {
	var opts clioptions.ControlPlaneOptions
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List sidecar injector and sidecar versions",
		Long:    `List sidecar injector and sidecar versions`,
		Example: `  istioctl experimental injector list`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := ctx.CLIClientWithRevision(opts.Revision)
			if err != nil {
				return fmt.Errorf("failed to create k8s client: %v", err)
			}

			nslist, err := getNamespaces(context.Background(), client)
			nslist = filterSystemNamespaces(nslist, ctx.IstioNamespace())
			if err != nil {
				return err
			}
			sort.Slice(nslist, func(i, j int) bool {
				return nslist[i].Name < nslist[j].Name
			})

			hooksList, err := client.Kube().AdmissionregistrationV1().MutatingWebhookConfigurations().List(context.Background(), metav1.ListOptions{})
			if err != nil {
				return err
			}
			hooks := hooksList.Items
			pods, err := getPods(context.Background(), client)
			if err != nil {
				return err
			}
			err = printNS(cmd.OutOrStdout(), nslist, hooks, pods)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout())
			injectedImages, err := getInjectedImages(context.Background(), client)
			if err != nil {
				return err
			}

			sort.Slice(hooks, func(i, j int) bool {
				return hooks[i].Name < hooks[j].Name
			})
			return printHooks(cmd.OutOrStdout(), nslist, hooks, injectedImages)
		},
	}

	return cmd
}

func filterSystemNamespaces(nss []corev1.Namespace, istioNamespace string) []corev1.Namespace {
	filtered := make([]corev1.Namespace, 0)
	for _, ns := range nss {
		if analyzer_util.IsSystemNamespace(resource.Namespace(ns.Name)) || ns.Name == istioNamespace {
			continue
		}
		filtered = append(filtered, ns)
	}
	return filtered
}

func getNamespaces(ctx context.Context, client kube.CLIClient) ([]corev1.Namespace, error) {
	nslist, err := client.Kube().CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return []corev1.Namespace{}, err
	}
	return nslist.Items, nil
}

func printNS(writer io.Writer, namespaces []corev1.Namespace, hooks []admitv1.MutatingWebhookConfiguration,
	allPods map[resource.Namespace][]corev1.Pod,
) error {
	outputCount := 0

	w := new(tabwriter.Writer).Init(writer, 0, 8, 1, ' ', 0)
	for _, namespace := range namespaces {
		revision := getInjectedRevision(&namespace, hooks)
		podCount := podCountByRevision(allPods[resource.Namespace(namespace.Name)], revision)
		if len(podCount) == 0 {
			// This namespace has no pods, but we wish to display it if new pods will be auto-injected
			if revision != "" {
				podCount[revision] = revisionCount{}
			}
		}
		for injectedRevision, count := range podCount {
			if outputCount == 0 {
				fmt.Fprintln(w, "NAMESPACE\tISTIO-REVISION\tPOD-REVISIONS")
			}
			outputCount++

			fmt.Fprintf(w, "%s\t%s\t%s\n", namespace.Name, revision, renderCounts(injectedRevision, count))
		}
	}
	if outputCount == 0 {
		fmt.Fprintf(writer, "No Istio injected namespaces present.\n")
	}

	return w.Flush()
}

func printHooks(writer io.Writer, namespaces []corev1.Namespace, hooks []admitv1.MutatingWebhookConfiguration, injectedImages map[string]string) error {
	if len(hooks) == 0 {
		fmt.Fprintf(writer, "No Istio injection hooks present.\n")
		return nil
	}

	w := new(tabwriter.Writer).Init(writer, 0, 8, 1, ' ', 0)
	fmt.Fprintln(w, "NAMESPACES\tINJECTOR-HOOK\tISTIO-REVISION\tSIDECAR-IMAGE")
	for _, hook := range hooks {
		revision := hook.ObjectMeta.GetLabels()[label.IoIstioRev.Name]
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

func getInjector(namespace *corev1.Namespace, hooks []admitv1.MutatingWebhookConfiguration) *admitv1.MutatingWebhookConfiguration {
	// find matching hook
	for _, hook := range hooks {
		for _, webhook := range hook.Webhooks {
			nsSelector, err := metav1.LabelSelectorAsSelector(webhook.NamespaceSelector)
			if err != nil {
				continue
			}
			if nsSelector.Matches(api_pkg_labels.Set(namespace.ObjectMeta.Labels)) {
				return &hook
			}
		}
	}
	return nil
}

func getInjectedRevision(namespace *corev1.Namespace, hooks []admitv1.MutatingWebhookConfiguration) string {
	injector := getInjector(namespace, hooks)
	if injector != nil {
		return injector.ObjectMeta.GetLabels()[label.IoIstioRev.Name]
	}
	newRev := namespace.ObjectMeta.GetLabels()[label.IoIstioRev.Name]
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

func getMatchingNamespaces(hook *admitv1.MutatingWebhookConfiguration, namespaces []corev1.Namespace) []corev1.Namespace {
	retval := make([]corev1.Namespace, 0)
	for _, webhook := range hook.Webhooks {
		nsSelector, err := metav1.LabelSelectorAsSelector(webhook.NamespaceSelector)
		if err != nil {
			return retval
		}

		for _, namespace := range namespaces {
			if nsSelector.Matches(api_pkg_labels.Set(namespace.Labels)) {
				retval = append(retval, namespace)
			}
		}
	}
	return retval
}

func getPods(ctx context.Context, client kube.CLIClient) (map[resource.Namespace][]corev1.Pod, error) {
	retval := map[resource.Namespace][]corev1.Pod{}
	// All pods in all namespaces
	pods, err := client.Kube().CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return retval, err
	}
	for _, pod := range pods.Items {
		podList, ok := retval[resource.Namespace(pod.GetNamespace())]
		if !ok {
			retval[resource.Namespace(pod.GetNamespace())] = []corev1.Pod{pod}
		} else {
			retval[resource.Namespace(pod.GetNamespace())] = append(podList, pod)
		}
	}
	return retval, nil
}

// getInjectedImages() returns a map of revision->dockerimage
func getInjectedImages(ctx context.Context, client kube.CLIClient) (map[string]string, error) {
	retval := map[string]string{}

	// All configs in all namespaces that are Istio revisioned
	configMaps, err := client.Kube().CoreV1().ConfigMaps("").List(ctx, metav1.ListOptions{LabelSelector: label.IoIstioRev.Name})
	if err != nil {
		return retval, err
	}

	for _, configMap := range configMaps.Items {
		image := injection.GetIstioProxyImage(&configMap)
		if image != "" {
			retval[configMap.ObjectMeta.GetLabels()[label.IoIstioRev.Name]] = image
		}
	}

	return retval, nil
}

// podCountByRevision() returns a map of revision->pods, with "<non-Istio>" as the dummy "revision" for uninjected pods
func podCountByRevision(pods []corev1.Pod, expectedRevision string) map[string]revisionCount {
	retval := map[string]revisionCount{}
	for _, pod := range pods {
		revision := extractRevisionFromPod(&pod)
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

func extractRevisionFromPod(pod *corev1.Pod) string {
	statusAnno, ok := pod.GetAnnotations()[annotation.SidecarStatus.Name]
	if !ok {
		return ""
	}
	var sidecarinjection inject.SidecarInjectionStatus
	if err := json.Unmarshal([]byte(statusAnno), &sidecarinjection); err != nil {
		return ""
	}
	return sidecarinjection.Revision
}

func injectionDisabled(pod *corev1.Pod) bool {
	inject := pod.ObjectMeta.GetAnnotations()[annotation.SidecarInject.Name]
	if lbl, labelPresent := pod.ObjectMeta.GetLabels()[label.SidecarInject.Name]; labelPresent {
		inject = lbl
	}
	return strings.EqualFold(inject, "false")
}

func renderCounts(injectedRevision string, counts revisionCount) string {
	if counts.pods == 0 {
		return "<no pods>"
	}

	podText := strconv.Itoa(counts.pods)
	if counts.disabled > 0 {
		podText += fmt.Sprintf(" (injection disabled: %d)", counts.disabled)
	}
	if counts.needsRestart > 0 {
		podText += fmt.Sprintf(" NEEDS RESTART: %d", counts.needsRestart)
	}
	return fmt.Sprintf("%s: %s", injectedRevision, podText)
}
