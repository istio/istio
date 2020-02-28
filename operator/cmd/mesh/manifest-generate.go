// Copyright 2019 Istio Authors
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
	"sort"
	"strings"

	"k8s.io/client-go/rest"

	"istio.io/api/operator/v1alpha1"
	"istio.io/istio/operator/pkg/component/controlplane"
	"istio.io/istio/operator/pkg/translate"
	"istio.io/istio/operator/version"

	"istio.io/istio/operator/pkg/helm"

	"github.com/spf13/cobra"

	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/name"
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
}

func addManifestGenerateFlags(cmd *cobra.Command, args *manifestGenerateArgs) {
	cmd.PersistentFlags().StringSliceVarP(&args.inFilename, "filename", "f", nil, filenameFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&args.outFilename, "output", "o", "", "Manifest output directory path")
	cmd.PersistentFlags().StringArrayVarP(&args.set, "set", "s", nil, SetFlagHelpStr)
	cmd.PersistentFlags().BoolVar(&args.force, "force", false, "Proceed even with validation errors")
}

func manifestGenerateCmd(rootArgs *rootArgs, mgArgs *manifestGenerateArgs) *cobra.Command {
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
			l := NewLogger(rootArgs.logToStdErr, cmd.OutOrStdout(), cmd.ErrOrStderr())
			return manifestGenerate(rootArgs, mgArgs, l)
		}}

}

func manifestGenerate(args *rootArgs, mgArgs *manifestGenerateArgs, l *Logger) error {
	if err := configLogs(args.logToStdErr); err != nil {
		return fmt.Errorf("could not configure logs: %s", err)
	}

	ysf, err := yamlFromSetFlags(mgArgs.set, mgArgs.force, l)
	if err != nil {
		return err
	}

	manifests, _, err := GenManifests(mgArgs.inFilename, ysf, mgArgs.force, nil, l)
	if err != nil {
		return err
	}

	if mgArgs.outFilename == "" {
		for _, m := range orderedManifests(manifests) {
			l.print(m + "\n")
		}
	} else {
		if err := os.MkdirAll(mgArgs.outFilename, os.ModePerm); err != nil {
			return err
		}
		if err := manifest.RenderToDir(manifests, mgArgs.outFilename, args.dryRun); err != nil {
			return err
		}
	}

	return nil
}

// GenManifests generates a manifest map, keyed by the component name, from input file list and a YAML tree
// representation of path-values passed through the --set flag.
// If force is set, validation errors will not cause processing to abort but will result in warnings going to the
// supplied logger.
func GenManifests(inFilename []string, setOverlayYAML string, force bool,
	kubeConfig *rest.Config, l *Logger) (name.ManifestMap, *v1alpha1.IstioOperatorSpec, error) {
	mergedYAML, _, err := GenerateConfig(inFilename, setOverlayYAML, force, kubeConfig, l)
	if err != nil {
		return nil, nil, err
	}
	mergedIOPS, err := unmarshalAndValidateIOPS(mergedYAML, force, l)
	if err != nil {
		return nil, nil, err
	}

	t, err := translate.NewTranslator(version.OperatorBinaryVersion.MinorVersion)
	if err != nil {
		return nil, nil, err
	}

	cp, err := controlplane.NewIstioOperator(mergedIOPS, t)
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
