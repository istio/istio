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
	"time"

	"istio.io/istio/operator/pkg/kubectlcmd"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/version"

	"github.com/spf13/cobra"
)

type manifestApplyArgs struct {
	// inFilenames is an array of paths to the input IstioOperator CR files.
	inFilenames []string
	// kubeConfigPath is the path to kube config file.
	kubeConfigPath string
	// context is the cluster context in the kube config
	context string
	// readinessTimeout is maximum time to wait for all Istio resources to be ready.
	readinessTimeout time.Duration
	// wait is flag that indicates whether to wait resources ready before exiting.
	wait bool
	// skipConfirmation determines whether the user is prompted for confirmation.
	// If set to true, the user is not prompted and a Yes response is assumed in all cases.
	skipConfirmation bool
	// force proceeds even if there are validation errors
	force bool
	// set is a string with element format "path=value" where path is an IstioOperator path and the value is a
	// value to set the node at that path to.
	set []string
}

func addManifestApplyFlags(cmd *cobra.Command, args *manifestApplyArgs) {
	cmd.PersistentFlags().StringSliceVarP(&args.inFilenames, "filename", "f", nil, filenameFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&args.kubeConfigPath, "kubeconfig", "c", "", "Path to kube config")
	cmd.PersistentFlags().StringVar(&args.context, "context", "", "The name of the kubeconfig context to use")
	cmd.PersistentFlags().BoolVarP(&args.skipConfirmation, "skip-confirmation", "y", false, skipConfirmationFlagHelpStr)
	cmd.PersistentFlags().BoolVar(&args.force, "force", false, "Proceed even with validation errors")
	cmd.PersistentFlags().DurationVar(&args.readinessTimeout, "readiness-timeout", 300*time.Second, "Maximum seconds to wait for all Istio resources to be ready."+
		" The --wait flag must be set for this flag to apply")
	cmd.PersistentFlags().BoolVarP(&args.wait, "wait", "w", false, "Wait, if set will wait until all Pods, Services, and minimum number of Pods "+
		"of a Deployment are in a ready state before the command exits. It will wait for a maximum duration of --readiness-timeout seconds")
	cmd.PersistentFlags().StringArrayVarP(&args.set, "set", "s", nil, SetFlagHelpStr)
}

func manifestApplyCmd(rootArgs *rootArgs, maArgs *manifestApplyArgs) *cobra.Command {
	return &cobra.Command{
		Use:   "apply",
		Short: "Generates and applies an Istio install manifest.",
		Long:  "The apply subcommand generates an Istio install manifest and applies it to a cluster.",
		Example: "istioctl manifest apply  # installs the default profile on the current Kubernetes cluster context\n" +
			"istioctl manifest apply --set values.global.mtls.enabled=true --set values.global.controlPlaneSecurityEnabled=true\n" +
			"istioctl manifest apply --set profile=demo\n" +
			"istioctl manifest apply --set installPackagePath=~/istio-releases/istio-1.4.3/install/kubernetes/operator/charts",
		Args: cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			l := NewLogger(rootArgs.logToStdErr, cmd.OutOrStdout(), cmd.ErrOrStderr())
			// Warn users if they use `manifest apply` without any config args.
			if len(maArgs.inFilenames) == 0 && len(maArgs.set) == 0 && !rootArgs.dryRun && !maArgs.skipConfirmation {
				if !confirm("This will install the default Istio profile into the cluster. Proceed? (y/N)", cmd.OutOrStdout()) {
					cmd.Print("Cancelled.\n")
					os.Exit(1)
				}
			}
			if err := configLogs(rootArgs.logToStdErr); err != nil {
				return fmt.Errorf("could not configure logs: %s", err)
			}
			if err := ApplyManifests(maArgs.set, maArgs.inFilenames, maArgs.force, rootArgs.dryRun, rootArgs.verbose,
				maArgs.kubeConfigPath, maArgs.context, maArgs.wait, maArgs.readinessTimeout, l); err != nil {
				return fmt.Errorf("failed to generate and apply manifests, error: %v", err)
			}

			return nil
		}}
}

// ApplyManifests generates manifests from the given input files and --set flag overlays and applies them to the
// cluster. See GenManifests for more description of the manifest generation process.
//  force   validation warnings are written to logger but command is not aborted
//  dryRun  all operations are done but nothing is written
//  verbose full manifests are output
//  wait    block until Services and Deployments are ready, or timeout after waitTimeout
func ApplyManifests(setOverlay []string, inFilenames []string, force bool, dryRun bool, verbose bool,
	kubeConfigPath string, context string, wait bool, waitTimeout time.Duration, l *Logger) error {

	ysf, err := yamlFromSetFlags(setOverlay, force, l)
	if err != nil {
		return err
	}

	kubeconfig, err := manifest.InitK8SRestClient(kubeConfigPath, context)
	if err != nil {
		return err
	}
	manifests, _, err := GenManifests(inFilenames, ysf, force, kubeconfig, l)
	if err != nil {
		return fmt.Errorf("failed to generate manifest: %v", err)
	}
	opts := &kubectlcmd.Options{
		DryRun:      dryRun,
		Verbose:     verbose,
		Wait:        wait,
		WaitTimeout: waitTimeout,
		Kubeconfig:  kubeConfigPath,
		Context:     context,
	}

	for cn := range name.DeprecatedComponentNamesMap {
		manifests[cn] = append(manifests[cn], fmt.Sprintf("# %s component has been deprecated.\n", cn))
	}

	out, err := manifest.ApplyAll(manifests, version.OperatorBinaryVersion, opts)
	if err != nil {
		return fmt.Errorf("failed to apply manifest with kubectl client: %v", err)
	}
	gotError := false

	for cn := range manifests {
		if out[cn].Err != nil {
			cs := fmt.Sprintf("Component %s - manifest apply returned the following errors:", cn)
			l.logAndPrintf("\n%s", cs)
			l.logAndPrint("Error: ", out[cn].Err, "\n")
			gotError = true
		}

		if !ignoreError(out[cn].Stderr) {
			l.logAndPrint("Error detail:\n", out[cn].Stderr, "\n", out[cn].Stdout, "\n")
			gotError = true
		}
	}

	if gotError {
		l.logAndPrint("\n\n✘ Errors were logged during apply operation. Please check component installation logs above.\n")
		return fmt.Errorf("errors were logged during apply operation")
	}

	l.logAndPrint("\n\n✔ Installation complete\n")
	return nil
}
