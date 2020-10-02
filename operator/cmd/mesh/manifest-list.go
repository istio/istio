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

package mesh

import (
	"context"
	"fmt"
	"io"
	"sort"
	"text/tabwriter"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"

	"github.com/spf13/cobra"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apimachinery_schema "k8s.io/apimachinery/pkg/runtime/schema"

	operator_istio "istio.io/istio/operator/pkg/apis/istio"
	iopv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/pkg/config"
	"istio.io/pkg/log"
)

type manifestListArgs struct {
	// kubeConfigPath is the path to kube config file.
	kubeConfigPath string
	// context is the cluster context in the kube config
	context string
	// manifestsPath is a path to a charts and profiles directory in the local filesystem, or URL with a release tgz.
	manifestsPath string
}

var (
	istioOperatorGVR = apimachinery_schema.GroupVersionResource{
		Group:    iopv1alpha1.SchemeGroupVersion.Group,
		Version:  iopv1alpha1.SchemeGroupVersion.Version,
		Resource: "istiooperators",
	}
)

func addManifestListFlags(cmd *cobra.Command, args *manifestListArgs) {
	cmd.PersistentFlags().StringVarP(&args.manifestsPath, "manifests", "d", "", ManifestsFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&args.kubeConfigPath, "kubeconfig", "c", "", KubeConfigFlagHelpStr)
	cmd.PersistentFlags().StringVar(&args.context, "context", "", ContextFlagHelpStr)
}

func manifestListCmd(listArgs *manifestListArgs, logOpts *log.Options) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Lists applied Istio manifests",
		Long:  "The list subcommand displays installed Istio control planes to the console",
		// nolint: lll
		Example: `  # List Istio installations
  istioctl manifest list

`,
		RunE: func(cmd *cobra.Command, args []string) error {
			l := clog.NewConsoleLogger(cmd.OutOrStdout(), cmd.ErrOrStderr(), installerScope)
			return manifestList(cmd.OutOrStdout(), listArgs, l)
		}}

}

func manifestList(writer io.Writer, listArgs *manifestListArgs, l clog.Logger) error {
	restConfig, _, _, err := K8sConfig(listArgs.kubeConfigPath, listArgs.context)
	if err != nil {
		return err
	}
	iops, err := getIOPs(restConfig)
	if err != nil {
		return err
	}
	return printIOPs(writer, iops, listArgs.manifestsPath, restConfig, l)
}

func printIOPs(writer io.Writer, iops []*iopv1alpha1.IstioOperator, manifestsPath string, restConfig *rest.Config, l clog.Logger) error {
	if len(iops) == 0 {
		_, err := fmt.Fprintf(writer, "No IstioOperators present.\n")
		return err
	}
	podCount := "1"         // @@@ TODO Lookup using restConfig
	deploymentAge := "2d7h" // @@@ TODO Lookup using restConfig

	// @@@ TODO sort
	w := new(tabwriter.Writer).Init(writer, 0, 8, 1, ' ', 0)
	fmt.Fprintln(w, "REVISION\tPROFILE\tCUSTOMIZATIONS\tPODS\tAGE")
	for _, iop := range iops {
		diffs, err := getDiffs(iop, manifestsPath, effectiveProfile(iop.Spec.Profile), restConfig, l)
		if err != nil {
			return err
		}
		for i, diff := range diffs {
			if i == 0 {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
					renderWithDefault(iop.Spec.Revision, "master"),
					renderWithDefault(iop.Spec.Profile, "default"),
					diff, podCount, deploymentAge)
			} else {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
					"",
					"",
					diff, "", "")
			}
		}
	}
	return w.Flush()
}

func getIOPs(restConfig *rest.Config) ([]*iopv1alpha1.IstioOperator, error) {
	client, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}
	ul, err := client.
		Resource(istioOperatorGVR).
		Namespace(istioDefaultNamespace). // @@@ TODO make a flag
		List(context.TODO(), meta_v1.ListOptions{})
	if err != nil {
		return nil, err
	}
	iops := []*iopv1alpha1.IstioOperator{}
	for _, un := range ul.Items {
		un.SetCreationTimestamp(meta_v1.Time{}) // UnmarshalIstioOperator chokes on these
		by := util.ToYAML(un.Object)
		iop, err := operator_istio.UnmarshalIstioOperator(by, true)
		if err != nil {
			return nil, err
		}
		iops = append(iops, iop)
	}
	return iops, nil
}

func getDiffs(installed *iopv1alpha1.IstioOperator, manifestsPath, profile string, restConfig *rest.Config, l clog.Logger) ([]string, error) {
	setFlags := applyFlagAliases(make([]string, 0), manifestsPath, "")
	setFlags = append(setFlags, "profile="+profile)

	_, base, err := manifest.GenerateConfig([]string{}, setFlags, true, nil, l)
	if err != nil {
		return []string{}, err
	}

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
		if v != orig {
			return []string{fmt.Sprintf("%s=%q", path, v)}, nil
		}
	default:
		if v != orig {
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
