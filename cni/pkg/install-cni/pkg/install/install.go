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
	"github.com/coreos/etcd/pkg/fileutil"
	"io/ioutil"
	"istio.io/istio/cni/pkg/install-cni/pkg/config"
	"istio.io/istio/cni/pkg/install-cni/pkg/constants"
)

// Run starts the installation process with given configuration
func Run(cfg *config.Config) (err error) {
	if err = copyBinaries(cfg.UpdateCNIBinaries, cfg.SkipCNIBinaries); err != nil {
		return
	}

	saToken, err := readServiceAccountToken()
	if err != nil {
		return err
	}

	if err = createKubeconfigFile(cfg, saToken); err != nil {
		return
	}

	if err = createCNIConfigFile(cfg, saToken); err != nil {
		return
	}

	return
}

func readServiceAccountToken() (string, error) {
	saToken := constants.ServiceAccountPath + "/token"
	if !fileutil.Exist(saToken) {
		return "", fmt.Errorf("SA Token file %s does not exist. Is this not running within a pod?", saToken)
	}

	token, err := ioutil.ReadFile(saToken)
	if err != nil {
		return "", err
	}

	return string(token), nil
}
