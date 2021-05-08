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

package env

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"istio.io/istio/prow/asm/tester/pkg/exec"
	"istio.io/istio/prow/asm/tester/pkg/kube"
	"istio.io/istio/prow/asm/tester/pkg/resource"
)

const (
	sharedGCPProject = "istio-prow-build"
	configDir        = "prow/asm/tester/configs"
)

func Setup(settings *resource.Settings) error {
	log.Println("🎬 start setting up the environment...")

	// Validate the settings before proceeding.
	if err := resource.ValidateSettings(settings); err != nil {
		return err
	}

	// Populate the settings that will be used in runtime.
	if err := populateRuntimeSettings(settings); err != nil {
		return err
	}

	// Fix the cluster configs before proceeding.
	if err := fixClusterConfigs(settings); err != nil {
		return err
	}

	// Setup permissions to allow pulling images from GCR registries.
	if err := setupPermissions(settings); err != nil {
		return fmt.Errorf("error setting up the permissions: %w", err)
	}

	// Inject system env vars that are required for the test flow.
	if err := injectEnvVars(settings); err != nil {
		return err
	}

	log.Printf("Running with %q CA, %q Workload Identity Pool, %q and --vm=%t control plane.", settings.CA, settings.WIP, settings.ControlPlane, settings.UseVMs)

	return nil
}

// populate extra settings that will be used during the runtime
func populateRuntimeSettings(settings *resource.Settings) error {
	settings.ConfigDir = filepath.Join(settings.RepoRootDir, configDir)

	var kubectlContexts string
	var err error
	kubectlContexts, err = kube.ContextStr()
	if err != nil {
		return err
	}
	settings.KubectlContexts = kubectlContexts

	var gcrProjectID string
	if settings.ClusterType == resource.GKEOnGCP {
		cs := kube.GKEClusterSpecsFromContexts(kubectlContexts)
		projectIDs := make([]string, len(cs))
		for i, c := range cs {
			projectIDs[i] = c.ProjectID
		}
		settings.GCPProjects = projectIDs
		// If it's using the gke clusters, use the first available project to hold the images.
		gcrProjectID = settings.GCPProjects[0]
	} else {
		// Otherwise use the shared GCP project to hold these images.
		gcrProjectID = sharedGCPProject
	}
	settings.GCRProject = gcrProjectID

	if settings.ClusterTopology == resource.MultiProject {
		settings.HostGCPProject = os.Getenv("HOST_PROJECT")
	}

	return nil
}

func setupPermissions(settings *resource.Settings) error {
	if settings.ControlPlane == resource.Unmanaged {
		if settings.ClusterType == resource.GKEOnGCP {
			log.Print("Set permissions to allow the Pods on the GKE clusters to pull images...")
			return setGcpPermissions(settings)
		} else {
			log.Print("Set permissions to allow the Pods on the multicloud clusters to pull images...")
			return setMulticloudPermissions(settings)
		}
	}
	return nil
}

func setGcpPermissions(settings *resource.Settings) error {
	cs := kube.GKEClusterSpecsFromContexts(settings.KubectlContexts)
	for _, c := range cs {
		if c.ProjectID != settings.GCRProject {
			projectNum, err := getProjectNumber(c.ProjectID)
			if err != nil {
				return err
			}
			err = exec.Run(
				fmt.Sprintf("gcloud projects add-iam-policy-binding %s "+
					"--member=serviceAccount:%s-compute@developer.gserviceaccount.com "+
					"--role=roles/storage.objectViewer",
					settings.GCRProject,
					projectNum),
			)
			if err != nil {
				return fmt.Errorf("error adding the binding for the service account to access GCR: %w", err)
			}
		}
	}
	return nil
}

// TODO: use kubernetes client-go library instead of kubectl.
func setMulticloudPermissions(settings *resource.Settings) error {
	if settings.ClusterType == resource.BareMetal || settings.ClusterType == resource.GKEOnAWS {
		os.Setenv("HTTP_PROXY", os.Getenv("MC_HTTP_PROXY"))
		defer os.Unsetenv("HTTP_PROXY")
	}

	secretName := "test-gcr-secret"
	cred := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
	configs := filepath.SplitList(settings.Kubeconfig)
	for i, config := range configs {
		err := exec.Run(
			fmt.Sprintf("kubectl create ns istio-system --kubeconfig=%s", config),
		)
		if err != nil {
			return fmt.Errorf("Error at 'kubectl create ns ...': %w", err)
		}

		// Create the secret that can be used to pull images from GCR.
		err = exec.Run(
			fmt.Sprintf(
				"bash -c 'kubectl create secret -n istio-system docker-registry %s "+
					"--docker-server=https://gcr.io "+
					"--docker-username=_json_key "+
					"--docker-email=\"$(gcloud config get-value account)\" "+
					"--docker-password=\"$(cat %s)\" "+
					"--kubeconfig=%s'",
				secretName,
				cred,
				config,
			),
		)
		if err != nil {
			return fmt.Errorf("Error at 'kubectl create secret ...': %w", err)
		}

		// Save secret data once (to be passed into the test framework),
		// deleting the line that contains 'namespace'.
		if i == 0 {
			err = exec.Run(
				fmt.Sprintf(
					"bash -c 'kubectl -n istio-system get secrets %s --kubeconfig=%s -o yaml "+
						"| sed \"/namespace/d\" > %s'",
					secretName,
					config,
					fmt.Sprintf("%s/test_image_pull_secret.yaml", os.Getenv("ARTIFACTS")),
				),
			)
			if err != nil {
				return fmt.Errorf("Error at 'kubectl get secrets ...': %w", err)
			}
		}

		// Patch the service accounts to use imagePullSecrets.
		serviceAccts := []string{
			"default",
			"istio-ingressgateway-service-account",
			"istio-reader-service-account",
			"istiod-service-account",
		}
		for _, serviceAcct := range serviceAccts {
			err = exec.Run(
				fmt.Sprintf(`bash -c 'cat <<EOF | kubectl --kubeconfig=%s apply -f -
apiVersion: v1
kind: ServiceAccount
metadata:
  name: %s
  namespace: istio-system
imagePullSecrets:
- name: %s
EOF'`,
					config,
					serviceAcct,
					secretName,
				),
			)
			if err != nil {
				return fmt.Errorf("Error at 'kubectl apply ...': %s", err)
			}
		}
	}

	return nil
}

func getProjectNumber(projectId string) (string, error) {
	projectNum, err := exec.RunWithOutput(
		fmt.Sprintf("gcloud projects describe %s --format=value(projectNumber)", projectId))
	if err != nil {
		err = fmt.Errorf("Error getting the project number for %q: %w", projectId, err)
	}
	return strings.TrimSpace(projectNum), err
}

func injectEnvVars(settings *resource.Settings) error {
	var hub, tag string
	tag = "BUILD_ID_" + os.Getenv("BUILD_ID")
	if settings.ControlPlane == resource.Unmanaged {
		hub = fmt.Sprintf("gcr.io/%s/asm", settings.GCRProject)
	} else {
		hub = "gcr.io/asm-staging-images/asm-mcp-e2e-test"
	}

	var meshID string
	if settings.ClusterType == resource.GKEOnGCP {
		projectNum, err := getProjectNumber(settings.GCPProjects[0])
		if err != nil {
			return err
		}
		meshID = "proj-" + strings.TrimSpace(projectNum)
	}
	// For onprem with Hub CI jobs, clusters are registered into the environ project
	if settings.WIP == resource.HUBWorkloadIdentityPool && settings.ClusterType == resource.OnPrem {
		environProjectID, err := kube.GetEnvironProjectID(settings.Kubeconfig)
		if err != nil {
			return err
		}
		projectNum, err := getProjectNumber(environProjectID)
		if err != nil {
			return err
		}
		meshID = "proj-" + strings.TrimSpace(projectNum)
	}

	// TODO(chizhg): delete most, if not all, the env var injections after we convert all the
	// bash to Go and remove the env var dependencies.
	envVars := map[string]string{
		// Run the Go tests with verbose logging.
		"T": "-v",
		// Do not start a container to run the build.
		"BUILD_WITH_CONTAINER": "0",
		// The GCP project we use when testing with multicloud clusters, or when we need to
		// hold some GCP resources that are shared across multiple jobs that are run in parallel.
		"SHARED_GCP_PROJECT": sharedGCPProject,

		"GCR_PROJECT_ID":   settings.GCRProject,
		"CONTEXT_STR":      settings.KubectlContexts,
		"CONFIG_DIR":       settings.ConfigDir,
		"CLUSTER_TYPE":     settings.ClusterType.String(),
		"CLUSTER_TOPOLOGY": settings.ClusterTopology.String(),
		"FEATURE_TO_TEST":  settings.FeatureToTest.String(),

		// exported TAG and HUB are used for ASM installation, and as the --istio.test.tag and
		// --istio-test.hub flags of the testing framework
		"TAG": tag,
		"HUB": hub,

		"MESH_ID": meshID,

		"CONTROL_PLANE":        settings.ControlPlane.String(),
		"CA":                   settings.CA.String(),
		"WIP":                  settings.WIP.String(),
		"REVISION_CONFIG_FILE": settings.RevisionConfig,
		"TEST_TARGET":          settings.TestTarget,
		"DISABLED_TESTS":       settings.DisabledTests,

		"USE_VM": strconv.FormatBool(settings.UseVMs),
		// TODO fully remove static vms from asm scripts
		"STATIC_VMS":    "",
		"GCE_VMS":       strconv.FormatBool(settings.UseGCEVMs || settings.VMStaticConfigDir != ""),
		"VM_DISTRO":     settings.VMImageFamily,
		"IMAGE_PROJECT": settings.VMImageProject,
	}

	for name, val := range envVars {
		log.Printf("Set env var: %s=%s", name, val)
		if err := os.Setenv(name, val); err != nil {
			return fmt.Errorf("error setting env var %q to %q", name, val)
		}
	}

	return nil
}

// Fix the cluster configs to meet the test requirements for ASM.
// These fixes are considered as hacky and temporary, ideally in the future they
// should all be handled by the corresponding deployer.
func fixClusterConfigs(settings *resource.Settings) error {
	switch resource.ClusterType(settings.ClusterType) {
	case resource.GKEOnGCP:
		return fixGKE(settings)
	case resource.OnPrem:
		return fixOnPrem(settings)
	case resource.BareMetal:
		return fixBareMetal(settings)
	case resource.GKEOnAWS:
		return fixAWS(settings)
	}

	return nil
}

func fixGKE(settings *resource.Settings) error {
	if settings.ClusterTopology == resource.MultiProject {
		// For MULTIPROJECT_MULTICLUSTER topology, firewall rules need to be added to
		// allow the clusters talking with each other for security tests.
		// See the details in b/175599359 and b/177919868
		createFirewallCmd := fmt.Sprintf(
			"gcloud compute --project=%q firewall-rules create extended-firewall-rule --network=test-network --allow=tcp,udp,icmp --direction=INGRESS",
			settings.HostGCPProject)
		if err := exec.Run(createFirewallCmd); err != nil {
			return fmt.Errorf("error creating the firewall rules for GKE multiproject tests: %w", err)
		}
	}

	if settings.FeatureToTest == resource.VPCSC {
		networkName := "default"
		if settings.ClusterTopology == resource.MultiProject {
			networkName = "test-network"
		}
		// Create the route as per the user guide in https://docs.google.com/document/d/11yYDxxI-fbbqlpvUYRtJiBmGdY_nIKPJLbssM3YQtKI/edit#heading=h.e2laig460f1d.
		createRouteCmd := fmt.Sprintf(`gcloud compute routes create restricted-vip --network=%s --destination-range=199.36.153.4/30 \
			--next-hop-gateway=default-internet-gateway`, networkName)
		if err := exec.Run(createRouteCmd); err != nil {
			return fmt.Errorf("error creating the restricted-vip route for VPC-SC testing: %w", err)
		}

		// Create router and NAT
		if err := exec.Run(fmt.Sprintf(
			"gcloud compute routers create test-router --network %s --region us-central1",
			networkName)); err != nil {
			log.Println("test-router already exists")
		}
		if err := exec.Run("gcloud compute routers nats create test-nat" +
			" --router=test-router --auto-allocate-nat-external-ips --nat-all-subnet-ip-ranges" +
			" --router-region=us-central1 --enable-logging"); err != nil {
			log.Println("test-nat already exists")
		}

		// Setup the firewall for VPC-SC
		for _, c := range kube.GKEClusterSpecsFromContexts(settings.KubectlContexts) {
			getFirewallRuleCmd := fmt.Sprintf("bash -c \"gcloud compute firewall-rules list --filter=\"name~gke-\"%s\"-[0-9a-z]*-master\" --format=json | jq -r '.[0].name'\"", c.Name)
			firewallRuleName, err := exec.RunWithOutput(getFirewallRuleCmd)
			if err != nil {
				return fmt.Errorf("failed to get firewall rule name: %w", err)
			}
			updateFirewallRuleCmd := fmt.Sprintf("gcloud compute firewall-rules update %s --allow tcp --source-ranges 0.0.0.0/0", firewallRuleName)
			if err := exec.Run(updateFirewallRuleCmd); err != nil {
				return fmt.Errorf("error updating firewall rule %q: %w", firewallRuleName, err)
			}
		}

		// Update subnets to enable private IP access
		if settings.ClusterTopology == resource.MultiProject {
			for _, project := range settings.GCPProjects {
				updateSubnetCmd := fmt.Sprintf(`gcloud compute networks subnets update "test-network-%s" \
				 	--project=%s \
					--region=us-central1 \
					--enable-private-ip-google-access`, project, settings.HostGCPProject)
				if err := exec.Run(updateSubnetCmd); err != nil {
					return fmt.Errorf("error updating the subnet for VPC-SC testing: %w", err)
				}
			}
		}
	}

	return nil
}

// Keeps only the user-kubeconfig.yaml entries in the KUBECONFIG for onprem
// by removing others including the admin-kubeconfig.yaml entries.
// This function will modify the KUBECONFIG env variable.
func fixOnPrem(settings *resource.Settings) error {
	return filterKubeconfigFiles(settings, func(name string) bool {
		return strings.HasSuffix(name, "user-kubeconfig.yaml")
	})
}

// Fix bare-metal cluster configs that are created by Tailorbird:
// 1. Keep only the artifacts/kubeconfig entries in the KUBECONFIG for baremetal
//    by removing any others entries.
// 2. Set required env vars that are needed for running ASM tests.
// 3. Modify proxy's default setup
func fixBareMetal(settings *resource.Settings) error {
	err := filterKubeconfigFiles(settings, func(name string) bool {
		return strings.HasSuffix(name, "artifacts/kubeconfig")
	})
	if err != nil {
		return err
	}

	sshPostprocess := func(bootstrapHostSSHKey, bootstrapHostSSHUser string) error {
		//  Increase proxy's max connection setup to avoid too many connections error
		sshCmd1 := fmt.Sprintf("ssh -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -i %s %s \"sudo sed -i 's/#max-client-connections.*/max-client-connections 512/' '/etc/privoxy/config'\"",
			bootstrapHostSSHKey, bootstrapHostSSHUser)
		sshCmd2 := fmt.Sprintf("ssh -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -i %s %s \"sudo sed -i 's/keep-alive-timeout 5/keep-alive-timeout 3000/' '/etc/privoxy/config'\"",
			bootstrapHostSSHKey, bootstrapHostSSHUser)
		sshCmd3 := fmt.Sprintf("ssh -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -i %s %s \"sudo sed -i 's/socket-timeout 300/socket-timeout 3000/' '/etc/privoxy/config'\"",
			bootstrapHostSSHKey, bootstrapHostSSHUser)
		sshCmd4 := fmt.Sprintf("ssh -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -i %s %s \"sudo sed -i 's/#default-server-timeout.*/default-server-timeout 3000/' '/etc/privoxy/config'\"",
			bootstrapHostSSHKey, bootstrapHostSSHUser)
		sshCmd5 := fmt.Sprintf("ssh -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -i %s %s \"sudo systemctl restart privoxy.service\"",
			bootstrapHostSSHKey, bootstrapHostSSHUser)
		if err := exec.RunMultiple([]string{sshCmd1, sshCmd2, sshCmd3, sshCmd4, sshCmd5}); err != nil {
			return fmt.Errorf("error running the commands to increase proxy's max connection setup: %w", err)
		}
		return nil
	}

	if err := injectMulticloudClusterEnvVars(settings, multicloudClusterConfig{
		// kubeconfig has the format of "${ARTIFACTS}"/.kubetest2-tailorbird/tf97d94df28f4277/artifacts/kubeconfig
		clusterArtifactsPath: filepath.Dir(settings.Kubeconfig),
		scriptRelPath:        "tunnel.sh",
		regexMatcher:         `.*\-L([0-9]*):localhost.* (root@[0-9]*\.[0-9]*\.[0-9]*\.[0-9]*)`,
		sshKeyRelPath:        "id_rsa",
	}, sshPostprocess); err != nil {
		return err
	}

	return nil
}

// Fix aws cluster configs that are created by Tailorbird:
// 1. Removes gke_aws_management.conf entry from the KUBECONFIG for aws
// 2. Set required env vars that are needed for running ASM tests.
// 3. Increase proxy's max connection setup to avoid too many connections error
func fixAWS(settings *resource.Settings) error {
	err := filterKubeconfigFiles(settings, func(name string) bool {
		return !strings.HasSuffix(name, "gke_aws_management.conf")
	})
	if err != nil {
		return err
	}

	sshPostprocess := func(bootstrapHostSSHKey, bootstrapHostSSHUser string) error {
		//  Increase proxy's max connection setup to avoid too many connections error
		sshCmd1 := fmt.Sprintf("ssh -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -i %s %s \"sudo sed -i 's/#max-client-connections.*/max-client-connections 512/' '/etc/privoxy/config'\"",
			bootstrapHostSSHKey, bootstrapHostSSHUser)
		sshCmd2 := fmt.Sprintf("ssh -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -i %s %s \"sudo sed -i 's/keep-alive-timeout 5/keep-alive-timeout 3000/' '/etc/privoxy/config'\"",
			bootstrapHostSSHKey, bootstrapHostSSHUser)
		sshCmd3 := fmt.Sprintf("ssh -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -i %s %s \"sudo sed -i 's/socket-timeout 300/socket-timeout 3000/' '/etc/privoxy/config'\"",
			bootstrapHostSSHKey, bootstrapHostSSHUser)
		sshCmd4 := fmt.Sprintf("ssh -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -i %s %s \"sudo sed -i 's/#default-server-timeout.*/default-server-timeout 3000/' '/etc/privoxy/config'\"",
			bootstrapHostSSHKey, bootstrapHostSSHUser)
		sshCmd5 := fmt.Sprintf("ssh -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -i %s %s \"sudo systemctl restart privoxy.service\"",
			bootstrapHostSSHKey, bootstrapHostSSHUser)
		if err := exec.RunMultiple([]string{sshCmd1, sshCmd2, sshCmd3, sshCmd4, sshCmd5}); err != nil {
			return fmt.Errorf("error running the commands to increase proxy's max connection setup: %w", err)
		}
		return nil
	}
	if err := injectMulticloudClusterEnvVars(settings, multicloudClusterConfig{
		// kubeconfig has the format of "${ARTIFACTS}"/.kubetest2-tailorbird/t96ea7cc97f047f5/.kube/gke_aws_default_t96ea7cc97f047f5.conf
		clusterArtifactsPath: filepath.Dir(filepath.Dir(settings.Kubeconfig)),
		scriptRelPath:        "tunnel-script.sh",
		regexMatcher:         `.*\-L([0-9]*):localhost.* (ubuntu@.*compute\.amazonaws\.com)`,
		sshKeyRelPath:        ".ssh/anthos-gke",
	}, sshPostprocess); err != nil {
		return err
	}

	return nil
}

func filterKubeconfigFiles(settings *resource.Settings, shouldKeep func(string) bool) error {
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		return errors.New("KUBECONFIG env var cannot be empty")
	}

	files := filepath.SplitList(kubeconfig)
	filteredFiles := make([]string, 0)
	for _, f := range files {
		if shouldKeep(f) {
			filteredFiles = append(filteredFiles, f)
		} else {
			log.Printf("Remove %q from KUBECONFIG", f)
		}
	}
	filteredKubeconfig := strings.Join(filteredFiles, string(os.PathListSeparator))
	os.Setenv("KUBECONFIG", filteredKubeconfig)
	settings.Kubeconfig = filteredKubeconfig

	return nil
}

type multicloudClusterConfig struct {
	// the path for storing the cluster artifacts files.
	clusterArtifactsPath string
	// tunnel script relative path to the cluster artifacts path.
	scriptRelPath string
	// ssh key file relative path to the cluster artifacts path.
	sshKeyRelPath string
	// regex to find the PORT_NUMBER and BOOTSTRAP_HOST_SSH_USER from the tunnel
	// script.
	regexMatcher string
}

func injectMulticloudClusterEnvVars(settings *resource.Settings, mcConf multicloudClusterConfig,
	postprocess func(bootstrapHostSSHKey, bootstrapHostSSHUser string) error) error {
	tunnelScriptPath := filepath.Join(mcConf.clusterArtifactsPath, mcConf.scriptRelPath)
	tunnelScriptContent, err := ioutil.ReadFile(tunnelScriptPath)
	if err != nil {
		return fmt.Errorf("error reading %q under the cluster artifacts path for aws: %w", mcConf.scriptRelPath, err)
	}

	patn := regexp.MustCompile(mcConf.regexMatcher)
	matches := patn.FindStringSubmatch(string(tunnelScriptContent))
	if len(matches) != 3 {
		return fmt.Errorf("error finding PORT_NUMBER and BOOTSTRAP_HOST_SSH_USER from: %q", tunnelScriptContent)
	}
	portNum, bootstrapHostSSHUser := matches[1], matches[2]
	httpProxy := "localhost:" + portNum
	bootstrapHostSSHKey := filepath.Join(mcConf.clusterArtifactsPath, mcConf.sshKeyRelPath)
	log.Printf("----------%s Cluster env----------", settings.ClusterType)
	log.Print("MC_HTTP_PROXY: ", httpProxy)
	log.Printf("BOOTSTRAP_HOST_SSH_USER: %s, BOOTSTRAP_HOST_SSH_KEY: %s", bootstrapHostSSHUser, bootstrapHostSSHKey)

	for name, val := range map[string]string{
		// Used by ingress related tests
		"BOOTSTRAP_HOST_SSH_USER": bootstrapHostSSHUser,
		"BOOTSTRAP_HOST_SSH_KEY":  bootstrapHostSSHKey,

		"MC_HTTP_PROXY": httpProxy,
	} {
		log.Printf("Set env var: %s=%s", name, val)
		if err := os.Setenv(name, val); err != nil {
			return fmt.Errorf("error setting env var %q to %q: %w", name, val, err)
		}
	}

	if postprocess != nil {
		return postprocess(bootstrapHostSSHKey, bootstrapHostSSHUser)
	}

	return nil
}
