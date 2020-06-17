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
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/client-go/rest"

	"istio.io/api/operator/v1alpha1"
	"istio.io/istio/operator/pkg/controlplane"
	"istio.io/istio/operator/pkg/helm"
	"istio.io/istio/operator/pkg/helmreconciler"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/translate"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/pkg/log"
)

type manifestGenerateArgs struct {
	// inFilenames is an array of paths to the input IstioOperator CR files.
	inFilename []string
	// outFilename is the path to the generated output directory.
	outFilename string
	// set is a string with element format "path=value" where path is an IstioOperator path and the value is a
	// value to set the node at that path to.
	set []string
	// force proceeds even if there are validation errors
	force bool
	// charts is a path to a charts and profiles directory in the local filesystem, or URL with a release tgz.
	charts string
	// revision is the Istio control plane revision the command targets.
	revision string
}

func addManifestGenerateFlags(cmd *cobra.Command, args *manifestGenerateArgs) {
	cmd.PersistentFlags().StringSliceVarP(&args.inFilename, "filename", "f", nil, filenameFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&args.outFilename, "output", "o", "", "Manifest output directory path.")
	cmd.PersistentFlags().StringArrayVarP(&args.set, "set", "s", nil, setFlagHelpStr)
	cmd.PersistentFlags().BoolVar(&args.force, "force", false, "Proceed even with validation errors.")
	cmd.PersistentFlags().StringVarP(&args.charts, "charts", "d", "", ChartsFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&args.revision, "revision", "r", "", revisionFlagHelpStr)
}

func manifestGenerateCmd(rootArgs *rootArgs, mgArgs *manifestGenerateArgs, logOpts *log.Options) *cobra.Command {
	return &cobra.Command{
		Use:   "generate",
		Short: "Generates an Istio install manifest",
		Long:  "The generate subcommand generates an Istio install manifest and outputs to the console by default.",
		// nolint: lll
		Example: `  # Generate a default Istio installation
  istioctl manifest generate

  # Enable grafana dashboard
  istioctl manifest generate --set values.grafana.enabled=true

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
			l := clog.NewConsoleLogger(cmd.OutOrStdout(), cmd.ErrOrStderr(), installerScope)
			return manifestGenerate(rootArgs, mgArgs, logOpts, l)
		}}

}

func manifestGenerate(args *rootArgs, mgArgs *manifestGenerateArgs, logopts *log.Options, l clog.Logger) error {
	if err := configLogs(logopts); err != nil {
		return fmt.Errorf("could not configure logs: %s", err)
	}

	manifests, _, err := GenManifests(mgArgs.inFilename, applyFlagAliases(mgArgs.set, mgArgs.charts, mgArgs.revision), mgArgs.force, nil, l)
	if err != nil {
		return err
	}

	if mgArgs.outFilename == "" {
		for _, m := range orderedManifests(manifests) {
			l.Print(m + "\n")
		}
	} else {
		if err := os.MkdirAll(mgArgs.outFilename, os.ModePerm); err != nil {
			return err
		}
		if err := RenderToDir(manifests, mgArgs.outFilename, args.dryRun, l); err != nil {
			return err
		}
	}

	return nil
}

// GenManifests generates a manifest map, keyed by the component name, from input file list and a YAML tree
// representation of path-values passed through the --set flag.
// If force is set, validation errors will not cause processing to abort but will result in warnings going to the
// supplied logger.
func GenManifests(inFilename []string, setFlags []string, force bool,
	kubeConfig *rest.Config, l clog.Logger) (name.ManifestMap, *v1alpha1.IstioOperatorSpec, error) {
	mergedYAML, _, err := GenerateConfig(inFilename, setFlags, force, kubeConfig, l)
	if err != nil {
		return nil, nil, err
	}
	mergedIOPS, err := unmarshalAndValidateIOPS(mergedYAML, force, l)
	if err != nil {
		return nil, nil, err
	}

	t := translate.NewTranslator()

	cp, err := controlplane.NewIstioControlPlane(mergedIOPS, t)
	if err != nil {
		return nil, nil, err
	}
	if err := cp.Run(); err != nil {
		return nil, nil, err
	}

	manifests, errs := cp.RenderManifest()
	if errs != nil {
		return manifests, mergedIOPS, errs.ToError()
	}
	return manifests, mergedIOPS, nil
}

// orderedManifests generates a list of manifests from the given map sorted by the map keys.
func orderedManifests(mm name.ManifestMap) []string {
	var keys, out []string
	for k := range mm {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)
	for _, k := range keys {
		out = append(out, strings.Join(mm[name.ComponentName(k)], helm.YAMLSeparator))
	}
	return out
}

// RenderToDir writes manifests to a local filesystem directory tree.
func RenderToDir(manifests name.ManifestMap, outputDir string, dryRun bool, l clog.Logger) error {
	l.LogAndPrint("Component dependencies tree: \n%s", helmreconciler.InstallTreeString())
	l.LogAndPrint("Rendering manifests to output dir %s", outputDir)
	return renderRecursive(manifests, helmreconciler.InstallTree, outputDir, dryRun, l)
}

func renderRecursive(manifests name.ManifestMap, installTree helmreconciler.ComponentTree, outputDir string, dryRun bool, l clog.Logger) error {
	for k, v := range installTree {
		componentName := string(k)
		// In cases (like gateways) where multiple instances can exist, concatenate the manifests and apply as one.
		ym := strings.Join(manifests[k], helm.YAMLSeparator)
		l.LogAndPrint("Rendering: %s", componentName)
		dirName := filepath.Join(outputDir, componentName)
		if !dryRun {
			if err := os.MkdirAll(dirName, os.ModePerm); err != nil {
				return fmt.Errorf("could not create directory %s; %s", outputDir, err)
			}
		}
		fname := filepath.Join(dirName, componentName) + ".yaml"
		l.LogAndPrint("Writing manifest to %s", fname)
		if !dryRun {
			if err := ioutil.WriteFile(fname, []byte(ym), 0644); err != nil {
				return fmt.Errorf("could not write manifest config; %s", err)
			}
		}

		kt, ok := v.(helmreconciler.ComponentTree)
		if !ok {
			// Leaf
			return nil
		}
		if err := renderRecursive(manifests, kt, dirName, dryRun, l); err != nil {
			return err
		}
	}
	return nil
}
