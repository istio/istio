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
	"archive/tar"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/spf13/cobra"

	networkingv1alpha3 "istio.io/api/networking/v1alpha3"
	clientv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	networkingclientv1alpha3 "istio.io/client-go/pkg/clientset/versioned/typed/networking/v1alpha3"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/util/gogoprotomarshal"

	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
)

var (
	tokenDuration  int64
	name           string
	serviceAccount string
	filename       string
	outputName     string
	clusterID      string
	ports          []string
)

const (
	tempPerms = os.FileMode(0644)
)

func workloadCommands() *cobra.Command {
	workloadCmd := &cobra.Command{
		Use:   "workload",
		Short: "Commands to assist in configuring and deploying workloads",
	}
	workloadCmd.AddCommand(groupCommand())
	workloadCmd.AddCommand(entryCommand())
	return workloadCmd
}

func groupCommand() *cobra.Command {
	groupCmd := &cobra.Command{
		Use:   "group",
		Short: "Commands dealing with workload groups",
	}
	groupCmd.AddCommand(createCommand())
	return groupCmd
}

func entryCommand() *cobra.Command {
	entryCmd := &cobra.Command{
		Use:   "entry",
		Short: "Commands dealing with workload entries",
	}
	entryCmd.AddCommand(configureCommand())
	return entryCmd
}

func createCommand() *cobra.Command {
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Creates a WorkloadGroup YAML artifact representing workload instances",
		Long: `Creates a WorkloadGroup API YAML artifact to send to the Kubernetes API server.
To send the generated artifact to Kubernetes, run kubectl apply -f workloadgroup.yaml`,
		Example: "create --name foo --namespace bar --labels app=foo,bar=baz --ports grpc=3550,http=8080 --serviceAccount sa",
		Args: func(cmd *cobra.Command, args []string) error {
			if name == "" {
				return fmt.Errorf("expecting a service name")
			}
			if namespace == "" {
				return fmt.Errorf("expecting a service namespace")
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
				Template: &networkingv1alpha3.WorkloadEntryTemplate{
					Metadata: &metav1.ObjectMeta{
						Labels: convertToStringMap(labels),
					},
					Spec: &networkingv1alpha3.WorkloadEntry{
						Ports:          convertToUnsignedInt32Map(ports),
						ServiceAccount: serviceAccount,
					},
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
	// remove creationTimestamp left behind by ObjectMeta
	delete(iSpec["template"].(map[string]interface{})["metadata"].(map[string]interface{}), "creationTimestamp")

	wgYAML, err := yaml.Marshal(u.Object)
	if err != nil {
		return nil, err
	}
	return wgYAML, nil
}

func configureCommand() *cobra.Command {
	configureCmd := &cobra.Command{
		Use:   "configure",
		Short: "Generates all the required configuration files for a workload deployment",
		Long: `Generates all the required configuration files for workload deployment from a WorkloadGroup artifact. 
This includes a MeshConfig resource, the cluster.env file, and necessary certificates and security tokens.`,
		Example: "configure -f workloadgroup.yaml -o config",
		Args: func(cmd *cobra.Command, args []string) error {
			if filename == "" && (name == "" || namespace == "") {
				return fmt.Errorf("expecting a WorkloadGroup artifact file or both the workload name and namespace")
			}
			if outputName == "" {
				return fmt.Errorf("expecting an output filename")
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
				networkingClient, err := networkingclientv1alpha3.NewForConfig(injectWorkloadGroup(kubeClient.RESTConfig()))
				if err != nil {
					return err
				}
				wg, err = networkingClient.WorkloadGroups(namespace).Get(context.Background(), name, metav1.GetOptions{})
				if err != nil {
					return err
				}
			}

			if err = createConfig(kubeClient, wg, clusterID, revision, outputName); err != nil {
				return err
			}
			return nil
		},
	}
	configureCmd.PersistentFlags().StringVarP(&filename, "file", "f", "", "filename of the WorkloadGroup artifact")
	configureCmd.PersistentFlags().StringVar(&name, "name", "", "The name of the workload group")
	configureCmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "", "The namespace that the workload instances belongs to")
	configureCmd.PersistentFlags().StringVarP(&outputName, "output", "o", "", "Name of the tarball to be created")
	configureCmd.PersistentFlags().StringVar(&clusterID, "clusterID", "", "The ID used to identify the cluster")
	configureCmd.PersistentFlags().Int64Var(&tokenDuration, "tokenDuration", 3600, "The token duration in seconds (default: 1 hour)")
	configureCmd.PersistentFlags().StringVar(&revision, "revision", "", "control plane revision (experimental)")
	return configureCmd
}

// return a modified REST config where WorkloadGroup is a recognized Kubernetes resource
func injectWorkloadGroup(config *rest.Config) *rest.Config {
	schemeGroupVersion := schema.GroupVersion{
		Group:   collections.IstioNetworkingV1Alpha3Workloadgroups.Resource().Group(),
		Version: collections.IstioNetworkingV1Alpha3Workloadgroups.Resource().Version(),
	}
	scheme := runtime.NewScheme()
	scheme.AddKnownTypes(schemeGroupVersion, &clientv1alpha3.WorkloadGroup{})
	config.GroupVersion = &schemeGroupVersion
	return config
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
func createConfig(kubeClient kube.ExtendedClient, wg *clientv1alpha3.WorkloadGroup, clusterID, revision, outputName string) error {
	fillWorkloadGroup(wg)
	temp, err := ioutil.TempDir("./", outputName)
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)

	if err = createClusterEnv(wg, temp); err != nil {
		return err
	}
	if err = createCertsTokens(kubeClient, wg, temp); err != nil {
		return err
	}
	if err = createMeshConfig(kubeClient, wg, clusterID, revision, temp); err != nil {
		return err
	}
	if err = Tar(temp, outputName); err != nil {
		return err
	}
	return nil
}

// used to populate nil WorkloadGroup structs (template, metadata, and spec)
func fillWorkloadGroup(wg *clientv1alpha3.WorkloadGroup) {
	if wg.Spec.Template == nil {
		wg.Spec.Template = &networkingv1alpha3.WorkloadEntryTemplate{}
	}
	if wg.Spec.Template.Metadata == nil {
		wg.Spec.Template.Metadata = &metav1.ObjectMeta{}
	}
	if wg.Spec.Template.Spec == nil {
		wg.Spec.Template.Spec = &networkingv1alpha3.WorkloadEntry{}
	}
}

// Write cluster.env into the given directory
func createClusterEnv(wg *clientv1alpha3.WorkloadGroup, dir string) error {
	we := wg.Spec.Template.Spec
	for _, v := range we.Ports {
		ports = append(ports, fmt.Sprint(v))
	}
	// default attributes and service name, namespace, ports, service account, service CIDR
	clusterEnv := map[string]string{
		"ISTIO_CP_AUTH":       "MUTUAL_TLS",
		"ISTIO_INBOUND_PORTS": strings.Join(ports, ","),
		"ISTIO_NAMESPACE":     wg.Namespace,
		"ISTIO_PILOT_PORT":    "15012",
		"ISTIO_SERVICE":       fmt.Sprintf("%s.%s", wg.Name, wg.Namespace),
		"ISTIO_SERVICE_CIDR":  "*",
		"SERVICE_ACCOUNT":     we.ServiceAccount,
	}
	return ioutil.WriteFile(dir+"/cluster.env", []byte(mapToString(clusterEnv)), tempPerms)
}

// Get and store the needed certificate and token. The certificate comes from the `istio-ca-root-cert`, and
// the token is generated by kubectl under the workload group's namespace and service account
// OSS requires both while ASM (will eventually) require only the token
func createCertsTokens(kubeClient kube.ExtendedClient, wg *clientv1alpha3.WorkloadGroup, dir string) error {
	rootCert, err := kubeClient.CoreV1().ConfigMaps(wg.Namespace).Get(context.Background(), "istio-ca-root-cert", metav1.GetOptions{})
	if err != nil {
		return err
	}
	if err = ioutil.WriteFile(dir+"/root-cert", []byte(rootCert.Data["root-cert.pem"]), tempPerms); err != nil {
		return err
	}

	token := &authenticationv1.TokenRequest{
		Spec: authenticationv1.TokenRequestSpec{
			Audiences:         []string{"istio-ca"},
			ExpirationSeconds: &tokenDuration,
		},
	}
	namespace, serviceAccount := wg.Namespace, wg.Spec.Template.Spec.ServiceAccount
	tokenReq, err := kubeClient.CoreV1().ServiceAccounts(namespace).CreateToken(context.Background(), serviceAccount, token, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	if err := ioutil.WriteFile(dir+"/istio-token", []byte(tokenReq.Status.Token), tempPerms); err != nil {
		return err
	}
	return nil
}

func createMeshConfig(kubeClient kube.ExtendedClient, wg *clientv1alpha3.WorkloadGroup, clusterID, revision, dir string) error {
	istioCM := "istio"
	// Case with multiple control planes
	if revision != "" {
		istioCM = fmt.Sprintf("%s-%s", istioCM, revision)
	}
	istio, err := kubeClient.CoreV1().ConfigMaps("istio-system").Get(context.Background(), istioCM, metav1.GetOptions{})
	if err != nil {
		return err
	}
	meshConfig, err := mesh.ApplyMeshConfigDefaults(istio.Data["mesh"])
	if err != nil {
		return err
	}

	labels := map[string]string{}
	for k, v := range wg.Spec.Template.Metadata.Labels {
		labels[k] = v
	}
	// case where a user provided custom workload group has labels in the workload entry spec field
	for k, v := range wg.Spec.Template.Spec.Labels {
		labels[k] = v
	}
	we := wg.Spec.Template.Spec
	md := meshConfig.DefaultConfig.ProxyMetadata
	md["CANONICAL_SERVICE"], md["CANONICAL_REVISION"] = inject.ExtractCanonicalServiceLabels(labels, wg.Name)
	md["DNS_AGENT"] = ""
	md["POD_NAMESPACE"] = wg.Namespace
	md["SERVICE_ACCOUNT"] = we.ServiceAccount
	md["TRUST_DOMAIN"] = meshConfig.TrustDomain

	md["ISTIO_META_CLUSTER_ID"] = clusterID
	md["ISTIO_META_MESH_ID"] = string(meshConfig.DefaultConfig.MeshId)
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
		return err
	}
	return ioutil.WriteFile(dir+"/mesh", []byte(meshYAML), tempPerms)
}

// Packs files inside a source folder into target.tar
func Tar(source, target string) error {
	target = fmt.Sprintf("%s.tar", target)
	tarfile, err := os.Create(target)
	if err != nil {
		return err
	}
	defer tarfile.Close()
	tw := tar.NewWriter(tarfile)
	defer tw.Close()

	// return an error if source folder doesn't exist
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a folder", source)
	}

	// match filepath walk format for trimming the path prefix later
	source = strings.TrimPrefix(source, "./")
	return filepath.Walk(source,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			header, err := tar.FileInfoHeader(info, info.Name())
			if err != nil {
				return err
			}
			header.Name = strings.TrimPrefix(path, source)

			// write the header, and write the content if a file
			if err := tw.WriteHeader(header); err != nil {
				return err
			}
			if !info.IsDir() {
				file, err := os.Open(path)
				if err != nil {
					return err
				}
				defer file.Close()

				_, err = io.Copy(tw, file)
				if err != nil {
					return err
				}
			}
			return nil
		})
}

func mapToString(m map[string]string) string {
	var b strings.Builder
	for k, v := range m {
		fmt.Fprintf(&b, "%s=%s\n", k, v)
	}
	return b.String()
}
