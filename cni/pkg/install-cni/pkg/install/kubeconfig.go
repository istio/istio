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
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"text/template"

	"github.com/coreos/etcd/pkg/fileutil"
	"github.com/pkg/errors"

	"istio.io/istio/cni/pkg/install-cni/pkg/config"
	"istio.io/istio/cni/pkg/install-cni/pkg/constants"
)

const kubeconfigTemplate = `# Kubeconfig file for Istio CNI plugin.
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

type kubeconfigFields struct {
	KubernetesServiceProtocol string
	KubernetesServiceHost     string
	KubernetesServicePort     string
	ServiceAccountToken       string
	TLSConfig                 string
}

func createKubeconfigFile(cfg *config.Config, saToken string) (kubeconfigFilepath string, err error) {
	if len(cfg.K8sServiceHost) == 0 {
		err = errors.New("KUBERNETES_SERVICE_HOST not set. Is this not running within a pod?")
		return
	}

	if len(cfg.K8sServicePort) == 0 {
		err = errors.New("KUBERNETES_SERVICE_PORT not set. Is this not running within a pod?")
		return
	}

	var tpl *template.Template
	tpl, err = template.New("kubeconfig").Parse(kubeconfigTemplate)
	if err != nil {
		return
	}

	protocol := cfg.K8sServiceProtocol
	if len(protocol) == 0 {
		protocol = "https"
	}

	caFile := cfg.KubeCAFile
	if len(caFile) == 0 {
		caFile = constants.ServiceAccountPath + "/ca.crt"
	}

	var tlsConfig string
	if cfg.SkipTLSVerify {
		tlsConfig = "insecure-skip-tls-verify: true"
	} else {
		if !fileutil.Exist(caFile) {
			return "", fmt.Errorf("file does not exist: %s", caFile)
		}
		var caContents []byte
		caContents, err = ioutil.ReadFile(caFile)
		if err != nil {
			return
		}
		caBase64 := base64.StdEncoding.EncodeToString(caContents)
		tlsConfig = "certificate-authority-data: " + caBase64
	}

	fields := kubeconfigFields{
		KubernetesServiceProtocol: protocol,
		KubernetesServiceHost:     cfg.K8sServiceHost,
		KubernetesServicePort:     cfg.K8sServicePort,
		ServiceAccountToken:       saToken,
		TLSConfig:                 tlsConfig,
	}

	var tmpFile *os.File
	tmpFile, err = ioutil.TempFile(cfg.MountedCNINetDir, cfg.KubeconfigFilename+".tmp.")
	if err != nil {
		return
	}
	defer func() {
		if fileutil.Exist(tmpFile.Name()) {
			if rmErr := os.Remove(tmpFile.Name()); rmErr != nil {
				if err != nil {
					err = errors.Wrap(err, rmErr.Error())
				} else {
					err = rmErr
				}
			}
		}
	}()

	if err = os.Chmod(tmpFile.Name(), os.FileMode(cfg.KubeconfigMode)); err != nil {
		return
	}

	if err = tpl.Execute(tmpFile, fields); err != nil {
		if closeErr := tmpFile.Close(); closeErr != nil {
			err = errors.Wrap(err, closeErr.Error())
		}
		return
	}

	if err = tmpFile.Close(); err != nil {
		return
	}

	kubeconfigFilepath = filepath.Join(cfg.MountedCNINetDir, cfg.KubeconfigFilename)
	if err = os.Rename(tmpFile.Name(), kubeconfigFilepath); err != nil {
		return "", err
	}

	return
}
