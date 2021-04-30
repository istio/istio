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

package install

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"istio.io/istio/prow/asm/tester/pkg/asm/install/revision"
	"istio.io/istio/prow/asm/tester/pkg/exec"
	"istio.io/istio/prow/asm/tester/pkg/kube"
	"istio.io/istio/prow/asm/tester/pkg/resource"
)

const (
	istioctlPath  = "out/linux_amd64/istioctl"
	kptBranch     = "master"
	subCaIdPrefix = "asm-test-sub-ca"

	// Envvar consts
	cloudAPIEndpointOverrides = "CLOUDSDK_API_ENDPOINT_OVERRIDES_CONTAINER"
	stagingEndpoint           = "https://staging-container.sandbox.googleapis.com/"
	staging2Endpoint          = "https://staging2-container.sandbox.googleapis.com/"
)

func (c *installer) install(r *revision.Config) error {
	if c.settings.ControlPlane == resource.Unmanaged {
		switch c.settings.ClusterType {
		case resource.GKEOnGCP:
			log.Println("🏄 performing ASM installation")
			return c.installASM(r)
		case resource.BareMetal:
			log.Println("🏄 performing ASM bare metal installation")
			return c.installASMOnBareMetal()
		default:
			log.Println("🏄 performing ASM multi cloud installation")
			return c.installASMOnMulticloud()
		}
	} else {
		log.Println("🏄 performing ASM MCP installation")
		return c.installASMOnManagedControlPlane()
	}
}

func (c *installer) installASM(rev *revision.Config) error {
	pkgPath := filepath.Join(c.settings.RepoRootDir, resource.ConfigDirPath, "kpt-pkg")
	kptSetPrefix := fmt.Sprintf("kpt cfg set %s", pkgPath)
	contexts := strings.Split(c.settings.KubectlContexts, ",")

	// Use the first project as the environ name
	// must do this here because each installation depends on the value
	environProjectNumber, err := exec.RunWithOutput(fmt.Sprintf(
		"gcloud projects describe %s --format=\"value(projectNumber)\"",
		kube.GKEClusterSpecFromContext(contexts[0]).ProjectID))
	if err != nil {
		return fmt.Errorf("failed to read environ number: %w", err)
	}
	os.Setenv("_CI_ENVIRON_PROJECT_NUMBER", strings.TrimSpace(environProjectNumber))

	for _, context := range contexts {
		contextLogger := log.New(os.Stdout,
			fmt.Sprintf("[kubeContext: %s] ", context), log.Ldate|log.Ltime)
		contextLogger.Println("Performing installation...")
		cluster := kube.GKEClusterSpecFromContext(context)
		var trustedGCPProjects string

		// Create the istio-system ns before running the install_asm script.
		// TODO(chizhg): remove this line after install_asm script can create it.
		if err := exec.Run(fmt.Sprintf("bash -c "+
			"\"kubectl create namespace istio-system --dry-run=client -o yaml "+
			"| kubectl apply -f - --context=%s \"", context)); err != nil {
			return fmt.Errorf("failed to create istio-system namespace: %w", err)
		}

		// Override CA with CA from revision
		// clunky but works
		ca := c.settings.CA
		if rev.CA != "" {
			ca = resource.CAType(rev.CA)
		}
		// Per-CA custom setup
		if ca == resource.MeshCA || ca == resource.PrivateCA {
			// add other projects to the trusted GCP projects for this cluster
			if c.settings.ClusterTopology == resource.MultiProject {
				var otherIds []string
				for _, otherContext := range contexts {
					if otherContext != context {
						otherIds = append(otherIds, kube.GKEClusterSpecFromContext(otherContext).ProjectID)
					}
				}
				trustedGCPProjects = strings.Join(otherIds, ",")
				contextLogger.Printf("Running with trusted GCP projects: %s", trustedGCPProjects)
			}

			// b/177358640: for Prow jobs running with GKE staging/staging2 clusters, overwrite
			// GKE_CLUSTER_URL with a custom overlay to fix the issue in installing ASM
			// with MeshCA.
			// TODO(samnaser) setting KPT properties cannot be done in parallel, must copy kpt directories
			// for each installation
			if os.Getenv(cloudAPIEndpointOverrides) == stagingEndpoint ||
				os.Getenv(cloudAPIEndpointOverrides) == staging2Endpoint {
				contextLogger.Println("Setting KPT for staging...")
				if err := exec.RunMultiple([]string{
					fmt.Sprintf("%s gcloud.core.project %s", kptSetPrefix, cluster.ProjectID),
					fmt.Sprintf("%s gcloud.compute.location %s", kptSetPrefix, cluster.Location),
					fmt.Sprintf("%s gcloud.container.cluster %s", kptSetPrefix, cluster.Name),
				}); err != nil {
					return err
				}
			}

			// Need to set kpt values per install
			if ca == resource.PrivateCA {
				subordinateCaId := fmt.Sprintf("%s-%s-%s",
					subCaIdPrefix, os.Getenv("BUILD_ID"), cluster.Name)
				caName := fmt.Sprintf("projects/%s/locations/%s/certificateAuthorities/%s",
					cluster.ProjectID, cluster.Location, subordinateCaId)
				if err := exec.RunMultiple([]string{
					fmt.Sprintf("%s anthos.servicemesh.external_ca.ca_name %s", kptSetPrefix, caName),
					fmt.Sprintf("%s gcloud.core.project %s", kptSetPrefix, cluster.ProjectID),
				}); err != nil {
					return err
				}
			}
		}

		contextLogger.Println("Downloading Scriptaro release...")
		scriptaroPath, err := downloadScriptaro(rev, cluster)
		if err != nil {
			return fmt.Errorf("failed to download Scriptaro: %w", err)
		}
		contextLogger.Println("Running Scriptaro installation...")
		installOutput, err := exec.RunWithOutput(scriptaroPath,
			exec.WithAdditionalEnvs(generateInstallEnvvars(c.settings, rev, trustedGCPProjects)),
			exec.WithAdditionalArgs(generateInstallFlags(c.settings, rev, pkgPath, cluster)))
		if err != nil {
			return fmt.Errorf("scriptaro ASM installation failed: %w", err)
		}
		contextLogger.Println(installOutput)
	}

	if err := createRemoteSecrets(c.settings, contexts); err != nil {
		return fmt.Errorf("failed to create remote secrets: %w", err)
	}
	return nil
}

// generateInstallEnvvars generates the environt variables needed when running install_asm script.
func generateInstallEnvvars(settings *resource.Settings, rev *revision.Config, trustedGCPProjects string) []string {
	var envvars []string
	varMap := map[string]string{
		"_CI_ISTIOCTL_REL_PATH":  filepath.Join(settings.RepoRootDir, istioctlPath),
		"_CI_ASM_IMAGE_LOCATION": os.Getenv("HUB"),
		"_CI_ASM_IMAGE_TAG":      os.Getenv("TAG"),
		"_CI_ASM_KPT_BRANCH":     kptBranch,
		"_CI_NO_VALIDATE":        "1",
		"_CI_NO_REVISION":        "1",
		"_CI_ASM_PKG_LOCATION":   "asm-staging-images",
	}
	if rev.Name != "" {
		varMap["_CI_NO_REVISION"] = "0"
	}
	if settings.ClusterTopology == resource.MultiProject {
		varMap["_CI_TRUSTED_GCP_PROJECTS"] = trustedGCPProjects
	}

	for k, v := range varMap {
		log.Printf("Setting envvar %s=%s", k, v)
		envvars = append(envvars, fmt.Sprintf("%s=%s", k, v))
	}

	return envvars
}

// generateInstallFlags returns the flags required when running install_asm script.
func generateInstallFlags(settings *resource.Settings, rev *revision.Config, pkgPath string, cluster *kube.GKEClusterSpec) []string {
	installFlags := []string{
		"--project_id", cluster.ProjectID,
		"--cluster_name", cluster.Name,
		"--cluster_location", cluster.Location,
		"--mode", "install",
		"--enable-all",
		"--verbose",
		"--option", "audit-authorizationpolicy",
	}

	// Use the CA from revision config for the revision we're installing
	ca := settings.CA
	if rev.CA != "" {
		ca = resource.CAType(rev.CA)
	}
	if ca == resource.MeshCA || ca == resource.PrivateCA {
		installFlags = append(installFlags, "--ca", "mesh_ca")
	} else if ca == resource.Citadel {
		installFlags = append(installFlags,
			"--ca", "citadel",
			"--ca_cert", "samples/certs/ca-cert.pem",
			"--ca_key", "samples/certs/ca-key.pem",
			"--root_cert", "samples/certs/root-cert.pem",
			"--cert_chain", "samples/certs/cert-chain.pem")
	}

	// Set kpt overlays
	overlays := []string{
		filepath.Join(pkgPath, "overlay/default.yaml"),
	}

	// Apply per-revision overlay customizations
	if rev.Overlay != "" {
		overlays = append(overlays, filepath.Join(pkgPath, rev.Overlay))
	}
	if ca == resource.PrivateCA && settings.WIP != resource.HUBWorkloadIdentityPool {
		overlays = append(overlays, filepath.Join(pkgPath, "overlay/private-ca.yaml"))
	}
	if settings.FeatureToTest == string(resource.UserAuth) {
		overlays = append(overlays, filepath.Join(pkgPath, "overlay/user-auth.yaml"))
	}
	if os.Getenv(cloudAPIEndpointOverrides) == stagingEndpoint {
		overlays = append(overlays, filepath.Join(pkgPath, "overlay/meshca-staging-gke.yaml"))
	}
	if os.Getenv(cloudAPIEndpointOverrides) == staging2Endpoint {
		overlays = append(overlays, filepath.Join(pkgPath, "overlay/meshca-staging2-gke.yaml"))
	}
	installFlags = append(installFlags, "--custom_overlay", strings.Join(overlays, ","))

	// Set the revision name if specified on the per-revision config
	// note that this flag only exists on newer Scriptaro versions
	if rev.Name != "" {
		installFlags = append(installFlags, "--revision_name", rev.Name)
	}

	// Other random options
	if settings.ClusterTopology == resource.MultiProject {
		installFlags = append(installFlags, "--option", "multiproject")
	}
	if settings.WIP == resource.HUBWorkloadIdentityPool {
		installFlags = append(installFlags, "--option", "hub-meshca")
	}
	if settings.UseVMs {
		installFlags = append(installFlags, "--option", "vm")
	}

	return installFlags
}

// createRemoteSecrets creates remote secrets for each cluster to each other cluster
func createRemoteSecrets(settings *resource.Settings, contexts []string) error {
	for _, context := range contexts {
		for _, otherContext := range contexts {
			if context == otherContext {
				continue
			}
			otherCluster := kube.GKEClusterSpecFromContext(otherContext)
			log.Printf("creating remote secret with context %s to cluster %s",
				context, otherCluster.Name)
			createRemoteSecretCmd := fmt.Sprintf("istioctl x create-remote-secret"+
				" --context %s --name %s", otherContext, otherCluster.Name)
			secretContents, err := exec.RunWithOutput(createRemoteSecretCmd)
			if err != nil {
				return fmt.Errorf("failed creating remote secret: %w", err)
			}
			secretFileName := fmt.Sprintf("%s_%s_%s.secret",
				otherCluster.ProjectID, otherCluster.Location, otherCluster.Name)
			if err := os.WriteFile(secretFileName, []byte(secretContents), 0o644); err != nil {
				return fmt.Errorf("failed to write secret to file: %w", err)
			}

			// for private clusters, convert the cluster master public IP to private IP
			if settings.FeatureToTest == string(resource.VPCSC) {
				privateIPCmd := fmt.Sprintf("gcloud container clusters describe %s"+
					" --project %s --zone %s --format \"value(privateClusterConfig.privateEndpoint)\"",
					otherCluster.Name, otherCluster.ProjectID, otherCluster.Location)
				privateIP, err := exec.RunWithOutput(privateIPCmd)
				if err != nil {
					return fmt.Errorf("failed to retrieve private IP: %w", err)
				}
				sedCmd := fmt.Sprintf("sed -i 's/server\\:.*/server\\: https:\\/\\/'%s'/' %s",
					privateIP, secretFileName)
				if err := exec.Run(sedCmd); err != nil {
					return fmt.Errorf("sed command failed: %w", err)
				}
			}

			kubeCreateSecretCmd := fmt.Sprintf("kubectl apply -f %s --context %s",
				secretFileName, context)
			if err := exec.Run(kubeCreateSecretCmd); err != nil {
				return fmt.Errorf("failed to create remote secret: %w", err)
			}
			if settings.UseVMs {
				// TODO(landow) this is temporary until we have a user-facing way to enable multi-cluster + VMs
				// we have to wait for new pods, but we do this later so the pods can come up in parallel per-cluster
				patchWLECmd := fmt.Sprintf("kubectl -n istio-system set env deployment/istiod"+
					" --context %s PILOT_ENABLE_CROSS_CLUSTER_WORKLOAD_ENTRY=true", context)
				if err := exec.Run(patchWLECmd); err != nil {
					return fmt.Errorf("failed to patch istiod: %w", err)
				}
			}
		}
	}
	return nil
}

func (c *installer) installASMOnBareMetal() error {
	return exec.Dispatch(
		c.settings.RepoRootDir,
		"install_asm_on_proxied_clusters",
		nil,
		exec.WithAdditionalEnvs([]string{
			fmt.Sprintf("HTTP_PROXY=%s", os.Getenv("MC_HTTP_PROXY")),
			fmt.Sprintf("HTTPS_PROXY=%s", os.Getenv("MC_HTTP_PROXY")),
		}),
	)
}

func (c *installer) installASMOnMulticloud() error {
	return exec.Dispatch(
		c.settings.RepoRootDir,
		"install_asm_on_multicloud",
		[]string{
			string(c.settings.WIP),
		},
		exec.WithAdditionalEnvs([]string{
			fmt.Sprintf("HTTP_PROXY=%s", os.Getenv("MC_HTTP_PROXY")),
			fmt.Sprintf("HTTPS_PROXY=%s", os.Getenv("MC_HTTP_PROXY")),
		}))
}

func (c *installer) installASMOnManagedControlPlane() error {
	return exec.Dispatch(
		c.settings.RepoRootDir,
		"install_asm_managed_control_plane",
		[]string{
			kube.ContextArr(c.settings.KubectlContexts),
		},
	)
}

// preInstall contains all steps required before performing the direct install
func (c *installer) preInstall() error {
	if c.settings.ControlPlane == resource.Unmanaged {
		if err := exec.Dispatch(c.settings.RepoRootDir,
			"prepare_images", nil); err != nil {
			return err
		}
	} else {
		if err := exec.Dispatch(c.settings.RepoRootDir,
			"prepare_images_for_managed_control_plane", nil); err != nil {
			return err
		}
	}

	if err := exec.Dispatch(c.settings.RepoRootDir,
		"build_istioctl",
		nil); err != nil {
		return err
	}

	if c.settings.ControlPlane == resource.Unmanaged {
		if c.settings.CA == resource.PrivateCA {
			if err := exec.Dispatch(
				c.settings.RepoRootDir,
				"setup_private_ca",
				[]string{
					c.settings.KubectlContexts,
				}); err != nil {
				return err
			}
		}
		if c.settings.WIP == resource.HUBWorkloadIdentityPool {
			if err := exec.Dispatch(
				c.settings.RepoRootDir,
				"register_clusters_in_hub",
				[]string{
					c.settings.GCRProject,
					kube.ContextArr(c.settings.KubectlContexts),
				}); err != nil {
				return err
			}
		}
		if c.settings.ClusterType != resource.GKEOnGCP {
			if err := exec.Dispatch(
				c.settings.RepoRootDir,
				"create_asm_revision_label",
				nil); err != nil {
				return err
			}
		}
	}
	revLabel := revisionLabel()
	log.Printf("setting ASM_REVISION_LABEL=%s", revLabel)
	os.Setenv("ASM_REVISION_LABEL", revLabel)
	return nil
}

// postInstall contains all steps required after installing
func (c *installer) postInstall() error {
	if err := c.processKubeconfigs(); err != nil {
		return err
	}
	return nil
}

// processKubeconfigs should perform steps required after running ASM installation
func (c *installer) processKubeconfigs() error {
	return exec.Dispatch(
		c.settings.RepoRootDir,
		"process_kubeconfigs",
		nil)
}
