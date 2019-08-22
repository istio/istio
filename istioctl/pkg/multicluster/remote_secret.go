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
	"encoding/base64"
	"errors"
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

type options struct {
	secretPrefix       string
	serviceAccountName string
	secretLabels       map[string]string

	// inherited from root command
	namespace  string
	kubeconfig string

	args []string
}

var (
	defaultSecretPrefix = "istio-pilot-remote-secret-"
	defaultSecretlabels = map[string]string{
		"istio/multiCluster":            "true", // legacy label
		"istio.io/remote-multi-cluster": "true",
	}
	defaultServiceAccountName = "istio-pilot-service-account"
)

// NewCreatePilotRemoteSecretCommand creates a new command for joining two clusters togeather in a multi-cluster mesh.
func NewCreatePilotRemoteSecretCommand(kubeconfig, namespace *string) *cobra.Command {
	o := options{
		secretPrefix:       defaultSecretPrefix,
		secretLabels:       defaultSecretlabels,
		serviceAccountName: defaultServiceAccountName,
	}

	c := &cobra.Command{
		Use:   "create-pilot-remote-secrets <cluster_context>... [OPTIONS]",
		Short: "Create secret(s) for Pilot to discovery and join remote k8s service registries",
		Example: `
# Create a secret for remote cluster c1 and install it in cluster c0.
istioctl x create-pilot-remote-secrets c1 > c1-secret.kubeconfigSecretYaml
kubectl --context=c0 -n istio-system apply -f c1-secret.kubeconfigSecretYaml

# Create and directly install secrets for multiple clusters. These 
# commands 'join' clusters c0, c1, and c2 together.
istioctl x create-pilot-remote-secrets c1 c2 \
    | kubectl --context=c0 apply -f - --prune -l istio/multiCluster=true
istioctl x create-pilot-remote-secrets c0 c2 \
    | kubectl --context=c1 apply -f - --prune -l istio/multiCluster=true
istioctl x create-pilot-remote-secrets c0 c1 \
    | kubectl --context=c2 apply -f - --prune -l istio/multiCluster=true

`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			o.args = args
			o.kubeconfig = *kubeconfig
			o.namespace = *namespace

			out, err := createPilotRemoteSecrets(o)
			if err != nil {
				fmt.Fprintf(c.OutOrStderr(), "%v", err)
				os.Exit(1)
			}
			fmt.Print(c.OutOrStdout(), out)
			return nil
		},
	}

	flags := c.PersistentFlags()
	flags.StringVar(&o.serviceAccountName, "service-account-name", o.serviceAccountName,
		"name of Pilot service account in the remote cluster(s)")

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

func createRemotePilotKubeconfig(in *v1.Secret, config *api.Config, context string) (*api.Config, error) {
	contextInfo, ok := config.Contexts[context]
	if !ok {
		return nil, fmt.Errorf("%q context not found in kubeconfig(s)", context)
	}

	clusterInfo, ok := config.Clusters[contextInfo.Cluster]
	if !ok {
		return nil, fmt.Errorf("%q cluster not found in kubeconfig(s) for context %q", contextInfo.Cluster, context)
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
				Token: base64.StdEncoding.EncodeToString(token),
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

func createRemotePilotServiceAccountSecret(kubeconfig *api.Config, name, prefix string, labels map[string]string) (*v1.Secret, error) { // nolint:interfacer
	var data bytes.Buffer
	if err := latest.Codec.Encode(kubeconfig, &data); err != nil {
		return nil, err
	}
	out := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:   fmt.Sprintf("%v%v", prefix, name),
			Labels: labels,
		},
		StringData: map[string]string{
			name: data.String(),
		},
	}
	return out, nil
}

func createPilotRemoteSecret(out *bytes.Buffer, config *api.Config, name, context string, o *options) error {
	if _, err := out.WriteString(fmt.Sprintf("# Remote pilot credentials for cluster context %q\n", context)); err != nil {
		return err
	}

	kube, err := newKubernetesInterface(o.kubeconfig, context)
	if err != nil {
		return err
	}

	// Get the remote pilot's service-account-token secret
	serviceAccount, err := kube.CoreV1().ServiceAccounts(o.namespace).Get(o.serviceAccountName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get serviceaccount %s/%s in cluster %v", o.namespace, name, context)
	}
	if len(serviceAccount.Secrets) != 1 {
		return fmt.Errorf("wrong number of secrets (%v) in serviceaccount %s/%s in cluster %v",
			len(serviceAccount.Secrets), o.namespace, name, context)
	}
	secretName := serviceAccount.Secrets[0].Name
	secretNamespace := serviceAccount.Secrets[0].Namespace
	if secretNamespace == "" {
		secretNamespace = o.namespace
	}
	saSecret, err := kube.CoreV1().Secrets(secretNamespace).Get(secretName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get secret %s/%s in cluster %v", secretNamespace, secretName, context)
	}

	// Create a kubeconfig to access the remote cluster using the remote pilot's service account credentials.
	kubeconfig, err := createRemotePilotKubeconfig(saSecret, config, context)
	if err != nil {
		return err
	}

	// Encode the kubeconfig in a secret that can be loaded by Pilot to dynamically discover and access the remote cluster.
	mcSecret, err := createRemotePilotServiceAccountSecret(kubeconfig, name, o.secretPrefix, o.secretLabels)
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

func createPilotRemoteSecrets(o options) (string, error) {
	if len(o.args) < 1 {
		return "", errors.New("no remote cluster contexts specified")
	}

	config, err := newStartingConfig(o.kubeconfig)
	if err != nil {
		return "", err
	}

	var out bytes.Buffer
	if _, err := out.WriteString(outputHeader); err != nil {
		return "", err
	}
	for _, context := range o.args {
		if err := createPilotRemoteSecret(&out, config, context, context, &o); err != nil {
			return "", err
		}
	}
	return out.String(), nil
}
