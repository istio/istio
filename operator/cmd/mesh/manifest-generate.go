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
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"istio.io/istio/operator/pkg/helm"
	"istio.io/istio/operator/pkg/helmreconciler"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/object"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/pkg/kube"
	"istio.io/pkg/log"
)

type ManifestGenerateArgs struct {
	// InFilenames is an array of paths to the input IstioOperator CR files.
	InFilenames []string
	// OutFilename is the path to the generated output directory.
	OutFilename string

	// EnableClusterSpecific determines if the current Kubernetes cluster will be used to autodetect values.
	// If false, generic defaults will be used. This is useful when generating once and then applying later.
	EnableClusterSpecific bool
	// KubeConfigPath is the path to kube config file.
	KubeConfigPath string
	// Context is the cluster context in the kube config
	Context string

	// Set is a string with element format "path=value" where path is an IstioOperator path and the value is a
	// value to set the node at that path to.
	Set []string
	// Force proceeds even if there are validation errors
	Force bool
	// ManifestsPath is a path to a charts and profiles directory in the local filesystem, or URL with a release tgz.
	ManifestsPath string
	// Revision is the Istio control plane revision the command targets.
	Revision string
	// Components is a list of strings specifying which component's manifests to be generated.
	Components []string
	// Filter is the list of components to render
	Filter []string
}

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

	cmd.PersistentFlags().StringVarP(&args.KubeConfigPath, "kubeconfig", "c", "", KubeConfigFlagHelpStr+" Requires --cluster-specific.")
	cmd.PersistentFlags().StringVar(&args.Context, "context", "", ContextFlagHelpStr+" Requires --cluster-specific.")
	cmd.PersistentFlags().BoolVar(&args.EnableClusterSpecific, "cluster-specific", false,
		"If enabled, the current cluster will be checked for cluster-specific setting detection.")
}

func ManifestGenerateCmd(rootArgs *RootArgs, mgArgs *ManifestGenerateArgs, logOpts *log.Options) *cobra.Command {
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

  # For setting boolean-string option, it should be enclosed quotes and escaped with a backslash (\).
  istioctl manifest generate --set meshConfig.defaultConfig.proxyMetadata.PROXY_XDS_VIA_AGENT=\"false\"
`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("generate accepts no positional arguments, got %#v", args)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			l := clog.NewConsoleLogger(cmd.OutOrStdout(), cmd.ErrOrStderr(), installerScope)
			return ManifestGenerate(rootArgs, mgArgs, logOpts, l)
		},
	}
}

func ManifestGenerate(args *RootArgs, mgArgs *ManifestGenerateArgs, logopts *log.Options, l clog.Logger) error {
	if err := configLogs(logopts); err != nil {
		return fmt.Errorf("could not configure logs: %s", err)
	}

	var kubeClient kube.CLIClient
	if mgArgs.EnableClusterSpecific {
		kc, _, err := KubernetesClients(mgArgs.KubeConfigPath, mgArgs.Context, l)
		if err != nil {
			return err
		}
		kubeClient = kc
	}

	manifests, _, err := manifest.GenManifests(mgArgs.InFilenames, applyFlagAliases(mgArgs.Set, mgArgs.ManifestsPath, mgArgs.Revision),
		mgArgs.Force, mgArgs.Filter, kubeClient, l)
	if err != nil {
		return err
	}

	if len(mgArgs.Components) != 0 {
		filteredManifests := name.ManifestMap{}
		for _, cArg := range mgArgs.Components {
			componentName := name.ComponentName(cArg)
			if cManifests, ok := manifests[componentName]; ok {
				filteredManifests[componentName] = cManifests
			} else {
				return fmt.Errorf("incorrect component name: %s. Valid options: %v", cArg, name.AllComponentNames)
			}
		}
		manifests = filteredManifests
	}

	if mgArgs.OutFilename == "" {
		ordered, err := orderedManifests(manifests)
		if err != nil {
			return fmt.Errorf("failed to order manifests: %v", err)
		}
		for _, m := range ordered {
			l.Print(m + object.YAMLSeparator)
		}
	} else {
		if err := os.MkdirAll(mgArgs.OutFilename, os.ModePerm); err != nil {
			return err
		}
		if err := RenderToDir(manifests, mgArgs.OutFilename, args.DryRun, l); err != nil {
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
	l.LogAndPrintf("Component dependencies tree: \n%s", helmreconciler.InstallTreeString())
	l.LogAndPrintf("Rendering manifests to output dir %s", outputDir)
	return renderRecursive(manifests, helmreconciler.InstallTree, outputDir, dryRun, l)
}

func renderRecursive(manifests name.ManifestMap, installTree helmreconciler.ComponentTree, outputDir string, dryRun bool, l clog.Logger) error {
	for k, v := range installTree {
		componentName := string(k)
		// In cases (like gateways) where multiple instances can exist, concatenate the manifests and apply as one.
		ym := strings.Join(manifests[k], helm.YAMLSeparator)
		l.LogAndPrintf("Rendering: %s", componentName)
		dirName := filepath.Join(outputDir, componentName)
		if !dryRun {
			if err := os.MkdirAll(dirName, os.ModePerm); err != nil {
				return fmt.Errorf("could not create directory %s; %s", outputDir, err)
			}
		}
		fname := filepath.Join(dirName, componentName) + ".yaml"
		l.LogAndPrintf("Writing manifest to %s", fname)
		if !dryRun {
			if err := os.WriteFile(fname, []byte(ym), 0o644); err != nil {
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
