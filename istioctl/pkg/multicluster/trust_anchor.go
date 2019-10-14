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
	"fmt"
	"os"

	"github.com/spf13/cobra"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd/api"

	"istio.io/istio/pkg/kube"
	secConfigMap "istio.io/istio/security/pkg/k8s/configmap"
)

// TrustAnchorOptions contains the options for creating a trust anchor.
type TrustAnchorOptions struct {
	KubeOptions
}

// NewCreateTrustAnchorCommand creates a new command for establishing trust between two clusters in a multi-cluster mesh.
func NewCreateTrustAnchorCommand() *cobra.Command {
	opts := TrustAnchorOptions{}
	c := &cobra.Command{
		Use:   "create-trust-anchor [<cluster-name>]",
		Short: "Create a configmap with an additional trusted root CA cert",
		Long: `Establish trust between two or more clusters by appending each 
cluster's public root CA cert to other cluster's list of trusted roots. This is 
useful when form a multi-cluster mesh from existing clusters with their own unique 
CAs.


`,
		Example: `
# Create a trust anchor configmap with c0's root CA cert and install it in cluster c1.
istioctl --Kubeconfig=c0.yaml x create-trust-anchor c0 \
    | kubectl -n istio-system --Kubeconfig=c1.yaml apply -f -

# Delete a trust anchor configmap that was previously installed in c1
istioctl --Kubeconfig=c0.yaml x create-trust-anchor c1 \
    | kubectl -n istio-system --Kubeconfig=c1.yaml delete -f -

`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, args []string) error {
			if err := opts.prepare(c.Flags()); err != nil {
				return err
			}
			out, err := CreateTrustAnchor(opts)
			if err != nil {
				fmt.Fprintf(c.OutOrStderr(), "%v", err)
				os.Exit(1)
			}
			fmt.Fprint(c.OutOrStdout(), out)
			return nil
		},
	}
	return c
}

type ClusterID struct {
	KubeSystemUID types.UID
	Context       string
}

func (c *ClusterID) String() string {
	return string(c.KubeSystemUID) + "-" + c.Context
}

func NewClusterID(uid types.UID, context string) *ClusterID {
	return &ClusterID{
		KubeSystemUID: uid,
		Context:       context,
	}
}

const (
	extraTrustAnchorPrefix = "extra-trust-anchor"
)

func clusterUID(client kubernetes.Interface) (types.UID, error) {
	// Use UUID of kube-system Namespace as unique identifer for cluster.
	// (see https://docs.google.com/document/d/1F__vEKeI41P7PPUCMM9PVPYY34pyrvQI5rbTJVnS5c4)
	kubeSystem, err := client.CoreV1().Namespaces().Get("kube-system", metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to auto-generate name: %v", err)
	}
	return kubeSystem.UID, nil
}

func trustAnchorName(client kubernetes.Interface) (string, error) {
	uid, err := clusterUID(client)
	if err != nil {
		return "", err
	}
	return extraTrustAnchorPrefix + "-" + string(uid), nil
}

// CreateTrustAnchor creates a configmap with the public root CA of the current cluster's Istio control plane.
// This can be used to establish trust between two or more clusters.
func CreateTrustAnchor(opt TrustAnchorOptions) (string, error) {
	client, err := kube.CreateClientset(opt.Kubeconfig, opt.Context)
	if err != nil {
		return "", err
	}
	config, err := kube.BuildClientCmd(opt.Kubeconfig, opt.Context).ConfigAccess().GetStartingConfig()
	if err != nil {
		return "", err
	}

	trustAnchor, err := createTrustAnchor(client, config, opt)
	if err != nil {
		return "", err
	}

	w := makeOutputWriterTestHook()
	if err := writeEncodedObject(w, trustAnchor); err != nil {
		return "", err
	}
	return w.String(), nil
}

func createTrustAnchor(client kubernetes.Interface, config *api.Config, opts TrustAnchorOptions) (*v1.ConfigMap, error) {
	if opts.Context == "" {
		opts.Context = config.CurrentContext
	}

	name, err := trustAnchorName(client)
	if err != nil {
		return nil, err
	}

	caConfigMap, err := client.CoreV1().ConfigMaps(opts.Namespace).Get(secConfigMap.IstioSecurityConfigMapName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	caRootCert, ok := caConfigMap.Data[secConfigMap.CATLSRootCertName]
	if !ok {
		return nil, fmt.Errorf("%q not found in configmap %v", secConfigMap.CATLSRootCertName, secConfigMap.IstioSecurityConfigMapName)
	}

	return &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Annotations: map[string]string{
				"istio.io/clusterContext": opts.Context,
			},
			Labels: map[string]string{
				"security.istio.io/extra-trust-anchors": "true", // TODO replace with type when trust anchor PR is merged.
			},
		},
		Data: map[string]string{
			name: caRootCert,
		},
	}, nil
}
