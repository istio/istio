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
	"net"
	"os"
	"path/filepath"

	"k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/client-go/tools/clientcmd/api/latest"
	"sigs.k8s.io/yaml"

	"istio.io/istio/cni/pkg/config"
	"istio.io/istio/cni/pkg/constants"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/file"
)

type kubeconfig struct {
	// The full kubeconfig
	Full string
	// Kubeconfig with confidential data redacted.
	Redacted string
}

func createKubeConfig(cfg *config.InstallConfig) (kubeconfig, error) {
	if len(cfg.K8sServiceHost) == 0 {
		return kubeconfig{}, fmt.Errorf("KUBERNETES_SERVICE_HOST not set. Is this not running within a pod?")
	}

	if len(cfg.K8sServicePort) == 0 {
		return kubeconfig{}, fmt.Errorf("KUBERNETES_SERVICE_PORT not set. Is this not running within a pod?")
	}

	protocol := model.GetOrDefault(cfg.K8sServiceProtocol, "https")
	cluster := &api.Cluster{
		Server: fmt.Sprintf("%s://%s", protocol, net.JoinHostPort(cfg.K8sServiceHost, cfg.K8sServicePort)),
	}

	if cfg.SkipTLSVerify {
		// User explicitly opted into insecure.
		cluster.InsecureSkipTLSVerify = true
	} else {
		caFile := model.GetOrDefault(cfg.KubeCAFile, constants.ServiceAccountPath+"/ca.crt")
		caContents, err := os.ReadFile(caFile)
		if err != nil {
			return kubeconfig{}, err
		}
		cluster.CertificateAuthorityData = caContents
	}

	token, err := os.ReadFile(constants.ServiceAccountPath + "/token")
	if err != nil {
		return kubeconfig{}, err
	}

	const contextName = "istio-cni-context"
	const clusterName = "local"
	const userName = "istio-cni"
	kcfg := &api.Config{
		Kind:        "Config",
		APIVersion:  "v1",
		Preferences: api.Preferences{},
		Clusters: map[string]*api.Cluster{
			clusterName: cluster,
		},
		AuthInfos: map[string]*api.AuthInfo{
			userName: {
				Token: string(token),
			},
		},
		Contexts: map[string]*api.Context{
			contextName: {
				AuthInfo: userName,
				Cluster:  clusterName,
			},
		},
		CurrentContext: contextName,
	}

	lcfg, err := latest.Scheme.ConvertToVersion(kcfg, latest.ExternalVersion)
	if err != nil {
		return kubeconfig{}, err
	}
	// Convert to v1 schema which has proper encoding
	fullYaml, err := yaml.Marshal(lcfg)
	if err != nil {
		return kubeconfig{}, err
	}

	// Log with redaction
	if err := api.RedactSecrets(kcfg); err != nil {
		return kubeconfig{}, err
	}
	for _, c := range kcfg.Clusters {
		// Not actually sensitive, just annoyingly verbose.
		c.CertificateAuthority = "REDACTED"
	}
	lrcfg, err := latest.Scheme.ConvertToVersion(kcfg, latest.ExternalVersion)
	if err != nil {
		return kubeconfig{}, err
	}
	redacted, err := yaml.Marshal(lrcfg)
	if err != nil {
		return kubeconfig{}, err
	}

	return kubeconfig{
		Full:     string(fullYaml),
		Redacted: string(redacted),
	}, nil
}

// maybeWriteKubeConfigFile will validate the existing kubeConfig file, and rewrite/replace it if required.
func maybeWriteKubeConfigFile(cfg *config.InstallConfig) error {
	kc, err := createKubeConfig(cfg)
	if err != nil {
		return err
	}

	if err := checkExistingKubeConfigFile(cfg, kc); err != nil {
		installLog.Info("kubeconfig either does not exist or is out of date, writing a new one")
		kubeconfigFilepath := filepath.Join(cfg.MountedCNINetDir, cfg.KubeconfigFilename)
		if err := file.AtomicWrite(kubeconfigFilepath, []byte(kc.Full), os.FileMode(cfg.KubeconfigMode)); err != nil {
			return err
		}
		installLog.Infof("wrote kubeconfig file %s with: \n%+v", kubeconfigFilepath, kc.Redacted)
	}
	return nil
}

// checkExistingKubeConfigFile returns an error if no kubeconfig exists at the configured path,
// or if a kubeconfig exists there, but differs from the current config.
// In any case, an error indicates the file must be (re)written, and no error means no action need be taken
func checkExistingKubeConfigFile(cfg *config.InstallConfig, expectedKC kubeconfig) error {
	kubeconfigFilepath := filepath.Join(cfg.MountedCNINetDir, cfg.KubeconfigFilename)

	existingKC, err := os.ReadFile(kubeconfigFilepath)
	if err != nil {
		installLog.Debugf("no preexisting kubeconfig at %s, assuming we need to create one", kubeconfigFilepath)
		return err
	}

	if expectedKC.Full == string(existingKC) {
		installLog.Debugf("preexisting kubeconfig %s is an exact match for expected, no need to update", kubeconfigFilepath)
		return nil
	}

	return fmt.Errorf("kubeconfig on disk differs from expected, assuming we need to rewrite it")
}
