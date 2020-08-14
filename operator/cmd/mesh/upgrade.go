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
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/docker/distribution/reference"
	goversion "github.com/hashicorp/go-version"
	"github.com/spf13/cobra"
	v1 "k8s.io/api/core/v1"
	client_v1beta1 "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1beta1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"

	iop "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/compare"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/tpath"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/util/clog"
	pkgversion "istio.io/istio/operator/pkg/version"
	"istio.io/pkg/log"
)

const (
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

	// releaseURLPathTemplete is used to construct a download URL for a tar at a given version. The osx tar is
	// used because it's stable between 1.5->1.6 and only the profiles are used, not binaries.
	releaseURLPathTemplete = "https://github.com/istio/istio/releases/download/%s/istio-%s-osx.tar.gz"
)

type upgradeArgs struct {
	// inFilenames is an array of paths to the input IstioOperator CR files.
	inFilenames []string
	// kubeConfigPath is the path to kube config file.
	kubeConfigPath string
	// context is the cluster context in the kube config.
	context string
	// readinessTimeout is maximum time to wait for all Istio resources to be ready.
	readinessTimeout time.Duration
	// set is a string with element format "path=value" where path is an IstioOperator path and the value is a
	// value to set the node at that path to.
	set []string
	// skipConfirmation means skipping the prompting confirmation for value changes in this upgrade.
	skipConfirmation bool
	// force means directly applying the upgrade without eligibility checks.
	force bool
	// manifestsPath is a path to a charts and profiles directory in the local filesystem, or URL with a release tgz.
	manifestsPath string
}

// addUpgradeFlags adds upgrade related flags into cobra command
func addUpgradeFlags(cmd *cobra.Command, args *upgradeArgs) {
	cmd.PersistentFlags().StringSliceVarP(&args.inFilenames, "filename",
		"f", nil, "Path to file containing IstioOperator custom resource")
	cmd.PersistentFlags().StringVarP(&args.kubeConfigPath, "kubeconfig",
		"c", "", "Path to kube config")
	cmd.PersistentFlags().StringVar(&args.context, "context", "",
		"The name of the kubeconfig context to use")
	cmd.PersistentFlags().BoolVarP(&args.skipConfirmation, "skip-confirmation", "y", false,
		"If skip-confirmation is set, skips the prompting confirmation for value changes in this upgrade")
	cmd.PersistentFlags().DurationVar(&args.readinessTimeout, "readiness-timeout", 300*time.Second,
		"Maximum time to wait for Istio resources in each component to be ready.")
	cmd.PersistentFlags().BoolVar(&args.force, "force", false,
		"Apply the upgrade without eligibility checks")
	cmd.PersistentFlags().StringArrayVarP(&args.set, "set", "s", nil, setFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&args.manifestsPath, "charts", "", "", ChartsDeprecatedStr)
	cmd.PersistentFlags().StringVarP(&args.manifestsPath, "manifests", "d", "", ManifestsFlagHelpStr)
}

// UpgradeCmd upgrades Istio control plane in-place with eligibility checks
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
			l := clog.NewConsoleLogger(cmd.OutOrStdout(), cmd.OutOrStderr(), installerScope)
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
func upgrade(rootArgs *rootArgs, args *upgradeArgs, l clog.Logger) (err error) {
	// Create a kube client from args.kubeConfigPath and  args.context
	kubeClient, err := NewClient(args.kubeConfigPath, args.context)
	if err != nil {
		return fmt.Errorf("failed to connect Kubernetes API server, error: %v", err)
	}
	setFlags := applyFlagAliases(args.set, args.manifestsPath, "")
	// Generate IOPS parseObjectSetFromManifest
	targetIOPSYaml, targetIOPS, err := manifest.GenerateConfig(args.inFilenames, setFlags, args.force, nil, l)
	if err != nil {
		return fmt.Errorf("failed to generate Istio configs from file %s, error: %s", args.inFilenames, err)
	}

	// Get the target version from the tag in the IOPS
	targetTag := targetIOPS.Tag
	targetVersion, err := pkgversion.TagToVersionString(fmt.Sprint(targetTag))
	if err != nil {
		if !args.force {
			return fmt.Errorf("failed to convert the target tag '%s' into a valid version, "+
				"you can use --force flag to skip the version check if you know the tag is correct", targetTag)
		}
	}

	// Get Istio control plane namespace
	//TODO(elfinhe): support components distributed in multiple namespaces
	istioNamespace := iop.Namespace(targetIOPS)

	// Read the current Istio version from the the cluster
	currentVersion, err := retrieveControlPlaneVersion(kubeClient, istioNamespace, l)
	if err != nil && !args.force {
		return fmt.Errorf("failed to read the current Istio version, error: %v", err)
	}

	// Check if the upgrade currentVersion -> targetVersion is supported
	err = checkSupportedVersions(kubeClient, currentVersion)
	if err != nil && !args.force {
		return fmt.Errorf("upgrade version check failed: %v -> %v. Error: %v",
			currentVersion, targetVersion, err)
	}
	l.LogAndPrintf("Upgrade version check passed: %v -> %v.\n", currentVersion, targetVersion)

	// Read the overridden IOPS from args.inFilenames
	overrideIOPSYaml := ""
	if args.inFilenames != nil {
		overrideIOPSYaml, err = manifest.ReadLayeredYAMLs(args.inFilenames)
		if err != nil {
			return fmt.Errorf("failed to read override IOPS from file: %v, error: %v", args.inFilenames, err)
		}
		if overrideIOPSYaml != "" {
			// Grab the IstioOperatorSpec subtree.
			overrideIOPSYaml, err = tpath.GetSpecSubtree(overrideIOPSYaml)
			if err != nil {
				return fmt.Errorf("failed to get spec subtree from IOPS yaml, error: %v", err)
			}
		}
	}

	// Read the current installation's profile IOPS yaml to check the changed profile settings between versions.
	currentSets := args.set
	if currentVersion != "" {
		currentSets = append(currentSets, "installPackagePath="+releaseURLFromVersion(currentVersion))
	}
	profile := targetIOPS.Profile
	if profile == "" {
		profile = name.DefaultProfileName
	} else {
		currentSets = append(currentSets, "profile="+targetIOPS.Profile)
	}
	currentProfileIOPSYaml, _, err := manifest.GenIOPSFromProfile(profile, "", currentSets, true, true, nil, l)
	if err != nil {
		return fmt.Errorf("failed to generate Istio configs from file %s for the current version: %s, error: %v",
			args.inFilenames, currentVersion, err)
	}
	checkUpgradeIOPS(currentProfileIOPSYaml, targetIOPSYaml, overrideIOPSYaml, l)

	waitForConfirmation(args.skipConfirmation, l)

	// Apply the Istio Control Plane specs reading from inFilenames to the cluster
	err = InstallManifests(applyFlagAliases(args.set, args.manifestsPath, ""), args.inFilenames, args.force, rootArgs.dryRun,
		args.kubeConfigPath, args.context, args.readinessTimeout, l)
	if err != nil {
		return fmt.Errorf("failed to apply the Istio Control Plane specs. Error: %v", err)
	}

	if !rootArgs.dryRun {
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

		l.LogAndPrintf("Success. Now the Istio control plane is running at version %v.\n", upgradeVer)
	} else {
		l.LogAndPrintf("Upgrade rollout completed. " +
			"All Istio control plane pods are running on the target version.\n\n")
		l.LogAndPrintf("Success. Now the Istio control plane is running at version %v.\n", targetVersion)
	}

	l.LogAndPrintf(upgradeSidecarMessage)
	return nil
}

// releaseURLFromVersion generates default installation url from version number.
func releaseURLFromVersion(version string) string {
	return fmt.Sprintf(releaseURLPathTemplete, version, version)
}

// checkUpgradeIOPS checks the upgrade eligibility by comparing the current IOPS with the target IOPS
func checkUpgradeIOPS(curIOPS, tarIOPS, ignoreIOPS string, l clog.Logger) {
	diff := compare.YAMLCmpWithIgnore(curIOPS, tarIOPS, nil, ignoreIOPS)
	if diff == "" {
		l.LogAndPrintf("Upgrade check: IOPS unchanged. The target IOPS are identical to the current IOPS.\n")
	} else {
		l.LogAndPrintf("Upgrade check: Warning!!! The following IOPS will be changed as part of upgrade. "+
			"Please double check they are correct:\n%s", diff)
	}
}

// waitForConfirmation waits for user's confirmation if skipConfirmation is not set
func waitForConfirmation(skipConfirmation bool, l clog.Logger) {
	if skipConfirmation {
		return
	}
	if !confirm("Confirm to proceed [y/N]?", os.Stdout) {
		l.LogAndFatalf("Abort.")
	}
}

var SupportedIstioVersions, _ = goversion.NewConstraint(">=1.6.0, <1.8")

func checkSupportedVersions(kubeClient *Client, currentVersion string) error {
	curGoVersion, err := goversion.NewVersion(currentVersion)
	if err != nil {
		return fmt.Errorf("failed to parse the current version %q: %v", currentVersion, err)
	}

	if !SupportedIstioVersions.Check(curGoVersion) {
		return fmt.Errorf("upgrade is currently not supported from version: %v", currentVersion)
	}

	return kubeClient.CheckUnsupportedAlphaSecurityCRD()
}

// retrieveControlPlaneVersion retrieves the version number from the Istio control plane
func retrieveControlPlaneVersion(kubeClient ExecClient, istioNamespace string, l clog.Logger) (string, error) {
	cv, e := kubeClient.GetIstioVersions(istioNamespace)
	if e != nil {
		return "", fmt.Errorf("failed to retrieve Istio control plane version, error: %v", e)
	}

	if len(cv) == 0 {
		return "", fmt.Errorf("istio control plane not found in namespace: %v", istioNamespace)
	}

	for _, remote := range cv {
		l.LogAndPrintf("Control Plane - %v", remote)
	}
	l.LogAndPrint("")

	v, e := coalesceVersions(cv)
	if e != nil {
		return "", e
	}
	return v, nil
}

// waitUpgradeComplete waits for the upgrade to complete by periodically comparing the current component version
// to the target version.
func waitUpgradeComplete(kubeClient ExecClient, istioNamespace string, targetVer string, l clog.Logger) error {
	for i := 1; i <= upgradeWaitCheckVerMaxAttempts; i++ {
		sleepSeconds(upgradeWaitSecCheckVerPerLoop)
		cv, e := kubeClient.GetIstioVersions(istioNamespace)
		if e != nil {
			l.LogAndPrintf("Failed to retrieve Istio control plane version, error: %v", e)
			continue
		}
		if cv == nil {
			l.LogAndPrintf("Failed to find Istio namespace: %v", istioNamespace)
			continue
		}
		if identicalVersions(cv) && targetVer == cv[0].Version {
			l.LogAndPrintf("Upgrade rollout completed. " +
				"All Istio control plane pods are running on the target version.\n\n")
			return nil
		}
		for _, remote := range cv {
			if targetVer != remote.Version {
				l.LogAndPrintf("Control Plane - %v does not match the target version %s",
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
func coalesceVersions(cv []ComponentVersion) (string, error) {
	if len(cv) == 0 {
		return "", fmt.Errorf("empty list of ComponentVersion")
	}
	if !identicalVersions(cv) {
		return "", fmt.Errorf("different versions of Istio components found: %v", cv)
	}
	return cv[0].Version, nil
}

// identicalVersions checks if Istio control plane components are on the same version
func identicalVersions(cv []ComponentVersion) bool {
	exemplar := cv[0]
	for i := 1; i < len(cv); i++ {
		if exemplar.Version != cv[i].Version {
			return false
		}
	}
	return true
}

// Client is a helper wrapper around the Kube RESTClient for istioctl -> Pilot/Envoy/Mesh related things
type Client struct {
	Config *rest.Config
	*rest.RESTClient
}

// ComponentVersion is a pair of component name and version
type ComponentVersion struct {
	Component string
	Version   string
	Pod       v1.Pod
}

func (cv ComponentVersion) String() string {
	return fmt.Sprintf("%s pod - %s - version: %s",
		cv.Component, cv.Pod.GetName(), cv.Version)
}

// ExecClient is an interface for remote execution
type ExecClient interface {
	GetIstioVersions(namespace string) ([]ComponentVersion, error)
	GetPods(namespace string, params map[string]string) (*v1.PodList, error)
	PodsForSelector(namespace, labelSelector string) (*v1.PodList, error)
	ConfigMapForSelector(namespace, labelSelector string) (*v1.ConfigMapList, error)
}

// NewClient is the constructor for the client wrapper
func NewClient(kubeconfig, configContext string) (*Client, error) {
	config, err := defaultRestConfig(kubeconfig, configContext)
	if err != nil {
		return nil, err
	}
	restClient, err := rest.RESTClientFor(config)
	if err != nil {
		return nil, err
	}
	return &Client{config, restClient}, nil
}

// GetIstioVersions gets the version for each Istio component
func (client *Client) GetIstioVersions(namespace string) ([]ComponentVersion, error) {
	pods, err := client.GetPods(namespace, map[string]string{
		"labelSelector": "istio",
		"fieldSelector": "status.phase=Running",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve Istio pods, error: %v", err)
	}
	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("istio pod not found in namespace %v", namespace)
	}

	var errs util.Errors
	var res []ComponentVersion
	for _, pod := range pods.Items {
		component := pod.Labels["istio"]

		switch component {
		case "statsd-prom-bridge":
			continue
		case "mixer":
			component = pod.Labels["istio-mixer-type"]
		}

		server := ComponentVersion{
			Component: component,
			Pod:       pod,
		}

		pv := ""
		for _, c := range pod.Spec.Containers {
			cv, err := parseTag(c.Image)
			if err != nil {
				errs = util.AppendErr(errs, err)
			}

			if pv == "" {
				pv = cv
			} else if pv != cv {
				err := fmt.Errorf("differrent versions of containers in the same pod: %v", pod.Spec.Containers)
				errs = util.AppendErr(errs, err)
			}
		}
		server.Version, err = pkgversion.TagToVersionString(pv)
		if err != nil {
			tagErr := fmt.Errorf("unable to convert tag %s into version in pod: %v", pv, pod.Spec.Containers)
			errs = util.AppendErr(errs, tagErr)
		}
		res = append(res, server)
	}
	return res, errs.ToError()
}

func parseTag(image string) (string, error) {
	ref, err := reference.Parse(image)
	if err != nil {
		return "", fmt.Errorf("could not parse image: %s, error: %v", image, err)
	}

	switch t := ref.(type) {
	case reference.Tagged:
		return t.Tag(), nil
	default:
		return "", fmt.Errorf("tag not found in image: %v", image)
	}
}

func (client *Client) PodsForSelector(namespace, labelSelector string) (*v1.PodList, error) {
	pods, err := client.GetPods(namespace, map[string]string{
		"labelSelector": labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve pods, error: %v", err)
	}
	return pods, nil
}

// GetPods retrieves the pod objects for Istio deployments
func (client *Client) GetPods(namespace string, params map[string]string) (*v1.PodList, error) {
	req := client.Get().
		Resource("pods").
		Namespace(namespace)
	for k, v := range params {
		req.Param(k, v)
	}

	res := req.Do(context.TODO())
	if res.Error() != nil {
		return nil, fmt.Errorf("unable to retrieve Pods: %v", res.Error())
	}
	list := &v1.PodList{}
	if err := res.Into(list); err != nil {
		return nil, fmt.Errorf("unable to parse PodList: %v", res.Error())
	}
	return list, nil
}

func (client *Client) ConfigMapForSelector(namespace, labelSelector string) (*v1.ConfigMapList, error) {
	cmGet := client.Get().Resource("configmaps").Namespace(namespace).Param("labelSelector", labelSelector)
	obj, err := cmGet.Do(context.TODO()).Get()
	if err != nil {
		return nil, fmt.Errorf("failed retrieving configmap: %v", err)
	}
	return obj.(*v1.ConfigMapList), nil
}

func (client *Client) CheckUnsupportedAlphaSecurityCRD() error {
	c, err := client_v1beta1.NewForConfig(client.Config)
	if err != nil {
		return err
	}
	crds, err := c.CustomResourceDefinitions().List(context.TODO(), meta_v1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to get CRDs: %v", err)
	}

	unsupportedCRD := func(name string) bool {
		crds := []string{
			"clusterrbacconfigs.rbac.istio.io",
			"rbacconfigs.rbac.istio.io",
			"servicerolebindings.rbac.istio.io",
			"serviceroles.rbac.istio.io",
			"policies.authentication.istio.io",
			"meshpolicies.authentication.istio.io",
		}
		for _, crd := range crds {
			if name == crd {
				return true
			}
		}
		return false
	}
	getResource := func(crd string) []string {
		type ResourceItem struct {
			Metadata meta_v1.ObjectMeta `json:"metadata,omitempty"`
		}
		type Resource struct {
			Items []ResourceItem `json:"items"`
		}

		parts := strings.Split(crd, ".")
		cmd := client.Get().AbsPath("apis", strings.Join(parts[1:], "."), "v1alpha1", parts[0])
		obj, err := cmd.DoRaw(context.TODO())
		if err != nil {
			log.Errorf("failed to get resources for crd %s: %v", crd, err)
			return nil
		}
		resource := &Resource{}
		if err := json.Unmarshal(obj, resource); err != nil {
			log.Errorf("failed decoding response for crd %s: %v", crd, err)
			return nil
		}
		var foundResources []string
		for _, res := range resource.Items {
			n := strings.Join([]string{crd, res.Metadata.Namespace, res.Metadata.Name}, "/")
			foundResources = append(foundResources, n)
		}
		return foundResources
	}

	var foundCRDs []string
	var foundResources []string
	for _, crd := range crds.Items {
		if unsupportedCRD(crd.Name) {
			foundCRDs = append(foundCRDs, crd.Name)
			foundResources = append(foundResources, getResource(crd.Name)...)
		}
	}
	if len(foundCRDs) != 0 {
		log.Warnf("found %d CRD of unsupported v1alpha1 security policy: %v. "+
			"The v1alpha1 security policy is no longer supported starting 1.6. It's strongly recommended to delete "+
			"the CRD of the v1alpha1 security policy to avoid applying any of the v1alpha1 security policy in the unsupported version",
			len(foundCRDs), foundCRDs)
	}
	if len(foundResources) != 0 {
		return fmt.Errorf("found %d unsupported v1alpha1 security policy: %v. "+
			"The v1alpha1 security policy is no longer supported starting 1.6. To continue the upgrade, "+
			"Please migrate to the v1beta1 security policy and delete all the v1alpha1 security policy, "+
			"See https://istio.io/news/releases/1.5.x/announcing-1.5/upgrade-notes/#authentication-policy and "+
			"https://istio.io/blog/2019/v1beta1-authorization-policy/#migration-from-the-v1alpha1-policy",
			len(foundResources), foundResources)
	}
	return nil
}
