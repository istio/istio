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
	"io/ioutil"
	"os"
	"strings"
	"time"

	goversion "github.com/hashicorp/go-version"
	"github.com/spf13/cobra"

	"istio.io/operator/pkg/compare"
	"istio.io/operator/pkg/hooks"
	"istio.io/operator/pkg/manifest"
	opversion "istio.io/operator/version"
	"istio.io/pkg/log"
)

const (
	// The maximum duration the command will wait until the apply deployment reaches a ready state
	upgradeWaitSecWhenApply = 300 * time.Second
	// The duration that the command will wait between each check of the upgraded version.
	upgradeWaitSecCheckVerPerLoop = 10 * time.Second
	// The maximum number of attempts that the command will check for the upgrade completion,
	// which means only the target version exist and the old version pods have been terminated.
	upgradeWaitCheckVerMaxAttempts = 60

	// This message provide the guide of how to upgrade Istio data plane
	upgradeSidecarMessage = "To upgrade the Istio data plane, you will need to re-inject it.\n" +
		"If you’re using automatic sidecar injection, you can upgrade the sidecar by doing a rolling" +
		" update for all the pods:\n" +
		"    kubectl rollout restart deployment --namespace <namespace with auto injection>\n" +
		"If you’re using manual injection, you can upgrade the sidecar by executing:\n" +
		"    kubectl apply -f < (istioctl kube-inject -f <original application deployment yaml>)"
)

type upgradeArgs struct {
	// inFilename is the path to the input IstioOperator CR.
	inFilename string
	// versionsURI is a URI pointing to a YAML formatted versions mapping.
	versionsURI string
	// kubeConfigPath is the path to kube config file.
	kubeConfigPath string
	// context is the cluster context in the kube config.
	context string
	// wait is flag that indicates whether to wait resources ready before exiting.
	wait bool
	// skipConfirmation means skipping the prompting confirmation for value changes in this upgrade.
	skipConfirmation bool
	// force means directly applying the upgrade without eligibility checks.
	force bool
}

// addUpgradeFlags adds upgrade related flags into cobra command
func addUpgradeFlags(cmd *cobra.Command, args *upgradeArgs) {
	cmd.PersistentFlags().StringVarP(&args.inFilename, "filename",
		"f", "", "Path to file containing IstioOperator CustomResource")
	cmd.PersistentFlags().StringVarP(&args.versionsURI, "versionsURI", "u",
		versionsMapURL, "URI for operator versions to Istio versions map")
	cmd.PersistentFlags().StringVarP(&args.kubeConfigPath, "kubeconfig",
		"c", "", "Path to kube config")
	cmd.PersistentFlags().StringVar(&args.context, "context", "",
		"The name of the kubeconfig context to use")
	cmd.PersistentFlags().BoolVar(&args.skipConfirmation, "skip-confirmation", false,
		"If skip-confirmation is set, skips the prompting confirmation for value changes in this upgrade")
	cmd.PersistentFlags().BoolVarP(&args.wait, "wait", "w", false,
		"Wait, if set will wait until all Pods, Services, and minimum number of Pods "+
			"of a Deployment are in a ready state before the command exits. "+
			"It will wait for a maximum duration of "+(upgradeWaitSecCheckVerPerLoop*
			upgradeWaitCheckVerMaxAttempts).String())
	cmd.PersistentFlags().BoolVar(&args.force, "force", false,
		"Apply the upgrade without eligibility checks")
}

// Upgrade command upgrades Istio control plane in-place with eligibility checks
func UpgradeCmd() *cobra.Command {
	macArgs := &upgradeArgs{}
	rootArgs := &rootArgs{}
	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade Istio control plane in-place",
		Long: "The upgrade command checks for upgrade version eligibility and," +
			" if eligible, upgrades the Istio control plane components in-place. Warning: " +
			"traffic may be disrupted during upgrade. Please ensure PodDisruptionBudgets " +
			"are defined to maintain service continuity.",
		RunE: func(cmd *cobra.Command, args []string) (e error) {
			l := NewLogger(rootArgs.logToStdErr, cmd.OutOrStdout(), cmd.OutOrStderr())
			initLogsOrExit(rootArgs)
			err := upgrade(rootArgs, macArgs, l)
			if err != nil {
				log.Infof("Error: %v\n", err)
			}
			return err
		},
	}
	addFlags(cmd, rootArgs)
	addUpgradeFlags(cmd, macArgs)
	return cmd
}

// upgrade is the main function for Upgrade command
func upgrade(rootArgs *rootArgs, args *upgradeArgs, l *Logger) (err error) {
	args.inFilename = strings.TrimSpace(args.inFilename)

	// Generate IOPS objects
	targetIOPSYaml, targetIOPS, err := genIOPS(args.inFilename, "", "", "", args.force, l)
	if err != nil {
		return fmt.Errorf("failed to generate IOPS from file %s, error: %s", args.inFilename, err)
	}

	// Get the target version from the tag in the IOPS
	targetVersion := targetIOPS.GetTag()
	if targetVersion != opversion.OperatorVersionString {
		if !args.force {
			return fmt.Errorf("the target version %v is not supported by istioctl %v, "+
				"please download istioctl %v and run upgrade again", targetVersion,
				opversion.OperatorVersionString, targetVersion)
		}
	}

	// Create a kube client from args.kubeConfigPath and  args.context
	kubeClient, err := manifest.NewClient(args.kubeConfigPath, args.context)
	if err != nil {
		return fmt.Errorf("failed to connect Kubernetes API server, error: %v", err)
	}

	// Get Istio control plane namespace
	//TODO(elfinhe): support components distributed in multiple namespaces
	istioNamespace := targetIOPS.MeshConfig.RootNamespace

	// Read the current Istio version from the the cluster
	currentVersion, err := retrieveControlPlaneVersion(kubeClient, istioNamespace, l)
	if err != nil && !args.force {
		return fmt.Errorf("failed to read the current Istio version, error: %v", err)
	}

	// Check if the upgrade currentVersion -> targetVersion is supported
	err = checkSupportedVersions(currentVersion, targetVersion, args.versionsURI, l)
	if err != nil && !args.force {
		return fmt.Errorf("upgrade version check failed: %v -> %v. Error: %v",
			currentVersion, targetVersion, err)
	}
	l.logAndPrintf("Upgrade version check passed: %v -> %v.\n", currentVersion, targetVersion)

	// Read the overridden IOPS from args.inFilename
	overrideIOPSYaml := ""
	if args.inFilename != "" {
		b, err := ioutil.ReadFile(args.inFilename)
		if err != nil {
			return fmt.Errorf("failed to read override IOPS from file: %v, error: %v", args.inFilename, err)
		}
		overrideIOPSYaml = string(b)
	}

	// Generates IOPS for args.inFilename IOP specs yaml. Param force is set to true to
	// skip the validation because the code only has the validation proto for the
	// target version.
	currentIOPSYaml, _, err := genIOPS(args.inFilename, "", "", currentVersion, true, l)
	if err != nil {
		return fmt.Errorf("failed to generate IOPS from file: %s for the current version: %s, error: %v",
			args.inFilename, currentVersion, err)
	}
	checkUpgradeIOPS(currentIOPSYaml, targetIOPSYaml, overrideIOPSYaml, l)

	waitForConfirmation(args.skipConfirmation, l)

	// Run pre-upgrade hooks
	hparams := &hooks.HookCommonParams{
		SourceVer:  currentVersion,
		TargetVer:  targetVersion,
		SourceIOPS: targetIOPS,
		TargetIOPS: targetIOPS,
	}
	errs := hooks.RunPreUpgradeHooks(kubeClient, hparams, rootArgs.dryRun)
	if len(errs) != 0 && !args.force {
		return fmt.Errorf("failed in pre-upgrade hooks, error: %v", errs.ToError())
	}

	// Apply the Istio Control Plane specs reading from inFilename to the cluster
	err = genApplyManifests(nil, args.inFilename, args.force, rootArgs.dryRun,
		rootArgs.verbose, args.kubeConfigPath, args.context, args.wait, upgradeWaitSecWhenApply, l)
	if err != nil {
		return fmt.Errorf("failed to apply the Istio Control Plane specs. Error: %v", err)
	}

	// Run post-upgrade hooks
	errs = hooks.RunPostUpgradeHooks(kubeClient, hparams, rootArgs.dryRun)
	if len(errs) != 0 && !args.force {
		return fmt.Errorf("failed in post-upgrade hooks, error: %v", errs.ToError())
	}

	if !args.wait {
		l.logAndPrintf("Upgrade submitted. Please use `istioctl version` to check the current versions.")
		l.logAndPrintf(upgradeSidecarMessage)
		return nil
	}

	// Waits for the upgrade to complete by periodically comparing the each
	// component version to the target version.
	err = waitUpgradeComplete(kubeClient, istioNamespace, targetVersion, l)
	if err != nil {
		return fmt.Errorf("failed to wait for the upgrade to complete. Error: %v", err)
	}

	// Read the upgraded Istio version from the the cluster
	upgradeVer, err := retrieveControlPlaneVersion(kubeClient, istioNamespace, l)
	if err != nil {
		return fmt.Errorf("failed to read the upgraded Istio version. Error: %v", err)
	}

	l.logAndPrintf("Success. Now the Istio control plane is running at version %v.\n", upgradeVer)
	l.logAndPrintf(upgradeSidecarMessage)
	return nil
}

// checkUpgradeIOPS checks the upgrade eligibility by comparing the current IOPS with the target IOPS
func checkUpgradeIOPS(curIOPS, tarIOPS, ignoreIOPS string, l *Logger) {
	diff := compare.YAMLCmpWithIgnore(curIOPS, tarIOPS, nil, ignoreIOPS)
	if diff == "" {
		l.logAndPrintf("Upgrade check: IOPS unchanged. The target IOPS are identical to the current IOPS.\n")
	} else {
		l.logAndPrintf("Upgrade check: Warning!!! The following IOPS will be changed as part of upgrade. "+
			"Please double check they are correct:\n%s", diff)
	}
}

// waitForConfirmation waits for user's confirmation if skipConfirmation is not set
func waitForConfirmation(skipConfirmation bool, l *Logger) {
	if skipConfirmation {
		return
	}
	if !confirm("Confirm to proceed [y/N]?", os.Stdout) {
		l.logAndFatalf("Abort.")
	}
}

// checkSupportedVersions checks if the upgrade cur -> tar is supported by the tool
func checkSupportedVersions(cur, tar, versionsURI string, l *Logger) error {
	tarGoVersion, err := goversion.NewVersion(tar)
	if err != nil {
		return fmt.Errorf("failed to parse the target version: %v", tar)
	}

	compatibleMap, err := getVersionCompatibleMap(versionsURI, tarGoVersion, l)
	if err != nil {
		return err
	}

	curGoVersion, err := goversion.NewVersion(cur)
	if err != nil {
		return fmt.Errorf("failed to parse the current version: %v, error: %v", cur, err)
	}

	if !compatibleMap.SupportedIstioVersions.Check(curGoVersion) {
		return fmt.Errorf("upgrade is currently not supported: %v -> %v", cur, tar)
	}

	return nil
}

// retrieveControlPlaneVersion retrieves the version number from the Istio control plane
func retrieveControlPlaneVersion(kubeClient manifest.ExecClient, istioNamespace string, l *Logger) (string, error) {
	cv, e := kubeClient.GetIstioVersions(istioNamespace)
	if e != nil {
		return "", fmt.Errorf("failed to retrieve Istio control plane version, error: %v", e)
	}

	if len(cv) == 0 {
		return "", fmt.Errorf("istio control plane not found in namespace: %v", istioNamespace)
	}

	for _, remote := range cv {
		l.logAndPrintf("Control Plane - %v", remote)
	}
	l.logAndPrint("")

	v, e := coalesceVersions(cv)
	if e != nil {
		return "", e
	}
	return v, nil
}

// waitUpgradeComplete waits for the upgrade to complete by periodically comparing the current component version
// to the target version.
func waitUpgradeComplete(kubeClient manifest.ExecClient, istioNamespace string, targetVer string, l *Logger) error {
	for i := 1; i <= upgradeWaitCheckVerMaxAttempts; i++ {
		sleepSeconds(upgradeWaitSecCheckVerPerLoop)
		cv, e := kubeClient.GetIstioVersions(istioNamespace)
		if e != nil {
			l.logAndPrintf("Failed to retrieve Istio control plane version, error: %v", e)
			continue
		}
		if cv == nil {
			l.logAndPrintf("Failed to find Istio namespace: %v", istioNamespace)
			continue
		}
		if identicalVersions(cv) && targetVer == cv[0].Version {
			l.logAndPrintf("Upgrade rollout completed. " +
				"All Istio control plane pods are running on the target version.\n\n")
			return nil
		}
		for _, remote := range cv {
			if targetVer != remote.Version {
				l.logAndPrintf("Control Plane - %v does not match the target version %s",
					remote, targetVer)
			}
		}
	}
	return fmt.Errorf("upgrade rollout unfinished. Maximum number of attempts exceeded")
}

// sleepSeconds sleeps for n seconds, printing a dot '.' per second
func sleepSeconds(duration time.Duration) {
	for t := time.Duration(0); t < duration; t += time.Second {
		time.Sleep(time.Second)
		fmt.Print(".")
	}
	fmt.Println()
}

// coalesceVersions coalesces all Istio control plane components versions
func coalesceVersions(cv []manifest.ComponentVersion) (string, error) {
	if len(cv) == 0 {
		return "", fmt.Errorf("empty list of ComponentVersion")
	}
	if !identicalVersions(cv) {
		return "", fmt.Errorf("different versions of Istio components found: %v", cv)
	}
	return cv[0].Version, nil
}

// identicalVersions checks if Istio control plane components are on the same version
func identicalVersions(cv []manifest.ComponentVersion) bool {
	exemplar := cv[0]
	for i := 1; i < len(cv); i++ {
		if exemplar.Version != cv[i].Version {
			return false
		}
	}
	return true
}
