package multicluster

import (
	"crypto/x509"
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pkg/kube/secretcontroller"
	pkiutil "istio.io/istio/security/pkg/pki/util"
)

// KubeCluster represents the current state  of a cluster in the mesh.
type KubeCluster struct {
	ClusterDesc

	// current Context referenced by the MeshDesc.
	context string

	// uuid of kube-system Namespace. Fixed for the lifetime of cluster.
	uid string

	client kubernetes.Interface

	state *clusterState
}

func (c *KubeCluster) UniqueName() string {
	return fmt.Sprintf("%v-%v", c.uid, c.context)
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

type clusterState struct {
	uid string

	// cached state from cluster
	installed        bool
	gatewayAddresses []string

	// TODO select precedence if both secrets are present
	externalCACert     *x509.Certificate
	externalRootCert   *x509.Certificate
	selfSignedCACert   *x509.Certificate
	selfSignedRootCert *x509.Certificate

	remoteSecrets map[string]*v1.Secret
}

func refreshClusterState(client kubernetes.Interface, desc *ClusterDesc, env Environment) (*clusterState, error) {
	var state clusterState

	kubeSystem, err := client.CoreV1().Namespaces().Get("kube-system", metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	state.uid = string(kubeSystem.UID)

	if _, err := client.AppsV1().Deployments(desc.Namespace).Get("istio-pilot", metav1.GetOptions{}); err == nil {
		state.installed = true
	}

	// informational
	secrets, err := client.CoreV1().Secrets(desc.Namespace).List(metav1.ListOptions{
		LabelSelector: fields.SelectorFromSet(fields.Set{secretcontroller.MultiClusterSecretLabel: "true"}).String(),
	})
	state.remoteSecrets = make(map[string]*v1.Secret)
	if err == nil {
		for i := range secrets.Items {
			secret := &secrets.Items[i]
			state.remoteSecrets[secret.Name] = secret
		}
	}

	externalCASecret, err := client.CoreV1().Secrets(desc.Namespace).Get("cacerts", metav1.GetOptions{})
	if err == nil {
		state.externalCACert, err = extractCert("ca-cert.pem", externalCASecret)
		if err != nil {
			fmt.Fprint(env.Stderr(), err)
		}
		state.externalRootCert, err = extractCert("root-cert.pem", externalCASecret)
		if err != nil {
			fmt.Fprint(env.Stderr(), err)
		}
	}
	selfSignedCASecret, err := client.CoreV1().Secrets(desc.Namespace).Get("istio-ca-secrets", metav1.GetOptions{})
	if err == nil {
		state.selfSignedCACert, err = extractCert("ca-cert.pem", selfSignedCASecret)
		if err != nil {
			fmt.Fprint(env.Stderr(), err)
		}
		state.selfSignedRootCert, err = extractCert("root-cert.pem", selfSignedCASecret)
		if err != nil {
			fmt.Fprint(env.Stderr(), err)
		}
	}

	var gatewayAddresses []string
	if ingress, err := client.CoreV1().Services(desc.Namespace).Get("istio-ingressgateway", metav1.GetOptions{}); err == nil {
		for _, ip := range ingress.Status.LoadBalancer.Ingress {
			if ip.IP != "" {
				gatewayAddresses = append(gatewayAddresses, ip.IP)
			}
			if ip.Hostname != "" {
				gatewayAddresses = append(gatewayAddresses, ip.Hostname)
			}
		}
	}

	return &state, nil
}

func NewCluster(kubeconfig, context string, desc ClusterDesc, env Environment) (*KubeCluster, error) {
	if desc.Namespace == "" {
		desc.Namespace = defaultIstioNamespace
	}
	if desc.ServiceAccountReader == "" {
		desc.ServiceAccountReader = defaultServiceAccountReader
	}

	client, err := env.CreateClientSet(kubeconfig, context)
	if err != nil {
		return nil, err
	}

	state, err := refreshClusterState(client, &desc, env)
	if err != nil {
		return nil, err
	}

	return &KubeCluster{
		ClusterDesc: desc,
		context:     context,
		uid:         state.uid,
		client:      client,
		state:       state,
	}, nil
}
