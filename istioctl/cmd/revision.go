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
	"text/tabwriter"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
	admit_v1 "k8s.io/api/admissionregistration/v1"
	v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apimachinery_schema "k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/duration"
	"k8s.io/apimachinery/pkg/util/validation"

	"istio.io/api/label"
	"istio.io/api/operator/v1alpha1"
	"istio.io/istio/operator/cmd/mesh"
	operator_istio "istio.io/istio/operator/pkg/apis/istio"
	iopv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/manifest"
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
	IstioOperatorCRSection = "ISTIO-OPERATOR-CR"
	ControlPlaneSection    = "CONTROL-PLANE"
	GatewaysSection        = "GATEWAYS"
	WebhooksSection        = "MUTATING-WEBHOOKS"
	PodsSection            = "PODS"

	// TODO: This should be moved to istio/api:label package
	istioTag = "istio.io/tag"

	jsonFormat  = "json"
	tableFormat = "table"
)

var (
	validFormats = []string{tableFormat, jsonFormat}

	defaultSections = []string{
		IstioOperatorCRSection,
		WebhooksSection,
		ControlPlaneSection,
		GatewaysSection,
	}

	verboseSections = []string{
		IstioOperatorCRSection,
		WebhooksSection,
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
		Long:   "The revision command provides a revision centric view of istio deployments. " +
			"It provides insight into IstioOperator CRs defining the revision, istiod and gateway pods " +
			"which are part of deployment of a particular revision.",
		Aliases: []string{"rev"},
	}
	revisionCmd.PersistentFlags().StringVarP(&revArgs.manifestsPath, "manifests", "d", "", mesh.ManifestsFlagHelpStr)
	revisionCmd.PersistentFlags().BoolVarP(&revArgs.verbose, "verbose", "v", false, "Enable verbose output")
	revisionCmd.PersistentFlags().StringVarP(&revArgs.output, "output", "o", "table", "Output format for revision description")

	revisionCmd.AddCommand(revisionListCommand())
	revisionCmd.AddCommand(revisionDescribeCommand())
	return revisionCmd
}

func revisionDescribeCommand() *cobra.Command {
	describeCmd := &cobra.Command{
		Use:     "describe",
		Example: `  # View the details of a revision named 'canary'    
  istioctl experimental revision describe canary

  # View the details of a revision named 'canary' and also the pods
  # under that particular revision
  istioctl experimental revision describe canary -v

  # Get details about a revision in json format (default format is human-friendly table format)
  istioctl experimental revision describe canary -v -o json 
`,
		Short:   "Show details of a revision - customizations, number of pods pointing to it, istiod, gateways etc",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			revArgs.name = args[0]
			if len(args) == 0 {
				return fmt.Errorf("revision must be specified")
			}
			if len(args) > 1 {
				return fmt.Errorf("exactly 1 revision should be specified")
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
			if errs := validation.IsDNS1123Label(revArgs.name); len(errs) > 0 {
				return fmt.Errorf("%s - invalid revision format: %v", revArgs.name, errs)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := clog.NewConsoleLogger(cmd.OutOrStdout(), cmd.ErrOrStderr(), scope)
			return printRevisionDescription(cmd.OutOrStdout(), &revArgs, logger)
		},
	}
	describeCmd.Flags().BoolVarP(&revArgs.verbose, "verbose", "v", false, "Enable verbose output")
	return describeCmd
}

func revisionListCommand() *cobra.Command {
	listCmd := &cobra.Command{
		Use:     "list",
		Short:   "Show list of control plane and gateway revisions that are currently installed in cluster",
		Example: `  # View summary of revisions installed in the current cluster
  # which can be overriden with --context parameter. 
  istioctl experimental revision list

  # View summary of revisions, along with customization in each IstioOperator CR,
  # control plane and gateway pods
  istioctl experimental revision list -v

  # View summary of revisions in json format
  istioctl experimental revision list -o json
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := clog.NewConsoleLogger(cmd.OutOrStdout(), cmd.ErrOrStderr(), scope)
			return revisionList(cmd.OutOrStdout(), &revArgs, logger)
		},
	}
	return listCmd
}

type podFilteredInfo struct {
	Namespace string      `json:"namespace"`
	Name      string      `json:"name"`
	Address   string      `json:"address"`
	Status    v1.PodPhase `json:"status"`
	Age       string      `json:"age"`
}

type istioOperatorCRInfo struct {
	IOP            *iopv1alpha1.IstioOperator `json:"-"`
	Namespace      string                     `json:"namespace"`
	Name           string                     `json:"name"`
	Profile        string                     `json:"profile"`
	Components     []string                   `json:"components,omitempty"`
	Customizations []iopDiff                  `json:"customizations,omitempty"`
}

type mutatingWebhookConfigInfo struct {
	Name     string `json:"name"`
	Revision string `json:"revision"`
	Tag      string `json:"tag,omitempty"`
}

type nsInfo struct {
	Name string            `json:"name,omitempty"`
	Pods []podFilteredInfo `json:"pods,omitempty"`
}

type revisionDescription struct {
	IstioOperatorCRs   []istioOperatorCRInfo       `json:"istio_operator_crs,omitempty"`
	Webhooks           []mutatingWebhookConfigInfo `json:"webhooks,omitempty"`
	ControlPlanePods   []podFilteredInfo           `json:"control_plane_pods,omitempty"`
	IngressGatewayPods []podFilteredInfo           `json:"ingess_gateways,omitempty"`
	EgressGatewayPods  []podFilteredInfo           `json:"egress_gateways,omitempty"`
	NamespaceSummary   map[string]*nsInfo          `json:"namespace_summary,omitempty"`
}

func revisionList(writer io.Writer, args *revisionArgs, logger clog.Logger) error {
	client, err := newKubeClient(kubeconfig, configContext)
	if err != nil {
		return fmt.Errorf("cannot create kubeclient for kubeconfig=%s, context=%s: %v",
			kubeconfig, configContext, err)
	}

	revisions := map[string]*revisionDescription{}

	// Get a list of control planes which are installed in remote clusters
	// In this case, it is possible that they only have webhooks installed.
	webhooks, err := getWebhooks(context.Background(), client)
	if err != nil {
		return fmt.Errorf("error while listing mutating webhooks: %v", err)
	}
	for _, hook := range webhooks {
		rev := renderWithDefault(hook.GetLabels()[label.IstioRev], "default")
		tag := hook.GetLabels()[istioTag]
		ri, revPresent := revisions[rev]
		if revPresent {
			if tag != "" {
				ri.Webhooks = append(ri.Webhooks, mutatingWebhookConfigInfo{
					Name:     hook.Name,
					Revision: rev,
					Tag:      tag,
				})
			}
		} else {
			revisions[rev] = &revisionDescription{
				IstioOperatorCRs: []istioOperatorCRInfo{},
				Webhooks:         []mutatingWebhookConfigInfo{{Name: hook.Name, Revision: rev, Tag: tag}},
			}
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
			iopInfo := istioOperatorCRInfo{
				Namespace:      iop.GetNamespace(),
				Name:           iop.GetName(),
				Profile:        iop.Spec.GetProfile(),
				Components:     getEnabledComponents(iop.Spec),
				Customizations: nil,
			}
			if args.verbose {
				iopInfo.Customizations, err = getDiffs(iop, revArgs.manifestsPath, iop.Spec.GetProfile(), logger)
				if err != nil {
					return fmt.Errorf("error while finding customizations: %v", err)
				}
			}
			ri.IstioOperatorCRs = append(ri.IstioOperatorCRs, iopInfo)
		}
	}

	if args.verbose {
		for rev, desc := range revisions {
			revClient, err := newKubeClientWithRevision(kubeconfig, configContext, rev)
			if err != nil {
				return fmt.Errorf("failed to get revision based kubeclient for revision: %s", rev)
			}
			if err = enrichWithControlPlanePodInfo(desc, revClient); err != nil {
				return fmt.Errorf("failed to get control plane pods for revision: %s", rev)
			}
			if err = enrichWithGatewayInfo(desc, revClient); err != nil {
				return fmt.Errorf("failed to get gateway pods for revision: %s", rev)
			}
		}
	}

	switch revArgs.output {
	case jsonFormat:
		return printJSON(writer, revisions)
	default:
		return printRevisionInfoTable(writer, args.verbose, revisions)
	}
}

func printRevisionInfoTable(writer io.Writer, verbose bool, revisions map[string]*revisionDescription) error {
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

func printControlPlaneSummaryTable(w io.Writer, revisions map[string]*revisionDescription) error {
	fmt.Fprintf(w, "\nCONTROL PLANE:\n")
	tw := new(tabwriter.Writer).Init(w, 0, 8, 1, ' ', 0)
	fmt.Fprintf(tw, "REVISION\tISTIOD ENABLED\tCONTROL-PLANE PODS\n")
	for rev, rd := range revisions {
		isIstiodEnabled := false
	outer:
		for _, iop := range rd.IstioOperatorCRs {
			for _, c := range iop.Components {
				if c == "istiod" {
					isIstiodEnabled = true
					break outer
				}
			}
		}
		maxRows := max(1, len(rd.ControlPlanePods))
		for i := 0; i < maxRows; i++ {
			var rowRev, rowEnabled, rowPod string
			if i == 0 {
				rowRev = rev
				if isIstiodEnabled {
					rowEnabled = "YES"
				} else {
					rowEnabled = "NO"
				}
			}
			if i < len(rd.ControlPlanePods) {
				rowPod = fmt.Sprintf("%s/%s",
					rd.ControlPlanePods[i].Namespace,
					rd.ControlPlanePods[i].Name)
			} else if i == 0 && len(rd.ControlPlanePods) == 0 {
				rowPod = "<no-istiod>"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\n", rowRev, rowEnabled, rowPod)
		}
	}
	return tw.Flush()
}

func printGatewaySummaryTable(w io.Writer, revisions map[string]*revisionDescription) error {
	if err := printIngressGatewaySummaryTable(w, revisions); err != nil {
		return fmt.Errorf("error while printing ingress gateway summary: %v", err)
	}
	if err := printEgressGatewaySummaryTable(w, revisions); err != nil {
		return fmt.Errorf("error while printing egress gateway summary: %v", err)
	}
	return nil
}

func printIngressGatewaySummaryTable(w io.Writer, revisions map[string]*revisionDescription) error {
	fmt.Fprintf(w, "\nINGRESS GATEWAYS:\n")
	tw := new(tabwriter.Writer).Init(w, 0, 8, 1, ' ', 0)
	fmt.Fprintf(tw, "REVISION\tDECLARED-GATEWAYS\tGATEWAY-POD\n")
	for rev, rd := range revisions {
		enabledIngressGateways := []string{}
		for _, iop := range rd.IstioOperatorCRs {
			for _, c := range iop.Components {
				if strings.HasPrefix(c, "ingress") {
					enabledIngressGateways = append(enabledIngressGateways, c)
				}
			}
		}
		maxRows := max(max(1, len(enabledIngressGateways)), len(rd.IngressGatewayPods))
		for i := 0; i < maxRows; i++ {
			var rowRev, rowEnabled, rowPod string
			if i == 0 {
				rowRev = rev
			}
			if i == 0 && len(enabledIngressGateways) == 0 {
				rowEnabled = "<no-ingress-enabled>"
			} else if i < len(enabledIngressGateways) {
				rowEnabled = enabledIngressGateways[i]
			}
			if i == 0 && len(rd.IngressGatewayPods) == 0 {
				rowPod = "<no-ingress-pod>"
			} else if i < len(enabledIngressGateways) {
				rowPod = fmt.Sprintf("%s/%s",
					rd.IngressGatewayPods[i].Namespace,
					rd.IngressGatewayPods[i].Name)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\n", rowRev, rowEnabled, rowPod)
		}
	}
	return tw.Flush()
}

// TODO(su225): This is a copy paste of corresponding function of ingress. Refactor these parts!
func printEgressGatewaySummaryTable(w io.Writer, revisions map[string]*revisionDescription) error {
	fmt.Fprintf(w, "\nEGRESS GATEWAYS:\n")
	tw := new(tabwriter.Writer).Init(w, 0, 8, 1, ' ', 0)
	fmt.Fprintf(tw, "REVISION\tDECLARED-GATEWAYS\tGATEWAY-POD\n")
	for rev, rd := range revisions {
		enabledEgressGateways := []string{}
		for _, iop := range rd.IstioOperatorCRs {
			for _, c := range iop.Components {
				if strings.HasPrefix(c, "egress") {
					enabledEgressGateways = append(enabledEgressGateways, c)
				}
			}
		}
		maxRows := max(max(1, len(enabledEgressGateways)), len(rd.EgressGatewayPods))
		for i := 0; i < maxRows; i++ {
			var rowRev, rowEnabled, rowPod string
			if i == 0 {
				rowRev = rev
			}
			if i == 0 && len(enabledEgressGateways) == 0 {
				rowEnabled = "<no-egress-enabled>"
			} else if i < len(enabledEgressGateways) {
				rowEnabled = enabledEgressGateways[i]
			}
			if i == 0 && len(rd.EgressGatewayPods) == 0 {
				rowPod = "<no-egress-pod>"
			} else if i < len(enabledEgressGateways) {
				rowPod = fmt.Sprintf("%s/%s",
					rd.EgressGatewayPods[i].Namespace,
					rd.EgressGatewayPods[i].Name)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\n", rowRev, rowEnabled, rowPod)
		}
	}
	return tw.Flush()
}

func printSummaryTable(writer io.Writer, verbose bool, revisions map[string]*revisionDescription) error {
	tw := new(tabwriter.Writer).Init(writer, 0, 8, 1, ' ', 0)
	if verbose {
		tw.Write([]byte("REVISION\tTAG\tISTIO-OPERATOR-CR\tPROFILE\tREQD-COMPONENTS\tCUSTOMIZATIONS\n"))
	} else {
		tw.Write([]byte("REVISION\tTAG\tISTIO-OPERATOR-CR\tPROFILE\tREQD-COMPONENTS\n"))
	}
	for r, ri := range revisions {
		rowId, tags := 0, []string{}
		for _, wh := range ri.Webhooks {
			if wh.Tag != "" {
				tags = append(tags, wh.Tag)
			}
		}
		if len(tags) == 0 {
			tags = append(tags, "<no-tag>")
		}
		for _, iop := range ri.IstioOperatorCRs {
			profile := effectiveProfile(iop.Profile)
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
				if rowId < len(tags) {
					rowTag = tags[rowId]
				}
				if rowId == 0 {
					rowRev = r
				}
				if verbose {
					fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
						rowRev, rowTag, rowIopName, rowProfile, rowComp, rowCust)
				} else {
					fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
						rowRev, rowTag, rowIopName, rowProfile, rowComp)
				}
				rowId++
			}
		}
		for rowId < len(tags) {
			var rowRev, rowTag, rowIopName string
			if rowId == 0 {
				rowRev = r
			}
			if rowId == 0 {
				rowIopName = "<no-iop>"
			}
			rowTag = tags[rowId]
			if verbose {
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

func printRevisionDescription(w io.Writer, args *revisionArgs, logger clog.Logger) error {
	revision := args.name
	client, err := newKubeClientWithRevision(kubeconfig, configContext, revision)
	if err != nil {
		return fmt.Errorf("cannot create kubeclient for kubeconfig=%s, context=%s: %v",
			kubeconfig, configContext, err)
	}
	allIops, err := getAllIstioOperatorCRs(client)
	if err != nil {
		return fmt.Errorf("error while fetching IstioOperator CR: %v", err)
	}
	iopsInCluster := getIOPWithRevision(allIops, revision)
	allWebhooks, err := getWebhooks(context.Background(), client)
	if err != nil {
		return fmt.Errorf("error while fetching mutating webhook configurations: %v", err)
	}
	webhooks := getWebhooksWithRevision(allWebhooks, revision)
	revDescription := getBasicRevisionDescription(iopsInCluster, webhooks)
	if err = enrichWithIOPCustomization(revDescription, args.manifestsPath, logger); err != nil {
		return err
	}
	enrichWithControlPlanePodInfo(revDescription, client)
	enrichWithGatewayInfo(revDescription, client)
	if isNonExistentRevision(revDescription) {
		return fmt.Errorf("revision %s is not present", revision)
	}
	if args.verbose {
		revAliases := []string{revision}
		for _, wh := range revDescription.Webhooks {
			revAliases = append(revAliases, wh.Tag)
		}
		enrichWithNamespaceAndPodInfo(revDescription, revAliases)
	}
	switch revArgs.output {
	case jsonFormat:
		return printJSON(w, revDescription)
	case tableFormat:
		sections := []string{}
		if args.verbose {
			sections = verboseSections
		} else {
			sections = defaultSections
		}
		return printTable(w, sections, revDescription)
	default:
		return fmt.Errorf("unknown format %s", revArgs.output)
	}
}

func isNonExistentRevision(revDescription *revisionDescription) bool {
	return len(revDescription.IstioOperatorCRs) == 0 &&
		len(revDescription.Webhooks) == 0 &&
		len(revDescription.ControlPlanePods) == 0 &&
		len(revDescription.IngressGatewayPods) == 0 &&
		len(revDescription.EgressGatewayPods) == 0
}

func enrichWithNamespaceAndPodInfo(revDescription *revisionDescription, revisionAliases []string) error {
	client, err := newKubeClient(kubeconfig, configContext)
	if err != nil {
		return fmt.Errorf("failed to create kubeclient: %v", err)
	}
	nsMap := make(map[string]*nsInfo)
	for _, ra := range revisionAliases {
		pods, err := getPodsWithSelector(client, "", &meta_v1.LabelSelector{
			MatchLabels: map[string]string{
				label.IstioRev: ra,
			},
		})
		if err != nil {
			return fmt.Errorf("failed to fetch pods for rev/tag: %s: %v", ra, err)
		}
		for _, po := range pods {
			fpo := getFilteredPodInfo(&po)
			_, ok := nsMap[po.Namespace]
			if !ok {
				nsMap[po.Namespace] = &nsInfo{
					Name: po.Namespace,
					Pods: []podFilteredInfo{fpo},
				}
			} else {
				nsMap[po.Namespace].Pods = append(nsMap[po.Namespace].Pods, fpo)
			}
		}
	}
	revDescription.NamespaceSummary = nsMap
	return nil
}

func enrichWithGatewayInfo(revDescription *revisionDescription, client kube.ExtendedClient) error {
	ingressPods, err := getPodsForComponent(client, "IngressGateways")
	if err != nil {
		return fmt.Errorf("error while fetching ingress gateway pods: %v", err)
	}
	revDescription.IngressGatewayPods = transformToFilteredPodInfo(ingressPods)
	egressPods, err := getPodsForComponent(client, "EgressGateways")
	if err != nil {
		return fmt.Errorf("error while fetching egress gateway pods: %v", err)
	}
	revDescription.EgressGatewayPods = transformToFilteredPodInfo(egressPods)
	return nil
}

func enrichWithControlPlanePodInfo(revDescription *revisionDescription, client kube.ExtendedClient) error {
	controlPlanePods, err := getPodsForComponent(client, "Pilot")
	if err != nil {
		return fmt.Errorf("error while fetching control plane pods: %v", err)
	}
	revDescription.ControlPlanePods = transformToFilteredPodInfo(controlPlanePods)
	return nil
}

func enrichWithIOPCustomization(revDesc *revisionDescription, manifestsPath string, logger clog.Logger) error {
	for _, cr := range revDesc.IstioOperatorCRs {
		cust, err := getDiffs(cr.IOP, manifestsPath, cr.IOP.Spec.GetProfile(), logger)
		if err != nil {
			return fmt.Errorf("error while computing customization for %s/%s: %v",
				cr.Name, cr.Namespace, err)
		}
		cr.Customizations = cust
	}
	return nil
}

func getBasicRevisionDescription(iopCRs []*iopv1alpha1.IstioOperator,
	mutatingWebhooks []admit_v1.MutatingWebhookConfiguration) *revisionDescription {
	revDescription := &revisionDescription{
		IstioOperatorCRs: []istioOperatorCRInfo{},
		Webhooks:         []mutatingWebhookConfigInfo{},
	}
	for _, iop := range iopCRs {
		revDescription.IstioOperatorCRs = append(revDescription.IstioOperatorCRs, istioOperatorCRInfo{
			IOP:            iop,
			Namespace:      iop.Namespace,
			Name:           iop.Name,
			Profile:        renderWithDefault(iop.Spec.Profile, "default"),
			Components:     getEnabledComponents(iop.Spec),
			Customizations: nil,
		})
	}
	for _, mwh := range mutatingWebhooks {
		revDescription.Webhooks = append(revDescription.Webhooks, mutatingWebhookConfigInfo{
			Name:     mwh.Name,
			Revision: renderWithDefault(mwh.Labels[label.IstioRev], "default"),
			Tag:      mwh.Labels[istioTag],
		})
	}
	return revDescription
}

func printJSON(w io.Writer, res interface{}) error {
	out, err := json.MarshalIndent(res, "", "\t")
	if err != nil {
		return fmt.Errorf("error while marshaling to JSON: %v", err)
	}
	fmt.Fprintln(w, string(out))
	return nil
}

func getWebhooksWithRevision(webhooks []admit_v1.MutatingWebhookConfiguration, revision string) []admit_v1.MutatingWebhookConfiguration {
	whFiltered := []admit_v1.MutatingWebhookConfiguration{}
	for _, wh := range webhooks {
		if wh.GetLabels()[label.IstioRev] == revision {
			whFiltered = append(whFiltered, wh)
		}
	}
	return whFiltered
}

func transformToFilteredPodInfo(pods []v1.Pod) []podFilteredInfo {
	pfilInfo := []podFilteredInfo{}
	for _, p := range pods {
		pfilInfo = append(pfilInfo, getFilteredPodInfo(&p))
	}
	return pfilInfo
}

func getFilteredPodInfo(pod *v1.Pod) podFilteredInfo {
	return podFilteredInfo{
		Namespace: pod.Namespace,
		Name:      pod.Name,
		Address:   pod.Status.PodIP,
		Status:    pod.Status.Phase,
		Age:       translateTimestampSince(pod.CreationTimestamp),
	}
}

func printTable(w io.Writer, sections []string, desc *revisionDescription) error {
	errs := &multierror.Error{}
	tablePrintFuncs := map[string]func(io.Writer, *revisionDescription) error{
		IstioOperatorCRSection: printIstioOperatorCRInfo,
		WebhooksSection:        printWebhookInfo,
		ControlPlaneSection:    printControlPlane,
		GatewaysSection:        printGateways,
		PodsSection:            printPodsInfo,
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

func printPodsInfo(w io.Writer, desc *revisionDescription) error {
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

func printIstioOperatorCRInfo(w io.Writer, desc *revisionDescription) error {
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

func printWebhookInfo(w io.Writer, desc *revisionDescription) error {
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

func printControlPlane(w io.Writer, desc *revisionDescription) error {
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

func printGateways(w io.Writer, desc *revisionDescription) error {
	if err := printIngressGateways(w, desc); err != nil {
		return fmt.Errorf("error while printing ingress-gateway info: %v", err)
	}
	if err := printEgressGateways(w, desc); err != nil {
		return fmt.Errorf("error while printing egress-gateway info: %v", err)
	}
	return nil
}

func printIngressGateways(w io.Writer, desc *revisionDescription) error {
	fmt.Fprintf(w, "\nINGRESS GATEWAYS: (%d)\n", len(desc.IngressGatewayPods))
	if len(desc.IngressGatewayPods) == 0 {
		if isIngressGatewayEnabled(desc) {
			fmt.Fprintln(w, "Ingress gateway is enabled for this revision. However there are no such pods. "+
				"It could be that it is replaced by ingress-gateway from another revision (as it is still upgraded in-place) "+
				"or it could be some issue with installation")
		} else {
			fmt.Fprintln(w, "Ingress gateway is disabled for this revision")
		}
		return nil
	}
	if !isIngressGatewayEnabled(desc) {
		fmt.Fprintln(w, "WARNING: Ingress gateway is not enabled for this revision.")
	}
	return printPodTable(w, desc.IngressGatewayPods)
}

func printEgressGateways(w io.Writer, desc *revisionDescription) error {
	fmt.Fprintf(w, "\nEGRESS GATEWAYS: (%d)\n", len(desc.IngressGatewayPods))
	if len(desc.EgressGatewayPods) == 0 {
		if isEgressGatewayEnabled(desc) {
			fmt.Fprintln(w, "Egress gateway is enabled for this revision. However there are no such pods. "+
				"It could be that it is replaced by egress-gateway from another revision (as it is still upgraded in-place) "+
				"or it could be some issue with installation")
		} else {
			fmt.Fprintln(w, "Egress gateway is disabled for this revision")
		}
		return nil
	}
	if !isEgressGatewayEnabled(desc) {
		fmt.Fprintln(w, "WARNING: Egress gateway is not enabled for this revision.")
	}
	return printPodTable(w, desc.EgressGatewayPods)
}

type istioGatewayType = string

const (
	ingress istioGatewayType = "ingress"
	egress  istioGatewayType = "egress"
)

func isIngressGatewayEnabled(desc *revisionDescription) bool {
	return isGatewayTypeEnabled(desc, ingress)
}

func isEgressGatewayEnabled(desc *revisionDescription) bool {
	return isGatewayTypeEnabled(desc, egress)
}

func isGatewayTypeEnabled(desc *revisionDescription, gwType istioGatewayType) bool {
	for _, iopdesc := range desc.IstioOperatorCRs {
		for _, comp := range iopdesc.Components {
			if strings.HasPrefix(comp, gwType) {
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

func printPodTable(w io.Writer, pods []podFilteredInfo) error {
	podTableW := new(tabwriter.Writer).Init(w, 0, 0, 1, ' ', 0)
	fmt.Fprintln(podTableW, "NAMESPACE\tNAME\tADDRESS\tSTATUS\tAGE")
	for _, pod := range pods {
		fmt.Fprintf(podTableW, "%s\t%s\t%s\t%s\t%s\n",
			pod.Namespace, pod.Name, pod.Address, pod.Status, pod.Age)
	}
	return podTableW.Flush()
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

func getPodsForComponent(client kube.ExtendedClient, component string) ([]v1.Pod, error) {
	return getPodsWithSelector(client, istioNamespace, &meta_v1.LabelSelector{
		MatchLabels: map[string]string{
			label.IstioRev:               client.Revision(),
			label.IstioOperatorComponent: component,
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

type iopDiff struct {
	Path  string `json:"path"`
	Value string `json:"value"`
}

func getDiffs(installed *iopv1alpha1.IstioOperator, manifestsPath, profile string, l clog.Logger) ([]iopDiff, error) {
	setFlags := []string{"profile=" + profile}
	if manifestsPath != "" {
		setFlags = append(setFlags, fmt.Sprintf("installPackagePath=%s", manifestsPath))
	}
	_, base, err := manifest.GenerateConfig([]string{}, setFlags, true, nil, l)
	if err != nil {
		return []iopDiff{}, err
	}
	mapInstalled, err := config.ToMap(installed.Spec)
	if err != nil {
		return []iopDiff{}, err
	}
	mapBase, err := config.ToMap(base.Spec)
	if err != nil {
		return []iopDiff{}, err
	}
	return diffWalk("", "", mapInstalled, mapBase)
}

func diffWalk(path, separator string, obj interface{}, orig interface{}) ([]iopDiff, error) {
	switch v := obj.(type) {
	case map[string]interface{}:
		accum := make([]iopDiff, 0)
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
		accum := make([]iopDiff, 0)
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
			return []iopDiff{{Path: path, Value: fmt.Sprintf("%q", v)}}, nil
		}
	default:
		if v != orig && orig != nil {
			return []iopDiff{{Path: path, Value: fmt.Sprintf("%v", v)}}, nil
		}
	}
	return []iopDiff{}, nil
}

func renderWithDefault(s, def string) string {
	if s != "" {
		return s
	}
	return fmt.Sprintf("%s", def)
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
