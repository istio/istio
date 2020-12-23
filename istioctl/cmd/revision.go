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
	"fmt"
	"github.com/spf13/cobra"
	"io"
	"istio.io/api/label"
	"istio.io/api/operator/v1alpha1"
	"istio.io/istio/operator/cmd/mesh"
	operator_istio "istio.io/istio/operator/pkg/apis/istio"
	iopv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/pkg/config"
	v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apimachinery_schema "k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/duration"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/utils/pointer"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
)

type revisionArgs struct {
	manifestsPath string
	name          string
	verbose       bool
}

var (
	istioOperatorGVR = apimachinery_schema.GroupVersionResource{
		Group:    iopv1alpha1.SchemeGroupVersion.Group,
		Version:  iopv1alpha1.SchemeGroupVersion.Version,
		Resource: "istiooperators",
	}

	kubeConfigFlags = &genericclioptions.ConfigFlags{
		Context:    pointer.StringPtr(""),
		Namespace:  pointer.StringPtr(""),
		KubeConfig: pointer.StringPtr(""),
	}

	revArgs = revisionArgs{}
)

func revisionCommand() *cobra.Command {
	revisionCmd := &cobra.Command{
		Use:     "revision",
		Short:   "Revision centric view of Istio deployment",
		Aliases: []string{"rev"},
	}
	revisionCmd.PersistentFlags().StringVarP(&revArgs.manifestsPath, "manifests", "d", "", mesh.ManifestsFlagHelpStr)
	revisionCmd.PersistentFlags().BoolVarP(&revArgs.verbose, "verbose", "v", false, "print customizations")

	revisionCmd.AddCommand(revisionListCommand())
	revisionCmd.AddCommand(revisionDescribeCommand())
	return revisionCmd
}

func revisionDescribeCommand() *cobra.Command {
	describeCmd := &cobra.Command{
		Use:     "describe",
		Example: `    istioctl experimental revision describe canary`,
		Short:   "Show details of a revision - customizations, number of pods pointing to it, istiod, gateways etc",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("revision is not specified")
			}
			if len(args) > 1 {
				return fmt.Errorf("exactly 1 revision should be specified")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			revArgs.name = args[0]
			if errs := validation.IsDNS1123Label(revArgs.name); len(errs) > 0 {
				return fmt.Errorf(strings.Join(errs, "\n"))
			}
			logger := clog.NewConsoleLogger(cmd.OutOrStdout(), cmd.ErrOrStderr(), scope)
			return revisionDescription(cmd.OutOrStdout(), &revArgs, kubeConfigFlags, logger)
		},
	}
	describeCmd.Flags().Bool("all", true, "show all related to a revision")
	return describeCmd
}

func revisionListCommand() *cobra.Command {
	listCmd := &cobra.Command{
		Use:     "list",
		Short:   "Show list of control plane and gateway revisions that are currently installed in cluster",
		Example: `   istioctl experimental revision list`,
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := clog.NewConsoleLogger(cmd.OutOrStdout(), cmd.ErrOrStderr(), scope)
			return revisionList(cmd.OutOrStdout(), &revArgs, kubeConfigFlags, logger)
		},
	}
	return listCmd
}

func revisionList(writer io.Writer, args *revisionArgs, restClientGetter genericclioptions.RESTClientGetter, logger clog.Logger) error {
	restConfig, err := restClientGetter.ToRESTConfig()
	if err != nil {
		return err
	}
	iops, err := getIOPs(restConfig)
	if err != nil {
		return err
	}
	return printIOPs(writer, iops, args.verbose, args.manifestsPath, logger)
}

// TODO: Refactor this to a function and wrap output in a struct so that we can
// TODO: easily support printing in different formats and make it more testable
func revisionDescription(w io.Writer, args *revisionArgs, restClientGetter *genericclioptions.ConfigFlags, logger *clog.ConsoleLogger) error {
	restConfig, err := restClientGetter.ToRESTConfig()
	if err != nil {
		return err
	}

	revision := args.name
	fmt.Fprintf(w, "Revision: %s\n", revision)
	err = printIstioOperatorCRInfoForRevision(w, args, restConfig, revision, logger)
	if err != nil {
		return err
	}

	// show Istiod and gateways that are installed under this revision
	istiodPods, err := getControlPlanePods(restConfig, revision)
	if err != nil {
		return err
	}
	if err = printPodTable(w, "CONTROL-PLANE", &istiodPods); err != nil {
		return err
	}

	// list ingress gateways
	ingressPods, err := getIngressGateways(restConfig, revision)
	if err != nil {
		return err
	}
	if err = printPodTable(w, "INGRESS-GATEWAYS", &ingressPods); err != nil {
		return err
	}

	// list egress gateways
	egressPods, err := getEgressGateways(restConfig, revision)
	if err != nil {
		return err
	}
	if err = printPodTable(w, "EGRESS-GATEWAYS", &egressPods); err != nil {
		return err
	}

	// print namespace summary
	pods, err := printNamespaceSummaryForRevision(w, restConfig, revision)
	if err != nil {
		return err
	}
	if args.verbose {
		return printPodTable(w, "PODS", &pods)
	}
	return nil
}

func printNamespaceSummaryForRevision(w io.Writer, restConfig *rest.Config, revision string) ([]v1.Pod, error) {
	pods, err := getPodsWithRevision(restConfig, revision)
	if err != nil {
		return nil, err
	}
	podPerNsCount := map[string]int{}
	for _, pod := range pods {
		podPerNsCount[pod.Namespace]++
	}

	fmt.Println("NAMESPACE-SUMMARY")
	nsSummaryWriter := new(tabwriter.Writer).Init(w, 0, 0, 1, ' ', 0)
	fmt.Fprintln(nsSummaryWriter, "NAMESPACE\tPOD-COUNT")
	for ns, podCount := range podPerNsCount {
		fmt.Fprintf(nsSummaryWriter, "%s\t%d\n", ns, podCount)
	}
	if err = nsSummaryWriter.Flush(); err != nil {
		return nil, err
	}
	fmt.Fprintln(w, "")
	return pods, nil
}

func printIstioOperatorCRInfoForRevision(w io.Writer, args *revisionArgs, restConfig *rest.Config, revision string, logger *clog.ConsoleLogger) error {
	iops, err := getIOPs(restConfig)
	if err != nil {
		return err
	}
	// It is not an efficient way. But for now, we live with this
	// This can be refined by selecting by label, annotation etc.
	filteredIOPs := getIOPWithRevision(iops, revision)
	if len(filteredIOPs) == 0 {
		fmt.Fprintf(w, "ISTIO-OPERATOR: (No CRDs found)\n")
	} else if len(filteredIOPs) == 1 {
		fmt.Fprintf(w, "ISTIO-OPERATOR: (1 CRD found)\n")
	} else {
		fmt.Fprintf(w, "ISTIO-OPERATOR: (%d CRDs found)\n", len(filteredIOPs))
	}

	for i, iop := range filteredIOPs {
		fmt.Fprintf(w, "%d. %s/%s\n", i+1, iop.Namespace, iop.Name)
		enabledComponents := getEnabledComponents(iop.Spec)
		fmt.Fprintf(w, "  COMPONENTS:\n")
		for _, c := range enabledComponents {
			fmt.Fprintf(w, "  - %s\n", c)
		}

		// For each IOP, print all customizations for it
		fmt.Fprintf(w, "  CUSTOMIZATIONS:\n")
		iopCustomizations, err := getDiffs(iop, args.manifestsPath, iop.Spec.GetProfile(), logger)
		if err != nil {
			return fmt.Errorf("<Error> Cannot fetch customizations for %s: %s", iop.Name, err.Error())
		}
		for _, customization := range iopCustomizations {
			fmt.Fprintf(w, "  - %s\n", customization)
		}
		fmt.Fprintln(w)
	}
	return nil
}

func getIOPWithRevision(iops []*iopv1alpha1.IstioOperator, revision string) []*iopv1alpha1.IstioOperator {
	filteredIOPs := []*iopv1alpha1.IstioOperator{}
	for _, iop := range iops {
		if iop.Spec == nil {
			continue
		}
		if iop.Spec.Revision == revision || (revision == "default" && len(iop.Spec.Revision) == 0) {
			filteredIOPs = append(filteredIOPs, iop)
		}
	}
	return filteredIOPs
}

func printPodTable(w io.Writer, header string, pods *[]v1.Pod) error {
	if len(*pods) == 0 {
		fmt.Fprintf(w, "%s: No pod(s) found\n\n", header)
		return nil
	} else if len(*pods) == 1 {
		fmt.Fprintf(w, "%s: (1 pod)\n", header)
	} else {
		fmt.Fprintf(w, "%s: (%d pods)\n", header, len(*pods))
	}
	podTableW := new(tabwriter.Writer).Init(w, 0, 0, 1, ' ', 0)
	fmt.Fprintln(podTableW, "NAMESPACE\tNAME\tADDRESS\tSTATUS\tAGE")
	for _, pod := range *pods {
		fmt.Fprintf(podTableW, "%s\t%s\t%s\t%s\t%s\n",
			pod.Namespace, pod.Name, pod.Status.PodIP,
			pod.Status.Phase, translateTimestampSince(pod.CreationTimestamp))
	}
	if err := podTableW.Flush(); err != nil {
		return nil
	}
	fmt.Fprintln(w)
	return nil
}

// TODO(su225): Refactor - This function coule be ugly because of verbose/non-verbose paths
func printIOPs(writer io.Writer, iops []*iopv1alpha1.IstioOperator, verbose bool, manifestsPath string,
	l clog.Logger) error {
	if len(iops) == 0 {
		_, err := fmt.Fprintf(writer, "No IstioOperators present.\n")
		return err
	}
	revToIOP := make(map[string][]*iopv1alpha1.IstioOperator)
	for _, iop := range iops {
		revision := iop.Spec.Revision
		if _, present := revToIOP[revision]; !present {
			revToIOP[revision] = []*iopv1alpha1.IstioOperator{iop}
		} else {
			revToIOP[revision] = append(revToIOP[revision], iop)
		}
	}
	w := new(tabwriter.Writer).Init(writer, 0, 8, 1, ' ', 0)
	if verbose {
		_, _ = fmt.Fprintln(w, "REVISION\tIOP\tPROFILE\tCOMPONENTS\tCUSTOMIZATIONS")
	} else {
		_, _ = fmt.Fprintln(w, "REVISION\tIOP\tPROFILE\tCOMPONENTS")
	}
	for revision, iopWithRev := range revToIOP {
		for i, iop := range iopWithRev {
			iopSpec := iop.Spec
			components := getEnabledComponents(iopSpec)
			customizations := []string{}
			if verbose {
				var err error
				customizations, err = getDiffs(iop, manifestsPath, effectiveProfile(iop.Spec.Profile), l)
				if err != nil {
					l.LogAndErrorf("error while computing customization: %s", err.Error())
				}
			}
			// The first row is for printing revision, IOP related information
			numRows := max(len(components), max(1, len(customizations)))
			for j := 0; j < numRows; j++ {
				var outRevision string
				if i == 0 && j == 0 {
					outRevision = renderWithDefault(revision, "default")
				}
				var outIopName, outProfile string
				if j == 0 {
					outIopName = fmt.Sprintf("%s/%s", iop.Namespace, iop.Name)
					outProfile = renderWithDefault(iopSpec.GetProfile(), "default")
				}
				outCompName := ""
				if j < len(components) {
					outCompName = components[j]
				}
				outCustomization := ""
				if j < len(customizations) {
					outCustomization = customizations[j]
				}
				if verbose {
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
						outRevision, outIopName, outProfile, outCompName, outCustomization)
				} else {
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
						outRevision, outIopName, outProfile, outCompName)
				}
			}
		}
	}
	return w.Flush()
}

func getEnabledComponents(iops *v1alpha1.IstioOperatorSpec) []string {
	enabledComponents := []string{}
	if iops.Components.Base.Enabled.GetValue() {
		enabledComponents = append(enabledComponents, "base")
	}
	if iops.Components.Cni.Enabled.GetValue() {
		enabledComponents = append(enabledComponents, "cni")
	}
	if iops.Components.Pilot.Enabled.GetValue() {
		enabledComponents = append(enabledComponents, "istiod")
	}
	if iops.Components.IstiodRemote.Enabled.GetValue() {
		enabledComponents = append(enabledComponents, "istiod-remote")
	}
	for _, gw := range iops.Components.IngressGateways {
		if gw.Enabled.Value {
			enabledComponents = append(enabledComponents, fmt.Sprintf("ingress:%s", gw.GetName()))
		}
	}
	for _, gw := range iops.Components.EgressGateways {
		if gw.Enabled.Value {
			enabledComponents = append(enabledComponents, fmt.Sprintf("egress:%s", gw.GetName()))
		}
	}
	return enabledComponents
}

func getIOPs(restConfig *rest.Config) ([]*iopv1alpha1.IstioOperator, error) {
	client, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}
	ul, err := client.
		Resource(istioOperatorGVR).
		Namespace(istioNamespace).
		List(context.TODO(), meta_v1.ListOptions{})
	if err != nil {
		return nil, err
	}
	iops := []*iopv1alpha1.IstioOperator{}
	for _, un := range ul.Items {
		un.SetCreationTimestamp(meta_v1.Time{}) // UnmarshalIstioOperator chokes on these
		un.SetManagedFields([]meta_v1.ManagedFieldsEntry{})
		by := util.ToYAML(un.Object)
		iop, err := operator_istio.UnmarshalIstioOperator(by, true)
		if err != nil {
			return nil, err
		}
		iops = append(iops, iop)
	}
	return iops, nil
}

// TODO: Get Rid of magic strings or get rid of these methods altogether
func getControlPlanePods(restConfig *rest.Config, revision string) ([]v1.Pod, error) {
	return getPodsForComponent(restConfig, "Pilot", revision)
}

func getIngressGateways(restConfig *rest.Config, revision string) ([]v1.Pod, error) {
	return getPodsForComponent(restConfig, "IngressGateways", revision)
}

func getEgressGateways(restConfig *rest.Config, revision string) ([]v1.Pod, error) {
	return getPodsForComponent(restConfig, "EgressGateways", revision)
}

func getPodsForComponent(restConfig *rest.Config, component, revision string) ([]v1.Pod, error) {
	return getPodsWithSelector(restConfig, istioNamespace, &meta_v1.LabelSelector{
		MatchLabels: map[string]string{
			label.IstioRev:               revision,
			label.IstioOperatorComponent: component,
		},
	})
}

func getPodsWithRevision(restConfig *rest.Config, revision string) ([]v1.Pod, error) {
	return getPodsWithSelector(restConfig, "", &meta_v1.LabelSelector{
		MatchLabels: map[string]string{
			label.IstioRev: revision,
		},
	})
}

func getPodsWithSelector(restConfig *rest.Config, ns string, selector *meta_v1.LabelSelector) ([]v1.Pod, error) {
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return []v1.Pod{}, err
	}
	labelSelector, err := meta_v1.LabelSelectorAsSelector(selector)
	if err != nil {
		return []v1.Pod{}, err
	}
	podList, err := client.CoreV1().Pods(ns).List(context.TODO(),
		meta_v1.ListOptions{LabelSelector: labelSelector.String()})
	if err != nil {
		return []v1.Pod{}, err
	}

	pods := []v1.Pod{}
	for _, pod := range podList.Items {
		pods = append(pods, pod)
	}
	return pods, err
}

func getDiffs(installed *iopv1alpha1.IstioOperator, manifestsPath, profile string, l clog.Logger) ([]string, error) {
	setFlags := []string{"profile=" + profile}
	if manifestsPath != "" {
		setFlags = append(setFlags, fmt.Sprintf("installPackagePath=%s", manifestsPath))
	}
	_, base, err := manifest.GenerateConfig([]string{}, setFlags, true, nil, l)
	if err != nil {
		return []string{}, err
	}
	mapInstalled, err := config.ToMap(installed.Spec)
	if err != nil {
		return []string{}, err
	}
	mapBase, err := config.ToMap(base.Spec)
	if err != nil {
		return []string{}, err
	}
	return diffIOPs(mapInstalled, mapBase)
}

func diffIOPs(installed, base map[string]interface{}) ([]string, error) {
	setflags, err := diffWalk("", "", installed, base)
	if err != nil {
		return []string{}, err
	}
	sort.Strings(setflags)
	return setflags, nil
}

func diffWalk(path, separator string, obj interface{}, orig interface{}) ([]string, error) {
	switch v := obj.(type) {
	case map[string]interface{}:
		accum := make([]string, 0)
		typedOrig, ok := orig.(map[string]interface{})
		if ok {
			for key, vv := range v {
				childwalk, err := diffWalk(fmt.Sprintf("%s%s%s", path, separator, pathComponent(key)), ".", vv, typedOrig[key])
				if err != nil {
					return accum, err
				}
				accum = append(accum, childwalk...)
			}
		}
		return accum, nil
	case []interface{}:
		accum := make([]string, 0)
		typedOrig, ok := orig.([]interface{})
		if ok {
			for idx, vv := range v {
				indexwalk, err := diffWalk(fmt.Sprintf("%s[%d]", path, idx), ".", vv, typedOrig[idx])
				if err != nil {
					return accum, err
				}
				accum = append(accum, indexwalk...)
			}
		}
		return accum, nil
	case string:
		if v != orig && orig != nil {
			return []string{fmt.Sprintf("%s=%q", path, v)}, nil
		}
	default:
		if v != orig && orig != nil {
			return []string{fmt.Sprintf("%s=%v", path, v)}, nil
		}
	}
	return []string{}, nil
}

func renderWithDefault(s, def string) string {
	if s != "" {
		return s
	}
	return fmt.Sprintf("<%s>", def)
}

func effectiveProfile(profile string) string {
	if profile != "" {
		return profile
	}
	return "default"
}

func pathComponent(component string) string {
	if !strings.Contains(component, util.PathSeparator) {
		return component
	}
	return strings.ReplaceAll(component, util.PathSeparator, util.EscapedPathSeparator)
}

// Human-readable age.  (This is from kubectl pkg/describe/describe.go)
func translateTimestampSince(timestamp meta_v1.Time) string {
	if timestamp.IsZero() {
		return "<unknown>"
	}
	return duration.HumanDuration(time.Since(timestamp.Time))
}

func max(x, y int) int {
	if x > y {
		return x
	}
	return y
}
