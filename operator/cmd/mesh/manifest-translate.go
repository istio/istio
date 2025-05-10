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
	_ "embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/operator/pkg/component"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/render"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/test/util/tmpl"
)

//go:embed readme.tpl
var readmeTemplate string

type ManifestTranslateArgs struct {
	// InFilenames is an array of paths to the input IstioOperator CR files.
	InFilenames []string

	// Set is a string with element format "path=value" where path is an IstioOperator path and the value is a
	// value to set the node at that path to.
	Set []string
	// ManifestsPath is a path to a charts and profiles directory in the local filesystem with a release tgz.
	ManifestsPath string
	// Revision is the Istio control plane revision the command targets.
	Revision string

	// Output path to print out instructions and configurations
	Output string
}

func addManifestTranslateFlags(cmd *cobra.Command, args *ManifestTranslateArgs) {
	cmd.PersistentFlags().StringSliceVarP(&args.InFilenames, "filename", "f", nil, filenameFlagHelpStr)
	cmd.PersistentFlags().StringArrayVarP(&args.Set, "set", "s", nil, setFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&args.ManifestsPath, "manifests", "d", "", ManifestsFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&args.Revision, "revision", "r", "", revisionFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&args.Output, "output", "o", "", "where to put translated outputs")
}

func ManifestTranslateCmd(ctx cli.Context, mgArgs *ManifestTranslateArgs) *cobra.Command {
	return &cobra.Command{
		Use:   "translate",
		Short: "Translates an Istio install manifest to Helm values",
		Long:  "The translate subcommand translates an Istio install manifest and outputs to the console by default.",
		// nolint: lll
		Example: `  # Translate an IstioOperator yaml file into helm values
  istioctl manifest translate -f istio.yaml

  # Translate a default Istio installation
  istioctl manifest translate

  # Enable Tracing
  istioctl manifest translate --set meshConfig.enableTracing=true

  # Translate the demo profile
  istioctl manifest translate --set profile=demo

  # To override a setting that includes dots, escape them with a backslash (\).  Your shell may require enclosing quotes.
  istioctl manifest translate --set "values.sidecarInjectorWebhook.injectedAnnotations.container\.apparmor\.security\.beta\.kubernetes\.io/istio-proxy=runtime/default"
`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("translate accepts no positional arguments, got %#v", args)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if kubeClientFunc == nil {
				kubeClientFunc = ctx.CLIClient
			}
			if mgArgs.Output == "" {
				var err error
				mgArgs.Output, err = os.MkdirTemp(os.TempDir(), "istioctl-migrate-")
				if err != nil {
					return err
				}
			}
			var kubeClient kube.CLIClient
			l := clog.NewConsoleLogger(cmd.OutOrStdout(), cmd.ErrOrStderr(), installerScope)
			return ManifestTranslate(kubeClient, mgArgs, l)
		},
	}
}

func ManifestTranslate(kubeClient kube.CLIClient, mgArgs *ManifestTranslateArgs, l clog.Logger) error {
	setFlags := applyFlagAliases(mgArgs.Set, mgArgs.ManifestsPath, mgArgs.Revision)
	istioctlGeneratedManifests, _, err := render.GenerateManifest(mgArgs.InFilenames, setFlags, false, kubeClient, nil)
	if err != nil {
		return err
	}
	generatedManifestMap := make(map[component.Name]manifest.ManifestSet)
	for _, m := range istioctlGeneratedManifests {
		generatedManifestMap[m.Component] = m
	}
	res, err := render.Migrate(mgArgs.InFilenames, setFlags, kubeClient)
	if err != nil {
		return err
	}
	out := mgArgs.Output
	write := func(name string, contents string) error {
		perm := 0o644
		if filepath.Ext(name) == ".sh" {
			perm = 0o755
		}
		return os.WriteFile(filepath.Join(out, name), []byte(contents), fs.FileMode(perm))
	}
	results := []string{}
	for _, info := range res.Components {
		name := ptr.NonEmptyOrDefault(info.ComponentSpec.Name, info.Component.SpecName)
		if info.Component.ReleaseName == "" {
			results = append(results, fmt.Sprintf(`* ❌ **Component %s**: migration is **NOT** directly supported!`,
				"`"+name+"`"))
			continue
		}
		commands := []string{"#!/usr/bin/env bash", "", "# Label/Annotate resources to mark them a part of the Helm release."}
		ns := info.ComponentSpec.Namespace
		vals, _ := info.Values.GetPathMap("spec.values")
		valuesName := fmt.Sprintf("%v-values.yaml", name)
		if err := write(valuesName, vals.YAML()); err != nil {
			return err
		}
		for _, m := range info.Manifest {
			gk := m.GetObjectKind().GroupVersionKind().GroupKind().String()
			nsFlag := ""
			if m.GetNamespace() != "" {
				nsFlag = " --namespace=" + m.GetNamespace()
			}
			commands = append(commands,
				fmt.Sprintf("kubectl annotate %s%s %s meta.helm.sh/release-name=%s", gk, nsFlag, m.GetName(), name),
				fmt.Sprintf("kubectl annotate %s%s %s meta.helm.sh/release-namespace=%s", gk, nsFlag, m.GetName(), ns),
				fmt.Sprintf("kubectl label %s%s %s app.kubernetes.io/managed-by=Helm", gk, nsFlag, m.GetName()))
		}
		commands = append(commands, "\n", "# Run the actual Helm install operation",
			fmt.Sprintf("helm upgrade --install %s --namespace %s -f %s oci://gcr.io/istio-release/charts/%s",
				name, ns, valuesName, info.Component.ReleaseName))

		if err := write(fmt.Sprintf("install-%s.sh", name), strings.Join(commands, "\n")+"\n"); err != nil {
			return err
		}
		diffWarn := ""
		helmManifests := strings.Join(sortManifests(info.Manifest), "\n---\n")
		generatedManifest, ok := generatedManifestMap[info.Component.UserFacingName]
		if !ok {
			continue
		}
		istioctlManifests := strings.Join(sortManifests(generatedManifest.Manifests), "\n---\n")
		if helmManifests != istioctlManifests {
			helmName := fmt.Sprintf("diff-%s-helm-output.yaml", name)
			istioctlName := fmt.Sprintf("diff-%s-istioctl-output.yaml", name)
			if err := write(helmName, helmManifests); err != nil {
				return err
			}
			if err := write(istioctlName, istioctlManifests); err != nil {
				return err
			}
			diffWarn = fmt.Sprintf(`
  ⚠️ Component rendering is different between Istioctl and Helm!
  This may be from incompatibilities between the two installation methods.
  Review the difference between the two and take appropriate actions to resolve these, if needed: %s.
`, "`"+"diff "+helmName+" "+istioctlName+"`")
		}
		results = append(results, fmt.Sprintf(`* ✅ **Component %s**: migration is supported!
%s
  The translated values have been written to %s.
  You may use these directly, or follow the guided %s script.`,
			"`"+name+"`", diffWarn, valuesName, fmt.Sprintf("`install-%s.sh`", name)))

	}
	args := map[string]any{
		"Results": results,
	}
	readme, err := tmpl.Evaluate(readmeTemplate, args)
	if err != nil {
		return err
	}
	if err := write("README.md", readme); err != nil {
		return err
	}
	l.LogAndPrintf("Output written to %v! See the README.md for next steps", mgArgs.Output)
	return nil
}
