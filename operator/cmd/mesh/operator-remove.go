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

	"istio.io/istio/operator/pkg/kubectlcmd"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/object"
	"istio.io/istio/operator/pkg/util/log"
)

type operatorRemoveArgs struct {
	operatorInitArgs
	// force proceeds even if there are validation errors
	force bool
}

type manifestDeleter func(manifestStr, componentName string, opts *kubectlcmd.Options, l *log.ConsoleLogger) bool

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
			l := log.NewConsoleLogger(rootArgs.logToStdErr, cmd.OutOrStdout(), cmd.OutOrStderr())
			operatorRemove(rootArgs, orArgs, l, defaultManifestDeleter)
		}}
}

// operatorRemove removes the Istio operator controller from the cluster.
func operatorRemove(args *rootArgs, orArgs *operatorRemoveArgs, l *log.ConsoleLogger, deleteManifestFunc manifestDeleter) {
	initLogsOrExit(args)

	installed, err := isControllerInstalled(orArgs.kubeConfigPath, orArgs.context, orArgs.common.operatorNamespace)
	if installed && err != nil {
		l.LogAndFatal(err)
	}
	if !installed {
		l.LogAndPrintf("Operator controller is not installed in %s namespace (no Deployment detected).", orArgs.common.operatorNamespace)
		if !orArgs.force {
			l.LogAndFatal("Aborting, use --force to override.")
		}
	}

	l.LogAndPrintf("Using operator Deployment image: %s/operator:%s", orArgs.common.hub, orArgs.common.tag)

	_, mstr, err := renderOperatorManifest(args, &orArgs.common, l)
	if err != nil {
		l.LogAndFatal(err)
	}

	scope.Debugf("Using the following manifest to remove operator:\n%s\n", mstr)

	opts := &kubectlcmd.Options{
		DryRun:      args.dryRun,
		Verbose:     args.verbose,
		WaitTimeout: 1 * time.Minute,
		Kubeconfig:  orArgs.kubeConfigPath,
		Context:     orArgs.context,
	}

	if _, _, err := manifest.InitK8SRestClient(opts.Kubeconfig, opts.Context); err != nil {
		l.LogAndFatal(err)
	}

	success := deleteManifestFunc(mstr, "Operator", opts, l)
	if !success {
		l.LogAndPrint("\n*** Errors were logged during manifest deletion. Please check logs above. ***\n")
		return
	}

	l.LogAndPrint("\n*** Success. ***\n")
}

func deleteManifest(manifestStr, componentName string, opts *kubectlcmd.Options, l *log.ConsoleLogger) bool {
	l.LogAndPrintf("Deleting manifest for component %s...", componentName)
	objs, err := object.ParseK8sObjectsFromYAMLManifest(manifestStr)
	if err != nil {
		l.LogAndPrint("Parse error: ", err, "\n")
		return false
	}
	stdout, stderr, err := kubectlcmd.New().Delete(manifestStr, opts)

	success := true
	if err != nil {
		cs := fmt.Sprintf("Component %s delete returned the following errors:", componentName)
		l.LogAndPrintf("\n%s\n%s", cs, strings.Repeat("=", len(cs)))
		l.LogAndPrint("Error: ", err, "\n")
		success = false
	} else {
		l.LogAndPrintf("Component %s deleted successfully.", componentName)
		if opts.Verbose {
			l.LogAndPrintf("The following parseObjectSetFromManifest were deleted:\n%s", k8sObjectsString(objs))
		}
	}

	if !ignoreError(stderr) {
		l.LogAndPrint("Error detail:\n", stderr, "\n")
		l.LogAndPrint(stdout, "\n")
		success = false
	}
	return success
}
