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

package istio

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	pb "istio.io/api/security/v1alpha1"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	pkiutil "istio.io/istio/security/pkg/pki/util"
)

type Cert struct {
	ClientCert, Key, RootCert []byte
}

func CreateCertificateForCluster(t framework.TestContext, i Instance, serviceAccount, namespace string, c cluster.Cluster) (Cert, error) {
	rootCert, err := FetchRootCert(c.Kube())
	if err != nil {
		return Cert{}, fmt.Errorf("failed to fetch root cert: %v", err)
	}

	token, err := GetServiceAccountToken(c.Kube(), "istio-ca", namespace, serviceAccount)
	if err != nil {
		return Cert{}, err
	}

	san := fmt.Sprintf("spiffe://%s/ns/%s/sa/%s", "cluster.local", namespace, serviceAccount)
	options := pkiutil.CertOptions{
		Host:       san,
		RSAKeySize: 2048,
	}
	// Generate the cert/key, send CSR to CA.
	csrPEM, keyPEM, err := pkiutil.GenCSR(options)
	if err != nil {
		return Cert{}, err
	}
	a, err := i.InternalDiscoveryAddressFor(c)
	if err != nil {
		return Cert{}, err
	}
	client, err := newCitadelClient(a, []byte(rootCert))
	if err != nil {
		return Cert{}, fmt.Errorf("creating citadel client: %v", err)
	}
	req := &pb.IstioCertificateRequest{
		Csr:              string(csrPEM),
		ValidityDuration: int64((time.Hour * 24 * 7).Seconds()),
	}
	clusterName := constants.DefaultClusterName
	if t.Settings().AmbientMultiNetwork {
		clusterName = c.Name()
	}
	rctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("Authorization", "Bearer "+token, "ClusterID", clusterName))
	resp, err := client.CreateCertificate(rctx, req)
	if err != nil {
		return Cert{}, fmt.Errorf("send CSR: %v", err)
	}
	certChain := []byte{}
	for _, c := range resp.CertChain {
		certChain = append(certChain, []byte(c)...)
	}
	return Cert{certChain, keyPEM, []byte(rootCert)}, nil
}

func CreateCertificate(t framework.TestContext, i Instance, serviceAccount, namespace string) (Cert, error) {
	return CreateCertificateForCluster(t, i, serviceAccount, namespace, t.Clusters().Default())
}

// 7 days
var saTokenExpiration int64 = 60 * 60 * 24 * 7

func GetServiceAccountToken(c kubernetes.Interface, aud, ns, sa string) (string, error) {
	san := san(ns, sa)

	if got, f := cachedTokens.Load(san); f {
		t := got.(token)
		if t.expiration.After(time.Now().Add(time.Minute)) {
			return t.token, nil
		}
		// Otherwise, its expired, load a new one
	}
	rt, err := c.CoreV1().ServiceAccounts(ns).CreateToken(context.Background(), sa,
		&authenticationv1.TokenRequest{
			Spec: authenticationv1.TokenRequestSpec{
				Audiences:         []string{aud},
				ExpirationSeconds: &saTokenExpiration,
			},
		}, metav1.CreateOptions{})
	if err != nil {
		return "", err
	}
	exp := rt.Status.ExpirationTimestamp.Time
	cachedTokens.Store(san, token{rt.Status.Token, exp})
	return rt.Status.Token, nil
}

// map of SAN to jwt token. Used to avoid repetitive calls
var cachedTokens sync.Map

type token struct {
	token      string
	expiration time.Time
}

// NewCitadelClient create a CA client for Citadel.
func newCitadelClient(endpoint string, rootCert []byte) (pb.IstioCertificateServiceClient, error) {
	certPool := x509.NewCertPool()
	ok := certPool.AppendCertsFromPEM(rootCert)
	if !ok {
		return nil, fmt.Errorf("failed to append certificates")
	}
	config := tls.Config{
		RootCAs:            certPool,
		InsecureSkipVerify: true, // nolint: gosec // test only code
	}
	transportCreds := credentials.NewTLS(&config)

	conn, err := grpc.Dial(endpoint, grpc.WithTransportCredentials(transportCreds))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to endpoint %s", endpoint)
	}

	client := pb.NewIstioCertificateServiceClient(conn)
	return client, nil
}

func san(ns, sa string) string {
	return fmt.Sprintf("spiffe://%s/ns/%s/sa/%s", "cluster.local", ns, sa)
}

func FetchRootCert(c kubernetes.Interface) (string, error) {
	cm, err := c.CoreV1().ConfigMaps("istio-system").Get(context.TODO(), "istio-ca-root-cert", metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return cm.Data["root-cert.pem"], nil
}
