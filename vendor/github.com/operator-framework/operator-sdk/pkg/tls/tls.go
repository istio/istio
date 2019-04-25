// Copyright 2018 The Operator-SDK Authors
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

package tlsutil

import (
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"fmt"
	"io/ioutil"
	"strings"

	"k8s.io/api/core/v1"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
)

// CertType defines the type of the cert.
type CertType int

const (
	// ClientAndServingCert defines both client and serving cert.
	ClientAndServingCert CertType = iota
	// ServingCert defines a serving cert.
	ServingCert
	// ClientCert defines a client cert.
	ClientCert
)

// CertConfig configures how to generate the Cert.
type CertConfig struct {
	// CertName is the name of the cert.
	CertName string
	// Optional CertType. Serving, client or both; defaults to both.
	CertType CertType
	// Optional CommonName is the common name of the cert; defaults to "".
	CommonName string
	// Optional Organization is Organization of the cert; defaults to "".
	Organization []string
	// Optional CA Key, if user wants to provide custom CA key via a file path.
	CAKey string
	// Optional CA Certificate, if user wants to provide custom CA cert via file path.
	CACert string
	// TODO: consider to add passed in SAN fields.
}

// CertGenerator is an operator specific TLS tool that generates TLS assets for the deploying a user's application.
type CertGenerator interface {
	// GenerateCert generates a secret containing TLS encryption key and cert, a Secret
	// containing the CA key, and a ConfigMap containing the CA Certificate given the Custom
	// Resource(CR) "cr", the Kubernetes Service "Service", and the CertConfig "config".
	//
	// GenerateCert creates and manages TLS key and cert and CA with the following:
	// CA creation and management:
	// - If CA is not given:
	//  - A unique CA is generated for the CR.
	//  - CA's key is packaged into a Secret as shown below.
	//  - CA's cert is packaged in a ConfigMap as shown below.
	//  - The CA Secret and ConfigMap are created on the k8s cluster in the CR's namespace before
	//    returned to the user. The CertGenerator manages the CA Secret and ConfigMap to ensure it's
	//    unqiue per CR.
	// - If CA is given:
	//  - CA's key is packaged into a Secret as shown below.
	//  - CA's cert is packaged in a ConfigMap as shown below.
	//  - The CA Secret and ConfigMap are returned but not created in the K8s cluster in the CR's
	//    namespace. The CertGenerator doesn't manage the CA because the user controls the lifecycle
	//    of the CA.
	//
	// TLS Key and Cert Creation and Management:
	// - A unique TLS cert and key pair is generated per CR + CertConfig.CertName.
	// - The CA is used to generate and sign the TLS cert.
	// - The signing process uses the passed in "service" to set the Subject Alternative Names(SAN)
	//   for the certificate. We assume that the deployed applications are typically communicated
	//   with via a Kubernetes Service. The SAN is set to the FQDN of the service
	//   `<service-name>.<service-namespace>.svc.cluster.local`.
	// - Once TLS key and cert are created, they are packaged into a secret as shown below.
	// - Finally, the secret are created on the k8s cluster in the CR's namespace before returned to
	//   the user. The CertGenerator manages this secret to ensure that it is unique per CR +
	//   CertConfig.CertName.
	//
	// TLS encryption key and cert Secret format:
	// kind: Secret
	// apiVersion: v1
	// metadata:
	//  name: <cr-kind>-<cr-name>-<CertConfig.CertName>
	//  namespace: <cr-namespace>
	// data:
	//  tls.crt: ...
	//  tls.key: ...
	//
	// CA Certificate ConfigMap format:
	// kind: ConfigMap
	//   apiVersion: v1
	//   metadata:
	//     name: <cr-kind>-<cr-name>-ca
	//     namespace: <cr-namespace>
	//   data:
	//     ca.crt: ...
	//
	// CA Key Secret format:
	//  kind: Secret
	//  apiVersion: v1
	//  metadata:
	//   name: <cr-kind>-<cr-name>-ca
	//   namespace: <cr-namespace>
	//  data:
	//   ca.key: ..
	GenerateCert(cr runtime.Object, service *v1.Service, config *CertConfig) (*v1.Secret, *v1.ConfigMap, *v1.Secret, error)
}

const (
	// TLSPrivateCAKeyKey is the key for the private CA key field.
	TLSPrivateCAKeyKey = "ca.key"
	// TLSCertKey is the key for tls CA certificates.
	TLSCACertKey = "ca.crt"
)

// NewSDKCertGenerator constructs a new CertGenerator given the kubeClient.
func NewSDKCertGenerator(kubeClient kubernetes.Interface) CertGenerator {
	return &SDKCertGenerator{KubeClient: kubeClient}
}

type SDKCertGenerator struct {
	KubeClient kubernetes.Interface
}

// GenerateCert returns a secret containing the TLS encryption key and cert,
// a ConfigMap containing the CA Certificate and a Secret containing the CA key or it
// returns a error incase something goes wrong.
func (scg *SDKCertGenerator) GenerateCert(cr runtime.Object, service *v1.Service, config *CertConfig) (*v1.Secret, *v1.ConfigMap, *v1.Secret, error) {
	if err := verifyConfig(config); err != nil {
		return nil, nil, nil, err
	}

	k, n, ns, err := toKindNameNamespace(cr)
	if err != nil {
		return nil, nil, nil, err
	}
	appSecretName := ToAppSecretName(k, n, config.CertName)
	appSecret, err := getAppSecretInCluster(scg.KubeClient, appSecretName, ns)
	if err != nil {
		return nil, nil, nil, err
	}
	caSecretAndConfigMapName := ToCASecretAndConfigMapName(k, n)

	var (
		caSecret    *v1.Secret
		caConfigMap *v1.ConfigMap
	)

	caSecret, caConfigMap, err = getCASecretAndConfigMapInCluster(scg.KubeClient, caSecretAndConfigMapName, ns)
	if err != nil {
		return nil, nil, nil, err
	}

	if config.CAKey != "" && config.CACert != "" {
		// custom CA provided by the user.
		customCAKeyData, err := ioutil.ReadFile(config.CAKey)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("error reading CA Key from the given file name: %v", err)
		}

		customCACertData, err := ioutil.ReadFile(config.CACert)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("error reading CA Cert from the given file name: %v", err)
		}

		customCAKey, err := parsePEMEncodedPrivateKey(customCAKeyData)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("error parsing CA Key from the given file name: %v", err)
		}

		customCACert, err := parsePEMEncodedCert(customCACertData)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("error parsing CA Cert from the given file name: %v", err)
		}
		caSecret, caConfigMap = toCASecretAndConfigmap(customCAKey, customCACert, caSecretAndConfigMapName)
	} else if config.CAKey != "" || config.CACert != "" {
		// if only one of the custom CA Key or Cert is provided
		return nil, nil, nil, ErrCAKeyAndCACertReq
	}

	hasAppSecret := appSecret != nil
	hasCASecretAndConfigMap := caSecret != nil && caConfigMap != nil

	switch {
	case hasAppSecret && hasCASecretAndConfigMap:
		return appSecret, caConfigMap, caSecret, nil

	case hasAppSecret && !hasCASecretAndConfigMap:
		return nil, nil, nil, ErrCANotFound

	case !hasAppSecret && hasCASecretAndConfigMap:
		// Note: if a custom CA is passed in my the user it takes preference over an already
		// generated CA secret and CA configmap that might exist in the cluster
		caKey, err := parsePEMEncodedPrivateKey(caSecret.Data[TLSPrivateCAKeyKey])
		if err != nil {
			return nil, nil, nil, err
		}
		caCert, err := parsePEMEncodedCert([]byte(caConfigMap.Data[TLSCACertKey]))
		if err != nil {
			return nil, nil, nil, err
		}
		key, err := newPrivateKey()
		if err != nil {
			return nil, nil, nil, err
		}
		cert, err := newSignedCertificate(config, service, key, caCert, caKey)
		if err != nil {
			return nil, nil, nil, err
		}
		appSecret, err := scg.KubeClient.CoreV1().Secrets(ns).Create(toTLSSecret(key, cert, appSecretName))
		if err != nil {
			return nil, nil, nil, err
		}
		return appSecret, caConfigMap, caSecret, nil

	case !hasAppSecret && !hasCASecretAndConfigMap:
		// If no custom CAKey and CACert are provided we have to generate them
		caKey, err := newPrivateKey()
		if err != nil {
			return nil, nil, nil, err
		}
		caCert, err := newSelfSignedCACertificate(caKey)
		if err != nil {
			return nil, nil, nil, err
		}

		caSecret, caConfigMap := toCASecretAndConfigmap(caKey, caCert, caSecretAndConfigMapName)
		caSecret, err = scg.KubeClient.CoreV1().Secrets(ns).Create(caSecret)
		if err != nil {
			return nil, nil, nil, err
		}
		caConfigMap, err = scg.KubeClient.CoreV1().ConfigMaps(ns).Create(caConfigMap)
		if err != nil {
			return nil, nil, nil, err
		}
		key, err := newPrivateKey()
		if err != nil {
			return nil, nil, nil, err
		}
		cert, err := newSignedCertificate(config, service, key, caCert, caKey)
		if err != nil {
			return nil, nil, nil, err
		}
		appSecret, err := scg.KubeClient.CoreV1().Secrets(ns).Create(toTLSSecret(key, cert, appSecretName))
		if err != nil {
			return nil, nil, nil, err
		}
		return appSecret, caConfigMap, caSecret, nil
	default:
		return nil, nil, nil, ErrInternal
	}
}

func verifyConfig(config *CertConfig) error {
	if config == nil {
		return errors.New("nil CertConfig not allowed")
	}
	if config.CertName == "" {
		return errors.New("empty CertConfig.CertName not allowed")
	}
	return nil
}

func ToAppSecretName(kind, name, certName string) string {
	return strings.ToLower(kind) + "-" + name + "-" + certName
}

func ToCASecretAndConfigMapName(kind, name string) string {
	return strings.ToLower(kind) + "-" + name + "-ca"
}

func getAppSecretInCluster(kubeClient kubernetes.Interface, name, namespace string) (*v1.Secret, error) {
	se, err := kubeClient.CoreV1().Secrets(namespace).Get(name, metav1.GetOptions{})
	if err != nil && !apiErrors.IsNotFound(err) {
		return nil, err
	}
	if apiErrors.IsNotFound(err) {
		return nil, nil
	}
	return se, nil
}

// getCASecretAndConfigMapInCluster gets CA secret and configmap of the given name and namespace.
// it only returns both if they are found and nil if both are not found. In the case if only one of them is found,
// then we error out because we expect either both CA secret and configmap exit or not.
//
// NOTE: both the CA secret and configmap have the same name with template `<cr-kind>-<cr-name>-ca` which is what the
// input parameter `name` refers to.
func getCASecretAndConfigMapInCluster(kubeClient kubernetes.Interface, name, namespace string) (*v1.Secret, *v1.ConfigMap, error) {
	hasConfigMap := true
	cm, err := kubeClient.CoreV1().ConfigMaps(namespace).Get(name, metav1.GetOptions{})
	if err != nil && !apiErrors.IsNotFound(err) {
		return nil, nil, err
	}
	if apiErrors.IsNotFound(err) {
		hasConfigMap = false
	}

	hasSecret := true
	se, err := kubeClient.CoreV1().Secrets(namespace).Get(name, metav1.GetOptions{})
	if err != nil && !apiErrors.IsNotFound(err) {
		return nil, nil, err
	}
	if apiErrors.IsNotFound(err) {
		hasSecret = false
	}

	if hasConfigMap != hasSecret {
		// TODO: this case can happen if creating CA configmap succeeds and creating CA secret failed. We need to handle this case properly.
		return nil, nil, fmt.Errorf("expect either both ca configmap and secret both exist or not exist, but got hasCAConfigmap==%v and hasCASecret==%v", hasConfigMap, hasSecret)
	}
	if hasConfigMap == false {
		return nil, nil, nil
	}
	return se, cm, nil
}

func toKindNameNamespace(cr runtime.Object) (string, string, string, error) {
	a := meta.NewAccessor()
	k, err := a.Kind(cr)
	if err != nil {
		return "", "", "", err
	}
	n, err := a.Name(cr)
	if err != nil {
		return "", "", "", err
	}
	ns, err := a.Namespace(cr)
	if err != nil {
		return "", "", "", err
	}
	return k, n, ns, nil
}

// toTLSSecret returns a client/server "kubernetes.io/tls" secret.
// TODO: add owner ref.
func toTLSSecret(key *rsa.PrivateKey, cert *x509.Certificate, name string) *v1.Secret {
	return &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Data: map[string][]byte{
			v1.TLSPrivateKeyKey: encodePrivateKeyPEM(key),
			v1.TLSCertKey:       encodeCertificatePEM(cert),
		},
		Type: v1.SecretTypeTLS,
	}
}

// TODO: add owner ref.
func toCASecretAndConfigmap(key *rsa.PrivateKey, cert *x509.Certificate, name string) (*v1.Secret, *v1.ConfigMap) {
	return &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Data: map[string][]byte{
				TLSPrivateCAKeyKey: encodePrivateKeyPEM(key),
			},
		}, &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Data: map[string]string{
				TLSCACertKey: string(encodeCertificatePEM(cert)),
			},
		}
}
