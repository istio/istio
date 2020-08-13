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

package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/spf13/cobra"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networkingv1alpha3 "istio.io/api/networking/v1alpha3"
	clientv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	"istio.io/istio/istioctl/pkg/multicluster"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/util/gogoprotomarshal"
)

var (
	tokenDuration  int64
	name           string
	serviceAccount string
	filename       string
	outputDir      string
	clusterID      string
	ports          []string
)

const (
	filePerms = os.FileMode(0744)
)

func workloadCommands() *cobra.Command {
	workloadCmd := &cobra.Command{
		Use:   "workload",
		Short: "Commands to assist in configuring and deploying workloads running on VMs and other non-k8s environments",
	}
	workloadCmd.AddCommand(groupCommand())
	workloadCmd.AddCommand(entryCommand())
	return workloadCmd
}

func groupCommand() *cobra.Command {
	groupCmd := &cobra.Command{
		Use:   "group",
		Short: "Commands dealing with WorkloadGroup resources",
	}
	groupCmd.AddCommand(createCommand())
	return groupCmd
}

func entryCommand() *cobra.Command {
	entryCmd := &cobra.Command{
		Use:   "entry",
		Short: "Commands dealing with WorkloadEntry resources",
	}
	entryCmd.AddCommand(configureCommand())
	return entryCmd
}

func createCommand() *cobra.Command {
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Creates a WorkloadGroup resource that provides a template for associated WorkloadEntries",
		Long: `Creates a WorkloadGroup resource that provides a template for associated WorkloadEntries.
The default output is serialized YAML, which can be piped into 'kubectl apply -f -' to send the artifact to the API Server.`,
		Example: "create --name foo --namespace bar --labels app=foo,bar=baz --ports grpc=3550,http=8080 --serviceAccount sa",
		Args: func(cmd *cobra.Command, args []string) error {
			if name == "" {
				return fmt.Errorf("expecting a workload name")
			}
			if namespace == "" {
				return fmt.Errorf("expecting a workload namespace")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			u := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": collections.IstioNetworkingV1Alpha3Workloadgroups.Resource().APIVersion(),
					"kind":       collections.IstioNetworkingV1Alpha3Workloadgroups.Resource().Kind(),
					"metadata": map[string]interface{}{
						"name":      name,
						"namespace": namespace,
					},
				},
			}
			spec := &networkingv1alpha3.WorkloadGroup{
				Metadata: &networkingv1alpha3.WorkloadGroup_ObjectMeta{
					Labels: convertToStringMap(labels),
				},
				Template: &networkingv1alpha3.WorkloadEntry{
					Ports:          convertToUnsignedInt32Map(ports),
					ServiceAccount: serviceAccount,
				},
			}
			wgYAML, err := generateWorkloadGroupYAML(u, spec)
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(wgYAML)
			return err
		},
	}
	createCmd.PersistentFlags().StringVar(&name, "name", "", "The name of the workload group")
	createCmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "", "The namespace that the workload instances will belong to")
	createCmd.PersistentFlags().StringSliceVarP(&labels, "labels", "l", nil, "The labels to apply to the workload instances; e.g. -l env=prod,vers=2")
	createCmd.PersistentFlags().StringSliceVarP(&ports, "ports", "p", nil, "The incoming ports exposed by the workload instance")
	createCmd.PersistentFlags().StringVarP(&serviceAccount, "serviceAccount", "s", "default", "The service identity to associate with the workload instances")
	return createCmd
}

func generateWorkloadGroupYAML(u *unstructured.Unstructured, spec *networkingv1alpha3.WorkloadGroup) ([]byte, error) {
	iSpec, err := unstructureIstioType(spec)
	if err != nil {
		return nil, err
	}
	u.Object["spec"] = iSpec

	wgYAML, err := yaml.Marshal(u.Object)
	if err != nil {
		return nil, err
	}
	return wgYAML, nil
}

func configureCommand() *cobra.Command {
	configureCmd := &cobra.Command{
		Use:   "configure",
		Short: "Generates all the required configuration files for a workload instance running on a VM or non-k8s environment",
		Long: `Generates all the required configuration files for workload instance on a VM or non-k8s environment from a WorkloadGroup artifact.
This includes a MeshConfig resource, the cluster.env file, and necessary certificates and security tokens.
Configure requires either the WorkloadGroup artifact path or its location on the API server.`,
		Example: `# configure example using a local WorkloadGroup artifact
configure -f workloadgroup.yaml -o config

# configure example using the API server
configure --name foo --namespace bar -o config`,
		Args: func(cmd *cobra.Command, args []string) error {
			if filename == "" && (name == "" || namespace == "") {
				return fmt.Errorf("expecting a WorkloadGroup artifact file or the name and namespace of an existing WorkloadGroup")
			}
			if outputDir == "" {
				return fmt.Errorf("expecting an output directory")
			}
			if clusterID == "" {
				return fmt.Errorf("expecting a cluster id")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			wg := &clientv1alpha3.WorkloadGroup{}
			kubeClient, err := kube.NewExtendedClient(kube.BuildClientCmd(kubeconfig, configContext), "")
			if err != nil {
				return err
			}
			if filename != "" {
				if err := readWorkloadGroup(filename, wg); err != nil {
					return err
				}
			} else {
				wg, err = kubeClient.Istio().NetworkingV1alpha3().WorkloadGroups(namespace).Get(context.Background(), name, metav1.GetOptions{})
				// errors if the requested workload group does not exist in the given namespace
				if err != nil {
					return fmt.Errorf("workloadgroup %s not found in namespace %s: %v", name, namespace, err)
				}
			}

			if err = createConfig(kubeClient, wg, clusterID, revision, outputDir); err != nil {
				return err
			}
			return nil
		},
	}
	configureCmd.PersistentFlags().StringVarP(&filename, "file", "f", "", "filename of the WorkloadGroup artifact")
	configureCmd.PersistentFlags().StringVar(&name, "name", "", "The name of the workload group")
	configureCmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "", "The namespace that the workload instances belongs to")
	configureCmd.PersistentFlags().StringVarP(&outputDir, "output", "o", "", "Output directory for generated files")
	configureCmd.PersistentFlags().StringVar(&clusterID, "clusterID", "", "The ID used to identify the cluster")
	configureCmd.PersistentFlags().Int64Var(&tokenDuration, "tokenDuration", 3600, "The token duration in seconds (default: 1 hour)")
	configureCmd.PersistentFlags().StringVar(&revision, "revision", "", "control plane revision (experimental)")
	return configureCmd
}

func readWorkloadGroup(filename string, wg *clientv1alpha3.WorkloadGroup) error {
	f, err := ioutil.ReadFile(filename)
	if err != nil {
		return err
	}
	if err = yaml.Unmarshal(f, wg); err != nil {
		return err
	}
	return nil
}

// Creates all the relevant config for the given workload group and cluster
func createConfig(kubeClient kube.ExtendedClient, wg *clientv1alpha3.WorkloadGroup, clusterID, revision, outputDir string) error {
	if err := validateWorkloadGroup(wg); err != nil {
		return err
	}
	if err := os.MkdirAll(outputDir, filePerms); err != nil {
		return err
	}
	if err := createClusterEnv(wg, outputDir); err != nil {
		return err
	}
	if err := createCertsTokens(kubeClient, wg, outputDir); err != nil {
		return err
	}
	meshConfig, err := createMeshConfig(kubeClient, wg, clusterID, revision, outputDir)
	if err != nil {
		return err
	}
	if err := createHosts(kubeClient, wg, meshConfig, outputDir); err != nil {
		return err
	}
	return nil
}

// Used to populate nil WorkloadGroup structs (metadata and template)
// Additionally checks the invalid fields in template
func validateWorkloadGroup(wg *clientv1alpha3.WorkloadGroup) error {
	if wg.Spec.Metadata == nil {
		wg.Spec.Metadata = &networkingv1alpha3.WorkloadGroup_ObjectMeta{}
	}
	if wg.Spec.Template == nil {
		wg.Spec.Template = &networkingv1alpha3.WorkloadEntry{}
	}
	// error checks and warnings
	template := wg.Spec.Template
	if template.Address != "" {
		return fmt.Errorf("address %s should not be set in the WorkloadEntry template", template.Address)
	}
	if len(template.Labels) != 0 {
		fmt.Printf("Labels should be set in the metadata. The following WorkloadEntry labels will override metadata labels: %s", template.Labels)
	}
	// default service account for an empty field is "default"
	if template.ServiceAccount == "" {
		template.ServiceAccount = "default"
	}
	return nil
}

// Write cluster.env into the given directory
func createClusterEnv(wg *clientv1alpha3.WorkloadGroup, dir string) error {
	we := wg.Spec.Template
	ports := []string{}
	for _, v := range we.Ports {
		ports = append(ports, fmt.Sprint(v))
	}
	// respect the inbound port annotation and capture all traffic if no inbound ports are set
	portBehavior := "*"
	if len(ports) > 0 {
		portBehavior = strings.Join(ports, ",")
	}

	// default attributes and service name, namespace, ports, service account, service CIDR
	clusterEnv := map[string]string{
		"ISTIO_CP_AUTH":       "MUTUAL_TLS",
		"ISTIO_INBOUND_PORTS": portBehavior,
		"ISTIO_NAMESPACE":     wg.Namespace,
		"ISTIO_PILOT_PORT":    "15012",
		"ISTIO_SERVICE":       fmt.Sprintf("%s.%s", wg.Name, wg.Namespace),
		"ISTIO_SERVICE_CIDR":  "*",
		"SERVICE_ACCOUNT":     we.ServiceAccount,
	}
	return ioutil.WriteFile(filepath.Join(dir, "cluster.env"), []byte(mapToString(clusterEnv)), filePerms)
}

// Get and store the needed certificate and token. The certificate comes from the CA root cert, and
// the token is generated by kubectl under the workload group's namespace and service account
// TODO: Make the following accurate when using the Kubernetes certificate signer
func createCertsTokens(kubeClient kube.ExtendedClient, wg *clientv1alpha3.WorkloadGroup, dir string) error {
	rootCert, err := kubeClient.CoreV1().ConfigMaps(wg.Namespace).Get(context.Background(), controller.CACertNamespaceConfigMap, metav1.GetOptions{})
	// errors if the requested configmap does not exist in the given namespace
	if err != nil {
		return fmt.Errorf("configmap %s was not found in namespace %s: %v", controller.CACertNamespaceConfigMap, wg.Namespace, err)
	}
	if err = ioutil.WriteFile(filepath.Join(dir, "root-cert"), []byte(rootCert.Data[constants.CACertNamespaceConfigMapDataName]), filePerms); err != nil {
		return err
	}

	token := &authenticationv1.TokenRequest{
		Spec: authenticationv1.TokenRequestSpec{
			Audiences:         []string{"istio-ca"},
			ExpirationSeconds: &tokenDuration,
		},
	}
	serviceAccount := wg.Spec.Template.ServiceAccount
	tokenReq, err := kubeClient.CoreV1().ServiceAccounts(wg.Namespace).CreateToken(context.Background(), serviceAccount, token, metav1.CreateOptions{})
	// errors if the token could not be created with the given service account in the given namespace
	if err != nil {
		return fmt.Errorf("could not create a token under service account %s in namespace %s: %v", serviceAccount, wg.Namespace, err)
	}
	if err := ioutil.WriteFile(filepath.Join(dir, "istio-token"), []byte(tokenReq.Status.Token), filePerms); err != nil {
		return err
	}
	return nil
}

// TODO: Support the proxy.istio.io/config annotation
func createMeshConfig(kubeClient kube.ExtendedClient, wg *clientv1alpha3.WorkloadGroup, clusterID, revision, dir string) (*meshconfig.MeshConfig, error) {
	istioCM := "istio"
	// Case with multiple control planes
	if revision != "" {
		istioCM = fmt.Sprintf("%s-%s", istioCM, revision)
	}
	istio, err := kubeClient.CoreV1().ConfigMaps(istioNamespace).Get(context.Background(), istioCM, metav1.GetOptions{})
	// errors if the requested configmap does not exist in the given namespace
	if err != nil {
		return nil, fmt.Errorf("configmap %s was not found in namespace %s: %v", istioCM, istioNamespace, err)
	}
	meshConfig, err := mesh.ApplyMeshConfigDefaults(istio.Data[configMapKey])
	if err != nil {
		return nil, err
	}

	labels := map[string]string{}
	for k, v := range wg.Spec.Metadata.Labels {
		labels[k] = v
	}
	// case where a user provided custom workload group has labels in the workload entry template field
	for k, v := range wg.Spec.Template.Labels {
		labels[k] = v
	}
	we := wg.Spec.Template
	md := meshConfig.DefaultConfig.ProxyMetadata
	md["CANONICAL_SERVICE"], md["CANONICAL_REVISION"] = inject.ExtractCanonicalServiceLabels(labels, wg.Name)
	md["DNS_AGENT"] = ""
	md["POD_NAMESPACE"] = wg.Namespace
	md["SERVICE_ACCOUNT"] = we.ServiceAccount
	md["TRUST_DOMAIN"] = meshConfig.TrustDomain

	md["ISTIO_META_CLUSTER_ID"] = clusterID
	md["ISTIO_META_MESH_ID"] = meshConfig.DefaultConfig.MeshId
	md["ISTIO_META_NETWORK"] = we.Network
	if portsJSON, err := json.Marshal(we.Ports); err == nil {
		md["ISTIO_META_POD_PORTS"] = string(portsJSON)
	}
	md["ISTIO_META_WORKLOAD_NAME"] = wg.Name
	labels["service.istio.io/canonical-name"] = md["CANONICAL_SERVICE"]
	labels["service.istio.io/canonical-version"] = md["CANONICAL_REVISION"]
	if labelsJSON, err := json.Marshal(labels); err == nil {
		md["ISTIO_METAJSON_LABELS"] = string(labelsJSON)
	}

	meshYAML, err := gogoprotomarshal.ToYAML(meshConfig)
	if err != nil {
		return nil, err
	}
	return meshConfig, ioutil.WriteFile(filepath.Join(dir, "mesh"), []byte(meshYAML), filePerms)
}

// Retrieves the external IP of the ingress-gateway for the hosts file additions
func createHosts(kubeClient kube.ExtendedClient, wg *clientv1alpha3.WorkloadGroup, meshConfig *meshconfig.MeshConfig, dir string) error {
	ingress, err := kubeClient.CoreV1().Services(istioNamespace).Get(context.Background(), multicluster.IstioIngressGatewayServiceName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("service %s was not found in namespace %s: %v", multicluster.IstioIngressGatewayServiceName, istioNamespace, err)
	}
	serviceIP := ingress.Status.LoadBalancer.Ingress[0].IP

	var hosts []string
	hosts = append(hosts, fmt.Sprintf("%s istiod.%s.svc\n", serviceIP, istioNamespace),
		fmt.Sprintf("1.1.1.1 pod.%s.svc.%s\n", wg.Namespace, meshConfig.DefaultConfig.MeshId),
		fmt.Sprintf("1.1.1.1 vm.%s.svc.%s\n", wg.Namespace, meshConfig.DefaultConfig.MeshId))
	return ioutil.WriteFile(filepath.Join(dir, "hosts"), []byte(strings.Join(hosts, "")), filePerms)
}

// Returns a map with each k,v entry on a new line
func mapToString(m map[string]string) string {
	var b strings.Builder
	for k, v := range m {
		fmt.Fprintf(&b, "%s=%s\n", k, v)
	}
	return b.String()
}
