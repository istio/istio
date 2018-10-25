// Copyright 2017 Istio Authors
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

package framework

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pkg/errors"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/log"
	"istio.io/istio/tests/util"
)

const (
	yamlSuffix                     = ".yaml"
	istioInstallDir                = "install/kubernetes"
	istioAddonsDir                 = "install/kubernetes/addons"
	nonAuthInstallFile             = "istio.yaml"
	authInstallFile                = "istio-auth.yaml"
	nonAuthWithMCPInstallFile      = "istio-mcp.yaml"
	authWithMCPInstallFile         = "istio-auth-mcp.yaml"
	nonAuthInstallFileNamespace    = "istio-one-namespace.yaml"
	authInstallFileNamespace       = "istio-one-namespace-auth.yaml"
	mcNonAuthInstallFileNamespace  = "istio-multicluster.yaml"
	mcAuthInstallFileNamespace     = "istio-auth-multicluster.yaml"
	mcRemoteInstallFile            = "istio-remote.yaml"
	istioSystem                    = "istio-system"
	istioIngressServiceName        = "istio-ingress"
	istioIngressLabel              = "ingress"
	istioIngressGatewayServiceName = "istio-ingressgateway"
	istioIngressGatewayLabel       = "ingressgateway"
	istioEgressGatewayServiceName  = "istio-egressgateway"
	defaultSidecarInjectorFile     = "istio-sidecar-injector.yaml"
	ingressCertsName               = "istio-ingress-certs"
	maxDeploymentRolloutTime       = 480 * time.Second
	maxValidationReadyCheckTime    = 30 * time.Second
	helmServiceAccountFile         = "helm-service-account.yaml"
	istioHelmInstallDir            = istioInstallDir + "/helm/istio"
	caCertFileName                 = "samples/certs/ca-cert.pem"
	caKeyFileName                  = "samples/certs/ca-key.pem"
	rootCertFileName               = "samples/certs/root-cert.pem"
	certChainFileName              = "samples/certs/cert-chain.pem"
	helmInstallerName              = "helm"
	// PrimaryCluster identifies the primary cluster
	PrimaryCluster = "primary"
	// RemoteCluster identifies the remote cluster
	RemoteCluster = "remote"
)

var (
	namespace          = flag.String("namespace", "", "Namespace to use for testing (empty to create/delete temporary one)")
	mixerHub           = flag.String("mixer_hub", os.Getenv("HUB"), "Mixer hub")
	mixerTag           = flag.String("mixer_tag", os.Getenv("TAG"), "Mixer tag")
	pilotHub           = flag.String("pilot_hub", os.Getenv("HUB"), "Pilot hub")
	pilotTag           = flag.String("pilot_tag", os.Getenv("TAG"), "Pilot tag")
	proxyHub           = flag.String("proxy_hub", os.Getenv("HUB"), "Proxy hub")
	proxyTag           = flag.String("proxy_tag", os.Getenv("TAG"), "Proxy tag")
	caHub              = flag.String("ca_hub", os.Getenv("HUB"), "Ca hub")
	caTag              = flag.String("ca_tag", os.Getenv("TAG"), "Ca tag")
	galleyHub          = flag.String("galley_hub", os.Getenv("HUB"), "Galley hub")
	galleyTag          = flag.String("galley_tag", os.Getenv("TAG"), "Galley tag")
	sidecarInjectorHub = flag.String("sidecar_injector_hub", os.Getenv("HUB"), "Sidecar injector hub")
	sidecarInjectorTag = flag.String("sidecar_injector_tag", os.Getenv("TAG"), "Sidecar injector tag")
	authEnable         = flag.Bool("auth_enable", false, "Enable auth")
	rbacEnable         = flag.Bool("rbac_enable", true, "Enable rbac")
	localCluster       = flag.Bool("use_local_cluster", false,
		"If true any LoadBalancer type services will be converted to a NodePort service during testing. If running on minikube, this should be set to true")
	skipSetup           = flag.Bool("skip_setup", false, "Skip namespace creation and istio cluster setup")
	sidecarInjectorFile = flag.String("sidecar_injector_file", defaultSidecarInjectorFile, "Sidecar injector yaml file")
	clusterWide         = flag.Bool("cluster_wide", false, "If true Pilot/Mixer will observe all namespaces rather than just the testing namespace")
	imagePullPolicy     = flag.String("image_pull_policy", "", "Specifies an override for the Docker image pull policy to be used")
	multiClusterDir     = flag.String("cluster_registry_dir", "",
		"Directory name for the cluster registry config. When provided a multicluster test to be run across two clusters.")
	useGalleyConfigValidator = flag.Bool("use_galley_config_validator", false, "Use galley configuration validation webhook")
	installer                = flag.String("installer", "kubectl", "Istio installer, default to kubectl, or helm")
	useMCP                   = flag.Bool("use_mcp", false, "use MCP for configuring Istio components")
	kubeInjectCM             = flag.String("kube_inject_configmap", "",
		"Configmap to use by the istioctl kube-inject command.")


	addons = []string{
		"zipkin",
	}
)

type appPodsInfo struct {
	// A map of app label values to the pods for that app
	Pods      map[string][]string
	PodsMutex sync.Mutex
}

// KubeInfo gathers information for kubectl
type KubeInfo struct {
	Namespace string

	TmpDir  string
	yamlDir string

	inglock    sync.Mutex
	ingress    string
	ingressErr error

	ingressGatewayLock sync.Mutex
	ingressGateway     string
	ingressGatewayErr  error

	localCluster     bool
	namespaceCreated bool
	AuthEnabled      bool
	RBACEnabled      bool
	InstallAddons    bool

	// Istioctl installation
	Istioctl *Istioctl
	// App Manager
	AppManager *AppManager

	// Release directory
	ReleaseDir string
	// Use baseversion if not empty.
	BaseVersion string

	appPods  map[string]*appPodsInfo
	Clusters map[string]string

	KubeConfig       string
	KubeClient       kubernetes.Interface
	RemoteKubeConfig string
	RemoteKubeClient kubernetes.Interface
	RemoteAppManager *AppManager
}

func getClusterWideInstallFile() string {
	var istioYaml string
	if *authEnable {
		if *useMCP {
			istioYaml = authWithMCPInstallFile
		} else {
			istioYaml = authInstallFile
		}
	} else {
		if *useMCP {
			istioYaml = nonAuthWithMCPInstallFile
		} else {
			istioYaml = nonAuthInstallFile
		}
	}
	return istioYaml
}

// newKubeInfo create a new KubeInfo by given temp dir and runID
// If baseVersion is not empty, will use the specified release of Istio instead of the local one.
func newKubeInfo(tmpDir, runID, baseVersion string) (*KubeInfo, error) {
	if *namespace == "" {
		if *clusterWide {
			*namespace = istioSystem
		} else {
			*namespace = runID
		}
	}
	yamlDir := filepath.Join(tmpDir, "yaml")
	i, err := NewIstioctl(yamlDir, *namespace, *proxyHub, *proxyTag, *imagePullPolicy, *kubeInjectCM)
	if err != nil {
		return nil, err
	}

	// Download the base release if baseVersion is specified.
	var releaseDir string
	if baseVersion != "" {
		releaseDir, err = util.DownloadRelease(baseVersion, tmpDir)
		if err != nil {
			return nil, err
		}
		// Use istioctl from base version to inject the sidecar.
		i.localPath = filepath.Join(releaseDir, "/bin/istioctl")
		if err = os.Chmod(i.localPath, 0755); err != nil {
			return nil, err
		}
		i.defaultProxy = true
	} else {
		releaseDir = util.GetResourcePath("")
	}
	// Note the kubectl commands used by the test will default to use the local
	// environment's kubeconfig if an empty string is provided.  Therefore in the
	// default case kubeConfig will not be set.
	var kubeConfig, remoteKubeConfig string
	var kubeClient, remoteKubeClient kubernetes.Interface
	var aRemote *AppManager
	if *multiClusterDir != "" {
		// multiClusterDir indicates the Kubernetes cluster config should come from files versus
		// the in cluster config. At the current time only the remote kubeconfig is read from a
		// file.
		tmpfile := *namespace + "_kubeconfig"
		tmpfile = path.Join(tmpDir, tmpfile)
		if err = util.GetKubeConfig(tmpfile); err != nil {
			return nil, err
		}
		kubeConfig = tmpfile
		remoteKubeConfig, err = getKubeConfigFromFile(*multiClusterDir)
		if err != nil {
			// TODO could change this to continue tests if only a single cluster is in play
			return nil, err
		}
		if kubeClient, err = kube.CreateInterface(kubeConfig); err != nil {
			return nil, err
		}
		if remoteKubeClient, err = kube.CreateInterface(remoteKubeConfig); err != nil {
			return nil, err
		}
		// Create Istioctl for remote using injectConfigMap on remote (not the same as master cluster's)
		remoteI, err := NewIstioctl(yamlDir, *namespace, *proxyHub, *proxyTag, *imagePullPolicy, "istio-sidecar-injector")
		if err != nil {
			return nil, err
		}
		if baseVersion != "" {
			remoteI.localPath = filepath.Join(releaseDir, "/bin/istioctl")
			remoteI.defaultProxy = true
		}
		aRemote = NewAppManager(tmpDir, *namespace, remoteI, remoteKubeConfig)
	}

	a := NewAppManager(tmpDir, *namespace, i, kubeConfig)

	clusters := make(map[string]string)
	appPods := make(map[string]*appPodsInfo)
	clusters[PrimaryCluster] = kubeConfig
	appPods[PrimaryCluster] = &appPodsInfo{}
	if remoteKubeConfig != "" {
		clusters[RemoteCluster] = remoteKubeConfig
		appPods[RemoteCluster] = &appPodsInfo{}
	}

	log.Infof("Using release dir: %s", releaseDir)
	return &KubeInfo{
		Namespace:        *namespace,
		namespaceCreated: false,
		TmpDir:           tmpDir,
		yamlDir:          yamlDir,
		localCluster:     *localCluster,
		Istioctl:         i,
		AppManager:       a,
		RemoteAppManager: aRemote,
		AuthEnabled:      *authEnable,
		RBACEnabled:      *rbacEnable,
		ReleaseDir:       releaseDir,
		BaseVersion:      baseVersion,
		KubeConfig:       kubeConfig,
		KubeClient:       kubeClient,
		RemoteKubeConfig: remoteKubeConfig,
		RemoteKubeClient: remoteKubeClient,
		appPods:          appPods,
		Clusters:         clusters,
	}, nil
}

// IsClusterWide indicates whether or not the environment is configured for a cluster-wide deployment.
func (k *KubeInfo) IsClusterWide() bool {
	return *clusterWide
}

// IstioSystemNamespace returns the namespace used for the Istio system components.
func (k *KubeInfo) IstioSystemNamespace() string {
	if *clusterWide {
		return istioSystem
	}
	return k.Namespace
}

// IstioIngressService returns the service name for the ingress service
func (k *KubeInfo) IstioIngressService() string {
	return istioIngressServiceName
}

// IstioIngressGatewayService returns the service name for the ingress gateway service
func (k *KubeInfo) IstioIngressGatewayService() string {
	return istioIngressGatewayServiceName
}

// IstioEgressGatewayService returns the service name for the egress gateway service
func (k *KubeInfo) IstioEgressGatewayService() string {
	return istioEgressGatewayServiceName
}

// Setup set up Kubernetes prerequest for tests
func (k *KubeInfo) Setup() error {
	log.Infoa("Setting up kubeInfo setupSkip=", *skipSetup)
	var err error
	if err = os.Mkdir(k.yamlDir, os.ModeDir|os.ModePerm); err != nil {
		return err
	}

	if !*skipSetup {
		if *installer == helmInstallerName {
			// install helm tiller first
			yamlDir := filepath.Join(istioInstallDir+"/"+helmInstallerName, helmServiceAccountFile)
			baseHelmServiceAccountYaml := filepath.Join(k.ReleaseDir, yamlDir)
			if err = k.deployTiller(baseHelmServiceAccountYaml); err != nil {
				log.Error("Failed to deploy helm tiller.")
				return err
			}

			// install istio using helm
			if err = k.deployIstioWithHelm(); err != nil {
				log.Error("Failed to deploy Istio with helm.")
				return err
			}
		} else {
			if err = k.deployIstio(); err != nil {
				log.Error("Failed to deploy Istio.")
				return err
			}
		}
		// Create the ingress secret.
		certDir := util.GetResourcePath("./tests/testdata/certs")
		certFile := filepath.Join(certDir, "cert.crt")
		keyFile := filepath.Join(certDir, "cert.key")
		if _, err = util.CreateTLSSecret(ingressCertsName, k.IstioSystemNamespace(), keyFile, certFile, k.KubeConfig); err != nil {
			log.Warn("Secret already exists")
		}
	}

	return nil
}

// PilotHub exposes the Docker hub used for the pilot image.
func (k *KubeInfo) PilotHub() string {
	return *pilotHub
}

// PilotTag exposes the Docker tag used for the pilot image.
func (k *KubeInfo) PilotTag() string {
	return *pilotTag
}

// ProxyHub exposes the Docker hub used for the proxy image.
func (k *KubeInfo) ProxyHub() string {
	return *proxyHub
}

// ProxyTag exposes the Docker tag used for the proxy image.
func (k *KubeInfo) ProxyTag() string {
	return *proxyTag
}

// ImagePullPolicy exposes the pull policy override used for Docker images. May be "".
func (k *KubeInfo) ImagePullPolicy() string {
	return *imagePullPolicy
}

// IngressOrFail lazily initialize ingress and fail test if not found.
func (k *KubeInfo) IngressOrFail(t *testing.T) string {
	gw, err := k.Ingress()
	if err != nil {
		t.Fatalf("Unable to get ingress: %v", err)
	}
	return gw
}

// Ingress lazily initialize ingress
func (k *KubeInfo) Ingress() (string, error) {
	return k.doGetIngress(istioIngressServiceName, istioIngressLabel, &k.inglock, &k.ingress, &k.ingressErr)
}

// IngressGatewayOrFail lazily initialize ingress gateway and fail test if not found.
func (k *KubeInfo) IngressGatewayOrFail(t *testing.T) string {
	gw, err := k.IngressGateway()
	if err != nil {
		t.Fatalf("Unable to get ingress: %v", err)
	}
	return gw
}

// IngressGateway lazily initialize Ingress Gateway
func (k *KubeInfo) IngressGateway() (string, error) {
	return k.doGetIngress(istioIngressGatewayServiceName, istioIngressGatewayLabel,
		&k.ingressGatewayLock, &k.ingressGateway, &k.ingressGatewayErr)
}

func (k *KubeInfo) doGetIngress(serviceName string, podLabel string, lock sync.Locker,
	ingress *string, ingressErr *error) (string, error) {
	lock.Lock()
	defer lock.Unlock()

	// Previously fetched ingress or failed.
	if *ingressErr != nil || len(*ingress) != 0 {
		return *ingress, *ingressErr
	}
	if k.localCluster {
		*ingress, *ingressErr = util.GetIngress(serviceName, podLabel,
			k.Namespace, k.KubeConfig, util.NodePortServiceType)
	} else {
		*ingress, *ingressErr = util.GetIngress(serviceName, podLabel,
			k.Namespace, k.KubeConfig, util.LoadBalancerServiceType)
	}

	// So far we only do http ingress
	if len(*ingress) > 0 {
		*ingress = "http://" + *ingress
	}

	return *ingress, *ingressErr
}

// Teardown clean up everything created by setup
func (k *KubeInfo) Teardown() error {
	log.Info("Cleaning up kubeInfo")

	if *skipSetup || *skipCleanup || os.Getenv("SKIP_CLEANUP") != "" {
		return nil
	}
	if *installer == helmInstallerName {
		// clean up using helm
		err := util.HelmDelete(k.Namespace)
		if err != nil {
			return nil
		}

		if err := util.DeleteNamespace(k.Namespace, k.KubeConfig); err != nil {
			log.Errorf("Failed to delete namespace %s", k.Namespace)
		}
	} else {
		if *useAutomaticInjection {
			testSidecarInjectorYAML := filepath.Join(k.TmpDir, "yaml", *sidecarInjectorFile)

			if err := util.KubeDelete(k.Namespace, testSidecarInjectorYAML, k.KubeConfig); err != nil {
				log.Errorf("Istio sidecar injector %s deletion failed", testSidecarInjectorYAML)
				return err
			}
		}

		if *clusterWide {
			istioYaml := getClusterWideInstallFile()

			testIstioYaml := filepath.Join(k.TmpDir, "yaml", istioYaml)

			if err := util.KubeDelete(k.Namespace, testIstioYaml, k.KubeConfig); err != nil {
				log.Infof("Safe to ignore resource not found errors in kubectl delete -f %s", testIstioYaml)
			}
		} else {
			if err := util.DeleteNamespace(k.Namespace, k.KubeConfig); err != nil {
				log.Errorf("Failed to delete namespace %s", k.Namespace)
				return err
			}

			// ClusterRoleBindings are not namespaced and need to be deleted separately
			if _, err := util.Shell("kubectl get --kubeconfig=%s clusterrolebinding -o jsonpath={.items[*].metadata.name}"+
				"|xargs -n 1|fgrep %s|xargs kubectl delete --kubeconfig=%s clusterrolebinding", k.KubeConfig,
				k.Namespace, k.KubeConfig); err != nil {
				log.Errorf("Failed to delete clusterrolebindings associated with namespace %s", k.Namespace)
				return err
			}

			// ClusterRoles are not namespaced and need to be deleted separately
			if _, err := util.Shell("kubectl get --kubeconfig=%s clusterrole -o jsonpath={.items[*].metadata.name}"+
				"|xargs -n 1|fgrep %s|xargs kubectl delete --kubeconfig=%s clusterrole", k.KubeConfig,
				k.Namespace, k.KubeConfig); err != nil {
				log.Errorf("Failed to delete clusterroles associated with namespace %s", k.Namespace)
				return err
			}
		}
	}
	if *multiClusterDir != "" {
		if err := util.DeleteNamespace(k.Namespace, k.RemoteKubeConfig); err != nil {
			log.Errorf("Failed to delete namespace %s on remote cluster", k.Namespace)
		}
	}

	// confirm the namespace is deleted as it will cause future creation to fail
	maxAttempts := 600
	namespaceDeleted := false
	validatingWebhookConfigurationDeleted := false
	log.Infof("Deleting namespace %v", k.Namespace)
	for attempts := 1; attempts <= maxAttempts; attempts++ {
		namespaceDeleted, _ = util.NamespaceDeleted(k.Namespace, k.KubeConfig)
		// As validatingWebhookConfiguration "istio-galley" will
		// be delete by kubernetes GC controller asynchronously,
		// we need to ensure it's deleted before return.
		// TODO: find a more general way as long term solution.
		validatingWebhookConfigurationDeleted = util.ValidatingWebhookConfigurationDeleted("istio-galley", k.KubeConfig)

		if namespaceDeleted && validatingWebhookConfigurationDeleted {
			break
		}

		time.Sleep(1 * time.Second)
	}

	if !namespaceDeleted {
		log.Errorf("Failed to delete namespace %s after %v seconds", k.Namespace, maxAttempts)
		return nil
	}

	if !validatingWebhookConfigurationDeleted {
		log.Errorf("Failed to delete validatingwebhookconfiguration istio-galley after %d seconds", maxAttempts)
		return nil
	}

	log.Infof("Namespace %s deletion status: %v", k.Namespace, namespaceDeleted)

	return nil
}

// GetAppPods gets a map of app name to pods for that app. If pods are found, the results are cached.
func (k *KubeInfo) GetAppPods(cluster string) map[string][]string {
	// Get a copy of the internal map.
	newMap := k.getAppPods(cluster)

	if len(newMap) == 0 {
		var err error
		if newMap, err = util.GetAppPods(k.Namespace, k.Clusters[cluster]); err != nil {
			log.Errorf("Failed to get retrieve the app pods for namespace %s", k.Namespace)
		} else {
			// Copy the new results to the internal map.
			log.Infof("Fetched pods with the `app` label: %v", newMap)
			k.setAppPods(cluster, newMap)
		}
	}
	return newMap
}

// GetRoutes gets routes from the pod or returns error
func (k *KubeInfo) GetRoutes(app string) (routes string, err error) {
	routesURL := "http://localhost:15000/config_dump"
	for cluster := range k.Clusters {
		appPods := k.GetAppPods(cluster)
		if len(appPods[app]) == 0 {
			return "", errors.Errorf("missing pod names for app %q", app)
		}

		pod := appPods[app][0]

		r, e := util.PodExec(k.Namespace, pod, "app", fmt.Sprintf("client -url %s", routesURL), true, k.Clusters[cluster])
		if e != nil {
			return "", errors.WithMessage(err, "failed to get routes")
		}
		routes += fmt.Sprintf("Routes From %s Cluster: \n", cluster)
		routes += r
	}

	return routes, nil
}

// getAppPods returns a copy of the appPods map. Should only be called by GetAppPods.
func (k *KubeInfo) getAppPods(cluster string) map[string][]string {
	k.appPods[cluster].PodsMutex.Lock()
	defer k.appPods[cluster].PodsMutex.Unlock()

	return k.deepCopy(k.appPods[cluster].Pods)
}

// setAppPods sets the app pods with a copy of the given map. Should only be called by GetAppPods.
func (k *KubeInfo) setAppPods(cluster string, newMap map[string][]string) {
	k.appPods[cluster].PodsMutex.Lock()
	defer k.appPods[cluster].PodsMutex.Unlock()

	k.appPods[cluster].Pods = k.deepCopy(newMap)
}

func (k *KubeInfo) deepCopy(src map[string][]string) map[string][]string {
	newMap := make(map[string][]string, len(src))
	for k, v := range src {
		newMap[k] = v
	}
	return newMap
}

func (k *KubeInfo) deployAddons() error {
	for _, addon := range addons {
		addonPath := filepath.Join(istioAddonsDir, fmt.Sprintf("%s.yaml", addon))
		baseYamlFile := filepath.Join(k.ReleaseDir, addonPath)
		content, err := ioutil.ReadFile(baseYamlFile)
		if err != nil {
			log.Errorf("Cannot read file %s", baseYamlFile)
			return err
		}

		if !*clusterWide {
			content = replacePattern(content, istioSystem, k.Namespace)
		}

		yamlFile := filepath.Join(k.TmpDir, "yaml", addon+".yaml")
		err = ioutil.WriteFile(yamlFile, content, 0600)
		if err != nil {
			log.Errorf("Cannot write into file %s", yamlFile)
		}

		if err := util.KubeApply(k.Namespace, yamlFile, k.KubeConfig); err != nil {
			log.Errorf("Kubectl apply %s failed", yamlFile)
			return err
		}
	}
	return nil
}

func (k *KubeInfo) deployIstio() error {
	istioYaml := nonAuthInstallFileNamespace
	if *multiClusterDir != "" {
		istioYaml = mcNonAuthInstallFileNamespace
	}
	if *clusterWide {
		istioYaml = getClusterWideInstallFile()
	} else {
		if *authEnable {
			istioYaml = authInstallFileNamespace
			if *multiClusterDir != "" {
				istioYaml = mcAuthInstallFileNamespace
			}
		}
		if *useGalleyConfigValidator {
			return errors.New("cannot enable useGalleyConfigValidator in one namespace tests")
		}
	}

	yamlDir := filepath.Join(istioInstallDir, istioYaml)
	baseIstioYaml := filepath.Join(k.ReleaseDir, yamlDir)
	testIstioYaml := filepath.Join(k.TmpDir, "yaml", istioYaml)

	if err := k.generateIstio(baseIstioYaml, testIstioYaml); err != nil {
		log.Errorf("Generating yaml %s failed", testIstioYaml)
		return err
	}

	if err := util.CreateNamespace(k.Namespace, k.KubeConfig); err != nil {
		log.Errorf("Unable to create namespace %s: %s", k.Namespace, err.Error())
		return err
	}

	if *multiClusterDir != "" {
		if err := k.createCacerts(false); err != nil {
			log.Infof("Failed to create Cacerts with namespace %s in primary cluster", k.Namespace)
		}
	}
	if err := util.KubeApply(k.Namespace, testIstioYaml, k.KubeConfig); err != nil {
		log.Errorf("Istio core %s deployment failed", testIstioYaml)
		return err
	}

	if k.InstallAddons {
		if err := k.deployAddons(); err != nil {
			log.Error("Failed to deploy istio addons")
			return err
		}
	}

	if *multiClusterDir != "" {
		// Create namespace on any remote clusters
		if err := util.CreateNamespace(k.Namespace, k.RemoteKubeConfig); err != nil {
			log.Errorf("Unable to create namespace %s on remote cluster: %s", k.Namespace, err.Error())
			return err
		}
		// Create the local secrets and configmap to start pilot
		if err := util.CreateMultiClusterSecrets(k.Namespace, k.RemoteKubeConfig, k.KubeConfig); err != nil {
			log.Errorf("Unable to create secrets on local cluster %s", err.Error())
			return err
		}

		if err := k.createCacerts(true); err != nil {
			log.Infof("Failed to create Cacerts with namespace %s in remote cluster", k.Namespace)
		}

		testIstioYaml := filepath.Join(k.TmpDir, "yaml", mcRemoteInstallFile)
		if err := k.generateRemoteIstio(testIstioYaml, *useAutomaticInjection, *proxyHub, *proxyTag); err != nil {
			log.Errorf("Generating Remote yaml %s failed", testIstioYaml)
			return err
		}
		if err := util.KubeApply(k.Namespace, testIstioYaml, k.RemoteKubeConfig); err != nil {
			log.Errorf("Remote Istio %s deployment failed", testIstioYaml)
			return err
		}
	}

	if *useAutomaticInjection {
		baseSidecarInjectorYAML := util.GetResourcePath(filepath.Join(istioInstallDir, *sidecarInjectorFile))
		testSidecarInjectorYAML := filepath.Join(k.TmpDir, "yaml", *sidecarInjectorFile)
		if err := k.generateSidecarInjector(baseSidecarInjectorYAML, testSidecarInjectorYAML); err != nil {
			log.Errorf("Generating sidecar injector yaml failed")
			return err
		}
		if err := util.KubeApply(k.Namespace, testSidecarInjectorYAML, k.KubeConfig); err != nil {
			log.Errorf("Istio sidecar injector %s deployment failed", testSidecarInjectorYAML)
			return err
		}
	}

	if err := util.CheckDeployments(k.Namespace, maxDeploymentRolloutTime, k.KubeConfig); err != nil {
		return err
	}

	if *useGalleyConfigValidator {
		timeout := time.Now().Add(maxValidationReadyCheckTime)
		var validationReady bool
		for time.Now().Before(timeout) {
			if _, err := util.ShellSilent("kubectl get validatingwebhookconfiguration istio-galley --kubeconfig=%s", k.KubeConfig); err == nil {
				validationReady = true
				break
			}
		}
		if !validationReady {
			return errors.New("timeout waiting for validatingwebhookconfiguration istio-galley to be created")
		}
	}
	return nil
}

// DeployTiller deploys tiller in Istio mesh or returns error
func (k *KubeInfo) DeployTiller() error {
	// no need to deploy tiller when Istio is deployed using helm as Tiller is already deployed as part of it.
	if *installer == helmInstallerName {
		return nil
	}

	yamlDir := filepath.Join(istioInstallDir+"/"+helmInstallerName, helmServiceAccountFile)
	baseHelmServiceAccountYaml := filepath.Join(k.ReleaseDir, yamlDir)
	return k.deployTiller(baseHelmServiceAccountYaml)
}

func (k *KubeInfo) deployTiller(yamlFileName string) error {
	// apply helm service account
	if err := util.KubeApply("kube-system", yamlFileName, k.KubeConfig); err != nil {
		log.Errorf("Failed to apply %s", yamlFileName)
		return err
	}

	// deploy tiller, helm cli is already available
	if err := util.HelmInit("tiller"); err != nil {
		log.Errorf("Failed to init helm tiller ")
		return err
	}
	// wait till tiller reaches running
	return util.CheckPodRunning("kube-system", "name=tiller", k.KubeConfig)
}

func (k *KubeInfo) deployIstioWithHelm() error {
	yamlFileName := filepath.Join(istioInstallDir, helmInstallerName, "istio", "templates", "crds.yaml")
	yamlFileName = filepath.Join(k.ReleaseDir, yamlFileName)

	if err := util.KubeApply("kube-system", yamlFileName, k.KubeConfig); err != nil {
		log.Errorf("Failed to apply %s", yamlFileName)
		return err
	}

	// install istio helm chart, which includes addon
	isSecurityOn := false
	if *authEnable {
		// enable mTLS
		isSecurityOn = true
	}

	// construct setValue to pass into helm install
	// mTLS
	setValue := "--set global.mtls.enabled=" + strconv.FormatBool(isSecurityOn)
	// side car injector
	if *useAutomaticInjection {
		setValue += " --set sidecarInjectorWebhook.enabled=true"
	}
	if *useMCP {
		setValue += " --set galley.enabled=true --set global.useMCP=true"
	} else if *useGalleyConfigValidator {
		setValue += " --set galley.enabled=true"
	}
	// hubs and tags replacement.
	// Helm chart assumes hub and tag are the same among multiple istio components.
	if *pilotHub != "" && *pilotTag != "" {
		setValue = setValue + " --set global.hub=" + *pilotHub + " --set global.tag=" + *pilotTag
	}

	if !*clusterWide {
		setValue += " --set istiotesting.oneNameSpace=true"
	}

	// CRDs installed ahead of time with 2.9.x
	setValue += " --set global.crds=false"

	err := util.HelmClientInit()
	if err != nil {
		// helm client init
		log.Errorf("Helm clienty init")
		return err
	}
	// Generate dependencies for Helm
	workDir := filepath.Join(k.ReleaseDir, istioHelmInstallDir)
	err = util.HelmDepUpdate(workDir)
	if err != nil {
		// helm dep upgrade
		log.Errorf("Helm dep update %s", workDir)
		return err
	}

	// helm install dry run - dry run seems to have problems
	// with CRDs even in 2.9.2, pre-install is not executed
	err = util.HelmInstallDryRun(workDir, k.Namespace, k.Namespace, setValue)
	if err != nil {
		// dry run fail, let's fail early
		log.Errorf("Helm dry run of istio install failed %s, setValue=%s", istioHelmInstallDir, setValue)
		return err
	}

	// helm install
	err = util.HelmInstall(workDir, k.Namespace, k.Namespace, setValue)
	if err != nil {
		log.Errorf("Helm install istio install failed %s, setValue=%s", istioHelmInstallDir, setValue)
		return err
	}

	return nil
}

func updateInjectImage(name, module, hub, tag string, content []byte) []byte {
	image := []byte(fmt.Sprintf("%s: %s/%s:%s", name, hub, module, tag))
	r := regexp.MustCompile(fmt.Sprintf("%s: .*(\\/%s):.*", name, module))
	return r.ReplaceAllLiteral(content, image)
}

func updateInjectVersion(version string, content []byte) []byte {
	versionLine := []byte(fmt.Sprintf("version: %s", version))
	r := regexp.MustCompile("version: .*")
	return r.ReplaceAllLiteral(content, versionLine)
}

func (k *KubeInfo) generateSidecarInjector(src, dst string) error {
	content, err := ioutil.ReadFile(src)
	if err != nil {
		log.Errorf("Cannot read original yaml file %s", src)
		return err
	}

	if !*clusterWide {
		content = replacePattern(content, istioSystem, k.Namespace)
	}

	if *pilotHub != "" && *pilotTag != "" {
		content = updateImage("sidecar_injector", *pilotHub, *pilotTag, content)
		content = updateInjectVersion(*pilotTag, content)
		content = updateInjectImage("initImage", "proxy_init", *proxyHub, *proxyTag, content)
		content = updateInjectImage("proxyImage", "proxy", *proxyHub, *proxyTag, content)
	}

	err = ioutil.WriteFile(dst, content, 0600)
	if err != nil {
		log.Errorf("Cannot write into generate sidecar injector file %s", dst)
	}
	return err
}

func (k *KubeInfo) generateGalleyConfigValidator(src, dst string) error {
	content, err := ioutil.ReadFile(src)
	if err != nil {
		log.Errorf("Cannot read original yaml file %s", src)
		return err
	}

	if !*clusterWide {
		content = replacePattern(content, istioSystem, k.Namespace)
	}

	if *galleyHub != "" && *galleyTag != "" {
		content = updateImage("galley", *galleyHub, *galleyTag, content)
	}

	err = ioutil.WriteFile(dst, content, 0600)
	if err != nil {
		log.Errorf("Cannot write into generate galley config validator %s", dst)
	}
	return err
}

func replacePattern(content []byte, src, dest string) []byte {
	r := []byte(dest)
	p := regexp.MustCompile(src)
	content = p.ReplaceAllLiteral(content, r)
	return content
}

func (k *KubeInfo) generateIstio(src, dst string) error {
	content, err := ioutil.ReadFile(src)
	if err != nil {
		log.Errorf("Cannot read original yaml file %s", src)
		return err
	}

	if !*clusterWide {
		content = replacePattern(content, istioSystem, k.Namespace)
		// Customize mixer's configStoreURL to limit watching resources in the testing namespace.
		vs := url.Values{}
		vs.Add("ns", *namespace)
		content = replacePattern(content, "--configStoreURL=k8s://", "--configStoreURL=k8s://?"+vs.Encode())
	}

	// Replace long refresh delays with short ones for the sake of tests.
	content = replacePattern(content, "connectTimeout: 10s", "connectTimeout: 1s")
	content = replacePattern(content, "drainDuration: 45s", "drainDuration: 2s")
	content = replacePattern(content, "parentShutdownDuration: 1m0s", "parentShutdownDuration: 3s")

	// A very flimsy and unreliable regexp to replace delays in ingress pod Spec
	content = replacePattern(content, "'10s' #connectTimeout", "'1s' #connectTimeout")
	content = replacePattern(content, "'45s' #drainDuration", "'2s' #drainDuration")
	content = replacePattern(content, "'1m0s' #parentShutdownDuration", "'3s' #parentShutdownDuration")

	if k.BaseVersion == "" {
		if *mixerHub != "" && *mixerTag != "" {
			content = updateImage("mixer", *mixerHub, *mixerTag, content)
		}
		if *pilotHub != "" && *pilotTag != "" {
			content = updateImage("pilot", *pilotHub, *pilotTag, content)
		}
		if *proxyHub != "" && *proxyTag != "" {
			//Need to be updated when the string "proxy" is changed as the default image name
			content = updateImage("proxy_init", *proxyHub, *proxyTag, content)
		}
		if *proxyHub != "" && *proxyTag != "" {
			//Need to be updated when the string "proxy" is changed as the default image name
			content = updateImage("proxyv2", *proxyHub, *proxyTag, content)
		}
		if *sidecarInjectorHub != "" && *sidecarInjectorTag != "" {
			//Need to be updated when the string "proxy" is changed as the default image name
			content = updateImage("sidecar_injector", *sidecarInjectorHub, *sidecarInjectorTag, content)
		}
		if *caHub != "" && *caTag != "" {
			//Need to be updated when the string "citadel" is changed
			content = updateImage("citadel", *caHub, *caTag, content)
		}
		if *galleyHub != "" && *galleyTag != "" {
			//Need to be updated when the string "citadel" is changed
			content = updateImage("galley", *galleyHub, *galleyTag, content)
		}
		if *imagePullPolicy != "" {
			content = updateImagePullPolicy(*imagePullPolicy, content)
		}
	}

	if *localCluster {
		content = []byte(strings.Replace(string(content), util.LoadBalancerServiceType,
			util.NodePortServiceType, 1))
	}

	err = ioutil.WriteFile(dst, content, 0600)
	if err != nil {
		log.Errorf("Cannot write into generated yaml file %s", dst)
	}
	return err
}

func updateImage(module, hub, tag string, content []byte) []byte {
	image := []byte(fmt.Sprintf("image: %s/%s:%s", hub, module, tag))
	r := regexp.MustCompile(fmt.Sprintf("image: .*(\\/%s):.*", module))
	return r.ReplaceAllLiteral(content, image)
}

func updateImagePullPolicy(policy string, content []byte) []byte {
	image := []byte(fmt.Sprintf("imagePullPolicy: %s", policy))
	r := regexp.MustCompile("imagePullPolicy:.*")
	return r.ReplaceAllLiteral(content, image)
}
