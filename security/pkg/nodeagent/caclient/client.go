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

	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"

	"istio.io/istio/pkg/log"
	"istio.io/istio/security/pkg/k8s/controller"
	caClientInterface "istio.io/istio/security/pkg/nodeagent/caclient/interface"
	citadel "istio.io/istio/security/pkg/nodeagent/caclient/providers/citadel"
	gca "istio.io/istio/security/pkg/nodeagent/caclient/providers/google"
)

const (
	googleCAName = "GoogleCA"
	citadelName  = "Citadel"

	retryInterval = time.Second * 2
	maxRetries    = 100
)

// NewCAClient create an CA client.
func NewCAClient(endpoint, CAProviderName string, tlsFlag bool) (caClientInterface.Client, error) {
	switch CAProviderName {
	case googleCAName:
		return gca.NewGoogleCAClient(endpoint, tlsFlag)
	case citadelName:
		rootCert, err := getCATLSRootCertFromConfigMap()
		if err != nil {
			return nil, err
		}
		return citadel.NewCitadelClient(endpoint, tlsFlag, rootCert)
	default:
		return nil, fmt.Errorf(
			"CA provider %q isn't supported. Currently Istio supports %q", CAProviderName, strings.Join([]string{googleCAName, citadelName}, ","))
	}
}

func getCATLSRootCertFromConfigMap() ([]byte, error) {
	// Get the kubeconfig from the K8s cluster the node agent is running in.
	kubeconfig, err := restclient.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("cannot load kubeconfig: %v", err)
	}
	clientSet, err := kubernetes.NewForConfig(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create a clientset (error: %v)", err)
	}
	controller := controller.NewConfigMapController("", clientSet.CoreV1())

	cert := ""
	// Keep retrying until the root cert is fetched.
	for i := 0; i < maxRetries; i++ {
		cert, err = controller.GetCATLSRootCert()
		if err == nil {
			break
		}
		time.Sleep(retryInterval)
		log.Infof("unalbe to fetch CA TLS root cert, retry in %v", retryInterval)
	}
	if cert == "" {
		return nil, fmt.Errorf("exhausted all the retries (%d) to fetch the CA TLS root cert", maxRetries)
	}

	certDecoded, err := base64.StdEncoding.DecodeString(cert)
	if err != nil {
		return nil, fmt.Errorf("cannot decode the CA TLS root cert: %v", err)
	}
	return certDecoded, nil
}
