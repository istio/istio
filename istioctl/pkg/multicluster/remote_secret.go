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
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apimachinery/pkg/runtime/serializer/versioning"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth" // to avoid 'No Auth Provider found for name "gcp"'
	"k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/client-go/tools/clientcmd/api/latest"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/secretcontroller"
)

var (
	Codec  runtime.Codec
	Scheme *runtime.Scheme
)

func init() {
	Scheme = runtime.NewScheme()
	utilruntime.Must(v1.AddToScheme(Scheme))
	yamlSerializer := json.NewYAMLSerializer(json.DefaultMetaFactory, Scheme, Scheme)
	Codec = versioning.NewDefaultingCodecForScheme(
		Scheme,
		yamlSerializer,
		yamlSerializer,
		v1.SchemeGroupVersion,
		runtime.InternalGroupVersioner,
	)
}

type commandFlags struct {
	serviceAccountName string
}

var (
	defaultSecretPrefix       = "istio-remote-secret-"
	DefaultServiceAccountName = "istio-pilot-service-account"
)

// NewCreateRemoteSecretCommand creates a new command for joining two clusters togeather in a multi-cluster mesh.
func NewCreateRemoteSecretCommand(kubeconfig, context, namespace *string) *cobra.Command {
	f := commandFlags{
		serviceAccountName: DefaultServiceAccountName,
	}

	c := &cobra.Command{
		Use:   "create-remote-secret <cluster-name>",
		Short: "Create a secret with credentials to allow Istio to access remote Kubernetes apiservers",
		Example: `
# Create a secret to access cluster c0's apiserver and install it in cluster c1.
istioctl --kubeconfig=c0.yaml x create-remote-secret c0 \
    | kubectl -n istio-system --kubeconfig=c1.yaml apply -f -

# Delete a secret that was previously installed in c1
istioctl --kubeconfig=c0.yaml x create-remote-secret c1 \
    | kubectl -n istio-system --kubeconfig=c1.yaml delete -f -

`,
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			out, err := CreateRemoteSecret(*kubeconfig, *context, *namespace, f.serviceAccountName, args[0])
			if err != nil {
				fmt.Fprintf(c.OutOrStderr(), "%v", err)
				os.Exit(1)
			}
			fmt.Fprint(c.OutOrStdout(), out)
			return nil
		},
	}

	flags := c.PersistentFlags()
	flags.StringVar(&f.serviceAccountName, "service-account", f.serviceAccountName,
		"create a secret with this service account's credentials.")

	return c
}

// hooks for testing
var (
	newStartingConfig = func(kubeconfig, context string) (*api.Config, error) {
		return kube.BuildClientCmd(kubeconfig, context).ConfigAccess().GetStartingConfig()
	}

	newKubernetesInterface = func(kubeconfig, context string) (kubernetes.Interface, error) {
		return kube.CreateClientset(kubeconfig, context)
	}
)

func createRemoteServiceAccountSecret(kubeconfig *api.Config, name string) (*v1.Secret, error) { // nolint:interfacer
	var data bytes.Buffer
	if err := latest.Codec.Encode(kubeconfig, &data); err != nil {
		return nil, err
	}
	out := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%v%v", defaultSecretPrefix, name),
			Labels: map[string]string{
				secretcontroller.MultiClusterSecretLabel: "true",
			},
		},
		StringData: map[string]string{
			name: data.String(),
		},
	}
	return out, nil
}

func createKubeconfig(caData, token []byte, context, server string) *api.Config {
	return &api.Config{
		Clusters: map[string]*api.Cluster{
			context: {
				CertificateAuthorityData: caData,
				Server:                   server,
			},
		},
		AuthInfos: map[string]*api.AuthInfo{
			context: {
				Token: string(token),
			},
		},
		Contexts: map[string]*api.Context{
			context: {
				Cluster:  context,
				AuthInfo: context,
			},
		},
		CurrentContext: context,
	}
}

var (
	errMissingRootCAKey = fmt.Errorf("no %q data found", v1.ServiceAccountRootCAKey)
	errMissingTokenKey  = fmt.Errorf("no %q data found", v1.ServiceAccountTokenKey)
)

func createRemoteSecretFromTokenAndServer(tokenSecret *v1.Secret, name, server string) (*v1.Secret, error) {
	caData, ok := tokenSecret.Data[v1.ServiceAccountRootCAKey]
	if !ok {
		return nil, errMissingRootCAKey
	}
	token, ok := tokenSecret.Data[v1.ServiceAccountTokenKey]
	if !ok {
		return nil, errMissingTokenKey
	}

	// Create a Kubeconfig to access the remote cluster using the remote service account credentials.
	kubeconfig := createKubeconfig(caData, token, name, server)

	// Encode the Kubeconfig in a secret that can be loaded by Istio to dynamically discover and access the remote cluster.
	return createRemoteServiceAccountSecret(kubeconfig, name)
}

func getServiceAccountSecretToken(kube kubernetes.Interface, saName, saNamespace string) (*v1.Secret, error) {
	serviceAccount, err := kube.CoreV1().ServiceAccounts(saNamespace).Get(saName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if len(serviceAccount.Secrets) != 1 {
		return nil, fmt.Errorf("wrong number of secrets (%v) in serviceaccount %s/%s",
			len(serviceAccount.Secrets), saNamespace, saName)
	}
	secretName := serviceAccount.Secrets[0].Name
	secretNamespace := serviceAccount.Secrets[0].Namespace
	if secretNamespace == "" {
		secretNamespace = saNamespace
	}
	return kube.CoreV1().Secrets(secretNamespace).Get(secretName, metav1.GetOptions{})
}

func getClusterServerFromKubeconfig(kubeconfig, context string) (string, error) {
	config, err := newStartingConfig(kubeconfig, context)
	if err != nil {
		return "", err
	}

	if context == "" {
		context = config.CurrentContext
	}

	configContext, ok := config.Contexts[context]
	if !ok {
		return "", fmt.Errorf("could not find cluster for context %q", context)
	}
	cluster, ok := config.Clusters[configContext.Cluster]
	if !ok {
		return "", fmt.Errorf("could not find server for context %q", context)
	}
	return cluster.Server, nil
}

const (
	outputHeader  = "# This file is autogenerated, do not edit.\n"
	outputTrailer = "---\n"
)

func writeEncodedSecret(out io.Writer, secret *v1.Secret) error {
	if _, err := fmt.Fprint(out, outputHeader); err != nil {
		return err
	}
	if err := Codec.Encode(secret, out); err != nil {
		return err
	}
	if _, err := fmt.Fprint(out, outputTrailer); err != nil {
		return err
	}
	return nil
}

type writer interface {
	io.Writer
	String() string
}

func makeOutputWriter() writer {
	return &bytes.Buffer{}
}

var makeOutputWriterTestHook = makeOutputWriter

// CreateRemoteSecret creates a remote secret with credentials of the specified service account.
// This is useful for providing a cluster access to a remote apiserver.
//
// TODO extend to use other forms of k8s auth (see https://kubernetes.io/docs/reference/access-authn-authz/authentication/#authentication-strategies)
func CreateRemoteSecret(kubeconfig, context, namespace, serviceAccountName, name string) (string, error) {
	kube, err := newKubernetesInterface(kubeconfig, context)
	if err != nil {
		return "", err
	}

	tokenSecret, err := getServiceAccountSecretToken(kube, serviceAccountName, namespace)
	if err != nil {
		return "", err
	}

	server, err := getClusterServerFromKubeconfig(kubeconfig, context)
	if err != nil {
		return "", err
	}

	remoteSecret, err := createRemoteSecretFromTokenAndServer(tokenSecret, name, server)
	if err != nil {
		return "", err
	}

	w := makeOutputWriterTestHook()
	if err := writeEncodedSecret(w, remoteSecret); err != nil {
		return "", err
	}
	return w.String(), nil
}
