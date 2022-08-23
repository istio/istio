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
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"istio.io/api/annotation"
	"istio.io/api/label"
	meshconfig "istio.io/api/mesh/v1alpha1"
	networkingv1alpha3 "istio.io/api/networking/v1alpha3"
	clientv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/istioctl/pkg/multicluster"
	"istio.io/istio/operator/pkg/tpath"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/validation"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/labels"
	"istio.io/istio/pkg/url"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/util/shellescape"
	"istio.io/pkg/log"
)

var (
	// TODO refactor away from package vars and add more UTs
	tokenDuration  int64
	name           string
	serviceAccount string
	filename       string
	outputDir      string
	clusterID      string
	ingressIP      string
	internalIP     string
	externalIP     string
	ingressSvc     string
	autoRegister   bool
	dnsCapture     bool
	ports          []string
	resourceLabels []string
	annotations    []string
	svcAcctAnn     string
)

const (
	filePerms = os.FileMode(0o744)
)

func workloadCommands() *cobra.Command {
	workloadCmd := &cobra.Command{
		Use:   "workload",
		Short: "Commands to assist in configuring and deploying workloads running on VMs and other non-Kubernetes environments",
		Example: `  # workload group yaml generation
  workload group create

  # workload entry configuration generation
  workload entry configure`,
	}
	workloadCmd.AddCommand(groupCommand())
	workloadCmd.AddCommand(entryCommand())
	return workloadCmd
}

func groupCommand() *cobra.Command {
	groupCmd := &cobra.Command{
		Use:     "group",
		Short:   "Commands dealing with WorkloadGroup resources",
		Example: "group create --name foo --namespace bar --labels app=foobar",
	}
	groupCmd.AddCommand(createCommand())
	return groupCmd
}

func entryCommand() *cobra.Command {
	entryCmd := &cobra.Command{
		Use:     "entry",
		Short:   "Commands dealing with WorkloadEntry resources",
		Example: "entry configure -f workloadgroup.yaml -o outputDir",
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
		Example: "create --name foo --namespace bar --labels app=foo,bar=baz --ports grpc=3550,http=8080 --annotations annotation=foobar --serviceAccount sa",
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
				Object: map[string]any{
					"apiVersion": collections.IstioNetworkingV1Alpha3Workloadgroups.Resource().APIVersion(),
					"kind":       collections.IstioNetworkingV1Alpha3Workloadgroups.Resource().Kind(),
					"metadata": map[string]any{
						"name":      name,
						"namespace": namespace,
					},
				},
			}
			spec := &networkingv1alpha3.WorkloadGroup{
				Metadata: &networkingv1alpha3.WorkloadGroup_ObjectMeta{
					Labels:      convertToStringMap(resourceLabels),
					Annotations: convertToStringMap(annotations),
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
	createCmd.PersistentFlags().StringSliceVarP(&resourceLabels, "labels", "l", nil, "The labels to apply to the workload instances; e.g. -l env=prod,vers=2")
	createCmd.PersistentFlags().StringSliceVarP(&annotations, "annotations", "a", nil, "The annotations to apply to the workload instances")
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
	var opts clioptions.ControlPlaneOptions

	configureCmd := &cobra.Command{
		Use:   "configure",
		Short: "Generates all the required configuration files for a workload instance running on a VM or non-Kubernetes environment",
		Long: `Generates all the required configuration files for workload instance on a VM or non-Kubernetes environment from a WorkloadGroup artifact.
This includes a MeshConfig resource, the cluster.env file, and necessary certificates and security tokens.
Configure requires either the WorkloadGroup artifact path or its location on the API server.`,
		Example: `  # configure example using a local WorkloadGroup artifact
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
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeClient, err := kubeClientWithRevision(kubeconfig, configContext, opts.Revision)
			if err != nil {
				return err
			}

			wg := &clientv1alpha3.WorkloadGroup{}
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

			// extract the cluster ID from the injector config (.Values.global.multiCluster.clusterName)
			if !validateFlagIsSetManuallyOrNot(cmd, "clusterID") {
				// extract the cluster ID from the injector config if it is not set by user
				clusterName, err := extractClusterIDFromInjectionConfig(kubeClient)
				if err != nil {
					return fmt.Errorf("failed to automatically determine the --clusterID: %v", err)
				}
				if clusterName != "" {
					clusterID = clusterName
				}
			}

			if err = createConfig(kubeClient, wg, clusterID, ingressIP, internalIP, externalIP, outputDir, cmd.OutOrStderr()); err != nil {
				return err
			}
			fmt.Printf("Configuration generation into directory %s was successful\n", outputDir)
			return nil
		},
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if len(internalIP) > 0 && len(externalIP) > 0 {
				return fmt.Errorf("the flags --internalIP and --externalIP are mutually exclusive")
			}
			return nil
		},
	}
	configureCmd.PersistentFlags().StringVarP(&filename, "file", "f", "", "filename of the WorkloadGroup artifact. Leave this field empty if using the API server")
	configureCmd.PersistentFlags().StringVar(&name, "name", "", "The name of the workload group")
	configureCmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "", "The namespace that the workload instances belongs to")
	configureCmd.PersistentFlags().StringVarP(&outputDir, "output", "o", "", "Output directory for generated files")
	configureCmd.PersistentFlags().StringVar(&clusterID, "clusterID", "", "The ID used to identify the cluster")
	configureCmd.PersistentFlags().Int64Var(&tokenDuration, "tokenDuration", 3600, "The token duration in seconds (default: 1 hour)")
	configureCmd.PersistentFlags().StringVar(&ingressSvc, "ingressService", multicluster.IstioEastWestGatewayServiceName, "Name of the Service to be"+
		" used as the ingress gateway, in the format <service>.<namespace>. If no namespace is provided, the default "+istioNamespace+" namespace will be used.")
	configureCmd.PersistentFlags().StringVar(&ingressIP, "ingressIP", "", "IP address of the ingress gateway")
	configureCmd.PersistentFlags().BoolVar(&autoRegister, "autoregister", false, "Creates a WorkloadEntry upon connection to istiod (if enabled in pilot).")
	configureCmd.PersistentFlags().BoolVar(&dnsCapture, "capture-dns", true, "Enables the capture of outgoing DNS packets on port 53, redirecting to istio-agent")
	configureCmd.PersistentFlags().StringVar(&internalIP, "internalIP", "", "Internal IP address of the workload")
	configureCmd.PersistentFlags().StringVar(&externalIP, "externalIP", "", "External IP address of the workload")
	opts.AttachControlPlaneFlags(configureCmd)
	return configureCmd
}

// Reads a WorkloadGroup yaml. Additionally populates default values if unset
// TODO: add WorkloadGroup validation in pkg/config/validation
func readWorkloadGroup(filename string, wg *clientv1alpha3.WorkloadGroup) error {
	f, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	if err = yaml.Unmarshal(f, wg); err != nil {
		return err
	}
	// fill empty structs
	if wg.Spec.Metadata == nil {
		wg.Spec.Metadata = &networkingv1alpha3.WorkloadGroup_ObjectMeta{}
	}
	if wg.Spec.Template == nil {
		wg.Spec.Template = &networkingv1alpha3.WorkloadEntry{}
	}
	// default service account for an empty field is "default"
	if wg.Spec.Template.ServiceAccount == "" {
		wg.Spec.Template.ServiceAccount = "default"
	}
	return nil
}

// Creates all the relevant config for the given workload group and cluster
func createConfig(kubeClient kube.CLIClient, wg *clientv1alpha3.WorkloadGroup, clusterID, ingressIP, internalIP,
	externalIP string, outputDir string, out io.Writer,
) error {
	if err := os.MkdirAll(outputDir, filePerms); err != nil {
		return err
	}
	var (
		err         error
		proxyConfig *meshconfig.ProxyConfig
	)
	revision := kubeClient.Revision()
	if proxyConfig, err = createMeshConfig(kubeClient, wg, clusterID, outputDir, revision); err != nil {
		return err
	}
	if err := createClusterEnv(wg, proxyConfig, revision, internalIP, externalIP, outputDir); err != nil {
		return err
	}
	if err := createCertsTokens(kubeClient, wg, outputDir, out); err != nil {
		return err
	}
	if err := createHosts(kubeClient, ingressIP, outputDir, revision); err != nil {
		return err
	}
	return nil
}

// Write cluster.env into the given directory
func createClusterEnv(wg *clientv1alpha3.WorkloadGroup, config *meshconfig.ProxyConfig, revision, internalIP, externalIP, dir string) error {
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

	// 22: ssh is extremely common for VMs, and we do not want to make VM inaccessible if there is an issue
	// 15090: prometheus
	// 15021/15020: agent
	excludePorts := "22,15090,15021"
	if config.StatusPort != 15090 && config.StatusPort != 15021 {
		if config.StatusPort != 0 {
			// Explicit status port set, use that
			excludePorts += fmt.Sprintf(",%d", config.StatusPort)
		} else {
			// use default status port
			excludePorts += ",15020"
		}
	}
	// default attributes and service name, namespace, ports, service account, service CIDR
	overrides := map[string]string{
		"ISTIO_INBOUND_PORTS":       portBehavior,
		"ISTIO_NAMESPACE":           wg.Namespace,
		"ISTIO_SERVICE":             fmt.Sprintf("%s.%s", wg.Name, wg.Namespace),
		"ISTIO_SERVICE_CIDR":        "*",
		"ISTIO_LOCAL_EXCLUDE_PORTS": excludePorts,
		"SERVICE_ACCOUNT":           we.ServiceAccount,
	}

	if isRevisioned(revision) {
		overrides["CA_ADDR"] = IstiodAddr(istioNamespace, revision)
	}
	if len(internalIP) > 0 {
		overrides["ISTIO_SVC_IP"] = internalIP
	} else if len(externalIP) > 0 {
		overrides["ISTIO_SVC_IP"] = externalIP
		overrides["REWRITE_PROBE_LEGACY_LOCALHOST_DESTINATION"] = "true"
	}

	// clusterEnv will use proxyMetadata from the proxyConfig + overrides specific to the WorkloadGroup and cmd args
	// this is similar to the way the injector sets all values proxyConfig.proxyMetadata to the Pod's env
	clusterEnv := map[string]string{}
	for _, metaMap := range []map[string]string{config.ProxyMetadata, overrides} {
		for k, v := range metaMap {
			clusterEnv[k] = v
		}
	}

	return os.WriteFile(filepath.Join(dir, "cluster.env"), []byte(mapToString(clusterEnv)), filePerms)
}

// Get and store the needed certificate and token. The certificate comes from the CA root cert, and
// the token is generated by kubectl under the workload group's namespace and service account
// TODO: Make the following accurate when using the Kubernetes certificate signer
func createCertsTokens(kubeClient kube.CLIClient, wg *clientv1alpha3.WorkloadGroup, dir string, out io.Writer) error {
	rootCert, err := kubeClient.Kube().CoreV1().ConfigMaps(wg.Namespace).Get(context.Background(), controller.CACertNamespaceConfigMap, metav1.GetOptions{})
	// errors if the requested configmap does not exist in the given namespace
	if err != nil {
		return fmt.Errorf("configmap %s was not found in namespace %s: %v", controller.CACertNamespaceConfigMap, wg.Namespace, err)
	}
	if err = os.WriteFile(filepath.Join(dir, "root-cert.pem"), []byte(rootCert.Data[constants.CACertNamespaceConfigMapDataName]), filePerms); err != nil {
		return err
	}

	serviceAccount := wg.Spec.Template.ServiceAccount
	tokenPath := filepath.Join(dir, "istio-token")
	jwtPolicy, err := util.DetectSupportedJWTPolicy(kubeClient.Kube())
	if err != nil {
		fmt.Fprintf(out, "Failed to determine JWT policy support: %v", err)
	}
	if jwtPolicy == util.FirstPartyJWT {
		fmt.Fprintf(out, "Warning: cluster does not support third party JWT authentication. "+
			"Falling back to less secure first party JWT. "+
			"See "+url.ConfigureSAToken+" for details."+"\n")
		sa, err := kubeClient.Kube().CoreV1().ServiceAccounts(wg.Namespace).Get(context.TODO(), serviceAccount, metav1.GetOptions{})
		if err != nil {
			return err
		}
		secret, err := kubeClient.Kube().CoreV1().Secrets(wg.Namespace).Get(context.TODO(), sa.Secrets[0].Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if err := os.WriteFile(tokenPath, secret.Data["token"], filePerms); err != nil {
			return err
		}
		fmt.Fprintf(out, "Warning: a security token for namespace %q and service account %q has been generated "+
			"and stored at %q\n", wg.Namespace, serviceAccount, tokenPath)
		return nil
	}
	token := &authenticationv1.TokenRequest{
		// ObjectMeta isn't required in real k8s, but needed for tests
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceAccount,
			Namespace: wg.Namespace,
		},
		Spec: authenticationv1.TokenRequestSpec{
			Audiences:         []string{"istio-ca"},
			ExpirationSeconds: &tokenDuration,
		},
	}
	tokenReq, err := kubeClient.Kube().CoreV1().ServiceAccounts(wg.Namespace).CreateToken(context.Background(), serviceAccount, token, metav1.CreateOptions{})
	// errors if the token could not be created with the given service account in the given namespace
	if err != nil {
		return fmt.Errorf("could not create a token under service account %s in namespace %s: %v", serviceAccount, wg.Namespace, err)
	}
	if err := os.WriteFile(tokenPath, []byte(tokenReq.Status.Token), filePerms); err != nil {
		return err
	}
	fmt.Fprintf(out, "Warning: a security token for namespace %q and service account %q has been generated and "+
		"stored at %q\n", wg.Namespace, serviceAccount, tokenPath)
	return nil
}

func createMeshConfig(kubeClient kube.CLIClient, wg *clientv1alpha3.WorkloadGroup, clusterID, dir, revision string) (*meshconfig.ProxyConfig, error) {
	istioCM := "istio"
	// Case with multiple control planes
	if isRevisioned(revision) {
		istioCM = fmt.Sprintf("%s-%s", istioCM, revision)
	}
	istio, err := kubeClient.Kube().CoreV1().ConfigMaps(istioNamespace).Get(context.Background(), istioCM, metav1.GetOptions{})
	// errors if the requested configmap does not exist in the given namespace
	if err != nil {
		return nil, fmt.Errorf("configmap %s was not found in namespace %s: %v", istioCM, istioNamespace, err)
	}
	// fill some fields before applying the yaml to prevent errors later
	meshConfig := &meshconfig.MeshConfig{
		DefaultConfig: &meshconfig.ProxyConfig{
			ProxyMetadata: map[string]string{},
		},
	}
	if err := protomarshal.ApplyYAML(istio.Data[configMapKey], meshConfig); err != nil {
		return nil, err
	}
	if isRevisioned(revision) && meshConfig.DefaultConfig.DiscoveryAddress == "" {
		meshConfig.DefaultConfig.DiscoveryAddress = IstiodAddr(istioNamespace, revision)
	}

	// performing separate map-merge, apply seems to completely overwrite all metadata
	proxyMetadata := meshConfig.DefaultConfig.ProxyMetadata

	// support proxy.istio.io/config on the WorkloadGroup, in the WorkloadGroup spec
	for _, annotations := range []map[string]string{wg.Annotations, wg.Spec.Metadata.Annotations} {
		if pcYaml, ok := annotations[annotation.ProxyConfig.Name]; ok {
			if err := protomarshal.ApplyYAML(pcYaml, meshConfig.DefaultConfig); err != nil {
				return nil, err
			}
			for k, v := range meshConfig.DefaultConfig.ProxyMetadata {
				proxyMetadata[k] = v
			}
		}
	}

	meshConfig.DefaultConfig.ProxyMetadata = proxyMetadata

	lbls := map[string]string{}
	for k, v := range wg.Spec.Metadata.Labels {
		lbls[k] = v
	}
	// case where a user provided custom workload group has labels in the workload entry template field
	we := wg.Spec.Template
	if len(we.Labels) > 0 {
		fmt.Printf("Labels should be set in the metadata. The following WorkloadEntry labels will override metadata labels: %s\n", we.Labels)
		for k, v := range we.Labels {
			lbls[k] = v
		}
	}

	meshConfig.DefaultConfig.ReadinessProbe = wg.Spec.Probe

	md := meshConfig.DefaultConfig.ProxyMetadata
	if md == nil {
		md = map[string]string{}
		meshConfig.DefaultConfig.ProxyMetadata = md
	}
	md["CANONICAL_SERVICE"], md["CANONICAL_REVISION"] = labels.CanonicalService(lbls, wg.Name)
	md["POD_NAMESPACE"] = wg.Namespace
	md["SERVICE_ACCOUNT"] = we.ServiceAccount
	md["TRUST_DOMAIN"] = meshConfig.TrustDomain

	md["ISTIO_META_CLUSTER_ID"] = clusterID
	md["ISTIO_META_MESH_ID"] = meshConfig.DefaultConfig.MeshId
	md["ISTIO_META_NETWORK"] = we.Network
	if portsStr := marshalWorkloadEntryPodPorts(we.Ports); portsStr != "" {
		md["ISTIO_META_POD_PORTS"] = portsStr
	}
	md["ISTIO_META_WORKLOAD_NAME"] = wg.Name
	lbls[label.ServiceCanonicalName.Name] = md["CANONICAL_SERVICE"]
	lbls[label.ServiceCanonicalRevision.Name] = md["CANONICAL_REVISION"]
	if labelsJSON, err := json.Marshal(lbls); err == nil {
		md["ISTIO_METAJSON_LABELS"] = string(labelsJSON)
	}

	// TODO the defaults should be controlled by meshConfig/proxyConfig; if flags not given to the command proxyCOnfig takes precedence
	if dnsCapture {
		md["ISTIO_META_DNS_CAPTURE"] = strconv.FormatBool(dnsCapture)
	}
	if autoRegister {
		md["ISTIO_META_AUTO_REGISTER_GROUP"] = wg.Name
	}

	proxyConfig, err := protomarshal.ToJSONMap(meshConfig.DefaultConfig)
	if err != nil {
		return nil, err
	}

	proxyYAML, err := yaml.Marshal(map[string]any{"defaultConfig": proxyConfig})
	if err != nil {
		return nil, err
	}

	return meshConfig.DefaultConfig, os.WriteFile(filepath.Join(dir, "mesh.yaml"), proxyYAML, filePerms)
}

func marshalWorkloadEntryPodPorts(p map[string]uint32) string {
	var out []model.PodPort
	for name, port := range p {
		out = append(out, model.PodPort{Name: name, ContainerPort: int(port)})
	}
	if len(out) == 0 {
		return ""
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	str, err := json.Marshal(out)
	if err != nil {
		return ""
	}
	return string(str)
}

// Retrieves the external IP of the ingress-gateway for the hosts file additions
func createHosts(kubeClient kube.CLIClient, ingressIP, dir string, revision string) error {
	// try to infer the ingress IP if the provided one is invalid
	if validation.ValidateIPAddress(ingressIP) != nil {
		p := strings.Split(ingressSvc, ".")
		ingressNs := istioNamespace
		if len(p) == 2 {
			ingressSvc = p[0]
			ingressNs = p[1]
		}
		ingress, err := kubeClient.Kube().CoreV1().Services(ingressNs).Get(context.Background(), ingressSvc, metav1.GetOptions{})
		if err == nil {
			if ingress.Status.LoadBalancer.Ingress != nil && len(ingress.Status.LoadBalancer.Ingress) > 0 {
				ingressIP = ingress.Status.LoadBalancer.Ingress[0].IP
			} else if len(ingress.Spec.ExternalIPs) > 0 {
				ingressIP = ingress.Spec.ExternalIPs[0]
			}
			// TODO: add case where the load balancer is a DNS name
		}
	}

	var hosts string
	if net.ParseIP(ingressIP) != nil {
		hosts = fmt.Sprintf("%s %s\n", ingressIP, IstiodHost(istioNamespace, revision))
	} else {
		log.Warnf("Could not auto-detect IP for %s/%s. Use --ingressIP to manually specify the Gateway address to reach istiod from the VM.",
			IstiodHost(istioNamespace, revision), istioNamespace)
	}
	return os.WriteFile(filepath.Join(dir, "hosts"), []byte(hosts), filePerms)
}

func isRevisioned(revision string) bool {
	return revision != "" && revision != "default"
}

func IstiodHost(ns string, revision string) string {
	istiod := "istiod"
	if isRevisioned(revision) {
		istiod = fmt.Sprintf("%s-%s", istiod, revision)
	}
	return fmt.Sprintf("%s.%s.svc", istiod, ns)
}

func IstiodAddr(ns, revision string) string {
	// TODO make port configurable
	return fmt.Sprintf("%s:%d", IstiodHost(ns, revision), 15012)
}

// Returns a map with each k,v entry on a new line
func mapToString(m map[string]string) string {
	lines := []string{}
	for k, v := range m {
		lines = append(lines, fmt.Sprintf("%s=%s", k, shellescape.Quote(v)))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n") + "\n"
}

// extractClusterIDFromInjectionConfig can extract clusterID from injection configmap
func extractClusterIDFromInjectionConfig(kubeClient kube.CLIClient) (string, error) {
	injectionConfigMap := "istio-sidecar-injector"
	// Case with multiple control planes
	revision := kubeClient.Revision()
	if isRevisioned(revision) {
		injectionConfigMap = fmt.Sprintf("%s-%s", injectionConfigMap, revision)
	}
	istioInjectionCM, err := kubeClient.Kube().CoreV1().ConfigMaps(istioNamespace).Get(context.Background(), injectionConfigMap, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("fetch injection template: %v", err)
	}

	var injectedCMValues map[string]any
	if err := json.Unmarshal([]byte(istioInjectionCM.Data[valuesConfigMapKey]), &injectedCMValues); err != nil {
		return "", err
	}
	v, f, err := tpath.GetFromStructPath(injectedCMValues, "global.multiCluster.clusterName")
	if err != nil {
		return "", err
	}
	vs, ok := v.(string)
	if !f || !ok {
		return "", fmt.Errorf("could not retrieve global.multiCluster.clusterName from injection config")
	}
	return vs, nil
}
