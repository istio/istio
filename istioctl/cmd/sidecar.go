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
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/spf13/cobra"

	networkingv1alpha3 "istio.io/api/networking/v1alpha3"
	clientv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/inject"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var (
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

func sidecarCommands() *cobra.Command {
	sidecarCmd := &cobra.Command{
		Use:   "sidecar",
		Short: "Commands to assist in managing sidecar configuration",
	}
	sidecarCmd.AddCommand(createGroupCommand())
	sidecarCmd.AddCommand(generateConfigCommand())
	return sidecarCmd
}

func createGroupCommand() *cobra.Command {
	createGroupCmd := &cobra.Command{
		Use:   "create-group",
		Short: "Creates a WorkloadGroup YAML artifact representing workload instances",
		Long: `Creates a WorkloadGroup API YAML artifact to send to the Kubernetes API server.
To send the generated artifact to Kubernetes, run kubectl apply -f workloadgroup.yaml`,
		Example: "create-group --name foo --namespace bar --labels app=foo,bar=baz --ports grpc=3550,http=8080 --serviceAccount sa",
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
	createGroupCmd.PersistentFlags().StringVar(&name, "name", "", "The name of the workload group")
	createGroupCmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "", "The namespace that the workload instances will belong to")
	createGroupCmd.PersistentFlags().StringSliceVarP(&labels, "labels", "l", nil, "The labels to apply to the workload instances; e.g. -l env=prod,vers=2")
	createGroupCmd.PersistentFlags().StringSliceVarP(&ports, "ports", "p", nil, "The incoming ports exposed by the workload instance")
	createGroupCmd.PersistentFlags().StringVarP(&serviceAccount, "serviceAccount", "s", "default", "The service identity to associate with the workload instances")
	return createGroupCmd
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

func generateConfigCommand() *cobra.Command {
	generateConfigCmd := &cobra.Command{
		Use:   "generate-config",
		Short: "Generates all the required configuration files for a workload deployment",
		Long: `Generates all the required configuration files for workload deployment from a WorkloadGroup artifact. 
This includes a MeshConfig resource, the cluster.env file, and necessary certificates and security tokens.`,
		Example: "generate-config -f workloadgroup.yaml -o config",
		Args: func(cmd *cobra.Command, args []string) error {
			if filename == "" {
				return fmt.Errorf("expecting a WorkloadGroup artifact file")
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
			if err := readWorkloadGroup(filename, wg); err != nil {
				return err
			}
			kubeClient, err := kube.NewExtendedClient(kube.BuildClientCmd(kubeconfig, configContext), "")
			if err != nil {
				return err
			}

			if err = createConfig(kubeClient, wg, clusterID, revision, outputName); err != nil {
				return err
			}
			return nil
		},
	}
	generateConfigCmd.PersistentFlags().StringVarP(&filename, "file", "f", "", "filename of the WorkloadGroup artifact")
	generateConfigCmd.PersistentFlags().StringVarP(&outputName, "output", "o", "", "Name of the tarball to be created")
	generateConfigCmd.PersistentFlags().StringVar(&clusterID, "clusterID", "", "The ID used to identify the cluster")
	generateConfigCmd.PersistentFlags().StringVar(&revision, "revision", "", "control plane revision (experimental)")
	return generateConfigCmd
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
	temp, err := ioutil.TempDir("./", outputName)
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)

	if err = createClusterEnv(wg, temp); err != nil {
		return err
	}
	if err = createCertsTokens(kubeClient, temp); err != nil {
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

// Get and store the needed certificates
// TODO: use internal generate-cert
func createCertsTokens(kubeClient kube.ExtendedClient, dir string) error {
	rootCert, err := kubeClient.CoreV1().ConfigMaps("istio-system").Get(context.Background(), "istio-ca-root-cert", metav1.GetOptions{})
	if err != nil {
		return err
	}
	if err = ioutil.WriteFile(dir+"/root-cert.pem", []byte(rootCert.Data["root-cert.pem"]), tempPerms); err != nil {
		return err
	}

	args := strings.Split("run istio.io/istio/security/tools/generate_cert -client -host spiffee://cluster.local/vm/vmname --out-priv key.pem --out-cert cert-chain.pem -mode citadel", " ")
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	if err = cmd.Run(); err != nil {
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

	temp, err := ioutil.TempFile("./", "")
	defer os.Remove(temp.Name())
	if _, err = temp.WriteString(istio.Data["mesh"]); err != nil {
		return err
	}
	meshConfig, err := mesh.ReadMeshConfig(temp.Name())

	labels := wg.Spec.Template.Metadata.Labels
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

	meshYAML, err := yaml.Marshal(meshConfig)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(dir+"/mesh", meshYAML, tempPerms)
}

// Packs files inside source into target.tar
func Tar(source, target string) error {
	target = fmt.Sprintf("%s.tar", filepath.Base(target))
	tarfile, err := os.Create(target)
	if err != nil {
		return err
	}
	defer tarfile.Close()
	tw := tar.NewWriter(tarfile)
	defer tw.Close()

	info, err := os.Stat(source)
	if err != nil {
		return nil
	}

	var baseDir string
	if info.IsDir() {
		baseDir = filepath.Base(source)
	}

	return filepath.Walk(source,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			header, err := tar.FileInfoHeader(info, info.Name())
			if err != nil {
				return err
			}

			if baseDir != "" {
				header.Name = strings.TrimPrefix(path, source)
			}

			if err := tw.WriteHeader(header); err != nil {
				return err
			}

			if info.IsDir() {
				return nil
			}

			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()
			_, err = io.Copy(tw, file)
			return err
		})
}

func mapToString(m map[string]string) string {
	var b strings.Builder
	for k, v := range m {
		fmt.Fprintf(&b, "%s=%s\n", k, v)
	}
	return b.String()
}
