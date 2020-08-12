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
	"strings"

	"github.com/spf13/cobra"

	"istio.io/istio/operator/pkg/helm"
	"istio.io/istio/operator/pkg/helmreconciler"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/object"
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
	// manifestsPath is a path to a charts and profiles directory in the local filesystem, or URL with a release tgz.
	manifestsPath string
	// revision is the Istio control plane revision the command targets.
	revision string
}

func addManifestGenerateFlags(cmd *cobra.Command, args *manifestGenerateArgs) {
	cmd.PersistentFlags().StringSliceVarP(&args.inFilename, "filename", "f", nil, filenameFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&args.outFilename, "output", "o", "", "Manifest output directory path.")
	cmd.PersistentFlags().StringArrayVarP(&args.set, "set", "s", nil, setFlagHelpStr)
	cmd.PersistentFlags().BoolVar(&args.force, "force", false, "Proceed even with validation errors.")
	cmd.PersistentFlags().StringVarP(&args.manifestsPath, "charts", "", "", ChartsDeprecatedStr)
	cmd.PersistentFlags().StringVarP(&args.manifestsPath, "manifests", "d", "", ManifestsFlagHelpStr)
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

  # Enable Tracing
  istioctl install --set meshConfig.enableTracing=true

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

	manifests, _, err := manifest.GenManifests(mgArgs.inFilename, applyFlagAliases(mgArgs.set, mgArgs.manifestsPath, mgArgs.revision), mgArgs.force, nil, l)
	if err != nil {
		return err
	}

	if mgArgs.outFilename == "" {
		ordered, err := orderedManifests(manifests)
		if err != nil {
			return fmt.Errorf("failed to order manifests: %v", err)
		}
		for _, m := range ordered {
			l.Print(m + object.YAMLSeparator)
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

// orderedManifests generates a list of manifests from the given map sorted by the default object order
// This allows
func orderedManifests(mm name.ManifestMap) ([]string, error) {
	var rawOutput []string
	var output []string
	for _, mfs := range mm {
		rawOutput = append(rawOutput, mfs...)
	}
	objects, err := object.ParseK8sObjectsFromYAMLManifest(strings.Join(rawOutput, helm.YAMLSeparator))
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
