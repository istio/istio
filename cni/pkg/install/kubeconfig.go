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
	"text/template"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/client-go/tools/clientcmd/api/latest"
	"sigs.k8s.io/yaml"

	"istio.io/istio/cni/pkg/config"
	"istio.io/istio/cni/pkg/constants"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/file"
)

var kubeconfigTemplate = func() *template.Template {
	t := `# Kubeconfig file for Istio CNI plugin.
apiVersion: v1
kind: Config
clusters:
- name: local
  cluster:
    server: {{.KubernetesServiceProtocol}}://[{{.KubernetesServiceHost}}]:{{.KubernetesServicePort}}
    {{.TLSConfig}}
users:
- name: istio-cni
  user:
    token: "{{.ServiceAccountToken}}"
contexts:
- name: istio-cni-context
  context:
    cluster: local
    user: istio-cni
current-context: istio-cni-context
`
	tpl, err := template.New("kubeconfig").Parse(t)
	if err != nil {
		panic(fmt.Errorf("failed to parse kubeconfig template: %v", err))
	}
	return tpl
}

type kubeconfigFields struct {
	KubernetesServiceProtocol string
	KubernetesServiceHost     string
	KubernetesServicePort     string
	ServiceAccountToken       string
	TLSConfig                 string
}

func createKubeconfig(cfg *config.InstallConfig) (runtime.Object, error) {
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
		caFile := model.GetOrDefault(cfg.KubeCAFile, constants.ServiceAccountCAPath)
		caContents, err := os.ReadFile(caFile)
		if err != nil {
			return nil, err
		}
		cluster.CertificateAuthorityData = caContents
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
				TokenFile: constants.ServiceAccountTokenPath,
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

	return latest.Scheme.ConvertToVersion(kcfg, latest.ExternalVersion)
}

func writeKubeconfigFile(cfg *config.InstallConfig) error {
	kcfg, err := createKubeconfig(cfg)
	if err != nil {
		return err
	}
	kubeconfigFilepath := filepath.Join(cfg.MountedCNINetDir, cfg.KubeconfigFilename)
	installLog.Infof("write kubeconfig file %s with: \n%+v", kubeconfigFilepath, kcfg)

	kubeconfig, err := yaml.Marshal(kcfg)
	if err != nil {
		return err
	}
	if err := file.AtomicWrite(kubeconfigFilepath, kubeconfig, os.FileMode(cfg.KubeconfigMode)); err != nil {
		return err
	}

	return nil
}
