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
	"istio.io/api/operator/v1alpha1"
	"istio.io/istio/operator/cmd/mesh"
	operator_istio "istio.io/istio/operator/pkg/apis/istio"
	iopv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/pkg/config"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apimachinery_schema "k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/utils/pointer"
	"sort"
	"strings"
	"text/tabwriter"
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
)

func revisionCommand() *cobra.Command {
	revisionCmd := &cobra.Command{
		Use: "revision",
		Short: "Revision centric view of Istio deployment",
		Aliases: []string{"rev"},
	}
	revisionCmd.AddCommand(revisionListCommand())
	revisionCmd.AddCommand(revisionDescribeCommand())
	return revisionCmd
}

func revisionDescribeCommand() *cobra.Command {
	describeCmd := &cobra.Command{
		Use: "describe",
		Short: "Show details of a revision - customizations, number of pods pointing to it, istiod, gateways etc",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.OutOrStderr().Write([]byte("called revision describe\n"))
			return nil
		},
	}
	describeCmd.Flags().Bool("all", true, "show all related to a revision")
	return describeCmd
}

func revisionListCommand() *cobra.Command {
	kubeConfigFlags := &genericclioptions.ConfigFlags{
		Context: pointer.StringPtr(""),
		Namespace: pointer.StringPtr(""),
		KubeConfig: pointer.StringPtr(""),
	}
	revArgs := revisionArgs{}
	listCmd := &cobra.Command{
		Use: "list",
		Short: "Show list of control plane and gateway revisions that are currently installed in cluster",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := clog.NewConsoleLogger(cmd.OutOrStdout(), cmd.ErrOrStderr(), scope)
			return revisionList(cmd.OutOrStdout(), &revArgs, kubeConfigFlags, logger)
		},
	}
	listCmd.PersistentFlags().StringVarP(&revArgs.manifestsPath, "manifests", "d", "", mesh.ManifestsFlagHelpStr)
	listCmd.PersistentFlags().BoolVarP(&revArgs.verbose, "verbose", "v", false, "print customizations")
	return listCmd
}

func revisionList(writer io.Writer, revArgs *revisionArgs, restClientGetter genericclioptions.RESTClientGetter, logger clog.Logger) error {
	restConfig, err := restClientGetter.ToRESTConfig()
	if err != nil {
		return err
	}
	iops, err := getIOPs(restConfig)
	if err != nil {
		return err
	}
	return printIOPs(writer, iops, revArgs.verbose, revArgs.manifestsPath, logger)
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
			iops := iop.Spec
			components := getEnabledComponents(iops)
			customizations := []string{}
			if verbose {
				var err error
				customizations, err = getDiffs(iop, manifestsPath, effectiveProfile(iop.Spec.Profile), l)
				if err != nil {
					l.LogAndErrorf("error while computing customization: %s", err.Error())
				}
			}
			// The first row is for printing revision, IOP related information
			numRows := max(len(customizations), max(1, len(customizations)))
			for j := 0; j < numRows; j++ {
				var outRevision string
				if i == 0 && j == 0 {
					outRevision = renderWithDefault(revision, "<default>")
				}
				var outIopName, outProfile string
				if j == 0 {
					outIopName = fmt.Sprintf("%s/%s", iop.Namespace, iop.Name)
					outProfile = renderWithDefault(iops.GetProfile(), "default")
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

func max(x, y int) int {
	if x > y {
		return x
	}
	return y
}