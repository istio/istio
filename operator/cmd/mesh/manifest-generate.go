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
	"istio.io/istio/pkg/slices"
	"strings"

	"github.com/spf13/cobra"

	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/operator/john"
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

func ManifestGenerateCmd(ctx cli.Context, _ *RootArgs, mgArgs *ManifestGenerateArgs) *cobra.Command {
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
	for _, manifest := range sortManifests(manifests) {
		l.Print(manifest + object.YAMLSeparator)
	}
	return nil
}

func sortManifests(raw []john.ManifestSet) ([]string) {
	all := []john.Manifest{}
	for _, m := range raw {
		all = append(all, m.Manifests...)
	}
	slices.SortBy(all, func(a john.Manifest) int {
		o := a.Unstructured
		gk := o.GroupVersionKind().Group + "/" + o.GroupVersionKind().Kind
		switch {
		// Create CRDs asap - both because they are slow and because we will likely create instances of them soon
		case gk == "apiextensions.k8s.io/CustomResourceDefinition":
			return -1000

			// We need to create ServiceAccounts, Roles before we bind them with a RoleBinding
		case gk == "/ServiceAccount" || gk == "rbac.authorization.k8s.io/ClusterRole":
			return 1
		case gk == "rbac.authorization.k8s.io/ClusterRoleBinding":
			return 2

			// validatingwebhookconfiguration is configured to FAIL-OPEN in the default install. For the
			// re-install case we want to apply the validatingwebhookconfiguration first to reset any
			// orphaned validatingwebhookconfiguration that is FAIL-CLOSE.
		case gk == "admissionregistration.k8s.io/ValidatingWebhookConfiguration":
			return 3

			// Pods might need configmap or secrets - avoid backoff by creating them first
		case gk == "/ConfigMap" || gk == "/Secrets":
			return 100

			// Create the pods after we've created other things they might be waiting for
		case gk == "extensions/Deployment" || gk == "apps/Deployment":
			return 1000

			// Autoscalers typically act on a deployment
		case gk == "autoscaling/HorizontalPodAutoscaler":
			return 1001

			// Create services late - after pods have been started
		case gk == "/Service":
			return 10000

		default:
			return 1000
		}
	})
	return slices.Map(all, func(e john.Manifest) string {
		return e.Content
	})
}
