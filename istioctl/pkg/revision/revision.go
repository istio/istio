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

package revision

import (
	"context"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
	admitv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apimachinery_schema "k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/duration"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/yaml"

	"istio.io/api/label"
	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/istioctl/pkg/root"
	"istio.io/istio/istioctl/pkg/tag"
	istioctlutil "istio.io/istio/istioctl/pkg/util"
	"istio.io/istio/operator/cmd/mesh"
	operator_istio "istio.io/istio/operator/pkg/apis/istio"
	iopv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/manifest"
	name2 "istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/translate"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/kube"
)

type revisionArgs struct {
	manifestsPath string
	name          string
	verbose       bool
	output        string
}

const (
	istioOperatorCRSection = "ISTIO-OPERATOR-CR"
	controlPlaneSection    = "CONTROL-PLANE"
	gatewaysSection        = "GATEWAYS"
	webhooksSection        = "MUTATING-WEBHOOKS"
	podsSection            = "PODS"
)

var (
	validFormats = map[string]bool{
		istioctlutil.JSONFormat:  true,
		istioctlutil.TableFormat: true,
	}

	defaultSections = []string{
		istioOperatorCRSection,
		webhooksSection,
		controlPlaneSection,
		gatewaysSection,
	}

	verboseSections = []string{
		istioOperatorCRSection,
		webhooksSection,
		controlPlaneSection,
		gatewaysSection,
		podsSection,
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

func Cmd(ctx cli.Context) *cobra.Command {
	revisionCmd := &cobra.Command{
		Use: "revision",
		Long: "The revision command provides a revision centric view of istio deployments. " +
			"It provides insight into IstioOperator CRs defining the revision, istiod and gateway pods " +
			"which are part of deployment of a particular revision.",
		Short:   "Provide insight into various revisions (istiod, gateways) installed in the cluster",
		Aliases: []string{"rev"},
	}
	revisionCmd.PersistentFlags().StringVarP(&revArgs.manifestsPath, "manifests", "d", "", mesh.ManifestsFlagHelpStr)
	revisionCmd.PersistentFlags().BoolVarP(&revArgs.verbose, "verbose", "v", false, "Enable verbose output")
	revisionCmd.PersistentFlags().StringVarP(&revArgs.output, "output", "o", istioctlutil.TableFormat, "Output format for revision description "+
		"(available formats: table,json)")

	revisionCmd.AddCommand(revisionListCommand(ctx))
	revisionCmd.AddCommand(revisionDescribeCommand(ctx))
	revisionCmd.AddCommand(tag.TagCommand(ctx))
	return revisionCmd
}

func revisionDescribeCommand(ctx cli.Context) *cobra.Command {
	describeCmd := &cobra.Command{
		Use: "describe",
		Example: `  # View the details of a revision named 'canary'
  istioctl x revision describe canary

  # View the details of a revision named 'canary' and also the pods
  # under that particular revision
  istioctl x revision describe canary -v

  # Get details about a revision in json format (default format is human-friendly table format)
  istioctl x revision describe canary -v -o json
`,
		Short: "Show information about a revision, including customizations, " +
			"istiod version and which pods/gateways are using it.",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("revision must be specified")
			}
			if len(args) != 1 {
				return fmt.Errorf("exactly 1 revision should be specified")
			}
			revArgs.name = args[0]
			if !validFormats[revArgs.output] {
				return fmt.Errorf("unknown format %s. It should be %#v", revArgs.output, validFormats)
			}
			if errs := validation.IsDNS1123Label(revArgs.name); len(errs) > 0 {
				return fmt.Errorf("%s - invalid revision format: %v", revArgs.name, errs)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := clog.NewConsoleLogger(cmd.OutOrStdout(), cmd.ErrOrStderr(), root.Scope)
			return printRevisionDescription(ctx, cmd.OutOrStdout(), &revArgs, logger, ctx.IstioNamespace())
		},
	}
	describeCmd.Flags().BoolVarP(&revArgs.verbose, "verbose", "v", false, "Enable verbose output")
	return describeCmd
}

func revisionListCommand(ctx cli.Context) *cobra.Command {
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "Show list of control plane and gateway revisions that are currently installed in cluster",
		Example: `  # View summary of revisions installed in the current cluster
  # which can be overridden with --context parameter.
  istioctl x revision list

  # View list of revisions including customizations, istiod and gateway pods
  istioctl x revision list -v
`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if !revArgs.verbose && revArgs.manifestsPath != "" {
				return fmt.Errorf("manifest path should only be specified with -v")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeClient, err := ctx.CLIClient()
			if err != nil {
				return fmt.Errorf("failed to create k8s client: %w", err)
			}
			logger := clog.NewConsoleLogger(cmd.OutOrStdout(), cmd.ErrOrStderr(), root.Scope)
			return revisionList(ctx, kubeClient, cmd.OutOrStdout(), &revArgs, logger)
		},
	}
	return listCmd
}

func revisionList(ctx cli.Context, kubeClient kube.CLIClient, writer io.Writer, args *revisionArgs, logger clog.Logger) error {
	revisions, err := tag.ListRevisionDescriptions(kubeClient)
	if err != nil {
		return fmt.Errorf("cannot list revisions: %v", err)
	}

	// Get a list of all CRs which are installed in this cluster
	iopcrs, err := getAllMergedIstioOperatorCRs(kubeClient, logger)
	if err != nil {
		return fmt.Errorf("error while listing IstioOperator CRs: %v", err)
	}
	for _, iop := range iopcrs {
		rev := renderWithDefault(iop.Spec.GetRevision(), "default")
		if ri := revisions[rev]; ri == nil {
			revisions[rev] = &tag.RevisionDescription{}
		}
		cs, err := getEnabledUserFacingComponents(iop)
		if err != nil {
			logger.LogAndErrorf("error while getting IstioOperator %s/%s Components: %v", iop.Namespace, iop.Name, err)
		}
		iopInfo := &tag.IstioOperatorCRInfo{
			IOP:            iop,
			Namespace:      iop.GetNamespace(),
			Name:           iop.GetName(),
			Profile:        manifest.GetProfile(iop),
			Components:     cs,
			Customizations: nil,
		}
		if args.verbose {
			iopInfo.Customizations, err = getDiffs(iop, revArgs.manifestsPath,
				profileWithDefault(iop.Spec.GetProfile()), logger)
			if err != nil {
				return fmt.Errorf("error while finding customizations: %v", err)
			}
		}
		revisions[rev].IstioOperatorCRs = append(revisions[rev].IstioOperatorCRs, iopInfo)
	}

	if args.verbose {
		for rev, desc := range revisions {
			revClient, err := ctx.CLIClientWithRevision(rev)
			if err != nil {
				return fmt.Errorf("failed to get revision based kubeclient for revision: %s", rev)
			}
			if err = annotateWithControlPlanePodInfo(desc, revClient, ctx.IstioNamespace()); err != nil {
				return fmt.Errorf("failed to get control plane pods for revision: %s", rev)
			}
			if err = annotateWithGatewayInfo(desc, revClient, ctx.IstioNamespace()); err != nil {
				return fmt.Errorf("failed to get gateway pods for revision: %s", rev)
			}
		}
	}

	switch revArgs.output {
	case istioctlutil.JSONFormat:
		return tag.PrintJSON(writer, revisions)
	case istioctlutil.TableFormat:
		if len(revisions) == 0 {
			_, err = fmt.Fprintln(writer, "No Istio installation found.\n"+
				"No IstioOperator CR or sidecar injectors found")
			return err
		}
		return printRevisionInfoTable(writer, args.verbose, revisions)
	default:
		return fmt.Errorf("unknown format: %s", revArgs.output)
	}
}

func printRevisionInfoTable(writer io.Writer, verbose bool, revisions map[string]*tag.RevisionDescription) error {
	if err := printSummaryTable(writer, verbose, revisions); err != nil {
		return fmt.Errorf("failed to print summary table: %v", err)
	}
	if verbose {
		if err := printControlPlaneSummaryTable(writer, revisions); err != nil {
			return fmt.Errorf("failed to print control plane table: %v", err)
		}
		if err := printGatewaySummaryTable(writer, revisions); err != nil {
			return fmt.Errorf("failed to print gateway summary table: %v", err)
		}
	}
	return nil
}

func printControlPlaneSummaryTable(w io.Writer, revisions map[string]*tag.RevisionDescription) error {
	fmt.Fprintf(w, "\nCONTROL PLANE:\n")
	tw := new(tabwriter.Writer).Init(w, 0, 8, 1, ' ', 0)
	fmt.Fprintf(tw, "REVISION\tISTIOD-ENABLED\tCONTROL-PLANE-PODS\n")
	for rev, rd := range revisions {
		isIstiodEnabled := false
	outer:
		for _, iop := range rd.IstioOperatorCRs {
			for _, c := range iop.Components {
				if c == name2.UserFacingComponentName(name2.PilotComponentName) {
					isIstiodEnabled = true
					break outer
				}
			}
		}
		maxRows := max(1, len(rd.ControlPlanePods))
		for i := 0; i < maxRows; i++ {
			rowRev, rowEnabled, rowPod := "", "NO", ""
			if i == 0 {
				rowRev = rev
				if isIstiodEnabled {
					rowEnabled = "YES"
				}
			}
			switch {
			case i < len(rd.ControlPlanePods):
				rowPod = fmt.Sprintf("%s/%s",
					rd.ControlPlanePods[i].Namespace,
					rd.ControlPlanePods[i].Name)
			case i == 0 && len(rd.ControlPlanePods) == 0:
				rowPod = "<no-istiod>"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\n", rowRev, rowEnabled, rowPod)
		}
	}
	return tw.Flush()
}

func printGatewaySummaryTable(w io.Writer, revisions map[string]*tag.RevisionDescription) error {
	if err := printGatewaySummaryTableWithType(ingressGWTyp, w, revisions); err != nil {
		return fmt.Errorf("error while printing ingress gateway summary: %v", err)
	}
	if err := printGatewaySummaryTableWithType(egressGWTyp, w, revisions); err != nil {
		return fmt.Errorf("error while printing egress gateway summary: %v", err)
	}
	return nil
}

type gwType int

const (
	ingressGWTyp gwType = 0
	egressGWTyp  gwType = 1
)

func printGatewaySummaryTableWithType(gT gwType, w io.Writer, revisions map[string]*tag.RevisionDescription) error {
	gTypeName := name2.IngressComponentName
	noEnabled := "<no-ingress-enabled>"
	noPod := "<no-ingress-pod>"
	if gT == ingressGWTyp {
		fmt.Fprintf(w, "\nINGRESS GATEWAYS:\n")
	} else {
		gTypeName = name2.EgressComponentName
		fmt.Fprintf(w, "\nEGRESS GATEWAYS:\n")
		noEnabled = "<no-egress-enabled>"
		noPod = "<no-egress-pod>"
	}

	getRowPodAndEnabled := func(index int, enableGws []string, gwPods []*tag.PodFilteredInfo) (rEnabled string, rPod string) {
		if index == 0 && len(enableGws) == 0 {
			rEnabled = noEnabled
		} else if index < len(enableGws) {
			rEnabled = enableGws[index]
		}
		if index == 0 && len(gwPods) == 0 {
			rPod = noPod
		} else if index < len(gwPods) {
			rPod = fmt.Sprintf("%s/%s",
				gwPods[index].Namespace,
				gwPods[index].Name)
		}
		return
	}

	prefix := name2.UserFacingComponentName(gTypeName) + ":"
	tw := new(tabwriter.Writer).Init(w, 0, 8, 1, ' ', 0)
	fmt.Fprintf(tw, "REVISION\tDECLARED-GATEWAYS\tGATEWAY-POD\n")
	for rev, rd := range revisions {
		enabledGateways := []string{}
		for _, iop := range rd.IstioOperatorCRs {
			for _, c := range iop.Components {
				if strings.HasPrefix(c, prefix) {
					enabledGateways = append(enabledGateways, strings.TrimPrefix(c, prefix))
				}
			}
		}
		gwPods := rd.IngressGatewayPods
		if gT == egressGWTyp {
			gwPods = rd.EgressGatewayPods
		}
		maxRows := max(max(1, len(enabledGateways)), len(gwPods))
		for i := 0; i < maxRows; i++ {
			var rowRev, rowEnabled, rowPod string
			if i == 0 {
				rowRev = rev
			}
			rowEnabled, rowPod = getRowPodAndEnabled(i, enabledGateways, gwPods)
			fmt.Fprintf(tw, "%s\t%s\t%s\n", rowRev, rowEnabled, rowPod)
		}
	}
	return tw.Flush()
}

//nolint:errcheck
func printSummaryTable(writer io.Writer, verbose bool, revisions map[string]*tag.RevisionDescription) error {
	tw := new(tabwriter.Writer).Init(writer, 0, 8, 1, ' ', 0)
	if verbose {
		_, _ = tw.Write([]byte("REVISION\tTAG\tISTIO-OPERATOR-CR\tPROFILE\tREQD-COMPONENTS\tCUSTOMIZATIONS\n"))
	} else {
		_, _ = tw.Write([]byte("REVISION\tTAG\tISTIO-OPERATOR-CR\tPROFILE\tREQD-COMPONENTS\n"))
	}
	for r, ri := range revisions {
		rowID, tags := 0, []string{}
		for _, wh := range ri.Webhooks {
			if wh.Tag != "" {
				tags = append(tags, wh.Tag)
			}
		}
		if len(tags) == 0 {
			tags = append(tags, "<no-tag>")
		}
		for _, iop := range ri.IstioOperatorCRs {
			profile := profileWithDefault(iop.Profile)
			components := iop.Components
			qualifiedName := fmt.Sprintf("%s/%s", iop.Namespace, iop.Name)

			customizations := []string{}
			for _, c := range iop.Customizations {
				customizations = append(customizations, fmt.Sprintf("%s=%s", c.Path, c.Value))
			}
			if len(customizations) == 0 {
				customizations = append(customizations, "<no-customization>")
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
				if rowID < len(tags) {
					rowTag = tags[rowID]
				}
				if rowID == 0 {
					rowRev = r
				}
				if verbose {
					fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
						rowRev, rowTag, rowIopName, rowProfile, rowComp, rowCust)
				} else {
					fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
						rowRev, rowTag, rowIopName, rowProfile, rowComp)
				}
				rowID++
			}
		}
		for rowID < len(tags) {
			var rowRev, rowTag, rowIopName string
			if rowID == 0 {
				rowRev = r
			}
			if rowID == 0 {
				rowIopName = "<no-iop>"
			}
			rowTag = tags[rowID]
			if verbose {
				fmt.Fprintf(tw, "%s\t%s\t%s\t \t \t \n", rowRev, rowTag, rowIopName)
			} else {
				fmt.Fprintf(tw, "%s\t%s\t%s\t \t \n", rowRev, rowTag, rowIopName)
			}
			rowID++
		}
	}
	return tw.Flush()
}

func getAllMergedIstioOperatorCRs(client kube.CLIClient, logger clog.Logger) ([]*iopv1alpha1.IstioOperator, error) {
	ucrs, err := client.Dynamic().Resource(istioOperatorGVR).
		List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return []*iopv1alpha1.IstioOperator{}, fmt.Errorf("cannot retrieve IstioOperator CRs: %v", err)
	}
	iopCRs := []*iopv1alpha1.IstioOperator{}
	for _, u := range ucrs.Items {
		u.SetCreationTimestamp(metav1.Time{})
		u.SetManagedFields([]metav1.ManagedFieldsEntry{})
		iop, err := operator_istio.UnmarshalIstioOperator(util.ToYAML(u.Object), true)
		if err != nil {
			return []*iopv1alpha1.IstioOperator{},
				fmt.Errorf("error while converting to IstioOperator CR - %s/%s: %v",
					u.GetNamespace(), u.GetName(), err)
		}
		by, err := yaml.Marshal(iop)
		if err != nil {
			return nil, fmt.Errorf("error while marshaling IstioOperator CR - %s/%s to yaml: %v", u.GetNamespace(), u.GetName(), err)
		}
		profile := manifest.GetProfile(iop)
		mergedIOP, err := manifest.GetMergedIOP(string(by), profile, "", "", client, logger)
		if err != nil {
			return nil, fmt.Errorf("error while merging IstioOperator CR - %s/%s with profile %s: %v", u.GetNamespace(), u.GetName(), profile, err)
		}
		iopCRs = append(iopCRs, mergedIOP)
	}
	return iopCRs, nil
}

func printRevisionDescription(ctx cli.Context, w io.Writer, args *revisionArgs, logger clog.Logger, istioNamespace string) error {
	revision := args.name
	client, err := ctx.CLIClientWithRevision(revision)
	if err != nil {
		return fmt.Errorf("cannot create kubeclient: %v", err)
	}
	allIops, err := getAllMergedIstioOperatorCRs(client, logger)
	if err != nil {
		return fmt.Errorf("error while fetching IstioOperator CR: %v", err)
	}
	iopsInCluster := getIOPWithRevision(allIops, revision)
	allWebhooks, err := tag.Webhooks(context.Background(), client)
	if err != nil {
		return fmt.Errorf("error while fetching mutating webhook configurations: %v", err)
	}
	webhooks := filterWebhooksWithRevision(allWebhooks, revision)
	revDescription := getBasicRevisionDescription(iopsInCluster, webhooks, logger)
	if err = annotateWithIOPCustomization(revDescription, args.manifestsPath, logger); err != nil {
		return err
	}
	if err = annotateWithControlPlanePodInfo(revDescription, client, istioNamespace); err != nil {
		return err
	}
	if err = annotateWithGatewayInfo(revDescription, client, istioNamespace); err != nil {
		return err
	}
	if !revisionExists(revDescription) {
		return fmt.Errorf("revision %s is not present", revision)
	}
	if args.verbose {
		revAliases := []string{revision}
		for _, wh := range revDescription.Webhooks {
			revAliases = append(revAliases, wh.Tag)
		}
		if err = annotateWithNamespaceAndPodInfo(client, revDescription, revAliases); err != nil {
			return err
		}
	}
	switch revArgs.output {
	case istioctlutil.JSONFormat:
		return tag.PrintJSON(w, revDescription)
	case istioctlutil.TableFormat:
		sections := defaultSections
		if args.verbose {
			sections = verboseSections
		}
		return printTable(w, sections, revDescription)
	default:
		return fmt.Errorf("unknown format %s", revArgs.output)
	}
}

func revisionExists(revDescription *tag.RevisionDescription) bool {
	return len(revDescription.IstioOperatorCRs) != 0 ||
		len(revDescription.Webhooks) != 0 ||
		len(revDescription.ControlPlanePods) != 0 ||
		len(revDescription.IngressGatewayPods) != 0 ||
		len(revDescription.EgressGatewayPods) != 0
}

func annotateWithNamespaceAndPodInfo(kubeClient kube.CLIClient, revDescription *tag.RevisionDescription, revisionAliases []string) error {
	nsMap := make(map[string]*tag.NsInfo)
	for _, ra := range revisionAliases {
		pods, err := getPodsWithSelector(kubeClient, "", &metav1.LabelSelector{
			MatchLabels: map[string]string{
				label.IoIstioRev.Name: ra,
			},
		})
		if err != nil {
			return fmt.Errorf("failed to fetch pods for rev/tag: %s: %v", ra, err)
		}
		for _, po := range pods {
			fpo := getFilteredPodInfo(&po)
			_, ok := nsMap[po.Namespace]
			if !ok {
				nsMap[po.Namespace] = &tag.NsInfo{
					Name: po.Namespace,
					Pods: []*tag.PodFilteredInfo{fpo},
				}
			} else {
				nsMap[po.Namespace].Pods = append(nsMap[po.Namespace].Pods, fpo)
			}
		}
	}
	revDescription.NamespaceSummary = nsMap
	return nil
}

func annotateWithGatewayInfo(revDescription *tag.RevisionDescription, client kube.CLIClient, istioNamespace string) error {
	ingressPods, err := getPodsForComponent(client, "IngressGateways", istioNamespace)
	if err != nil {
		return fmt.Errorf("error while fetching ingress gateway pods: %v", err)
	}
	revDescription.IngressGatewayPods = transformToFilteredPodInfo(ingressPods)
	egressPods, err := getPodsForComponent(client, "EgressGateways", istioNamespace)
	if err != nil {
		return fmt.Errorf("error while fetching egress gateway pods: %v", err)
	}
	revDescription.EgressGatewayPods = transformToFilteredPodInfo(egressPods)
	return nil
}

func annotateWithControlPlanePodInfo(revDescription *tag.RevisionDescription, client kube.CLIClient, istioNamespace string) error {
	controlPlanePods, err := getPodsForComponent(client, "Pilot", istioNamespace)
	if err != nil {
		return fmt.Errorf("error while fetching control plane pods: %v", err)
	}
	revDescription.ControlPlanePods = transformToFilteredPodInfo(controlPlanePods)
	return nil
}

func annotateWithIOPCustomization(revDesc *tag.RevisionDescription, manifestsPath string, logger clog.Logger) error {
	for _, cr := range revDesc.IstioOperatorCRs {
		cust, err := getDiffs(cr.IOP, manifestsPath, profileWithDefault(cr.IOP.Spec.GetProfile()), logger)
		if err != nil {
			return fmt.Errorf("error while computing customization for %s/%s: %v",
				cr.Name, cr.Namespace, err)
		}
		cr.Customizations = cust
	}
	return nil
}

func getBasicRevisionDescription(iopCRs []*iopv1alpha1.IstioOperator,
	mutatingWebhooks []admitv1.MutatingWebhookConfiguration, logger clog.Logger,
) *tag.RevisionDescription {
	revDescription := &tag.RevisionDescription{
		IstioOperatorCRs: []*tag.IstioOperatorCRInfo{},
		Webhooks:         []*tag.MutatingWebhookConfigInfo{},
	}
	for _, iop := range iopCRs {
		cs, err := getEnabledUserFacingComponents(iop)
		if err != nil {
			logger.LogAndErrorf("error while getting IstioOperator %s/%s Components: %v", iop.Namespace, iop.Name, err)
		}
		revDescription.IstioOperatorCRs = append(revDescription.IstioOperatorCRs, &tag.IstioOperatorCRInfo{
			IOP:            iop,
			Namespace:      iop.Namespace,
			Name:           iop.Name,
			Profile:        manifest.GetProfile(iop),
			Components:     cs,
			Customizations: nil,
		})
	}
	for _, mwh := range mutatingWebhooks {
		revDescription.Webhooks = append(revDescription.Webhooks, &tag.MutatingWebhookConfigInfo{
			Name:     mwh.Name,
			Revision: renderWithDefault(mwh.Labels[label.IoIstioRev.Name], "default"),
			Tag:      mwh.Labels[tag.IstioTagLabel],
		})
	}
	return revDescription
}

func filterWebhooksWithRevision(webhooks []admitv1.MutatingWebhookConfiguration, revision string) []admitv1.MutatingWebhookConfiguration {
	whFiltered := []admitv1.MutatingWebhookConfiguration{}
	for _, wh := range webhooks {
		if wh.GetLabels()[label.IoIstioRev.Name] == revision {
			whFiltered = append(whFiltered, wh)
		}
	}
	return whFiltered
}

func transformToFilteredPodInfo(pods []corev1.Pod) []*tag.PodFilteredInfo {
	pfilInfo := []*tag.PodFilteredInfo{}
	for _, p := range pods {
		pfilInfo = append(pfilInfo, getFilteredPodInfo(&p))
	}
	return pfilInfo
}

func getFilteredPodInfo(pod *corev1.Pod) *tag.PodFilteredInfo {
	return &tag.PodFilteredInfo{
		Namespace: pod.Namespace,
		Name:      pod.Name,
		Address:   pod.Status.PodIP,
		Status:    pod.Status.Phase,
		Age:       translateTimestampSince(pod.CreationTimestamp),
	}
}

func printTable(w io.Writer, sections []string, desc *tag.RevisionDescription) error {
	errs := &multierror.Error{}
	tablePrintFuncs := map[string]func(io.Writer, *tag.RevisionDescription) error{
		istioOperatorCRSection: printIstioOperatorCRInfo,
		webhooksSection:        printWebhookInfo,
		controlPlaneSection:    printControlPlane,
		gatewaysSection:        printGateways,
		podsSection:            printPodsInfo,
	}
	for _, s := range sections {
		f := tablePrintFuncs[s]
		if f == nil {
			errs = multierror.Append(fmt.Errorf("unknown section: %s", s), errs.Errors...)
			continue
		}
		err := f(w, desc)
		if err != nil {
			errs = multierror.Append(fmt.Errorf("error in section %s: %v", s, err))
		}
	}
	return errs.ErrorOrNil()
}

func printPodsInfo(w io.Writer, desc *tag.RevisionDescription) error {
	fmt.Fprintf(w, "\nPODS:\n")
	if len(desc.NamespaceSummary) == 0 {
		fmt.Fprintln(w, "No pods for this revision")
		return nil
	}
	for ns, nsi := range desc.NamespaceSummary {
		fmt.Fprintf(w, "NAMESPACE %s: (%d)\n", ns, len(nsi.Pods))
		if err := printPodTable(w, nsi.Pods); err != nil {
			return fmt.Errorf("error while printing pod table: %v", err)
		}
		fmt.Fprintln(w)
	}
	return nil
}

//nolint:unparam
func printIstioOperatorCRInfo(w io.Writer, desc *tag.RevisionDescription) error {
	fmt.Fprintf(w, "\nISTIO-OPERATOR CUSTOM RESOURCE: (%d)", len(desc.IstioOperatorCRs))
	if len(desc.IstioOperatorCRs) == 0 {
		if len(desc.Webhooks) > 0 {
			fmt.Fprintln(w, "\nThere are webhooks and Istiod could be external to the cluster")
		} else {
			// Ideally, it should not come here
			fmt.Fprintln(w, "\nNo CRs found.")
		}
		return nil
	}
	for i, iop := range desc.IstioOperatorCRs {
		fmt.Fprintf(w, "\n%d. %s/%s\n", i+1, iop.Namespace, iop.Name)
		fmt.Fprintf(w, "  COMPONENTS:\n")
		if len(iop.Components) > 0 {
			for _, c := range iop.Components {
				fmt.Fprintf(w, "  - %s\n", c)
			}
		} else {
			fmt.Fprintf(w, "  no enabled components\n")
		}

		// For each IOP, print all customizations for it
		fmt.Fprintf(w, "  CUSTOMIZATIONS:\n")
		if len(iop.Customizations) > 0 {
			for _, customization := range iop.Customizations {
				fmt.Fprintf(w, "  - %s=%s\n", customization.Path, customization.Value)
			}
		} else {
			fmt.Fprintf(w, "  <no-customizations>\n")
		}
	}
	return nil
}

//nolint:errcheck
func printWebhookInfo(w io.Writer, desc *tag.RevisionDescription) error {
	fmt.Fprintf(w, "\nMUTATING WEBHOOK CONFIGURATIONS: (%d)\n", len(desc.Webhooks))
	if len(desc.Webhooks) == 0 {
		fmt.Fprintln(w, "No mutating webhook found for this revision. Something could be wrong with installation")
		return nil
	}
	tw := new(tabwriter.Writer).Init(w, 0, 0, 1, ' ', 0)
	tw.Write([]byte("WEBHOOK\tTAG\n"))
	for _, wh := range desc.Webhooks {
		tw.Write([]byte(fmt.Sprintf("%s\t%s\n", wh.Name, renderWithDefault(wh.Tag, "<no-tag>"))))
	}
	return tw.Flush()
}

func printControlPlane(w io.Writer, desc *tag.RevisionDescription) error {
	fmt.Fprintf(w, "\nCONTROL PLANE PODS (ISTIOD): (%d)\n", len(desc.ControlPlanePods))
	if len(desc.ControlPlanePods) == 0 {
		if len(desc.Webhooks) > 0 {
			fmt.Fprintln(w, "No Istiod found in this cluster for the revision. "+
				"However there are webhooks. It is possible that Istiod is external to this cluster or "+
				"perhaps it is not uninstalled properly")
		} else {
			fmt.Fprintln(w, "No Istiod or the webhook found in this cluster for the revision. Something could be wrong")
		}
		return nil
	}
	return printPodTable(w, desc.ControlPlanePods)
}

func printGateways(w io.Writer, desc *tag.RevisionDescription) error {
	if err := printIngressGateways(w, desc); err != nil {
		return fmt.Errorf("error while printing ingress-gateway info: %v", err)
	}
	if err := printEgressGateways(w, desc); err != nil {
		return fmt.Errorf("error while printing egress-gateway info: %v", err)
	}
	return nil
}

func printIngressGateways(w io.Writer, desc *tag.RevisionDescription) error {
	fmt.Fprintf(w, "\nINGRESS GATEWAYS: (%d)\n", len(desc.IngressGatewayPods))
	if len(desc.IngressGatewayPods) == 0 {
		if ingressGatewayEnabled(desc) {
			fmt.Fprintln(w, "Ingress gateway is enabled for this revision. However there are no such pods. "+
				"It could be that it is replaced by ingress-gateway from another revision (as it is still upgraded in-place) "+
				"or it could be some issue with installation")
		} else {
			fmt.Fprintln(w, "Ingress gateway is disabled for this revision")
		}
		return nil
	}
	if !ingressGatewayEnabled(desc) {
		fmt.Fprintln(w, "WARNING: Ingress gateway is not enabled for this revision.")
	}
	return printPodTable(w, desc.IngressGatewayPods)
}

func printEgressGateways(w io.Writer, desc *tag.RevisionDescription) error {
	fmt.Fprintf(w, "\nEGRESS GATEWAYS: (%d)\n", len(desc.EgressGatewayPods))
	if len(desc.EgressGatewayPods) == 0 {
		if egressGatewayEnabled(desc) {
			fmt.Fprintln(w, "Egress gateway is enabled for this revision. However there are no such pods. "+
				"It could be that it is replaced by egress-gateway from another revision (as it is still upgraded in-place) "+
				"or it could be some issue with installation")
		} else {
			fmt.Fprintln(w, "Egress gateway is disabled for this revision")
		}
		return nil
	}
	if !egressGatewayEnabled(desc) {
		fmt.Fprintln(w, "WARNING: Egress gateway is not enabled for this revision.")
	}
	return printPodTable(w, desc.EgressGatewayPods)
}

func ingressGatewayEnabled(desc *tag.RevisionDescription) bool {
	return gatewayTypeEnabled(desc, name2.UserFacingComponentName(name2.IngressComponentName))
}

func egressGatewayEnabled(desc *tag.RevisionDescription) bool {
	return gatewayTypeEnabled(desc, name2.UserFacingComponentName(name2.EgressComponentName))
}

func gatewayTypeEnabled(desc *tag.RevisionDescription, gwComponentName string) bool {
	for _, iopdesc := range desc.IstioOperatorCRs {
		for _, comp := range iopdesc.Components {
			if strings.HasPrefix(comp, gwComponentName) {
				return true
			}
		}
	}
	return false
}

func getIOPWithRevision(iops []*iopv1alpha1.IstioOperator, revision string) []*iopv1alpha1.IstioOperator {
	filteredIOPs := []*iopv1alpha1.IstioOperator{}
	for _, iop := range iops {
		if iop.Spec == nil {
			continue
		}
		if iop.Spec.Revision == revision || (revision == "default" && iop.Spec.Revision == "") {
			filteredIOPs = append(filteredIOPs, iop)
		}
	}
	return filteredIOPs
}

func printPodTable(w io.Writer, pods []*tag.PodFilteredInfo) error {
	podTableW := new(tabwriter.Writer).Init(w, 0, 0, 1, ' ', 0)
	fmt.Fprintln(podTableW, "NAMESPACE\tNAME\tADDRESS\tSTATUS\tAGE")
	for _, pod := range pods {
		fmt.Fprintf(podTableW, "%s\t%s\t%s\t%s\t%s\n",
			pod.Namespace, pod.Name, pod.Address, pod.Status, pod.Age)
	}
	return podTableW.Flush()
}

func getEnabledUserFacingComponents(iop *iopv1alpha1.IstioOperator) ([]string, error) {
	if iop == nil {
		return nil, nil
	}

	var enabledComponents []string
	if iop.Spec.Components != nil {
		for _, c := range name2.AllCoreComponentNames {
			enabled, err := translate.IsComponentEnabledInSpec(c, iop.Spec)
			if err != nil {
				return nil, fmt.Errorf("error while resolving whether the component is enabled: %v", err)
			}
			if enabled {
				enabledComponents = append(enabledComponents, name2.UserFacingComponentName(c))
			}
		}
		for _, c := range iop.Spec.Components.IngressGateways {
			if c.Enabled.GetValue() {
				enabledComponents = append(enabledComponents, fmt.Sprintf("%s:%s", name2.UserFacingComponentName(name2.IngressComponentName), c.Name))
			}
		}
		for _, c := range iop.Spec.Components.EgressGateways {
			if c.Enabled.GetValue() {
				enabledComponents = append(enabledComponents, fmt.Sprintf("%s:%s", name2.UserFacingComponentName(name2.EgressComponentName), c.Name))
			}
		}
	}

	return enabledComponents, nil
}

func getPodsForComponent(client kube.CLIClient, component string, istioNamespace string) ([]corev1.Pod, error) {
	return getPodsWithSelector(client, istioNamespace, &metav1.LabelSelector{
		MatchLabels: map[string]string{
			label.IoIstioRev.Name:        client.Revision(),
			label.OperatorComponent.Name: component,
		},
	})
}

func getPodsWithSelector(client kube.CLIClient, ns string, selector *metav1.LabelSelector) ([]corev1.Pod, error) {
	labelSelector, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return []corev1.Pod{}, err
	}
	podList, err := client.Kube().CoreV1().Pods(ns).List(context.TODO(),
		metav1.ListOptions{LabelSelector: labelSelector.String()})
	if err != nil {
		return []corev1.Pod{}, err
	}
	return podList.Items, nil
}

func getDiffs(installed *iopv1alpha1.IstioOperator, manifestsPath, profile string, l clog.Logger) ([]tag.IopDiff, error) {
	setFlags := []string{"profile=" + profile}
	if manifestsPath != "" {
		setFlags = append(setFlags, fmt.Sprintf("installPackagePath=%s", manifestsPath))
	}
	_, base, err := manifest.GenerateConfig([]string{}, setFlags, true, nil, l)
	if err != nil {
		return []tag.IopDiff{}, err
	}
	mapInstalled, err := config.ToMap(installed.Spec)
	if err != nil {
		return []tag.IopDiff{}, err
	}
	mapBase, err := config.ToMap(base.Spec)
	if err != nil {
		return []tag.IopDiff{}, err
	}
	return diffWalk("", "", mapInstalled, mapBase)
}

// TODO(su225): Improve this and write tests for it.
func diffWalk(path, separator string, installed any, base any) ([]tag.IopDiff, error) {
	switch v := installed.(type) {
	case map[string]any:
		accum := make([]tag.IopDiff, 0)
		typedOrig, ok := base.(map[string]any)
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
	case []any:
		accum := make([]tag.IopDiff, 0)
		typedOrig, ok := base.([]any)
		if ok {
			for idx, vv := range v {
				var baseMap any
				if idx < len(typedOrig) {
					baseMap = typedOrig[idx]
				}
				indexwalk, err := diffWalk(fmt.Sprintf("%s[%d]", path, idx), ".", vv, baseMap)
				if err != nil {
					return accum, err
				}
				accum = append(accum, indexwalk...)
			}
		}
		return accum, nil
	case string:
		if v != base && base != nil {
			return []tag.IopDiff{{Path: path, Value: fmt.Sprintf("%v", v)}}, nil
		}
	default:
		if v != base && base != nil {
			return []tag.IopDiff{{Path: path, Value: fmt.Sprintf("%v", v)}}, nil
		}
	}
	return []tag.IopDiff{}, nil
}

func renderWithDefault(s, def string) string {
	if s != "" {
		return s
	}
	return def
}

func profileWithDefault(profile string) string {
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
func translateTimestampSince(timestamp metav1.Time) string {
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
