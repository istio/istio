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
		caFile := model.GetOrDefault(cfg.KubeCAFile, cfg.K8sServiceAccountPath+"/ca.crt")
		caContents, err := os.ReadFile(caFile)
		if err != nil {
			return kubeconfig{}, err
		}
		cluster.CertificateAuthorityData = caContents
	}

	token, err := os.ReadFile(cfg.K8sServiceAccountPath + "/token")
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

// writeKubeConfigFile will rewrite/replace the kubeconfig used by the CNI plugin.
// We are the only consumers of this file and it resides in our owned rundir on the host node,
// so we are good to simply write it out if our watched svcacct token changes.
func writeKubeConfigFile(cfg *config.InstallConfig) error {
	kc, err := createKubeConfig(cfg)
	if err != nil {
		return err
	}

	kubeconfigFilepath := filepath.Join(cfg.CNIAgentRunDir, constants.CNIPluginKubeconfName)
	if err := file.AtomicWrite(kubeconfigFilepath, []byte(kc.Full), os.FileMode(cfg.KubeconfigMode)); err != nil {
		installLog.Debugf("error writing kubeconfig: %v", err)
		return err
	}
	installLog.Infof("wrote kubeconfig file %s with: \n%+v", kubeconfigFilepath, kc.Redacted)
	return nil
}
