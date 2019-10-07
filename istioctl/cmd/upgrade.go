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

package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ghodss/yaml"
	"github.com/spf13/cobra"
	"istio.io/operator/cmd/mesh"
	"istio.io/operator/pkg/compare"
	"istio.io/operator/pkg/manifest"
	"istio.io/pkg/version"
	opversion "istio.io/operator/version"
)

var (
	supportedVersionMap = map[string]map[string]bool{
		"1.2.0": {
			"1.2.3": true,
		},
		"1.3.0": {
			"1.3.1": true,
			"1.3.2": true,
		},
		"1.3.1": {
			"1.3.2": true,
		},
		"1.3.2": {
			"1.3.3": true,
		},
	}
)

type upgradeArgs struct {
	// inFilename is the path to the input IstioControlPlane CR.
	inFilename string
	// kubeConfigPath is the path to kube config file.
	kubeConfigPath string
	// context is the cluster context in the kube config
	context string
	// wait is flag that indicates whether to wait resources ready before exiting.
	wait bool
	// yes means don't ask for confirmation (asking for confirmation not implemented)
	yes bool
	// dryRun means running the upgrade process without actually applying the changes
	dryRun bool
}

func addUpgradeFlags(cmd *cobra.Command, args *upgradeArgs) {
	cmd.PersistentFlags().StringVarP(&args.inFilename, "filename", "f", "", "Path to file containing IstioControlPlane CustomResource")
	cmd.PersistentFlags().StringVarP(&args.kubeConfigPath, "kubeconfig", "c", "", "Path to kube config")
	cmd.PersistentFlags().StringVar(&args.context, "context", "", "The name of the kubeconfig context to use")
	cmd.PersistentFlags().BoolVarP(&args.yes, "yes", "y", false, "Do not ask for confirmation")
	cmd.PersistentFlags().BoolVarP(&args.wait, "wait", "w", false, "Wait, if set will wait until all Pods, Services, and minimum number of Pods "+
		"of a Deployment are in a ready state before the command exits. It will wait for a maximum duration of --readiness-timeout seconds")
	cmd.PersistentFlags().BoolVarP(&args.dryRun, "dryrun", "d", false,
		"Run the upgrade process without actually applying the changes")
}

// Upgrade command upgrade Istio control plane in-place with eligibility checks
func Upgrade() *cobra.Command {
	macArgs := &upgradeArgs{}
	cmd := &cobra.Command{
		Use:     "upgrade",
		Short:   "Upgrade Istio control plane in-place",
		Example: `istioctl x upgrade`,
		RunE: func(cmd *cobra.Command, args []string) (e error) {
			return upgrade(macArgs)
		},
	}

	addUpgradeFlags(cmd, macArgs)
	return cmd
}

func upgrade(args *upgradeArgs) (err error) {
	currentVer := retrieveControlPlaneVersion()
	targetVer := retrieveClientVersion()
	checkSupportedVersions(currentVer, targetVer)
	currentValues := readValuesFromInjectorConfigMap(args)
	targetValues := genValuesFromFile(args.inFilename)
	checkUpgradeValues(currentValues, targetValues)
	runUpgradeHooks(currentVer, targetVer, currentValues, targetValues)
	applyUpgradeManifest(args)
	upgradeVer := retrieveControlPlaneVersion()
	fmt.Printf("Success. Now the Istio control plane is running at version %v.", upgradeVer)
	return
}

func applyUpgradeManifest(args *upgradeArgs) {
	manifests, err := mesh.GenManifests(args.inFilename, "")
	if err != nil {
		fmt.Printf("Could not generate manifest: %v", err)
	}
	opts := &manifest.InstallOptions{
		DryRun:      args.dryRun,
		Verbose:     false,
		WaitTimeout: 300 * time.Second,
		Kubeconfig:  args.kubeConfigPath,
		Context:     args.context,
	}
	out, err := manifest.ApplyAll(manifests, opversion.OperatorBinaryVersion, opts)
	if err != nil {
		fmt.Printf("Failed to apply manifest with kubectl client: %v", err)
	}
	for cn := range manifests {
		if out[cn].Err != nil {
			cs := fmt.Sprintf("Component %s failed install:", cn)
			fmt.Print(fmt.Sprintf("\n%s\n%s\n", cs, strings.Repeat("=", len(cs))))
			fmt.Print("Error: ", out[cn].Err, "\n")
		} else {
			cs := fmt.Sprintf("Component %s installed successfully:", cn)
			fmt.Print(fmt.Sprintf("\n%s\n%s\n", cs, strings.Repeat("=", len(cs))))
		}

		if strings.TrimSpace(out[cn].Stderr) != "" {
			fmt.Print("Error detail:\n", out[cn].Stderr, "\n")
		}
		if strings.TrimSpace(out[cn].Stdout) != "" {
			fmt.Print("Stdout:\n", out[cn].Stdout, "\n")
		}
	}
}

type hook func(currentVer, targetVer, currentValues, targetValues string)

var (
	hooks = []hook{checkInit}
)

func runUpgradeHooks(currentVer, targetVer, currentValues, targetValues string) {
	for _, h := range hooks {
		h(currentVer, targetVer, currentValues, targetValues)
	}
}

func checkInit(currentVer, targetVer, currentValues, targetValues string) {
	kubeClient, err := clientExecFactory(kubeconfig, configContext)
	if err != nil {
		fmt.Printf("Abort. Failed to connect Kubernetes API server: %v", err)
		os.Exit(1)
	}

	pl, err := kubeClient.PodsForSelector(istioNamespace, "")
	for _, p := range pl.Items {
		if strings.Contains(p.Name, "istio-init-crd") {
			fmt.Printf("Abort. istio-init-crd pods exist: %v. " +
				"Istio was installed with non-operator methods, " +
				"please migrate to operator installation first.", p.Name)
			os.Exit(1)
		}
	}
}

func checkUpgradeValues(curValues string, tarValues string) {
	diff := compare.YAMLCmp(curValues, tarValues)
	if diff == "" {
		fmt.Println("Upgrade check: values are valid for upgrade.")
	} else {
		fmt.Printf("Upgrade check: values will be changed during the upgrade:\n%s", diff)
		os.Exit(1)
	}
}

func genValuesFromFile(filename string) string {
	values, err := mesh.GenValues(filename, "", "", "")
	if err != nil {
		fmt.Printf("Abort. Failed to generate values from file: %v, error: %v", filename, err)
		os.Exit(1)
	}
	return values
}

func readValuesFromInjectorConfigMap(args *upgradeArgs) string {
	kubeClient, err := clientExecFactory(kubeconfig, configContext)
	if err != nil {
		fmt.Printf("Abort. Failed to connect Kubernetes API server: %v", err)
		os.Exit(1)
	}
	configMapList, err := kubeClient.ConfigMapForSelector(istioNamespace, "istio=sidecar-injector")
	if err != nil || len(configMapList.Items) == 0 {
		fmt.Printf("Abort. Failed to retrieve sidecar-injector config map: %v", err)
		os.Exit(1)
	}

	jsonValues  := ""
	foundValues := false
	for _, item := range configMapList.Items {
		if item.Name == "istio-sidecar-injector" && item.Data != nil {
			jsonValues, foundValues = item.Data["values"]
			if foundValues {
				break
			}
		}
	}

	if !foundValues {
		fmt.Printf("Abort. Failed to find values in sidecar-injector config map: %v", configMapList)
		os.Exit(1)
	}

	yamlValues, err := yaml.JSONToYAML([]byte(jsonValues))
	if err != nil {
		fmt.Printf("jsonToYAML failed to parse values:\n%v\nError:\n%v", yamlValues, err)
		os.Exit(1)
	}

	return string(yamlValues)
}

func checkSupportedVersions(cur string, tar string) {
	if cur == tar {
		fmt.Printf("Abort. The current version %v equals to the target version %v.", cur, tar)
		os.Exit(1)
	}

	curMajor, curMinor, curPatch := parseVersionFormat(cur)
	tarMajor, tarMinor, tarPatch := parseVersionFormat(tar)

	if curMajor != tarMajor {
		fmt.Printf("Abort. Major version upgrade is not supported: %v -> %v.", cur, tar)
		os.Exit(1)
	}

	if curMinor != tarMinor {
		fmt.Printf("Abort. Minor version upgrade is not supported: %v -> %v.", cur, tar)
		os.Exit(1)
	}
	
	if curPatch > tarPatch {
		fmt.Printf("Abort. A newer version has been installed in the cluster.\n" +
			"istioctl: %v\nIstio control plane: %v", cur, tar)
		os.Exit(1)
	}

	if !supportedVersionMap[cur][tar] {
		fmt.Printf("Abort. Upgrade is currently not supported: %v -> %v.", cur, tar)		
	}
	fmt.Printf("Version check passed: %v -> %v.", cur, tar)
}

func parseVersionFormat(ver string) (int, int, int) {
	fullVerArray := strings.Split(ver, "-")
	if len(fullVerArray) == 0 {
		fmt.Printf("Abort. Incorrect version: %v.", ver)
		os.Exit(1)
	}
	verArray := strings.Split(fullVerArray[0], ".")
	if len(verArray) != 3 {
		fmt.Printf("Abort. Incorrect version: %v.", ver)
		os.Exit(1)
	}
	major, err := strconv.Atoi(verArray[0])
	if err != nil {
		fmt.Printf("Abort. Incorrect marjor version: %v.", verArray[0])
		os.Exit(1)
	}
	minor, err := strconv.Atoi(verArray[1])
	if err != nil {
		fmt.Printf("Abort. Incorrect minor version: %v.", verArray[1])
		os.Exit(1)
	}
	patch, err := strconv.Atoi(verArray[2])
	if err != nil {
		fmt.Printf("Abort. Incorrect patch version: %v.", verArray[2])
		os.Exit(1)
	}
	return major, minor, patch
}

func retrieveControlPlaneVersion() string {
	meshInfo, e := getRemoteInfo()
	if e != nil {
		fmt.Printf("Failed to retrieve Istio controle plane version, error: %v", e)
		os.Exit(1)
	}
	return coalesceVersions(meshInfo)
}

func retrieveClientVersion() string {
	//return version.Info.Version
	return "1.3.3"
}

func coalesceVersions(remoteVersion *version.MeshInfo) string {
	if !identicalVersions(*remoteVersion) {
		fmt.Printf("Different versions of Istio componets found: %v", remoteVersion)
		os.Exit(1)
	}
	return (*remoteVersion)[0].Info.GitTag
}

func identicalVersions(remoteVersion version.MeshInfo) bool {
	exemplar := remoteVersion[0].Info
	for i := 1; i < len(remoteVersion); i++ {
		candidate := (remoteVersion)[i].Info
		// Note that we don't compare GitRevision, BuildStatus,
		// or DockerHub because released Istio versions may use the same version tag
		// but differ in those fields.
		if exemplar.GitTag != candidate.GitTag {
			return false
		}
	}

	return true
}
