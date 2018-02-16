// Copyright 2017,2018 Istio Authors
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

package util

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"io/ioutil"
	"math/big"
	"os"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/ghodss/yaml"
	"k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/kube/inject"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/log"
)

const (
	ingressSecretName      = "istio-ingress-certs"
	sidecarInjectorService = "istio-sidecar-injector"
	mixerConfigFile        = "/etc/istio/proxy/envoy_mixer.json"
	mixerConfigAuthFile    = "/etc/istio/proxy/envoy_mixer_auth.json"
	pilotConfigFile        = "/etc/istio/proxy/envoy_pilot.json"
	pilotConfigAuthFile    = "/etc/istio/proxy/envoy_pilot_auth.json"
)

// Infra defines the test configuration.
type Infra struct { // nolint: maligned
	Name string

	KubeConfig string
	KubeClient kubernetes.Interface

	// Directory where test data files are located.
	testDataDir string

	// Directory where test cert files are located.
	certDir string

	// docker tags
	Hub, Tag string

	Namespace      string
	IstioNamespace string
	Registry       string
	Verbosity      int

	// map from app to pods
	Apps map[string][]string

	Auth                   meshconfig.MeshConfig_AuthPolicy
	ControlPlaneAuthPolicy meshconfig.AuthenticationPolicy
	MixerCustomConfigFile  string
	PilotCustomConfigFile  string

	// switches for infrastructure components
	Mixer     bool
	Ingress   bool
	Zipkin    bool
	DebugPort int

	APIVersions []string
	V1alpha1    bool
	V1alpha2    bool

	SkipCleanup          bool
	SkipCleanupOnFailure bool

	// check proxy logs
	CheckLogs bool

	// store error logs in specific directory
	ErrorLogsDir string

	// copy core files in this directory on the Kubernetes node machine
	CoreFilesDir string

	// The number of times to run each test.
	TestCount int

	// A user-specified test to run. If not provided, all tests will be run.
	SelectedTest string

	namespaceCreated      bool
	istioNamespaceCreated bool
	DebugImagesAndMode    bool

	// automatic sidecar injection
	UseAutomaticInjection bool
	SidecarTemplate       string
	meshConfig            *meshconfig.MeshConfig
	CABundle              string

	// External Admission Webhook for validation
	UseAdmissionWebhook  bool
	AdmissionServiceName string

	config model.IstioConfigStore

	Err error
}

const (
	// ConfigMapKey should match the expected MeshConfig file name
	ConfigMapKey = "mesh"
)

// NewInfra creates a new test configuration with default values.
func NewInfra() *Infra {
	return &Infra{
		Name:           "(default infra)",
		KubeConfig:     os.Getenv("KUBECONFIG"),
		testDataDir:    "testdata/",
		certDir:        "../../../../pilot/docker/certs/",
		Hub:            "gcr.io/istio-testing",
		Tag:            "",
		Namespace:      "",
		IstioNamespace: "",
		Registry:       string(serviceregistry.KubernetesRegistry),
		Verbosity:      2,
		Auth:           meshconfig.MeshConfig_NONE,
		MixerCustomConfigFile: mixerConfigFile,
		PilotCustomConfigFile: pilotConfigFile,
		Mixer:                 true,
		Ingress:               true,
		Zipkin:                true,
		DebugPort:             0,
		SkipCleanup:           false,
		SkipCleanupOnFailure:  false,
		CheckLogs:             false,
		ErrorLogsDir:          "",
		CoreFilesDir:          "",
		TestCount:             1,
		SelectedTest:          "",
		DebugImagesAndMode:    true,
		UseAutomaticInjection: false,
		UseAdmissionWebhook:   false,
		AdmissionServiceName:  "istio-pilot",
		V1alpha1:              false,
		V1alpha2:              true,
	}
}

// CopyWithDefaultAuth creates a copy of this configuration, but with mTLS enabled.
func (infra *Infra) CopyWithDefaultAuth() *Infra {
	// Make a copy of the config
	out := *infra
	out.Name = "(auth infra)"
	out.Auth = meshconfig.MeshConfig_MUTUAL_TLS
	out.ControlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS
	out.MixerCustomConfigFile = mixerConfigAuthFile
	out.PilotCustomConfigFile = pilotConfigAuthFile
	return &out
}

// GetMeshConfig fetches the ProxyMesh configuration from Kubernetes ConfigMap.
func GetMeshConfig(kube kubernetes.Interface, namespace, name string) (*v1.ConfigMap, *meshconfig.MeshConfig, error) { // nolint: lll
	config, err := kube.CoreV1().ConfigMaps(namespace).Get(name, meta_v1.GetOptions{})
	if err != nil {
		return nil, nil, err
	}

	// values in the data are strings, while proto might use a different data type.
	// therefore, we have to get a value by a key
	cfgYaml, exists := config.Data[ConfigMapKey]
	if !exists {
		return nil, nil, fmt.Errorf("missing configuration map key %q", ConfigMapKey)
	}

	mesh, err := model.ApplyMeshConfigDefaults(cfgYaml)
	if err != nil {
		return nil, nil, err
	}
	return config, mesh, nil
}

// Setup creates the k8s environment and deploys the test apps
func (infra *Infra) Setup() error {
	if infra.KubeConfig == "" {
		infra.KubeConfig = "pilot/pkg/kube/config"
		log.Info("Using linked in kube config. Set KUBECONFIG env before running the test.")
	}
	var err error
	if _, infra.KubeClient, err = kube.CreateInterface(infra.KubeConfig); err != nil {
		return err
	}

	crdclient, crderr := crd.NewClient(infra.KubeConfig, model.IstioConfigTypes, "")
	if crderr != nil {
		return crderr
	}
	if err = crdclient.RegisterResources(); err != nil {
		return err
	}

	infra.config = model.MakeIstioStore(crdclient)

	if infra.Namespace == "" {
		if infra.Namespace, err = util.CreateNamespaceWithPrefix(infra.KubeClient, "istio-test-app-", infra.UseAutomaticInjection); err != nil { // nolint: lll
			return err
		}
		infra.namespaceCreated = true
	} else {
		if _, err = infra.KubeClient.CoreV1().Namespaces().Get(infra.Namespace, meta_v1.GetOptions{}); err != nil {
			return err
		}
	}

	if infra.IstioNamespace == "" {
		if infra.IstioNamespace, err = util.CreateNamespaceWithPrefix(infra.KubeClient, "istio-test-", false); err != nil {
			return err
		}
		infra.istioNamespaceCreated = true
	} else {
		if _, err = infra.KubeClient.CoreV1().Namespaces().Get(infra.IstioNamespace, meta_v1.GetOptions{}); err != nil {
			return err
		}
	}

	deploy := func(name, namespace string) error {
		var filledYaml string
		if filledYaml, err = infra.Fill(name, infra); err != nil {
			return err
		} else if err = infra.KubeApply(filledYaml, namespace); err != nil {
			return err
		}
		return nil
	}
	if err = deploy("rbac-beta.yaml.tmpl", infra.IstioNamespace); err != nil {
		return err
	}

	if err = deploy("config.yaml.tmpl", infra.IstioNamespace); err != nil {
		return err
	}

	if _, infra.meshConfig, err = GetMeshConfig(infra.KubeClient, infra.IstioNamespace, "istio"); err != nil {
		return err
	}
	debugMode := infra.DebugImagesAndMode
	log.Infof("mesh %s", spew.Sdump(infra.meshConfig))

	infra.SidecarTemplate, err = inject.GenerateTemplateFromParams(&inject.Params{
		InitImage:       inject.InitImageName(infra.Hub, infra.Tag, debugMode),
		ProxyImage:      inject.ProxyImageName(infra.Hub, infra.Tag, debugMode),
		Verbosity:       infra.Verbosity,
		SidecarProxyUID: inject.DefaultSidecarProxyUID,
		EnableCoreDump:  true,
		Version:         "integration-test",
		Mesh:            infra.meshConfig,
		DebugMode:       debugMode,
	})
	if err != nil {
		return err
	}

	if infra.UseAutomaticInjection {
		if err = infra.createSidecarInjector(); err != nil {
			return err
		}
	}

	if infra.UseAdmissionWebhook {
		if err = infra.createAdmissionWebhookSecret(); err != nil {
			return err
		}
	}

	if err = deploy("pilot.yaml.tmpl", infra.IstioNamespace); err != nil {
		return err
	}
	if infra.Mixer {
		if err = deploy("mixer.yaml.tmpl", infra.IstioNamespace); err != nil {
			return err
		}
	}
	if serviceregistry.ServiceRegistry(infra.Registry) == serviceregistry.EurekaRegistry {
		if err = deploy("eureka.yaml.tmpl", infra.IstioNamespace); err != nil {
			return err
		}
	}

	if err = deploy("ca.yaml.tmpl", infra.IstioNamespace); err != nil {
		return err
	}
	if err = deploy("headless.yaml.tmpl", infra.Namespace); err != nil {
		return err
	}
	if infra.Ingress {
		if err = deploy("ingress-proxy.yaml.tmpl", infra.IstioNamespace); err != nil {
			return err
		}
		// Create ingress key/cert in secret
		var key []byte
		key, err = ioutil.ReadFile(infra.certDir + "cert.key")
		if err != nil {
			return err
		}
		var crt []byte
		crt, err = ioutil.ReadFile(infra.certDir + "cert.crt")
		if err != nil {
			return err
		}
		_, err = infra.KubeClient.CoreV1().Secrets(infra.IstioNamespace).Create(&v1.Secret{
			ObjectMeta: meta_v1.ObjectMeta{Name: ingressSecretName},
			Data: map[string][]byte{
				"tls.key": key,
				"tls.crt": crt,
			},
		})
		if err != nil {
			return err
		}
	}
	if infra.Zipkin {
		if err = deploy("zipkin.yaml", infra.IstioNamespace); err != nil {
			return err
		}
	}
	if err = deploy("external-wikipedia.yaml.tmpl", infra.Namespace); err != nil {
		return err
	}
	if err = deploy("externalbin.yaml.tmpl", infra.Namespace); err != nil {
		return err
	}

	if err = infra.deployApps(); err != nil {
		return err
	}

	nslist := []string{infra.IstioNamespace, infra.Namespace}
	infra.Apps, err = util.GetAppPods(infra.KubeClient, infra.KubeConfig, nslist)
	return err
}

func (infra *Infra) deployApps() error {
	// deploy a healthy mix of apps, with and without proxy
	if err := infra.deployApp("t", "t", 8080, 80, 9090, 90, 7070, 70, "unversioned", false, false); err != nil {
		return err
	}
	if err := infra.deployApp("a", "a", 8080, 80, 9090, 90, 7070, 70, "v1", true, false); err != nil {
		return err
	}
	if err := infra.deployApp("b", "b", 80, 8080, 90, 9090, 70, 7070, "unversioned", true, false); err != nil {
		return err
	}
	if err := infra.deployApp("c-v1", "c", 80, 8080, 90, 9090, 70, 7070, "v1", true, false); err != nil {
		return err
	}
	if err := infra.deployApp("c-v2", "c", 80, 8080, 90, 9090, 70, 7070, "v2", true, false); err != nil {
		return err
	}
	if err := infra.deployApp("d", "d", 80, 8080, 90, 9090, 70, 7070, "per-svc-auth", true, true); err != nil {
		return err
	}
	// Add another service without sidecar to test mTLS blacklisting (as in the e2e test
	// environment, pilot can see only services in the test namespaces). This service
	// will be listed in mtlsExcludedServices in the mesh config.
	return infra.deployApp("e", "fake-control", 80, 8080, 90, 9090, 70, 7070, "fake-control", false, false)
}

func (infra *Infra) deployApp(deployment, svcName string, port1, port2, port3, port4, port5, port6 int,
	version string, injectProxy bool, perServiceAuth bool) error {
	// Eureka does not support management ports
	healthPort := "true"
	if serviceregistry.ServiceRegistry(infra.Registry) == serviceregistry.EurekaRegistry {
		healthPort = "false"
	}

	w, err := infra.Fill("app.yaml.tmpl", map[string]string{
		"Hub":            infra.Hub,
		"Tag":            infra.Tag,
		"service":        svcName,
		"perServiceAuth": strconv.FormatBool(perServiceAuth),
		"deployment":     deployment,
		"port1":          strconv.Itoa(port1),
		"port2":          strconv.Itoa(port2),
		"port3":          strconv.Itoa(port3),
		"port4":          strconv.Itoa(port4),
		"port5":          strconv.Itoa(port5),
		"port6":          strconv.Itoa(port6),
		"version":        version,
		"istioNamespace": infra.IstioNamespace,
		"injectProxy":    strconv.FormatBool(injectProxy),
		"healthPort":     healthPort,
	})
	if err != nil {
		return err
	}

	writer := new(bytes.Buffer)

	if injectProxy && !infra.UseAutomaticInjection {
		if err := inject.IntoResourceFile(infra.SidecarTemplate, infra.meshConfig, strings.NewReader(w), writer); err != nil {
			return err
		}
	} else {
		if _, err := io.Copy(writer, strings.NewReader(w)); err != nil {
			return err
		}
	}

	return infra.KubeApply(writer.String(), infra.Namespace)
}

// Teardown cleans up the k8s environment, removing any resources that were created by the tests.
func (infra *Infra) Teardown() {
	if infra.KubeClient == nil {
		return
	}

	needToTeardown := !infra.SkipCleanup

	// spill all logs on error
	if infra.Err != nil {
		if infra.SkipCleanupOnFailure {
			needToTeardown = false
		}

		// Dump all logs on error.
		infra.dumpErrorLogs()
	}

	// If configured to cleanup after each test, do so now.
	if !needToTeardown {
		return
	}

	if infra.Ingress {
		if err := infra.KubeClient.ExtensionsV1beta1().Ingresses(infra.Namespace).
			DeleteCollection(&meta_v1.DeleteOptions{}, meta_v1.ListOptions{}); err != nil {
			log.Warna(err)
		}
		if err := infra.KubeClient.CoreV1().Secrets(infra.Namespace).
			Delete(ingressSecretName, &meta_v1.DeleteOptions{}); err != nil {
			log.Warna(err)
		}
	}

	if filledYaml, err := infra.Fill("rbac-beta.yaml.tmpl", infra); err != nil {
		log.Infof("RBAC template could could not be processed, please delete stale ClusterRoleBindings: %v",
			err)
	} else if err = infra.kubeDelete(filledYaml, infra.IstioNamespace); err != nil {
		log.Infof("RBAC config could could not be deleted: %v", err)
	}

	if infra.UseAdmissionWebhook {
		if err := infra.deleteAdmissionWebhookSecret(); err != nil {
			log.Infof("Could not delete admission webhook secret: %v", err)
		}
	}

	// automatic injection webhook is not namespaced.
	if infra.UseAutomaticInjection {
		infra.deleteSidecarInjector()
	}

	if infra.namespaceCreated {
		util.DeleteNamespace(infra.KubeClient, infra.Namespace)
		infra.Namespace = ""
	}
	if infra.istioNamespaceCreated {
		util.DeleteNamespace(infra.KubeClient, infra.IstioNamespace)
		infra.IstioNamespace = ""
	}
}

func (infra *Infra) dumpErrorLogs() {
	for _, pod := range util.GetPods(infra.KubeClient, infra.Namespace) {
		var filename, content string
		if strings.HasPrefix(pod, "istio-pilot") {
			Tlog("Discovery log", pod)
			filename = "istio-pilot"
			content = util.FetchLogs(infra.KubeClient, pod, infra.IstioNamespace, "discovery")
		} else if strings.HasPrefix(pod, "istio-mixer") {
			Tlog("Mixer log", pod)
			filename = "istio-mixer"
			content = util.FetchLogs(infra.KubeClient, pod, infra.IstioNamespace, "mixer")
		} else if strings.HasPrefix(pod, "istio-ingress") {
			Tlog("Ingress log", pod)
			filename = "istio-ingress"
			content = util.FetchLogs(infra.KubeClient, pod, infra.IstioNamespace, inject.ProxyContainerName)
		} else {
			Tlog("Proxy log", pod)
			filename = pod
			content = util.FetchLogs(infra.KubeClient, pod, infra.Namespace, inject.ProxyContainerName)
		}

		if len(infra.ErrorLogsDir) > 0 {
			if err := ioutil.WriteFile(infra.ErrorLogsDir+"/"+filename+".txt", []byte(content), 0644); err != nil {
				log.Errorf("Failed to save logs to %s:%s. Dumping on stderr\n", filename, err)
				log.Info(content)
			}
		} else {
			log.Info(content)
		}
	}
}

// KubeApply runs kubectl apply with the given yaml and namespace.
func (infra *Infra) KubeApply(yaml, namespace string) error {
	return util.RunInput(fmt.Sprintf("kubectl apply --kubeconfig %s -n %s -f -",
		infra.KubeConfig, namespace), yaml)
}

func (infra *Infra) kubeDelete(yaml, namespace string) error {
	return util.RunInput(fmt.Sprintf("kubectl delete --kubeconfig %s -n %s -f -",
		infra.KubeConfig, namespace), yaml)
}

// Response represents a response to a client request.
type Response struct {
	// Body is the body of the response
	Body string
	// ID is a unique identifier of the resource in the response
	ID []string
	// Version is the version of the resource in the response
	Version []string
	// Port is the port of the resource in the response
	Port []string
	// Code is the response code
	Code []string
}

const httpOk = "200"

// IsHTTPOk returns true if the response code was 200
func (r *Response) IsHTTPOk() bool {
	return len(r.Code) > 0 && r.Code[0] == httpOk
}

var (
	idRex      = regexp.MustCompile("(?i)X-Request-Id=(.*)")
	versionRex = regexp.MustCompile("ServiceVersion=(.*)")
	portRex    = regexp.MustCompile("ServicePort=(.*)")
	codeRex    = regexp.MustCompile("StatusCode=(.*)")
)

// ClientRequest makes the given request from within the k8s environment.
func (infra *Infra) ClientRequest(app, url string, count int, extra string) Response {
	out := Response{}
	if len(infra.Apps[app]) == 0 {
		log.Errorf("missing pod names for app %q", app)
		return out
	}

	pod := infra.Apps[app][0]
	cmd := fmt.Sprintf("kubectl exec %s --kubeconfig %s -n %s -c app -- client -url %s -count %d %s",
		pod, infra.KubeConfig, infra.Namespace, url, count, extra)
	request, err := util.Shell(cmd)

	if err != nil {
		log.Errorf("client request error %v for %s in %s", err, url, app)
		return out
	}

	out.Body = request

	ids := idRex.FindAllStringSubmatch(request, -1)
	for _, id := range ids {
		out.ID = append(out.ID, id[1])
	}

	versions := versionRex.FindAllStringSubmatch(request, -1)
	for _, version := range versions {
		out.Version = append(out.Version, version[1])
	}

	ports := portRex.FindAllStringSubmatch(request, -1)
	for _, port := range ports {
		out.Port = append(out.Port, port[1])
	}

	codes := codeRex.FindAllStringSubmatch(request, -1)
	for _, code := range codes {
		out.Code = append(out.Code, code[1])
	}

	return out
}

// ApplyConfig fills in the given template file (if necessary) and applies the configuration.
func (infra *Infra) ApplyConfig(inFile string, data map[string]string) error {
	config, err := infra.Fill(inFile, data)
	if err != nil {
		return err
	}

	vs, _, err := crd.ParseInputs(config)
	if err != nil {
		return err
	}

	for _, v := range vs {
		// fill up namespace for the config
		v.Namespace = infra.Namespace

		old, exists := infra.config.Get(v.Type, v.Name, v.Namespace)
		if exists {
			v.ResourceVersion = old.ResourceVersion
			_, err = infra.config.Update(v)
		} else {
			_, err = infra.config.Create(v)
		}
		if err != nil {
			return err
		}
	}

	sleepTime := time.Second * 3
	log.Infof("Sleeping %v for the config to propagate", sleepTime)
	time.Sleep(sleepTime)
	return nil
}

// DeleteConfig deletes the given configuration from the k8s environment
func (infra *Infra) DeleteConfig(inFile string, data map[string]string) error {
	config, err := infra.Fill(inFile, data)
	if err != nil {
		return err
	}

	vs, _, err := crd.ParseInputs(config)
	if err != nil {
		return err
	}

	for _, v := range vs {
		// fill up namespace for the config
		v.Namespace = infra.Namespace

		log.Infof("Delete config %s", v.Key())
		if err = infra.config.Delete(v.Type, v.Name, v.Namespace); err != nil {
			return err
		}
	}

	sleepTime := time.Second * 3
	log.Infof("Sleeping %v for the config to propagate", sleepTime)
	time.Sleep(sleepTime)
	return nil
}

// DeleteAllConfigs deletes any config resources that were installed by the tests.
func (infra *Infra) DeleteAllConfigs() error {
	for _, desc := range infra.config.ConfigDescriptor() {
		configs, err := infra.config.List(desc.Type, infra.Namespace)
		if err != nil {
			return err
		}
		for _, config := range configs {
			log.Infof("Delete config %s", config.Key())
			if err = infra.config.Delete(desc.Type, config.Name, config.Namespace); err != nil {
				return err
			}
		}
	}
	return nil
}

func createWebhookCerts(service, namespace string) (caCertPEM, serverCertPEM, serverKeyPEM []byte, err error) { // nolint: lll
	var (
		webhookCertValidFor = 365 * 24 * time.Hour
		rsaBits             = 2048
		maxSerialNumber     = new(big.Int).Lsh(big.NewInt(1), 128)

		notBefore = time.Now()
		notAfter  = notBefore.Add(webhookCertValidFor)
	)

	// Generate self-signed CA cert
	caKey, err := rsa.GenerateKey(rand.Reader, rsaBits)
	if err != nil {
		return nil, nil, nil, err
	}
	caSerialNumber, err := rand.Int(rand.Reader, maxSerialNumber)
	if err != nil {
		return nil, nil, nil, err
	}
	caTemplate := x509.Certificate{
		SerialNumber:          caSerialNumber,
		Subject:               pkix.Name{CommonName: fmt.Sprintf("%s_a", service)},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA: true,
	}
	caCert, err := x509.CreateCertificate(rand.Reader, &caTemplate, &caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, nil, err
	}

	// Generate server certificate signed by self-signed CA
	serverKey, err := rsa.GenerateKey(rand.Reader, rsaBits)
	if err != nil {
		return nil, nil, nil, err
	}
	serverSerialNumber, err := rand.Int(rand.Reader, maxSerialNumber)
	if err != nil {
		return nil, nil, nil, err
	}
	serverTemplate := x509.Certificate{
		SerialNumber: serverSerialNumber,
		Subject:      pkix.Name{CommonName: fmt.Sprintf("%s.%s.svc", service, namespace)},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	serverCert, err := x509.CreateCertificate(rand.Reader, &serverTemplate, &caTemplate, &serverKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, nil, err
	}

	// PEM encoding
	caCertPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCert})
	serverCertPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverCert})
	serverKeyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(serverKey)})

	return caCertPEM, serverCertPEM, serverKeyPEM, nil
}

func (infra *Infra) createAdmissionWebhookSecret() error {
	caCert, serverCert, serverKey, err := createWebhookCerts(infra.AdmissionServiceName, infra.IstioNamespace)
	if err != nil {
		return err
	}
	data := map[string]string{
		"webhookName": "pilot-webhook",
		"caCert":      base64.StdEncoding.EncodeToString(caCert),
		"serverCert":  base64.StdEncoding.EncodeToString(serverCert),
		"serverKey":   base64.StdEncoding.EncodeToString(serverKey),
	}
	filledYaml, err := infra.Fill("pilot-webhook-secret.yaml.tmpl", data)
	if err != nil {
		return err
	}
	return infra.KubeApply(filledYaml, infra.IstioNamespace)
}

func (infra *Infra) deleteAdmissionWebhookSecret() error {
	return util.Run(fmt.Sprintf("kubectl delete --kubeconfig %s -n %s secret pilot-webhook",
		infra.KubeConfig, infra.IstioNamespace))
}

func (infra *Infra) createSidecarInjector() error {
	configData, err := yaml.Marshal(&inject.Config{
		Policy:   inject.InjectionPolicyEnabled,
		Template: infra.SidecarTemplate,
	})
	if err != nil {
		return err
	}

	// sidecar configuration template
	if _, err = infra.KubeClient.CoreV1().ConfigMaps(infra.IstioNamespace).Create(&v1.ConfigMap{
		ObjectMeta: meta_v1.ObjectMeta{
			Name: "istio-inject",
		},
		Data: map[string]string{
			"config": string(configData),
		},
	}); err != nil {
		return err
	}

	// webhook certificates
	ca, cert, key, err := createWebhookCerts(sidecarInjectorService, infra.IstioNamespace) // nolint: vetshadow
	if err != nil {
		return err
	}
	if _, err := infra.KubeClient.CoreV1().Secrets(infra.IstioNamespace).Create(&v1.Secret{ // nolint: vetshadow
		ObjectMeta: meta_v1.ObjectMeta{Name: "sidecar-injector-certs"},
		Data: map[string][]byte{
			"cert.pem": cert,
			"key.pem":  key,
		},
		Type: v1.SecretTypeOpaque,
	}); err != nil {
		return err
	}

	// webhook deployment
	infra.CABundle = base64.StdEncoding.EncodeToString(ca)
	if filledYaml, err := infra.Fill("sidecar-injector.yaml.tmpl", infra); err != nil { // nolint: vetshadow
		return err
	} else if err = infra.KubeApply(filledYaml, infra.IstioNamespace); err != nil {
		return err
	}

	// wait until injection webhook service is running before
	// proceeding with deploying test applications
	if _, err = util.GetAppPods(infra.KubeClient, infra.KubeConfig, []string{infra.IstioNamespace}); err != nil {
		return fmt.Errorf("sidecar injector failed to start: %v", err)
	}
	return nil
}

func (infra *Infra) deleteSidecarInjector() {
	if filledYaml, err := infra.Fill("sidecar-injector.yaml.tmpl", infra); err != nil {
		log.Infof("Sidecar injector template could not be processed, please delete stale injector webhook: %v",
			err)
	} else if err = infra.kubeDelete(filledYaml, infra.IstioNamespace); err != nil {
		log.Infof("Sidecar injector could not be deleted: %v", err)
	}
}

// Fill fills in a template with the given values
func (infra *Infra) Fill(inFile string, values interface{}) (string, error) {
	var out bytes.Buffer
	w := bufio.NewWriter(&out)

	tmpl, err := template.ParseFiles(infra.testDataDir + inFile)
	if err != nil {
		return "", err
	}

	if err := tmpl.Execute(w, values); err != nil {
		return "", err
	}

	if err := w.Flush(); err != nil {
		return "", err
	}

	return out.String(), nil
}
