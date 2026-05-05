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
	"os"
	"strings"

	"github.com/spf13/cobra"

	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/render"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/pkg/kube"
)

const (
	crdGroup = "apiextensions.k8s.io"
	crdKind  = "CustomResourceDefinition"
)

// ManifestGenerateCRDsArgs holds args for the `manifest generate-crds` subcommand.
//
// This is intentionally a strict subset of ManifestGenerateArgs: the only
// supported output is the rendered set of CustomResourceDefinitions. It exists
// so downstream consumers (e.g. a sidecar in a multi-container pod sharing a
// volume with the distroless istioctl image) can obtain just the CRDs without
// shelling out to text filters.
type ManifestGenerateCRDsArgs struct {
	// InFilenames is an array of paths to the input IstioOperator CR files.
	InFilenames []string

	// Set is a string with element format "path=value" where path is an IstioOperator path and the value is a
	// value to set the node at that path to.
	Set []string
	// Force proceeds even if there are validation errors.
	Force bool
	// ManifestsPath is a path to a charts and profiles directory in the local filesystem with a release tgz.
	ManifestsPath string
	// Revision is the Istio control plane revision the command targets.
	Revision string

	// Output is the path of the file to write the rendered CRDs to.
	// If empty, output is written to stdout.
	Output string
}

func (a *ManifestGenerateCRDsArgs) String() string {
	var b strings.Builder
	b.WriteString("InFilenames:   " + fmt.Sprint(a.InFilenames) + "\n")
	b.WriteString("Set:           " + fmt.Sprint(a.Set) + "\n")
	b.WriteString("Force:         " + fmt.Sprint(a.Force) + "\n")
	b.WriteString("ManifestsPath: " + a.ManifestsPath + "\n")
	b.WriteString("Revision:      " + a.Revision + "\n")
	b.WriteString("Output:        " + a.Output + "\n")
	return b.String()
}

func addManifestGenerateCRDsFlags(cmd *cobra.Command, args *ManifestGenerateCRDsArgs) {
	cmd.PersistentFlags().StringSliceVarP(&args.InFilenames, "filename", "f", nil, filenameFlagHelpStr)
	cmd.PersistentFlags().StringArrayVarP(&args.Set, "set", "s", nil, setFlagHelpStr)
	cmd.PersistentFlags().BoolVar(&args.Force, "force", false, ForceFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&args.ManifestsPath, "manifests", "d", "", ManifestsFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&args.Revision, "revision", "r", "", revisionFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&args.Output, "output", "o", "",
		"Path of the file to write the rendered CRDs to. If empty, writes to stdout.")
}

// ManifestGenerateCRDsCmd creates the `istioctl manifest generate-crds` subcommand.
func ManifestGenerateCRDsCmd(ctx cli.Context, _ *RootArgs, mgArgs *ManifestGenerateCRDsArgs) *cobra.Command {
	return &cobra.Command{
		Use:   "generate-crds",
		Short: "Generates only the Istio CRDs portion of the install manifest",
		Long: "The generate-crds subcommand renders only the CustomResourceDefinition objects from the Istio install " +
			"manifest. This is intended for use cases where CRDs need to be installed independently from the rest of " +
			"the control plane (for example, a sidecar in a multi-container pod sharing a volume with the distroless " +
			"istioctl image, applying just the CRDs ahead of time).",
		// nolint: lll
		Example: `  # Write all default Istio CRDs to a shared volume
  istioctl manifest generate-crds -o /shared/istio-crds.yaml

  # Print CRDs to stdout (matches 'manifest generate' behavior)
  istioctl manifest generate-crds

  # Exclude specific CRDs (honored by the base chart)
  istioctl manifest generate-crds --set 'base.excludedCRDs={envoyfilters.networking.istio.io}' -o /shared/istio-crds.yaml

  # Use a custom IstioOperator file
  istioctl manifest generate-crds -f my-iop.yaml -o /shared/istio-crds.yaml
`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("generate-crds accepts no positional arguments, got %#v", args)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			l := clog.NewConsoleLogger(cmd.OutOrStdout(), cmd.ErrOrStderr(), installerScope)
			// generate-crds never needs cluster-specific values: CRDs are static
			// and the same regardless of the target cluster. Avoid a kube client
			// round-trip so the command works with no kubeconfig present.
			var kubeClient kube.CLIClient
			return ManifestGenerateCRDs(kubeClient, mgArgs, l)
		},
	}
}

// ManifestGenerateCRDs renders the install manifest and emits only the
// CustomResourceDefinition objects.
func ManifestGenerateCRDs(kubeClient kube.CLIClient, mgArgs *ManifestGenerateCRDsArgs, l clog.Logger) error {
	setFlags := applyFlagAliases(mgArgs.Set, mgArgs.ManifestsPath, mgArgs.Revision)
	manifests, _, err := render.GenerateManifest(mgArgs.InFilenames, setFlags, mgArgs.Force, kubeClient, nil)
	if err != nil {
		return err
	}

	crds := filterCRDs(manifests)
	if len(crds) == 0 {
		return fmt.Errorf("no CustomResourceDefinitions were rendered; check that the base component is enabled " +
			"and base.enableCRDTemplates is true")
	}

	sorted := sortManifests(crds)
	out := strings.Join(sorted, YAMLSeparator)
	// Trailing separator keeps the document well-formed when concatenated with others.
	if !strings.HasSuffix(out, YAMLSeparator) {
		out += YAMLSeparator
	}

	if mgArgs.Output == "" {
		l.Print(out)
		return nil
	}
	if err := os.WriteFile(mgArgs.Output, []byte(out), 0o644); err != nil {
		return fmt.Errorf("write CRDs to %q: %v", mgArgs.Output, err)
	}
	return nil
}

// filterCRDs returns the subset of manifests that are CustomResourceDefinitions.
func filterCRDs(sets []manifest.ManifestSet) []manifest.Manifest {
	var out []manifest.Manifest
	for _, ms := range sets {
		for _, m := range ms.Manifests {
			gvk := m.GroupVersionKind()
			if gvk.Group == crdGroup && gvk.Kind == crdKind {
				out = append(out, m)
			}
		}
	}
	return out
}
