// Copyright Istio Authors.
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
	"context"
	"crypto/x509"
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	pkiutil "istio.io/istio/security/pkg/pki/util"
)

// Cluster represents the current state  of a cluster in the mesh.
//type Cluster struct {
//	ClusterDesc
//
//	// Current context referenced by the MeshDesc. This context corresponds to the `context` in
//	// the current kubeconfig file. It is essentially the human friendly display
//	// name. It can be changed by the user with`kubectl config rename-context`.
//	Context string
//	// generated cluster name. The uuid of kube-system Namespace. Fixed for the lifetime of cluster.
//	clusterName string
//	// TODO - differentiate NO_INSTALL, REMOTE, and MASTER
//	installed bool
//	client    kube.ExtendedClient
//}

const (
	defaultIstioNamespace = "istio-system"
)

// Use UUID of kube-system Namespace as unique identifier for cluster.
// (see https://docs.google.com/document/d/1F__vEKeI41P7PPUCMM9PVPYY34pyrvQI5rbTJVnS5c4)
func clusterUID(client kubernetes.Interface) (types.UID, error) {
	kubeSystem, err := client.CoreV1().Namespaces().Get(context.TODO(), "kube-system", metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return kubeSystem.UID, nil
}

//func NewCluster(ctx string, desc ClusterDesc, env Environment) (*Cluster, error) {
//	if desc.Namespace == "" {
//		desc.Namespace = defaultIstioNamespace
//	}
//	if desc.ServiceAccountReader == "" {
//		desc.ServiceAccountReader = constants.DefaultServiceAccountName
//	}
//
//	client, err := env.CreateClient(ctx)
//	if err != nil {
//		return nil, err
//	}
//
//	uid, err := clusterUID(client)
//	if err != nil {
//		return nil, err
//	}
//
//	// use the existence of pilot as assurance the control plane is present in the specified namespace.
//	var installed bool
//	_, err = client.CoreV1().Namespaces().Get(context.TODO(), desc.Namespace, metav1.GetOptions{})
//	switch {
//	case kerrors.IsNotFound(err):
//		installed = false
//	case err != nil:
//		env.Errorf("an error occurred trying to locate Istio in cluster %v: %v", ctx, err)
//	default:
//		installed = true
//	}
//
//	return &Cluster{
//		ClusterDesc: desc,
//		Context:     ctx,
//		clusterName: string(uid),
//		client:      client,
//		installed:   installed,
//	}, nil
//}

//func (c *Cluster) String() string {
//	return fmt.Sprintf("%v (%v)", c.clusterName, c.Context)
//}

type CACerts struct {
	// TODO select precedence if both secrets are present
	externalCACert   *x509.Certificate
	externalRootCert *x509.Certificate
	selfSignedCACert *x509.Certificate
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

type remoteSecrets map[string]*v1.Secret

const (
	//IstioIngressGatewayServiceName  = "istio-ingressgateway"
	IstioEastWestGatewayServiceName = "istio-eastwestgateway"
)
