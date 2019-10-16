// Copyright 2019 Istio Authors.
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

package multicluster

import (
	"crypto/x509"
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pkg/kube/secretcontroller"
	pkiutil "istio.io/istio/security/pkg/pki/util"
)

// Cluster represents the current state  of a cluster in the mesh.
type Cluster struct {
	ClusterDesc

	// Current context referenced by the MeshDesc. This context corresponds to the `context` in
	// the current kubeconfig file. It is essentially the human friendly display
	// name. It can be changed by the user with`kubectl config rename-context`.
	context string
	// uuid of kube-system Namespace. Fixed for the lifetime of cluster.
	uid       types.UID
	installed bool
	client    kubernetes.Interface
}

const (
	defaultIstioNamespace       = "istio-system"
	defaultServiceAccountReader = "istio-multi"
)

// Use UUID of kube-system Namespace as unique identifier for cluster.
// (see https://docs.google.com/document/d/1F__vEKeI41P7PPUCMM9PVPYY34pyrvQI5rbTJVnS5c4)
func clusterUID(client kubernetes.Interface) (types.UID, error) {
	kubeSystem, err := client.CoreV1().Namespaces().Get("kube-system", metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return kubeSystem.UID, nil
}

func NewCluster(context string, desc ClusterDesc, env Environment) (*Cluster, error) {
	if desc.Namespace == "" {
		desc.Namespace = defaultIstioNamespace
	}
	if desc.ServiceAccountReader == "" {
		desc.ServiceAccountReader = defaultServiceAccountReader
	}

	client, err := env.CreateClientSet(context)
	if err != nil {
		return nil, err
	}

	uid, err := clusterUID(client)
	if err != nil {
		return nil, err
	}

	// use the existence of pilot as assurance the control plane is present in the specified namespace.
	var installed bool
	if _, err := client.AppsV1().Deployments(desc.Namespace).Get("istio-pilot", metav1.GetOptions{}); err == nil {
		installed = true
	}

	return &Cluster{
		ClusterDesc: desc,
		context:     context,
		uid:         uid,
		client:      client,
		installed:   installed,
	}, nil
}

func (c *Cluster) String() string {
	return fmt.Sprintf("%v (%v)", c.uid, c.context)
}

type CACerts struct {
	// TODO select precedence if both secrets are present
	externalCACert     *x509.Certificate
	externalRootCert   *x509.Certificate
	selfSignedCACert   *x509.Certificate
	selfSignedRootCert *x509.Certificate
}

func extractCert(filename string, secret *v1.Secret) (*x509.Certificate, error) {
	encoded, ok := secret.Data[filename]
	if !ok {
		return nil, fmt.Errorf("%q not found in secret %v", filename, secret.Name)
	}
	cert, err := pkiutil.ParsePemEncodedCertificate(encoded)
	if err != nil {
		return nil, fmt.Errorf("failed to parse %q from secret %v: %v", filename, secret.Name, err)
	}
	return cert, nil
}

type remoteSecrets map[types.UID]*v1.Secret

func (c *Cluster) readRemoteSecrets(env Environment) remoteSecrets {
	secretMap := make(remoteSecrets)
	listOptions := metav1.ListOptions{
		LabelSelector: fields.SelectorFromSet(fields.Set{secretcontroller.MultiClusterSecretLabel: "true"}).String(),
	}
	secrets, err := c.client.CoreV1().Secrets(c.Namespace).List(listOptions)
	if err != nil {
		env.Errorf("error: could not list secrets in cluster %v: %v\n", c, err)
		return secretMap
	}
	for i := range secrets.Items {
		secret := &secrets.Items[i]
		uid := types.UID(secret.Name)
		secretMap[uid] = secret
	}
	return secretMap
}

func (c *Cluster) readCACerts(env Environment) *CACerts {
	cs := &CACerts{}
	externalCASecret, err := c.client.CoreV1().Secrets(c.Namespace).Get("cacerts", metav1.GetOptions{})
	if err == nil {
		if cs.externalCACert, err = extractCert("ca-cert.pem", externalCASecret); err != nil {
			env.Errorf("error: %v\n", err)
		}
		if cs.externalRootCert, err = extractCert("root-cert.pem", externalCASecret); err != nil {
			env.Errorf("error: %v\n", err)
		}
	}
	selfSignedCASecret, err := c.client.CoreV1().Secrets(c.Namespace).Get("istio-ca-secrets", metav1.GetOptions{})
	if err == nil {
		if cs.selfSignedCACert, err = extractCert("ca-cert.pem", selfSignedCASecret); err != nil {
			env.Errorf("error: %v\n", err)
		}
		if cs.selfSignedRootCert, err = extractCert("root-cert.pem", selfSignedCASecret); err != nil {
			env.Errorf("error: %v\n", err)
		}
	}
	return cs
}

func (c *Cluster) readIngressGatewayAddresses(env Environment) []string {
	var addresses []string

	ingress, err := c.client.CoreV1().Services(c.Namespace).Get("istio-ingressgateway", metav1.GetOptions{})
	if err != nil {
		env.Errorf("error: istio-ingressgateway not found: %v\n", err)
		return addresses
	}

	for _, ip := range ingress.Status.LoadBalancer.Ingress {
		if ip.IP != "" {
			addresses = append(addresses, ip.IP)
		}
		if ip.Hostname != "" {
			addresses = append(addresses, ip.Hostname)
		}
	}
	return addresses
}
