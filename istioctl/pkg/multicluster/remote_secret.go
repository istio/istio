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

type Options struct {
	ServiceAccountName string

	// inherited from root command
	Namespace  string
	Kubeconfig string
	Context    string
}

var (
	defaultSecretPrefix       = "istio-remote-secret-"
	DefaultServiceAccountName = "istio-pilot-service-account"
)

// NewCreateRemoteSecretCommand creates a new command for joining two clusters togeather in a multi-cluster mesh.
func NewCreateRemoteSecretCommand(kubeconfig, namespace, context *string) *cobra.Command {
	o := Options{
		ServiceAccountName: DefaultServiceAccountName,
	}

	c := &cobra.Command{
		Use:   "create-remote-secret [OPTIONS]",
		Short: "Create a secret with credentials to allow Istio to access remote Kubernetes apiservers",
		Example: `
# Create a secret to access cluster c0's apiserver and install it in cluster c1.
istioctl --kubeconfig=c0.yaml x create-remote-secrets \
    | kubectl -n istio-system --kubeconfig=c1.yaml apply -f -

# Delete a secret that was previously installed in c1
istioctl --kubeconfig=c0.yaml x create-remote-secrets \
    | kubectl -n istio-system --kubeconfig=c1.yaml delete -f -

`,
		Args: cobra.NoArgs(),
		RunE: func(c *cobra.Command, args []string) error {
			o.Context = *context
			o.Kubeconfig = *kubeconfig
			o.Namespace = *namespace

			out, err := CreateRemoteSecrets(o)
			if err != nil {
				fmt.Fprintf(c.OutOrStderr(), "%v", err)
				os.Exit(1)
			}
			fmt.Fprint(c.OutOrStdout(), out)
			return nil
		},
	}

	flags := c.PersistentFlags()
	flags.StringVar(&o.ServiceAccountName, "service-account-name", o.ServiceAccountName,
		"name of service account in the remote cluster")

	return c
}

// hooks for testing
var (
	newStartingConfig = func(kubeconfig string) (*api.Config, error) {
		return kube.BuildClientCmd(kubeconfig, "").ConfigAccess().GetStartingConfig()
	}

	newKubernetesInterface = func(kubeconfig, context string) (kubernetes.Interface, error) {
		return kube.CreateClientset(kubeconfig, context)
	}
)

const (
	caDataSecretKey = "ca.crt"
	tokenSecretKey  = "token"
)

func createRemoteKubeconfig(in *v1.Secret, config *api.Config, context string) (*api.Config, error) {
	contextInfo, ok := config.Contexts[context]
	if !ok {
		return nil, fmt.Errorf("%q context not found in Kubeconfig(s)", context)
	}

	clusterInfo, ok := config.Clusters[contextInfo.Cluster]
	if !ok {
		return nil, fmt.Errorf("%q cluster not found in Kubeconfig(s) for context %q", contextInfo.Cluster, context)
	}

	caData, ok := in.Data[caDataSecretKey]
	if !ok {
		return nil, fmt.Errorf("no %q data found in secret %s/%s for context %q",
			caDataSecretKey, in.Namespace, in.Name, context)
	}

	token, ok := in.Data[tokenSecretKey]
	if !ok {
		return nil, fmt.Errorf("no %q data found in secret %s/%s for context %q",
			tokenSecretKey, in.Namespace, in.Name, context)
	}

	kubeconfig := &api.Config{
		Clusters: map[string]*api.Cluster{
			contextInfo.Cluster: {
				CertificateAuthorityData: caData,
				Server:                   clusterInfo.Server,
			},
		},
		AuthInfos: map[string]*api.AuthInfo{
			contextInfo.Cluster: {
				Token: string(token),
			},
		},
		Contexts: map[string]*api.Context{
			contextInfo.Cluster: {
				Cluster:  contextInfo.Cluster,
				AuthInfo: contextInfo.Cluster,
			},
		},
		CurrentContext: contextInfo.Cluster,
	}
	return kubeconfig, nil
}

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

// TODO extend to use other forms of k8s auth (see https://kubernetes.io/docs/reference/access-authn-authz/authentication/#authentication-strategies)
func createRemoteSecret(out *bytes.Buffer, config *api.Config, name, context string, o *Options) error {
	if _, err := out.WriteString(fmt.Sprintf("# Remote credentials for cluster context %q\n", context)); err != nil {
		return err
	}

	kube, err := newKubernetesInterface(o.Kubeconfig, context)
	if err != nil {
		return err
	}

	// Get the remote service-account-token secret
	serviceAccount, err := kube.CoreV1().ServiceAccounts(o.Namespace).Get(o.ServiceAccountName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get serviceaccount %s/%s in cluster %v", o.Namespace, o.ServiceAccountName, context)
	}
	if len(serviceAccount.Secrets) != 1 {
		return fmt.Errorf("wrong number of secrets (%v) in serviceaccount %s/%s in cluster %v",
			len(serviceAccount.Secrets), o.Namespace, name, context)
	}
	secretName := serviceAccount.Secrets[0].Name
	secretNamespace := serviceAccount.Secrets[0].Namespace
	if secretNamespace == "" {
		secretNamespace = o.Namespace
	}
	saSecret, err := kube.CoreV1().Secrets(secretNamespace).Get(secretName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get secret %s/%s in cluster %v", secretNamespace, secretName, context)
	}

	// Create a Kubeconfig to access the remote cluster using the remote service account credentials.
	kubeconfig, err := createRemoteKubeconfig(saSecret, config, context)
	if err != nil {
		return err
	}

	// Encode the Kubeconfig in a secret that can be loaded by Istio to dynamically discover and access the remote cluster.
	mcSecret, err := createRemoteServiceAccountSecret(kubeconfig, name)
	if err != nil {
		return err
	}

	// Output the secret in a multi-document YAML friendly format.
	if err := Codec.Encode(mcSecret, out); err != nil {
		return err
	}
	_, err = out.WriteString("---\n")
	return err
}

const outputHeader = "# This file is autogenerated, do not edit.\n#\n"

// CreateRemoteSecrets creates a remote secret with credentials of the specified service account.
// This is useful for providing a cluster access to a remote apiserver.
func CreateRemoteSecrets(o Options) (string, error) {
	config, err := newStartingConfig(o.Kubeconfig)
	if err != nil {
		return "", err
	}

	var out bytes.Buffer
	if _, err := out.WriteString(outputHeader); err != nil {
		return "", err
	}
	if err := createRemoteSecret(&out, config, o.Context, o.Context, &o); err != nil {
		return "", err
	}
	return out.String(), nil
}
