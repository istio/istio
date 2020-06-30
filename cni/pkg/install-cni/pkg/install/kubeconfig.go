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
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"text/template"

	"github.com/coreos/etcd/pkg/fileutil"
	"github.com/spf13/viper"

	"istio.io/istio/cni/pkg/install-cni/pkg/constants"
)

const kubeConfigTemplate = `# Kubeconfig file for Istio CNI plugin.
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
    token: {{.ServiceAccountToken}}
contexts:
- name: istio-cni-context
  context:
    cluster: local
    user: istio-cni
current-context: istio-cni-context
`

const serviceAccountPath = "/var/run/secrets/kubernetes.io/serviceaccount"

type kubeConfigFields struct {
	KubernetesServiceProtocol string
	KubernetesServiceHost     string
	KubernetesServicePort     string
	ServiceAccountToken       string
	TLSConfig                 string
}

func createKubeConfigFile() error {
	host := os.Getenv("KUBERNETES_SERVICE_HOST")
	if len(host) == 0 {
		return errors.New("KUBERNETES_SERVICE_HOST not set. Is this not running within a pod?")
	}

	port := os.Getenv("KUBERNETES_SERVICE_PORT")
	if len(port) == 0 {
		return errors.New("KUBERNETES_SERVICE_PORT not set. Is this not running within a pod?")
	}

	token, err := readServiceAccountToken()
	if err != nil {
		return err
	}

	tpl, err := template.New("kubeconfig").Parse(kubeConfigTemplate)
	if err != nil {
		return err
	}

	protocol := os.Getenv("KUBERNETES_SERVICE_PROTOCOL")
	if len(protocol) == 0 {
		protocol = "https"
	}

	caFile := viper.GetString(constants.KubeCAFile)
	if len(caFile) == 0 {
		caFile = serviceAccountPath + "/ca.crt"
	}

	var tlsConfig string
	if viper.GetBool(constants.SkipTLSVerify) {
		tlsConfig = "insecure-skip-tls-verify: true"
	} else if fileutil.Exist(caFile) {
		caContents, err := ioutil.ReadFile(caFile)
		if err != nil {
			return err
		}
		caBase64 := base64.StdEncoding.EncodeToString(caContents)
		tlsConfig = "certificate-authority-data: " + caBase64
	}

	fields := kubeConfigFields{
		KubernetesServiceProtocol: protocol,
		KubernetesServiceHost:     host,
		KubernetesServicePort:     port,
		ServiceAccountToken:       token,
		TLSConfig:                 tlsConfig,
	}

	tmpFile, err := ioutil.TempFile(viper.GetString(constants.MountedCNINetDir), "CNI_TEMP_")
	if err != nil {
		return err
	}

	if err = tpl.Execute(tmpFile, fields); err != nil {
		_ = tmpFile.Close()
		return err
	}

	if err = tmpFile.Close(); err != nil {
		return err
	}

	tmpFile.Close()

	filename := filepath.Join(viper.GetString(constants.MountedCNINetDir), viper.GetString(constants.KubeCfgFilename))
	if err = os.Rename(tmpFile.Name(), filename); err != nil {
		return err
	}

	return nil
}

func readServiceAccountToken() (string, error) {
	saToken := serviceAccountPath + "/token"
	if !fileutil.Exist(saToken) {
		return "", fmt.Errorf("SA Token file %s does not exist. Is this not running within a pod?", saToken)
	}

	token, err := ioutil.ReadFile(saToken)
	if err != nil {
		return "", err
	}

	return string(token), nil
}
