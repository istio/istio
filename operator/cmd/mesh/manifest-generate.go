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
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/operator/john"
	"istio.io/istio/operator/pkg/helm"
	"istio.io/istio/operator/pkg/object"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/pkg/kube"
)

type ManifestGenerateArgs struct {
	// InFilenames is an array of paths to the input IstioOperator CR files.
	InFilenames []string
	// OutFilename is the path to the generated output directory.
	OutFilename string

	// EnableClusterSpecific determines if the current Kubernetes cluster will be used to autodetect values.
	// If false, generic defaults will be used. This is useful when generating once and then applying later.
	EnableClusterSpecific bool

	// Set is a string with element format "path=value" where path is an IstioOperator path and the value is a
	// value to set the node at that path to.
	Set []string
	// Force proceeds even if there are validation errors
	Force bool
	// ManifestsPath is a path to a charts and profiles directory in the local filesystem with a release tgz.
	ManifestsPath string
	// Revision is the Istio control plane revision the command targets.
	Revision string
	// Components is a list of strings specifying which component's manifests to be generated.
	Components []string
	// Filter is the list of components to render
	Filter []string
}

var kubeClientFunc func() (kube.CLIClient, error)

func (a *ManifestGenerateArgs) String() string {
	var b strings.Builder
	b.WriteString("InFilenames:   " + fmt.Sprint(a.InFilenames) + "\n")
	b.WriteString("OutFilename:   " + a.OutFilename + "\n")
	b.WriteString("Set:           " + fmt.Sprint(a.Set) + "\n")
	b.WriteString("Force:         " + fmt.Sprint(a.Force) + "\n")
	b.WriteString("ManifestsPath: " + a.ManifestsPath + "\n")
	b.WriteString("Revision:      " + a.Revision + "\n")
	b.WriteString("Components:    " + fmt.Sprint(a.Components) + "\n")
	return b.String()
}

func addManifestGenerateFlags(cmd *cobra.Command, args *ManifestGenerateArgs) {
	cmd.PersistentFlags().StringSliceVarP(&args.InFilenames, "filename", "f", nil, filenameFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&args.OutFilename, "output", "o", "", "Manifest output directory path.")
	cmd.PersistentFlags().StringArrayVarP(&args.Set, "set", "s", nil, setFlagHelpStr)
	cmd.PersistentFlags().BoolVar(&args.Force, "force", false, ForceFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&args.ManifestsPath, "charts", "", "", ChartsDeprecatedStr)
	cmd.PersistentFlags().StringVarP(&args.ManifestsPath, "manifests", "d", "", ManifestsFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&args.Revision, "revision", "r", "", revisionFlagHelpStr)
	cmd.PersistentFlags().StringSliceVar(&args.Components, "component", nil, ComponentFlagHelpStr)
	cmd.PersistentFlags().StringSliceVar(&args.Filter, "filter", nil, "")
	_ = cmd.PersistentFlags().MarkHidden("filter")

	cmd.PersistentFlags().BoolVar(&args.EnableClusterSpecific, "cluster-specific", false,
		"If enabled, the current cluster will be checked for cluster-specific setting detection.")
}

func ManifestGenerateCmd(ctx cli.Context, rootArgs *RootArgs, mgArgs *ManifestGenerateArgs) *cobra.Command {
	return &cobra.Command{
		Use:   "generate",
		Short: "Generates an Istio install manifest",
		Long:  "The generate subcommand generates an Istio install manifest and outputs to the console by default.",
		// nolint: lll
		Example: `  # Generate a default Istio installation
  istioctl manifest generate

  # Enable Tracing
  istioctl manifest generate --set meshConfig.enableTracing=true

  # Generate the demo profile
  istioctl manifest generate --set profile=demo

  # To override a setting that includes dots, escape them with a backslash (\).  Your shell may require enclosing quotes.
  istioctl manifest generate --set "values.sidecarInjectorWebhook.injectedAnnotations.container\.apparmor\.security\.beta\.kubernetes\.io/istio-proxy=runtime/default"
`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("generate accepts no positional arguments, got %#v", args)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if kubeClientFunc == nil {
				kubeClientFunc = ctx.CLIClient
			}
			var kubeClient kube.CLIClient
			if mgArgs.EnableClusterSpecific {
				kc, err := kubeClientFunc()
				if err != nil {
					return err
				}
				kubeClient = kc
			}
			l := clog.NewConsoleLogger(cmd.OutOrStdout(), cmd.ErrOrStderr(), installerScope)
			return ManifestGenerate(kubeClient, mgArgs, l)
		},
	}
}

func ManifestGenerate(kubeClient kube.CLIClient, mgArgs *ManifestGenerateArgs, l clog.Logger) error {
	manifests, err := john.GenerateManifest(mgArgs.InFilenames, applyFlagAliases(mgArgs.Set, mgArgs.ManifestsPath, mgArgs.Revision),
		mgArgs.Force, mgArgs.Filter, kubeClient)
	if err != nil {
		return err
	}
	sorted, err := sortManifests(manifests)
	if err != nil {
		return err
	}
	for _, manifest := range sorted {
		l.Print(manifest + object.YAMLSeparator)
	}
	return nil
}

// TODO: do not do full parsing
func sortManifests(mm []string) ([]string, error) {
	var output []string
	objects, err := object.ParseK8sObjectsFromYAMLManifest(strings.Join(mm, helm.YAMLSeparator))
	if err != nil {
		return nil, err
	}
	// For a given group of objects, sort in order to avoid missing dependencies, such as creating CRDs first
	objects.Sort(object.DefaultObjectOrder())
	for _, obj := range objects {
		yml, err := obj.YAML()
		if err != nil {
			return nil, err
		}
		output = append(output, string(yml))
	}

	return output, nil
}
