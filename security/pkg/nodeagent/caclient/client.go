// Copyright 2018 Istio Authors
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

package caclient

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/log"
	"istio.io/istio/security/pkg/k8s/configmap"
	caClientInterface "istio.io/istio/security/pkg/nodeagent/caclient/interface"
	citadel "istio.io/istio/security/pkg/nodeagent/caclient/providers/citadel"
	gca "istio.io/istio/security/pkg/nodeagent/caclient/providers/google"
	vault "istio.io/istio/security/pkg/nodeagent/caclient/providers/vault"
)

const (
	googleCAName = "GoogleCA"
	citadelName  = "Citadel"
	vaultCAName  = "VaultCA"
	ns           = "istio-system"

	retryInterval = time.Second * 2
	maxRetries    = 100
)

type configMap interface {
	GetCATLSRootCert() (string, error)
}

// NewCAClient create an CA client.
func NewCAClient(endpoint, caProviderName string, tlsFlag bool, tlsRootCert []byte, vaultAddr, vaultRole,
	vaultAuthPath, vaultSignCsrPath string) (caClientInterface.Client, error) {
	switch caProviderName {
	case googleCAName:
		return gca.NewGoogleCAClient(endpoint, tlsFlag)
	case vaultCAName:
		return vault.NewVaultClient(tlsFlag, tlsRootCert, vaultAddr, vaultRole, vaultAuthPath, vaultSignCsrPath)
	case citadelName:
		cs, err := kube.CreateClientset("", "")
		if err != nil {
			return nil, fmt.Errorf("could not create k8s clientset: %v", err)
		}
		controller := configmap.NewController(ns, cs.CoreV1())
		rootCert, err := getCATLSRootCertFromConfigMap(controller, retryInterval, maxRetries)
		if err != nil {
			return nil, err
		}
		return citadel.NewCitadelClient(endpoint, tlsFlag, rootCert)
	default:
		return nil, fmt.Errorf(
			"CA provider %q isn't supported. Currently Istio supports %q", caProviderName, strings.Join([]string{googleCAName, citadelName, vaultCAName}, ","))
	}
}

func getCATLSRootCertFromConfigMap(controller configMap, interval time.Duration, max int) ([]byte, error) {
	cert := ""
	var err error
	// Keep retrying until the root cert is fetched or the number of retries is exhausted.
	for i := 0; i < max+1; i++ {
		cert, err = controller.GetCATLSRootCert()
		if err == nil {
			break
		}
		time.Sleep(retryInterval)
		log.Infof("unalbe to fetch CA TLS root cert: %v, retry in %v", err, interval)
	}
	if cert == "" {
		return nil, fmt.Errorf("exhausted all the retries (%d) to fetch the CA TLS root cert", max)
	}

	certDecoded, err := base64.StdEncoding.DecodeString(cert)
	if err != nil {
		return nil, fmt.Errorf("cannot decode the CA TLS root cert: %v", err)
	}
	return certDecoded, nil
}
