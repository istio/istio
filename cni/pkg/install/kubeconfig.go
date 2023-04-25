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

func createKubeConfig(cfg *config.InstallConfig) (*api.Config, error) {
	if len(cfg.K8sServiceHost) == 0 {
		return nil, fmt.Errorf("KUBERNETES_SERVICE_HOST not set. Is this not running within a pod?")
	}

	if len(cfg.K8sServicePort) == 0 {
		return nil, fmt.Errorf("KUBERNETES_SERVICE_PORT not set. Is this not running within a pod?")
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
			return nil, err
		}
		cluster.CertificateAuthorityData = caContents
	}

	token, err := os.ReadFile(constants.ServiceAccountPath + "/token")
	if err != nil {
		return nil, err
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

	return kcfg, nil
}

func writeKubeConfigFile(cfg *config.InstallConfig) error {
	kcfg, err := createKubeConfig(cfg)
	if err != nil {
		return err
	}
	kubeconfigFilepath := filepath.Join(cfg.MountedCNINetDir, cfg.KubeconfigFilename)

	lcfg, err := latest.Scheme.ConvertToVersion(kcfg, latest.ExternalVersion)
	if err != nil {
		return err
	}
	// Convert to v1 schema which has proper encoding
	kubeconfig, err := yaml.Marshal(lcfg)
	if err != nil {
		return err
	}
	if err := file.AtomicWrite(kubeconfigFilepath, kubeconfig, os.FileMode(cfg.KubeconfigMode)); err != nil {
		return err
	}

	// Log with redaction
	if err := api.RedactSecrets(kcfg); err == nil {
		lcfg, err := latest.Scheme.ConvertToVersion(kcfg, latest.ExternalVersion)
		if err != nil {
			return err
		}
		kcfgs, err := yaml.Marshal(lcfg)
		if err == nil {
			installLog.Infof("write kubeconfig file %s with: \n%+v", kubeconfigFilepath, string(kcfgs))
		}
	}

	return nil
}
