// Copyright 2017 Istio Authors
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

// Package kube implements the shared and reusable library for Kubernetes
package kube

import (
	"crypto/tls"
	"fmt"
	"os"
	"strings"

	"github.com/golang/glog"
	multierror "github.com/hashicorp/go-multierror"

	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	// import GKE cluster authentication plugin
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	// import OIDC cluster authentication plugin, e.g. for Tectonic
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"

	"istio.io/pilot/model"
)

// ResolveConfig checks whether to use the in-cluster or out-of-cluster config
func ResolveConfig(kubeconfig string) (string, error) {
	if kubeconfig != "" {
		info, err := os.Stat(kubeconfig)
		if err != nil {
			if os.IsNotExist(err) {
				err = fmt.Errorf("kubernetes configuration file %q does not exist", kubeconfig)
			} else {
				err = multierror.Append(err, fmt.Errorf("kubernetes configuration file %q", kubeconfig))
			}
			return "", err
		}

		// if it's an empty file, switch to in-cluster config
		if info.Size() == 0 {
			glog.Info("using in-cluster configuration")
			return "", nil
		}
	}
	return kubeconfig, nil
}

// CreateInterface is a helper function to create Kubernetes interface
func CreateInterface(kubeconfig string) (kubernetes.Interface, error) {
	kube, err := ResolveConfig(kubeconfig)
	if err != nil {
		return nil, err
	}

	config, err := clientcmd.BuildConfigFromFlags("", kube)
	if err != nil {
		return nil, err
	}

	client, err := kubernetes.NewForConfig(config)
	return client, err
}

const (
	secretCert = "tls.crt"
	secretKey  = "tls.key"
)

type kubeSecretRegistry struct {
	client kubernetes.Interface
}

// MakeSecretRegistry creates an adaptor for secrets on Kubernetes.
// The adaptor uses the following path for secrets: _name.namespace_ where
// name and namespace correpond to the secret name and namespace.
func MakeSecretRegistry(client kubernetes.Interface) model.SecretRegistry {
	return &kubeSecretRegistry{client: client}
}

func (sr *kubeSecretRegistry) GetTLSSecret(uri string) (*model.TLSSecret, error) {
	parts := strings.Split(uri, ".")
	if len(parts) != 2 {
		return nil, fmt.Errorf("URI %q does not match <name>.<namespace>", uri)
	}

	secret, err := sr.client.CoreV1().Secrets(parts[1]).Get(parts[0], meta_v1.GetOptions{})
	if err != nil {
		return nil, multierror.Prefix(err, "failed to retrieve secret "+uri)
	}

	cert := secret.Data[secretCert]
	key := secret.Data[secretKey]
	if len(cert) == 0 || len(key) == 0 {
		return nil, fmt.Errorf("Secret keys %q and/or %q are missing", secretCert, secretKey)
	}

	if _, err = tls.X509KeyPair(cert, key); err != nil {
		return nil, err
	}

	return &model.TLSSecret{
		Certificate: cert,
		PrivateKey:  key,
	}, nil
}
