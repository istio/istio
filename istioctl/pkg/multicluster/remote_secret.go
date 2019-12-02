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
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apimachinery/pkg/runtime/serializer/versioning"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth" // to avoid 'No Auth Provider found for name "gcp"'
	"k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/client-go/tools/clientcmd/api/latest"

	"istio.io/istio/pkg/kube/secretcontroller"
)

var (
	codec  runtime.Codec
	scheme *runtime.Scheme
)

func init() {
	scheme = runtime.NewScheme()
	utilruntime.Must(v1.AddToScheme(scheme))
	opt := json.SerializerOptions{true, false, false}
	yamlSerializer := json.NewSerializerWithOptions(json.DefaultMetaFactory, scheme, scheme, opt)
	codec = versioning.NewDefaultingCodecForScheme(
		scheme,
		yamlSerializer,
		yamlSerializer,
		v1.SchemeGroupVersion,
		runtime.InternalGroupVersioner,
	)
}

const (
	// default service account to use for remote cluster access.
	DefaultServiceAccountName = "istio-reader-service-account"

	remoteSecretPrefix = "istio-remote-secret-"
)

func remoteSecretNameFromUID(uid types.UID) string {
	return remoteSecretPrefix + string(uid)
}

func uidFromRemoteSecretName(name string) types.UID {
	return types.UID(strings.TrimPrefix(name, remoteSecretPrefix))
}

// NewCreateRemoteSecretCommand creates a new command for joining two contexts
// together in a multi-cluster mesh.
func NewCreateRemoteSecretCommand() *cobra.Command {
	opts := RemoteSecretOptions{
		ServiceAccountName: DefaultServiceAccountName,
		AuthType:           RemoteSecretAuthTypeBearerToken,
		AuthPluginConfig:   make(map[string]string),
	}
	c := &cobra.Command{
		Use:   "create-remote-secret <cluster-name>",
		Short: "Create a secret with credentials to allow Istio to access remote Kubernetes apiservers",
		Example: `
# Create a secret to access cluster c0's apiserver and install it in cluster c1.
istioctl --Kubeconfig=c0.yaml x create-remote-secret \
    | kubectl -n istio-system --Kubeconfig=c1.yaml apply -f -

# Delete a secret that was previously installed in c1
istioctl --Kubeconfig=c0.yaml x create-remote-secret \
    | kubectl -n istio-system --Kubeconfig=c1.yaml delete -f -

# Create a secret  access a remote cluster with an auth plugin
istioctl --Kubeconfig=c0.yaml x create-remote-secret --auth-type=plugin --auth-plugin-name=gcp \
    | kubectl -n istio-system --Kubeconfig=c1.yaml apply -f -
`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, args []string) error {
			opts.prepare(c.Flags())
			env, err := NewEnvironmentFromCobra(opts.Kubeconfig, opts.Context, c)
			if err != nil {
				return err
			}
			out, err := CreateRemoteSecret(opts, env)
			if err != nil {
				fmt.Fprintf(c.OutOrStderr(), "error: %v\n", err)
				os.Exit(1)
			}
			fmt.Fprint(c.OutOrStdout(), out)
			return nil
		},
	}
	opts.addFlags(c.PersistentFlags())
	return c
}

func createRemoteServiceAccountSecret(kubeconfig *api.Config, uid types.UID, context string) (*v1.Secret, error) { // nolint:interfacer
	var data bytes.Buffer
	if err := latest.Codec.Encode(kubeconfig, &data); err != nil {
		return nil, err
	}
	out := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: remoteSecretNameFromUID(uid),
			Annotations: map[string]string{
				clusterContextAnnotationKey: context,
			},
			Labels: map[string]string{
				secretcontroller.MultiClusterSecretLabel: "true",
			},
		},
		Data: map[string][]byte{
			string(uid): data.Bytes(),
		},
	}
	return out, nil
}

func createBaseKubeconfig(caData []byte, context, server string) *api.Config {
	return &api.Config{
		Clusters: map[string]*api.Cluster{
			context: {
				CertificateAuthorityData: caData,
				Server:                   server,
			},
		},
		AuthInfos: map[string]*api.AuthInfo{},
		Contexts: map[string]*api.Context{
			context: {
				Cluster:  context,
				AuthInfo: context,
			},
		},
		CurrentContext: context,
	}
}

func createBearerTokenKubeconfig(caData, token []byte, context, server string) *api.Config {
	c := createBaseKubeconfig(caData, context, server)
	c.AuthInfos[context] = &api.AuthInfo{
		Token: string(token),
	}
	return c
}

func createPluginKubeconfig(caData []byte, context, server string, authProviderConfig *api.AuthProviderConfig) *api.Config {
	c := createBaseKubeconfig(caData, context, server)
	c.AuthInfos[context] = &api.AuthInfo{
		AuthProvider: authProviderConfig,
	}
	return c
}

func createRemoteSecretFromPlugin(
	tokenSecret *v1.Secret,
	context, server string,
	uid types.UID,
	authProviderConfig *api.AuthProviderConfig,
) (*v1.Secret, error) {
	caData, ok := tokenSecret.Data[v1.ServiceAccountRootCAKey]
	if !ok {
		return nil, errMissingRootCAKey
	}

	// Create a Kubeconfig to access the remote cluster using the auth provider plugin.
	kubeconfig := createPluginKubeconfig(caData, context, server, authProviderConfig)

	// Encode the Kubeconfig in a secret that can be loaded by Istio to dynamically discover and access the remote cluster.
	return createRemoteServiceAccountSecret(kubeconfig, uid, context)
}

var (
	errMissingRootCAKey = fmt.Errorf("no %q data found", v1.ServiceAccountRootCAKey)
	errMissingTokenKey  = fmt.Errorf("no %q data found", v1.ServiceAccountTokenKey)
)

func createRemoteSecretFromTokenAndServer(tokenSecret *v1.Secret, uid types.UID, context, server string) (*v1.Secret, error) {
	caData, ok := tokenSecret.Data[v1.ServiceAccountRootCAKey]
	if !ok {
		return nil, errMissingRootCAKey
	}
	token, ok := tokenSecret.Data[v1.ServiceAccountTokenKey]
	if !ok {
		return nil, errMissingTokenKey
	}

	// Create a Kubeconfig to access the remote cluster using the remote service account credentials.
	kubeconfig := createBearerTokenKubeconfig(caData, token, context, server)

	// Encode the Kubeconfig in a secret that can be loaded by Istio to dynamically discover and access the remote cluster.
	return createRemoteServiceAccountSecret(kubeconfig, uid, context)
}

func getServiceAccountSecretToken(kube kubernetes.Interface, saName, saNamespace string) (*v1.Secret, error) {
	serviceAccount, err := kube.CoreV1().ServiceAccounts(saNamespace).Get(saName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("could not find %v in namespace %v: %v", saName, saNamespace, err)
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

func getCurrentContextAndClusterServerFromKubeconfig(context string, config *api.Config) (string, string, error) {
	if context == "" {
		context = config.CurrentContext
	}

	configContext, ok := config.Contexts[context]
	if !ok {
		return "", "", fmt.Errorf("could not find cluster for context %q", context)
	}
	cluster, ok := config.Clusters[configContext.Cluster]
	if !ok {
		return "", "", fmt.Errorf("could not find server for context %q", context)
	}
	return context, cluster.Server, nil
}

const (
	outputHeader  = "# This file is autogenerated, do not edit.\n"
	outputTrailer = "---\n"
)

func writeEncodedObject(out io.Writer, in runtime.Object) error {
	if _, err := fmt.Fprint(out, outputHeader); err != nil {
		return err
	}
	if err := codec.Encode(in, out); err != nil {
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

// RemoteSecretAuthType is a strongly typed authentication type suitable for use with pflags.Var().
type RemoteSecretAuthType string

var _ pflag.Value = (*RemoteSecretAuthType)(nil)

func (at *RemoteSecretAuthType) String() string { return string(*at) }
func (at *RemoteSecretAuthType) Type() string   { return "RemoteSecretAuthType" }
func (at *RemoteSecretAuthType) Set(in string) error {
	*at = RemoteSecretAuthType(in)
	return nil
}

const (
	// Use a bearer token for authentication to the remote kubernetes cluster.
	RemoteSecretAuthTypeBearerToken RemoteSecretAuthType = "bearer-token"

	// User a custom custom authentication plugin for the remote kubernetes cluster.
	RemoteSecretAuthTypePlugin RemoteSecretAuthType = "plugin"
)

// RemoteSecretOptions contains the options for creating a remote secret.
type RemoteSecretOptions struct {
	KubeOptions

	// Create a secret with this service account's credentials.
	ServiceAccountName string

	// Authentication method for the remote Kubernetes cluster.
	AuthType RemoteSecretAuthType

	// Authenticator plugin configuration
	AuthPluginName   string
	AuthPluginConfig map[string]string
}

func (o *RemoteSecretOptions) addFlags(flagset *pflag.FlagSet) {
	flagset.StringVar(&o.ServiceAccountName, "service-account", o.ServiceAccountName,
		"create a secret with this service account's credentials.")
	var supportedAuthType []string
	for _, at := range []RemoteSecretAuthType{RemoteSecretAuthTypeBearerToken, RemoteSecretAuthTypePlugin} {
		supportedAuthType = append(supportedAuthType, string(at))
	}
	flagset.Var(&o.AuthType, "auth-type",
		fmt.Sprintf("type of authentication to use. supported values = %v", supportedAuthType))
	flagset.StringVar(&o.AuthPluginName, "auth-plugin-name", o.AuthPluginName,
		fmt.Sprintf("authenticator plug-in name. --auth-type=%v must be set with this option",
			RemoteSecretAuthTypePlugin))
	flagset.StringToString("auth-plugin-config", o.AuthPluginConfig,
		fmt.Sprintf("authenticator plug-in configuration. --auth-type=%v must be set with this option",
			RemoteSecretAuthTypePlugin))
}

func createRemoteSecret(opt RemoteSecretOptions, client kubernetes.Interface, env Environment) (*v1.Secret, error) {
	uid, err := clusterUID(client)
	if err != nil {
		return nil, err
	}

	tokenSecret, err := getServiceAccountSecretToken(client, opt.ServiceAccountName, opt.Namespace)
	if err != nil {
		return nil, err
	}

	currentContext, server, err := getCurrentContextAndClusterServerFromKubeconfig(opt.Context, env.GetConfig())
	if err != nil {
		return nil, err
	}

	var remoteSecret *v1.Secret
	switch opt.AuthType {
	case RemoteSecretAuthTypeBearerToken:
		remoteSecret, err = createRemoteSecretFromTokenAndServer(tokenSecret, uid, currentContext, server)
	case RemoteSecretAuthTypePlugin:
		authProviderConfig := &api.AuthProviderConfig{
			Name:   opt.AuthPluginName,
			Config: opt.AuthPluginConfig,
		}
		remoteSecret, err = createRemoteSecretFromPlugin(tokenSecret, currentContext, server, uid, authProviderConfig)
	default:
		err = fmt.Errorf("unsupported authentication type: %v", opt.AuthType)
	}
	if err != nil {
		return nil, err
	}
	return remoteSecret, nil
}

// CreateRemoteSecret creates a remote secret with credentials of the specified service account.
// This is useful for providing a cluster access to a remote apiserver.
func CreateRemoteSecret(opt RemoteSecretOptions, env Environment) (string, error) {
	client, err := env.CreateClientSet(opt.Context)
	if err != nil {
		return "", err
	}

	remoteSecret, err := createRemoteSecret(opt, client, env)
	if err != nil {
		return "", err
	}

	// convert any binary data to the string equivalent for easier review. The
	// kube-apiserver will convert this to binary before it persists it to storage.
	remoteSecret.StringData = make(map[string]string, len(remoteSecret.Data))
	for k, v := range remoteSecret.Data {
		remoteSecret.StringData[k] = string(v)
	}
	remoteSecret.Data = nil

	w := makeOutputWriterTestHook()
	if err := writeEncodedObject(w, remoteSecret); err != nil {
		return "", err
	}
	return w.String(), nil
}
