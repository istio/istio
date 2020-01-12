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
	"strings"
	"time"

	"github.com/spf13/cobra"

	"istio.io/operator/pkg/kubectlcmd"
	"istio.io/operator/pkg/manifest"
	"istio.io/operator/pkg/object"
	"istio.io/pkg/log"
)

type operatorRemoveArgs struct {
	operatorInitArgs
	// force proceeds even if there are validation errors
	force bool
}

type manifestDeleter func(manifestStr, componentName string, opts *kubectlcmd.Options, l *Logger) bool

var (
	defaultManifestDeleter = deleteManifest
)

func addOperatorRemoveFlags(cmd *cobra.Command, oiArgs *operatorRemoveArgs) {
	addOperatorInitFlags(cmd, &oiArgs.operatorInitArgs)
	cmd.PersistentFlags().BoolVar(&oiArgs.force, "force", false, "Proceed even with errors")
}

func operatorRemoveCmd(rootArgs *rootArgs, orArgs *operatorRemoveArgs) *cobra.Command {
	return &cobra.Command{
		Use:   "remove",
		Short: "Removes the Istio operator controller from the cluster.",
		Long:  "The remove subcommand removes the Istio operator controller from the cluster.",
		Args:  cobra.ExactArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			l := NewLogger(rootArgs.logToStdErr, cmd.OutOrStdout(), cmd.OutOrStderr())
			operatorRemove(rootArgs, orArgs, l, defaultManifestDeleter)
		}}
}

// operatorRemove removes the Istio operator controller from the cluster.
func operatorRemove(args *rootArgs, orArgs *operatorRemoveArgs, l *Logger, deleteManifestFunc manifestDeleter) {
	initLogsOrExit(args)

	installed, err := isControllerInstalled(orArgs.kubeConfigPath, orArgs.context, orArgs.operatorNamespace)
	if installed && err != nil {
		l.logAndFatal(err)
	}
	if !installed {
		l.logAndPrintf("Operator controller is not installed in %s namespace (no Deployment detected).", orArgs.operatorNamespace)
		if !orArgs.force {
			l.logAndFatal("Aborting, use --force to override.")
		}
	}

	l.logAndPrintf("Using operator Deployment image: %s/operator:%s", orArgs.hub, orArgs.tag)

	mstr, err := renderOperatorManifest(args, &orArgs.operatorInitArgs, l)
	if err != nil {
		l.logAndFatal(err)
	}

	log.Infof("Using the following manifest to install operator:\n%s\n", mstr)

	opts := &kubectlcmd.Options{
		DryRun:      args.dryRun,
		Verbose:     args.verbose,
		WaitTimeout: 1 * time.Minute,
		Kubeconfig:  orArgs.kubeConfigPath,
		Context:     orArgs.context,
	}

	if err := manifest.InitK8SRestClient(opts.Kubeconfig, opts.Context); err != nil {
		l.logAndFatal(err)
	}

	success := deleteManifestFunc(mstr, "Operator", opts, l)
	if !success {
		l.logAndPrint("\n*** Errors were logged during deleteManifestFunc operation. Please check logs above. ***\n")
		return
	}

	l.logAndPrint("\n*** Success. ***\n")
}

func deleteManifest(manifestStr, componentName string, opts *kubectlcmd.Options, l *Logger) bool {
	l.logAndPrintf("Deleting manifest for component %s...", componentName)
	objs, err := object.ParseK8sObjectsFromYAMLManifest(manifestStr)
	if err != nil {
		l.logAndPrint("Parse error: ", err, "\n")
		return false
	}
	stdout, stderr, err := kubectlcmd.New().Delete(manifestStr, opts)

	success := true
	if err != nil {
		cs := fmt.Sprintf("Component %s delete returned the following errors:", componentName)
		l.logAndPrintf("\n%s\n%s", cs, strings.Repeat("=", len(cs)))
		l.logAndPrint("Error: ", err, "\n")
		success = false
	} else {
		l.logAndPrintf("Component %s deleted successfully.", componentName)
		if opts.Verbose {
			l.logAndPrintf("The following objects were deleted:\n%s", k8sObjectsString(objs))
		}
	}

	if !ignoreError(stderr) {
		l.logAndPrint("Error detail:\n", stderr, "\n")
		l.logAndPrint(stdout, "\n")
		success = false
	}
	return success
}
