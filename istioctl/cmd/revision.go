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
	"io"
	"istio.io/istio/pkg/kube"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
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
)

type revisionArgs struct {
	manifestsPath string
	name          string
	verbose       bool
	sections      []string
	output        string
}

const (
	IstioOperatorCRSection = "cr"
	ControlPlaneSection    = "cp"
	GatewaysSection        = "gw"
	PodsSection            = "po"

	// TODO: This should be moved to istio/api:label package
	istioTag = "istio.io/tag"
)

var (
	validFormats = []string{"table", "json"}

	defaultSections = []string{
		IstioOperatorCRSection,
		ControlPlaneSection,
		GatewaysSection,
	}

	verboseSections = []string{
		IstioOperatorCRSection,
		ControlPlaneSection,
		GatewaysSection,
		PodsSection,
	}
)

var (
	istioOperatorGVR = apimachinery_schema.GroupVersionResource{
		Group:    iopv1alpha1.SchemeGroupVersion.Group,
		Version:  iopv1alpha1.SchemeGroupVersion.Version,
		Resource: "istiooperators",
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
			if revArgs.verbose && len(revArgs.sections) != 0 {
				return fmt.Errorf("sections and verbose flags should not be specified at the same time")
			}
			isValidFormat := false
			for _, f := range validFormats {
				if revArgs.output == f {
					isValidFormat = true
					break
				}
			}
			if !isValidFormat {
				return fmt.Errorf("unknown format %s. It should be %#v", revArgs.output, validFormats)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			revArgs.name = args[0]
			if errs := validation.IsDNS1123Label(revArgs.name); len(errs) > 0 {
				return fmt.Errorf(strings.Join(errs, "\n"))
			}
			logger := clog.NewConsoleLogger(cmd.OutOrStdout(), cmd.ErrOrStderr(), scope)
			return revisionDescription(cmd.OutOrStdout(), &revArgs, logger)
		},
	}
	describeCmd.Flags().BoolVarP(&revArgs.verbose, "verbose", "v", false, "Dump all information related to the revision")
	describeCmd.Flags().StringSliceVarP(&revArgs.sections, "sections", "s", []string{}, "Sections that should be included in description output")
	describeCmd.Flags().StringVarP(&revArgs.output, "output", "o", "table", "Output format for revision description")
	return describeCmd
}

func revisionListCommand() *cobra.Command {
	listCmd := &cobra.Command{
		Use:     "list",
		Short:   "Show list of control plane and gateway revisions that are currently installed in cluster",
		Example: `   istioctl experimental revision list`,
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := clog.NewConsoleLogger(cmd.OutOrStdout(), cmd.ErrOrStderr(), scope)
			return revisionList(cmd.OutOrStdout(), &revArgs, logger)
		},
	}
	return listCmd
}

// TODO: Come up with a better name
type revisionInfo struct {
	Revision string
	Tags     []string
	IOPs     []*iopv1alpha1.IstioOperator
}

func newRevisionInfo(name string) *revisionInfo {
	return &revisionInfo{
		Revision: name,
		Tags:     []string{},
		IOPs:     []*iopv1alpha1.IstioOperator{},
	}
}

func revisionList(writer io.Writer, args *revisionArgs, logger clog.Logger) error {
	client, err := newKubeClient(kubeconfig, configContext)
	if err != nil {
		return fmt.Errorf("cannot create kubeclient for kubeconfig=%s, context=%s: %v",
			kubeconfig, configContext, err)
	}

	revisions := map[string]*revisionInfo{}

	// Get a list of control planes which are installed in remote clusters
	// In this case, it is possible that they only have webhooks installed.
	webhooks, err := getWebhooks(context.Background(), client)
	if err != nil {
		return fmt.Errorf("error while listing webhooks: %v", err)
	}
	for _, hook := range webhooks {
		rev := renderWithDefault(hook.GetLabels()[label.IstioRev], "default")
		tag := hook.GetLabels()[istioTag]
		ri, revPresent := revisions[rev]
		if revPresent {
			if tag != "" {
				ri.Tags = append(ri.Tags, tag)
			}
		} else {
			revisions[rev] = newRevisionInfo(rev)
		}
	}

	// Get a list of all CRs which are installed in this cluster
	iopcrs, err := getAllIstioOperatorCRs(client)
	if err != nil {
		return fmt.Errorf("error while listing IstioOperator CRs: %v", err)
	}
	for _, iop := range iopcrs {
		if iop == nil {
			continue
		}
		rev := renderWithDefault(iop.Spec.GetRevision(), "default")
		ri, revPresent := revisions[rev]
		if revPresent {
			ri.IOPs = append(ri.IOPs, iop)
		}
	}

	switch revArgs.output {
	case "json":
		return fmt.Errorf("not yet implemented")
	default:
		return printRevisionInfoTable(writer, args, revisions, logger)
	}
}

func printRevisionInfoTable(writer io.Writer, args *revisionArgs, revisions map[string]*revisionInfo, logger clog.Logger) error {
	tw := new(tabwriter.Writer).Init(writer, 0, 8, 1, ' ', 0)
	var err error
	if args.verbose {
		tw.Write([]byte("REVISION\tTAG\tISTIO-OPERATOR-CR\tPROFILE\tREQD-COMPONENTS\tCUSTOMIZATIONS\n"))
	} else {
		tw.Write([]byte("REVISION\tTAG\tISTIO-OPERATOR-CR\tPROFILE\tREQD-COMPONENTS\n"))
	}
	for r, ri := range revisions {
		rowId := 0
		for _, iop := range ri.IOPs {
			profile := iop.Spec.Profile
			components := getEnabledComponents(iop.Spec)
			qualifiedName := fmt.Sprintf("%s/%s", iop.Namespace, iop.Name)

			customizations := []string{}
			if args.verbose {
				customizations, err = getDiffs(iop, args.manifestsPath, profile, logger)
				if err != nil {
					return fmt.Errorf("error while computing customizations for %s: %v", qualifiedName, err)
				}
				if len(customizations) == 0 {
					customizations = append(customizations, "<no-customization>")
				}
			}
			if len(ri.Tags) == 0 {
				ri.Tags = append(ri.Tags, "<no-tag>")
			}
			maxIopRows := max(max(1, len(components)), len(customizations))
			for i := 0; i < maxIopRows; i++ {
				var rowTag, rowRev string
				var rowIopName, rowProfile, rowComp, rowCust string
				if i == 0 {
					rowIopName = qualifiedName
					rowProfile = profile
				}
				if i < len(components) {
					rowComp = components[i]
				}
				if i < len(customizations) {
					rowCust = customizations[i]
				}
				if rowId < len(ri.Tags) {
					rowTag = ri.Tags[rowId]
				}
				if rowId == 0 {
					rowRev = r
				}
				if args.verbose {
					fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
						rowRev, rowTag, rowIopName, rowProfile, rowComp, rowCust)
				} else {
					fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
						rowRev, rowTag, rowIopName, rowProfile, rowComp)
				}
				rowId++
			}
		}
		for rowId < len(ri.Tags) {
			var rowRev, rowTag, rowIopName string
			if rowId == 0 {
				rowRev = r
			}
			if rowId == 0 {
				rowIopName = "<no-iop>"
			}
			rowTag = ri.Tags[rowId]
			if args.verbose {
				fmt.Fprintf(tw, "%s\t%s\t%s\t \t \t \n", rowRev, rowTag, rowIopName)
			} else {
				fmt.Fprintf(tw, "%s\t%s\t%s\t \t \n", rowRev, rowTag, rowIopName)
			}
			rowId++
		}
	}
	return tw.Flush()
}

func getAllIstioOperatorCRs(client kube.ExtendedClient) ([]*iopv1alpha1.IstioOperator, error) {
	ucrs, err := client.Dynamic().Resource(istioOperatorGVR).
		Namespace(istioNamespace).
		List(context.Background(), meta_v1.ListOptions{})
	if err != nil {
		return []*iopv1alpha1.IstioOperator{}, fmt.Errorf("cannot retrieve IstioOperator CRs: %v", err)
	}
	iopCRs := []*iopv1alpha1.IstioOperator{}
	for _, u := range ucrs.Items {
		u.SetCreationTimestamp(meta_v1.Time{})
		u.SetManagedFields([]meta_v1.ManagedFieldsEntry{})
		iop, err := operator_istio.UnmarshalIstioOperator(util.ToYAML(u.Object), true)
		if err != nil {
			return []*iopv1alpha1.IstioOperator{},
				fmt.Errorf("error while converting to IstioOperator CR - %s/%s: %v",
					u.GetNamespace(), u.GetName(), err)
		}
		iopCRs = append(iopCRs, iop)
	}
	return iopCRs, nil
}

// TODO: Refactor this to a function and wrap output in a struct so that we can
// TODO: easily support printing in different formats and make it more testable
func revisionDescription(w io.Writer, args *revisionArgs, logger *clog.ConsoleLogger) error {
	revision := args.name
	client, err := newKubeClientWithRevision(kubeconfig, configContext, revision)
	if err != nil {
		return fmt.Errorf("cannot create kubeclient for kubeconfig=%s, context=%s: %v",
			kubeconfig, configContext, err)
	}

	fmt.Fprintf(w, "Revision: %s\n", revision)
	err = printIstioOperatorCRInfoForRevision(w, args, client, logger)
	if err != nil {
		return err
	}

	// show Istiod and gateways that are installed under this revision
	istiodPods, err := getControlPlanePods(client)
	if err != nil {
		return err
	}
	if err = printPodTable(w, "CONTROL-PLANE", &istiodPods); err != nil {
		return err
	}

	// list ingress gateways
	ingressPods, err := getIngressGateways(client)
	if err != nil {
		return err
	}
	if err = printPodTable(w, "INGRESS-GATEWAYS", &ingressPods); err != nil {
		return err
	}

	// list egress gateways
	egressPods, err := getEgressGateways(client)
	if err != nil {
		return err
	}
	if err = printPodTable(w, "EGRESS-GATEWAYS", &egressPods); err != nil {
		return err
	}

	// print namespace summary
	pods, err := printNamespaceSummaryForRevision(w, client)
	if err != nil {
		return err
	}
	if args.verbose {
		return printPodTable(w, "PODS", &pods)
	}
	return nil
}

func printNamespaceSummaryForRevision(w io.Writer, client kube.ExtendedClient) ([]v1.Pod, error) {
	pods, err := getPodsWithRevision(client)
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

func printIstioOperatorCRInfoForRevision(w io.Writer, args *revisionArgs, client kube.ExtendedClient, logger *clog.ConsoleLogger) error {
	iops, err := getAllIstioOperatorCRs(client)
	if err != nil {
		return err
	}

	// It is not an efficient way. But for now, we live with this
	// This can be refined by selecting by label, annotation etc.
	filteredIOPs := getIOPWithRevision(iops, args.name)
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

// TODO: Get Rid of magic strings or get rid of these methods altogether
func getControlPlanePods(client kube.ExtendedClient) ([]v1.Pod, error) {
	return getPodsForComponent(client, "Pilot")
}

func getIngressGateways(client kube.ExtendedClient) ([]v1.Pod, error) {
	return getPodsForComponent(client, "IngressGateways")
}

func getEgressGateways(client kube.ExtendedClient) ([]v1.Pod, error) {
	return getPodsForComponent(client, "EgressGateways")
}

func getPodsForComponent(client kube.ExtendedClient, component string) ([]v1.Pod, error) {
	return getPodsWithSelector(client, istioNamespace, &meta_v1.LabelSelector{
		MatchLabels: map[string]string{
			label.IstioRev:               client.Revision(),
			label.IstioOperatorComponent: component,
		},
	})
}

func getPodsWithRevision(client kube.ExtendedClient) ([]v1.Pod, error) {
	return getPodsWithSelector(client, "", &meta_v1.LabelSelector{
		MatchLabels: map[string]string{
			label.IstioRev: client.Revision(),
		},
	})
}

func getPodsWithSelector(client kube.ExtendedClient, ns string, selector *meta_v1.LabelSelector) ([]v1.Pod, error) {
	labelSelector, err := meta_v1.LabelSelectorAsSelector(selector)
	if err != nil {
		return []v1.Pod{}, err
	}
	podList, err := client.CoreV1().Pods(ns).List(context.TODO(),
		meta_v1.ListOptions{LabelSelector: labelSelector.String()})
	if err != nil {
		return []v1.Pod{}, err
	}
	return podList.Items, nil
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
