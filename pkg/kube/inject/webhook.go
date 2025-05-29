// Copyright Istio Authors
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

package inject

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	goversion "github.com/hashicorp/go-version"
	"github.com/prometheus/prometheus/util/strutil"
	"gomodules.xyz/jsonpatch/v2"
	admissionv1 "k8s.io/api/admission/v1"
	kubeApiAdmissionv1beta1 "k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	kjson "k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/mergepatch"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"sigs.k8s.io/yaml"

	"istio.io/api/annotation"
	"istio.io/api/label"
	meshconfig "istio.io/api/mesh/v1alpha1"
	opconfig "istio.io/istio/operator/pkg/apis"
	"istio.io/istio/pilot/cmd/pilot-agent/status"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kubetypes"
	"istio.io/istio/pkg/kube/multicluster"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/platform"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/util/sets"
)

var (
	runtimeScheme     = runtime.NewScheme()
	codecs            = serializer.NewCodecFactory(runtimeScheme)
	deserializer      = codecs.UniversalDeserializer()
	jsonSerializer    = kjson.NewSerializerWithOptions(kjson.DefaultMetaFactory, runtimeScheme, runtimeScheme, kjson.SerializerOptions{})
	URLParameterToEnv = map[string]string{
		"cluster": "ISTIO_META_CLUSTER_ID",
		"net":     "ISTIO_META_NETWORK",
	}
)

func init() {
	_ = corev1.AddToScheme(runtimeScheme)
	_ = admissionv1.AddToScheme(runtimeScheme)
	_ = kubeApiAdmissionv1beta1.AddToScheme(runtimeScheme)
}

const (
	// prometheus will convert annotation to this format
	// `prometheus.io/scrape` `prometheus.io.scrape` `prometheus-io/scrape` have the same meaning in Prometheus
	// for more details, please checkout [here](https://github.com/prometheus/prometheus/blob/71a0f42331566a8849863d77078083edbb0b3bc4/util/strutil/strconv.go#L40)
	prometheusScrapeAnnotation = "prometheus_io_scrape"
	prometheusPortAnnotation   = "prometheus_io_port"
	prometheusPathAnnotation   = "prometheus_io_path"

	watchDebounceDelay = 100 * time.Millisecond
)

const (
	// InitContainers is the name of the property in k8s pod spec
	InitContainers = "initContainers"

	// Containers is the name of the property in k8s pod spec
	Containers = "containers"
)

type WebhookConfig struct {
	Templates  Templates
	Values     ValuesConfig
	MeshConfig *meshconfig.MeshConfig
}

// Webhook implements a mutating webhook for automatic proxy injection.
type Webhook struct {
	mu           sync.RWMutex
	Config       *Config
	meshConfig   *meshconfig.MeshConfig
	valuesConfig ValuesConfig
	namespaces   *multicluster.KclientComponent[*corev1.Namespace]
	nodes        *multicluster.KclientComponent[*corev1.Node]

	// please do not call SetHandler() on this watcher, instead us MultiCast.AddHandler()
	watcher   Watcher
	MultiCast *WatcherMulticast

	env      *model.Environment
	revision string
}

func (wh *Webhook) GetConfig() WebhookConfig {
	wh.mu.RLock()
	defer wh.mu.RUnlock()
	return WebhookConfig{
		Templates:  wh.Config.Templates,
		Values:     wh.valuesConfig,
		MeshConfig: wh.meshConfig,
	}
}

// ParsedContainers holds the unmarshalled containers and initContainers
type ParsedContainers struct {
	Containers     []corev1.Container `json:"containers,omitempty"`
	InitContainers []corev1.Container `json:"initContainers,omitempty"`
}

func (p ParsedContainers) AllContainers() []corev1.Container {
	return append(slices.Clone(p.Containers), p.InitContainers...)
}

// nolint directives: interfacer
func loadConfig(injectFile, valuesFile string) (*Config, string, error) {
	data, err := os.ReadFile(injectFile)
	if err != nil {
		return nil, "", err
	}
	var c *Config
	if c, err = unmarshalConfig(data); err != nil {
		log.Warnf("Failed to parse injectFile %s", string(data))
		return nil, "", err
	}

	valuesConfig, err := os.ReadFile(valuesFile)
	if err != nil {
		return nil, "", err
	}
	return c, string(valuesConfig), nil
}

func unmarshalConfig(data []byte) (*Config, error) {
	c, err := UnmarshalConfig(data)
	if err != nil {
		return nil, err
	}

	log.Debugf("New inject configuration: sha256sum %x", sha256.Sum256(data))
	log.Debugf("Policy: %v", c.Policy)
	log.Debugf("AlwaysInjectSelector: %v", c.AlwaysInjectSelector)
	log.Debugf("NeverInjectSelector: %v", c.NeverInjectSelector)
	log.Debugf("Templates: %v", c.RawTemplates)
	return &c, nil
}

// WebhookParameters configures parameters for the sidecar injection
// webhook.
type WebhookParameters struct {
	// Watcher watches the sidecar injection configuration.
	Watcher Watcher

	// Port is the webhook port, e.g. typically 443 for https.
	// This is mainly used for tests. Webhook runs on the port started by Istiod.
	Port int

	Env *model.Environment

	// Use an existing mux instead of creating our own.
	Mux *http.ServeMux

	// The istio.io/rev this injector is responsible for
	Revision string

	// MultiCluster is used to access namespaces across clusters
	MultiCluster multicluster.ComponentBuilder
}

// NewWebhook creates a new instance of a mutating webhook for automatic sidecar injection.
func NewWebhook(p WebhookParameters) (*Webhook, error) {
	if p.Mux == nil {
		return nil, errors.New("expected mux to be passed, but was not passed")
	}

	wh := &Webhook{
		watcher:    p.Watcher,
		meshConfig: p.Env.Mesh(),
		env:        p.Env,
		revision:   p.Revision,
	}

	if p.MultiCluster != nil {
		if platform.IsOpenShift() {
			wh.namespaces = multicluster.BuildMultiClusterKclientComponent[*corev1.Namespace](p.MultiCluster, kubetypes.Filter{})
		}
	}

	if features.EnableNativeSidecars {
		wh.nodes = multicluster.BuildMultiClusterKclientComponent[*corev1.Node](p.MultiCluster, kubetypes.Filter{})
	}

	mc := NewMulticast(p.Watcher, wh.GetConfig)
	mc.AddHandler(wh.updateConfig)
	wh.MultiCast = mc
	sidecarConfig, valuesConfig, err := p.Watcher.Get()
	if err != nil {
		return nil, fmt.Errorf("failed to get initial configuration: %v", err)
	}
	if err := wh.updateConfig(sidecarConfig, valuesConfig); err != nil {
		return nil, fmt.Errorf("failed to process webhook config: %v", err)
	}

	p.Mux.HandleFunc("/inject", wh.serveInject)
	p.Mux.HandleFunc("/inject/", wh.serveInject)

	p.Env.Watcher.AddMeshHandler(func() {
		wh.mu.Lock()
		wh.meshConfig = p.Env.Mesh()
		wh.mu.Unlock()
	})

	return wh, nil
}

// Run implements the webhook server
func (wh *Webhook) Run(stop <-chan struct{}) {
	go wh.watcher.Run(stop)
}

func (wh *Webhook) updateConfig(sidecarConfig *Config, valuesConfig string) error {
	wh.mu.Lock()
	defer wh.mu.Unlock()
	wh.Config = sidecarConfig
	vc, err := NewValuesConfig(valuesConfig)
	if err != nil {
		return fmt.Errorf("failed to create new values config: %v", err)
	}
	wh.valuesConfig = vc
	return nil
}

type ContainerReorder int

const (
	MoveFirst ContainerReorder = iota
	MoveLast
	Remove
)

func moveContainer(from, to []corev1.Container, name string) ([]corev1.Container, []corev1.Container) {
	var container *corev1.Container
	for i, c := range from {
		if from[i].Name == name {
			from = slices.Delete(from, i)
			container = &c
			break
		}
	}
	if container != nil {
		to = append(to, *container)
	}
	return from, to
}

func modifyContainers(cl []corev1.Container, name string, modifier ContainerReorder) []corev1.Container {
	containers := []corev1.Container{}
	var match *corev1.Container
	for _, c := range cl {
		if c.Name != name {
			containers = append(containers, c)
		} else {
			match = &c
		}
	}
	if match == nil {
		return containers
	}
	switch modifier {
	case MoveFirst:
		return append([]corev1.Container{*match}, containers...)
	case MoveLast:
		return append(containers, *match)
	case Remove:
		return containers
	default:
		return cl
	}
}

func hasContainer(cl []corev1.Container, name string) bool {
	for _, c := range cl {
		if c.Name == name {
			return true
		}
	}
	return false
}

func enablePrometheusMerge(mesh *meshconfig.MeshConfig, anno map[string]string) bool {
	// If annotation is present, we look there first
	if val, f := anno[annotation.PrometheusMergeMetrics.Name]; f {
		bval, err := strconv.ParseBool(val)
		if err != nil {
			// This shouldn't happen since we validate earlier in the code
			log.Warnf("invalid annotation %v=%v", annotation.PrometheusMergeMetrics.Name, bval)
		} else {
			return bval
		}
	}
	// If mesh config setting is present, use that
	if mesh.GetEnablePrometheusMerge() != nil {
		return mesh.GetEnablePrometheusMerge().Value
	}
	// Otherwise, we default to enable
	return true
}

func toAdmissionResponse(err error) *kube.AdmissionResponse {
	return &kube.AdmissionResponse{Result: &metav1.Status{Message: err.Error()}}
}

func ParseTemplates(tmpls RawTemplates) (Templates, error) {
	ret := make(Templates, len(tmpls))
	for k, t := range tmpls {
		p, err := parseDryTemplate(t, InjectionFuncmap)
		if err != nil {
			return nil, err
		}
		ret[k] = p
	}
	return ret, nil
}

type ValuesConfig struct {
	raw      string
	asStruct *opconfig.Values
	asMap    map[string]any
}

func (v ValuesConfig) Struct() *opconfig.Values {
	return v.asStruct
}

func (v ValuesConfig) Map() map[string]any {
	return v.asMap
}

func NewValuesConfig(v string) (ValuesConfig, error) {
	c := ValuesConfig{raw: v}
	valuesStruct := &opconfig.Values{}
	if err := protomarshal.ApplyYAML(v, valuesStruct); err != nil {
		return c, fmt.Errorf("could not parse configuration values: %v", err)
	}
	c.asStruct = valuesStruct

	values := map[string]any{}
	if err := yaml.Unmarshal([]byte(v), &values); err != nil {
		return c, fmt.Errorf("could not parse configuration values: %v", err)
	}
	c.asMap = values
	return c, nil
}

type InjectionParameters struct {
	pod                 *corev1.Pod
	deployMeta          types.NamespacedName
	namespace           *corev1.Namespace
	nativeSidecar       bool
	typeMeta            metav1.TypeMeta
	templates           map[string]*template.Template
	defaultTemplate     []string
	aliases             map[string][]string
	meshConfig          *meshconfig.MeshConfig
	proxyConfig         *meshconfig.ProxyConfig
	valuesConfig        ValuesConfig
	revision            string
	proxyEnvs           map[string]string
	injectedAnnotations map[string]string
}

func checkPreconditions(params InjectionParameters) {
	spec := params.pod.Spec
	metadata := params.pod.ObjectMeta
	// If DNSPolicy is not ClusterFirst, the Envoy sidecar may not able to connect to Istio Pilot.
	if spec.DNSPolicy != "" && spec.DNSPolicy != corev1.DNSClusterFirst {
		podName := potentialPodName(metadata)
		log.Warnf("%q's DNSPolicy is not %q. The Envoy sidecar may not able to connect to Istio Pilot",
			metadata.Namespace+"/"+podName, corev1.DNSClusterFirst)
	}
}

func getInjectionStatus(podSpec corev1.PodSpec, revision string) string {
	stat := &SidecarInjectionStatus{}
	for _, c := range podSpec.InitContainers {
		stat.InitContainers = append(stat.InitContainers, c.Name)
	}
	for _, c := range podSpec.Containers {
		stat.Containers = append(stat.Containers, c.Name)
	}
	for _, c := range podSpec.Volumes {
		stat.Volumes = append(stat.Volumes, c.Name)
	}
	for _, c := range podSpec.ImagePullSecrets {
		stat.ImagePullSecrets = append(stat.ImagePullSecrets, c.Name)
	}
	// Rather than setting istio.io/rev label on injected pods include them here in status annotation.
	// This keeps us from overwriting the istio.io/rev label when using revision tags (i.e. istio.io/rev=<tag>).
	if revision == "" {
		revision = "default"
	}
	stat.Revision = revision
	statusAnnotationValue, err := json.Marshal(stat)
	if err != nil {
		return "{}"
	}
	return string(statusAnnotationValue)
}

// injectPod is the core of the injection logic. This takes a pod and injection
// template, as well as some inputs to the injection template, and produces a
// JSON patch.
//
// In the webhook, we will receive a Pod directly from Kubernetes, and return the
// patch directly; Kubernetes will take care of applying the patch.
//
// For kube-inject, we will parse out a Pod from YAML (which may involve
// extraction from higher level types like Deployment), then apply the patch
// locally.
//
// The injection logic works by first applying the rendered injection template on
// top of the input pod This is done using a Strategic Patch Merge
// (https://github.com/kubernetes/community/blob/master/contributors/devel/sig-api-machinery/strategic-merge-patch.md)
// Currently only a single template is supported, although in the future the template to use will be configurable
// and multiple templates will be supported by applying them in successive order.
//
// In addition to the plain templating, there is some post processing done to
// handle cases that cannot feasibly be covered in the template, such as
// re-ordering pods, rewriting readiness probes, etc.
func injectPod(req InjectionParameters) ([]byte, error) {
	checkPreconditions(req)

	// The patch will be built relative to the initial pod, capture its current state
	originalPodSpec, err := json.Marshal(req.pod)
	if err != nil {
		return nil, err
	}

	// Run the injection template, giving us a partial pod spec
	mergedPod, injectedPodData, err := RunTemplate(req)
	if err != nil {
		return nil, fmt.Errorf("failed to run injection template: %v", err)
	}

	mergedPod, err = reapplyOverwrittenContainers(mergedPod, req.pod, injectedPodData, req.proxyConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to re apply container: %v", err)
	}

	// Apply some additional transformations to the pod
	if err := postProcessPod(mergedPod, *injectedPodData, req); err != nil {
		return nil, fmt.Errorf("failed to process pod: %v", err)
	}

	patch, err := createPatch(mergedPod, originalPodSpec)
	if err != nil {
		return nil, fmt.Errorf("failed to create patch: %v", err)
	}

	log.Debugf("AdmissionResponse: patch=%v\n", string(patch))
	return patch, nil
}

// reapplyOverwrittenContainers enables users to provide container level overrides for settings in the injection template
// * originalPod: the pod before injection. If needed, we will apply some configurations from this pod on top of the final pod
// * templatePod: the rendered injection template. This is needed only to see what containers we injected
// * finalPod: the current result of injection, roughly equivalent to the merging of originalPod and templatePod
// There are essentially three cases we cover here:
//  1. There is no overlap in containers in original and template pod. We will do nothing.
//  2. There is an overlap (ie, both define istio-proxy), but that is because the pod is being re-injected.
//     In this case we do nothing, since we want to apply the new settings
//  3. There is an overlap. We will re-apply the original container.
//
// Where "overlap" is a container defined in both the original and template pod. Typically, this would mean
// the user has defined an `istio-proxy` container in their own pod spec.
func reapplyOverwrittenContainers(finalPod *corev1.Pod, originalPod *corev1.Pod, templatePod *corev1.Pod,
	proxyConfig *meshconfig.ProxyConfig,
) (*corev1.Pod, error) {
	overrides := ParsedContainers{}
	existingOverrides := ParsedContainers{}
	if annotationOverrides, f := originalPod.Annotations[annotation.ProxyOverrides.Name]; f {
		if err := json.Unmarshal([]byte(annotationOverrides), &existingOverrides); err != nil {
			return nil, err
		}
	}
	parsedInjectedStatus := ParsedContainers{}
	status, alreadyInjected := originalPod.Annotations[annotation.SidecarStatus.Name]
	if alreadyInjected {
		parsedInjectedStatus = parseStatus(status)
	}
	for _, c := range templatePod.Spec.Containers {
		// sidecarStatus annotation is added on the pod by webhook. We should use new container template
		// instead of restoring what may be previously injected. Doing this ensures we are correctly calculating
		// env variables like ISTIO_META_APP_CONTAINERS and ISTIO_META_POD_PORTS.
		if match := FindContainer(c.Name, parsedInjectedStatus.Containers); match != nil {
			continue
		}
		match := FindContainer(c.Name, existingOverrides.Containers)
		if match == nil {
			match = FindContainer(c.Name, originalPod.Spec.Containers)
		}
		if match == nil {
			continue
		}
		overlay := *match.DeepCopy()
		if overlay.Image == AutoImage {
			overlay.Image = ""
		}

		overrides.Containers = append(overrides.Containers, overlay)
		newMergedPod, err := applyContainer(finalPod, overlay)
		if err != nil {
			return nil, fmt.Errorf("failed to apply sidecar container: %v", err)
		}
		finalPod = newMergedPod
	}
	for _, c := range templatePod.Spec.InitContainers {
		if match := FindContainer(c.Name, parsedInjectedStatus.InitContainers); match != nil {
			continue
		}
		match := FindContainer(c.Name, existingOverrides.InitContainers)
		if match == nil {
			match = FindContainerFromPod(c.Name, originalPod)
		}
		if match == nil {
			continue
		}
		overlay := *match.DeepCopy()
		if overlay.Image == AutoImage {
			overlay.Image = ""
		}

		overrides.InitContainers = append(overrides.InitContainers, overlay)
		newMergedPod, err := applyInitContainer(finalPod, overlay)
		if err != nil {
			return nil, fmt.Errorf("failed to apply sidecar init container: %v", err)
		}
		finalPod = newMergedPod
	}

	if !alreadyInjected && (len(overrides.Containers) > 0 || len(overrides.InitContainers) > 0) {
		// We found any overrides. Put them in the pod annotation so we can re-apply them on re-injection
		js, err := json.Marshal(overrides)
		if err != nil {
			return nil, err
		}
		if finalPod.Annotations == nil {
			finalPod.Annotations = map[string]string{}
		}
		finalPod.Annotations[annotation.ProxyOverrides.Name] = string(js)
	}

	adjustInitContainerUser(finalPod, originalPod, proxyConfig)

	return finalPod, nil
}

// adjustInitContainerUser adjusts the RunAsUser/Group fields and iptables parameter "-u <uid>"
// in the init/validation container so that they match the value of SecurityContext.RunAsUser/Group
// when it is present in the custom istio-proxy container supplied by the user.
func adjustInitContainerUser(finalPod *corev1.Pod, originalPod *corev1.Pod, proxyConfig *meshconfig.ProxyConfig) {
	userContainer := FindSidecar(originalPod)
	if userContainer == nil {
		// if user doesn't override the istio-proxy container, there's nothing to do
		return
	}

	if userContainer.SecurityContext == nil || (userContainer.SecurityContext.RunAsUser == nil && userContainer.SecurityContext.RunAsGroup == nil) {
		// if user doesn't override SecurityContext.RunAsUser/Group, there's nothing to do
		return
	}

	// Locate the istio-init or istio-validation container
	var initContainer *corev1.Container
	for _, name := range []string{InitContainerName, ValidationContainerName} {
		if container := FindContainer(name, finalPod.Spec.InitContainers); container != nil {
			initContainer = container
			break
		}
	}
	if initContainer == nil {
		// should not happen
		log.Warn("Could not find either istio-init or istio-validation container")
		return
	}

	// Overriding RunAsUser is now allowed in TPROXY mode, it must always run with uid=0
	tproxy := false
	if proxyConfig.InterceptionMode == meshconfig.ProxyConfig_TPROXY {
		tproxy = true
	} else if mode, found := finalPod.Annotations[annotation.SidecarInterceptionMode.Name]; found && mode == "TPROXY" {
		tproxy = true
	}

	// RunAsUser cannot be overridden (ie, must remain 0) in TPROXY mode
	if tproxy && userContainer.SecurityContext.RunAsUser != nil {
		sidecar := FindSidecar(finalPod)
		if sidecar == nil {
			// Should not happen
			log.Warn("Could not find the istio-proxy container")
			return
		}
		*sidecar.SecurityContext.RunAsUser = 0
	}

	// Make sure the validation container runs with the same uid/gid as the proxy (init container is untouched, it must run with 0)
	if !tproxy && initContainer.Name == ValidationContainerName {
		if initContainer.SecurityContext == nil {
			initContainer.SecurityContext = &corev1.SecurityContext{}
		}
		if userContainer.SecurityContext.RunAsUser != nil {
			initContainer.SecurityContext.RunAsUser = userContainer.SecurityContext.RunAsUser
		}
		if userContainer.SecurityContext.RunAsGroup != nil {
			initContainer.SecurityContext.RunAsGroup = userContainer.SecurityContext.RunAsGroup
		}
	}

	// Find the "-u <uid>" parameter in the init container and replace it with the userid from SecurityContext.RunAsUser
	// but only if it's not 0. iptables --uid-owner argument must not be 0.
	if userContainer.SecurityContext.RunAsUser == nil || *userContainer.SecurityContext.RunAsUser == 0 {
		return
	}
	for i := range initContainer.Args {
		if initContainer.Args[i] == "-u" {
			initContainer.Args[i+1] = fmt.Sprintf("%d", *userContainer.SecurityContext.RunAsUser)
			return
		}
	}
}

// parseStatus extracts containers from injected SidecarStatus annotation
func parseStatus(status string) ParsedContainers {
	parsedContainers := ParsedContainers{}
	var unMarshalledStatus map[string]interface{}
	if err := json.Unmarshal([]byte(status), &unMarshalledStatus); err != nil {
		log.Errorf("Failed to unmarshal %s : %v", annotation.SidecarStatus.Name, err)
		return parsedContainers
	}
	parser := func(key string, obj map[string]interface{}) []corev1.Container {
		out := make([]corev1.Container, 0)
		if value, exist := obj[key]; exist && value != nil {
			for _, v := range value.([]interface{}) {
				out = append(out, corev1.Container{Name: v.(string)})
			}
		}
		return out
	}
	parsedContainers.Containers = parser(Containers, unMarshalledStatus)
	parsedContainers.InitContainers = parser(InitContainers, unMarshalledStatus)

	return parsedContainers
}

// reinsertOverrides applies the containers listed in OverrideAnnotation to a pod. This is to achieve
// idempotency by handling an edge case where an injection template is modifying a container already
// present in the pod spec. In these cases, the logic to strip injected containers would remove the
// original injected parts as well, leading to the templating logic being different (for example,
// reading the .Spec.Containers field would be empty).
func reinsertOverrides(pod *corev1.Pod) (*corev1.Pod, error) {
	type podOverrides struct {
		Containers     []corev1.Container `json:"containers,omitempty"`
		InitContainers []corev1.Container `json:"initContainers,omitempty"`
	}

	existingOverrides := podOverrides{}
	if annotationOverrides, f := pod.Annotations[annotation.ProxyOverrides.Name]; f {
		if err := json.Unmarshal([]byte(annotationOverrides), &existingOverrides); err != nil {
			return nil, err
		}
	}

	pod = pod.DeepCopy()
	for _, c := range existingOverrides.Containers {
		match := FindContainer(c.Name, pod.Spec.Containers)
		if match != nil {
			continue
		}
		pod.Spec.Containers = append(pod.Spec.Containers, c)
	}

	for _, c := range existingOverrides.InitContainers {
		match := FindContainer(c.Name, pod.Spec.InitContainers)
		if match != nil {
			continue
		}
		pod.Spec.InitContainers = append(pod.Spec.InitContainers, c)
	}

	return pod, nil
}

func createPatch(pod *corev1.Pod, original []byte) ([]byte, error) {
	reinjected, err := json.Marshal(pod)
	if err != nil {
		return nil, err
	}
	p, err := jsonpatch.CreatePatch(original, reinjected)
	if err != nil {
		return nil, err
	}
	return json.Marshal(p)
}

// postProcessPod applies additionally transformations to the pod after merging with the injected template
// This is generally things that cannot reasonably be added to the template
func postProcessPod(pod *corev1.Pod, injectedPod corev1.Pod, req InjectionParameters) error {
	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}
	if pod.Labels == nil {
		pod.Labels = map[string]string{}
	}

	overwriteClusterInfo(pod, req)

	if err := applyPrometheusMerge(pod, req.meshConfig); err != nil {
		return err
	}

	if err := applyRewrite(pod, req); err != nil {
		return err
	}

	applyMetadata(pod, injectedPod, req)

	if err := reorderPod(pod, req); err != nil {
		return err
	}

	return nil
}

func applyMetadata(pod *corev1.Pod, injectedPodData corev1.Pod, req InjectionParameters) {
	if nw, ok := req.proxyEnvs["ISTIO_META_NETWORK"]; ok {
		pod.Labels[label.TopologyNetwork.Name] = nw
	}
	// Add all additional injected annotations. These are overridden if needed
	pod.Annotations[annotation.SidecarStatus.Name] = getInjectionStatus(injectedPodData.Spec, req.revision)

	// Deprecated; should be set directly in the template instead
	for k, v := range req.injectedAnnotations {
		pod.Annotations[k] = v
	}
}

// reorderPod ensures containers are properly ordered after merging
func reorderPod(pod *corev1.Pod, req InjectionParameters) error {
	var merr error
	mc := req.meshConfig
	// Get copy of pod proxyconfig, to determine container ordering
	if pca, f := req.pod.ObjectMeta.GetAnnotations()[annotation.ProxyConfig.Name]; f {
		mc, merr = mesh.ApplyProxyConfig(pca, req.meshConfig)
		if merr != nil {
			return merr
		}
	}

	// nolint: staticcheck
	holdPod := mc.GetDefaultConfig().GetHoldApplicationUntilProxyStarts().GetValue() ||
		req.valuesConfig.asStruct.GetGlobal().GetProxy().GetHoldApplicationUntilProxyStarts().GetValue()

	proxyLocation := MoveLast
	// If HoldApplicationUntilProxyStarts is set, reorder the proxy location
	if holdPod {
		proxyLocation = MoveFirst
	}

	// Proxy container should be last, unless HoldApplicationUntilProxyStarts is set
	// This is to ensure `kubectl exec` and similar commands continue to default to the user's container
	pod.Spec.Containers = modifyContainers(pod.Spec.Containers, ProxyContainerName, proxyLocation)

	if hasContainer(pod.Spec.InitContainers, ProxyContainerName) {
		// This is using native sidecar support in K8s.
		// We want istio to be first in this case, so init containers are part of the mesh
		// This is {istio-init/istio-validation} => proxy => rest.
		pod.Spec.InitContainers = modifyContainers(pod.Spec.InitContainers, EnableCoreDumpName, MoveFirst)
		pod.Spec.InitContainers = modifyContainers(pod.Spec.InitContainers, ProxyContainerName, MoveFirst)
		pod.Spec.InitContainers = modifyContainers(pod.Spec.InitContainers, ValidationContainerName, MoveFirst)
		pod.Spec.InitContainers = modifyContainers(pod.Spec.InitContainers, InitContainerName, MoveFirst)
	} else {
		// Else, we want iptables setup last so we do not blackhole init containers
		// This is istio-validation => rest => istio-init (note: only one of istio-init or istio-validation should be present)
		// Validation container must be first to block any user containers
		pod.Spec.InitContainers = modifyContainers(pod.Spec.InitContainers, ValidationContainerName, MoveFirst)
		// Init container must be last to allow any traffic to pass before iptables is setup
		pod.Spec.InitContainers = modifyContainers(pod.Spec.InitContainers, InitContainerName, MoveLast)
		pod.Spec.InitContainers = modifyContainers(pod.Spec.InitContainers, EnableCoreDumpName, MoveLast)
	}

	return nil
}

func applyRewrite(pod *corev1.Pod, req InjectionParameters) error {
	sidecar := FindSidecar(pod)
	if sidecar == nil {
		return nil
	}

	rewrite := ShouldRewriteAppHTTPProbers(pod.Annotations, req.valuesConfig.asStruct.GetSidecarInjectorWebhook().GetRewriteAppHTTPProbe().GetValue())
	// We don't have to escape json encoding here when using golang libraries.
	if rewrite {
		if prober := DumpAppProbers(pod, req.meshConfig.GetDefaultConfig().GetStatusPort()); prober != "" {
			// If sidecar.istio.io/status is not present then append instead of merge.
			_, previouslyInjected := pod.Annotations[annotation.SidecarStatus.Name]
			sidecar.Env = mergeOrAppendProbers(previouslyInjected, sidecar.Env, prober)
		}
		patchRewriteProbe(pod.Annotations, pod, req.meshConfig.GetDefaultConfig().GetStatusPort())
	}
	return nil
}

// mergeOrAppendProbers ensures that if sidecar has existing ISTIO_KUBE_APP_PROBERS,
// then probers should be merged.
func mergeOrAppendProbers(previouslyInjected bool, envVars []corev1.EnvVar, newProbers string) []corev1.EnvVar {
	if !previouslyInjected {
		return append(envVars, corev1.EnvVar{Name: status.KubeAppProberEnvName, Value: newProbers})
	}
	for idx, env := range envVars {
		if env.Name == status.KubeAppProberEnvName {
			var existingKubeAppProber KubeAppProbers
			err := json.Unmarshal([]byte(env.Value), &existingKubeAppProber)
			if err != nil {
				log.Errorf("failed to unmarshal existing kubeAppProbers %v", err)
				return envVars
			}
			var newKubeAppProber KubeAppProbers
			err = json.Unmarshal([]byte(newProbers), &newKubeAppProber)
			if err != nil {
				log.Errorf("failed to unmarshal new kubeAppProbers %v", err)
				return envVars
			}
			for k, v := range existingKubeAppProber {
				// merge old and new probers.
				newKubeAppProber[k] = v
			}
			marshalledKubeAppProber, err := json.Marshal(newKubeAppProber)
			if err != nil {
				log.Errorf("failed to serialize the merged app prober config %v", err)
				return envVars
			}
			// replace old env var with new value.
			envVars[idx] = corev1.EnvVar{Name: status.KubeAppProberEnvName, Value: string(marshalledKubeAppProber)}
			return envVars
		}
	}
	return append(envVars, corev1.EnvVar{Name: status.KubeAppProberEnvName, Value: newProbers})
}

var emptyScrape = status.PrometheusScrapeConfiguration{}

// applyPrometheusMerge configures prometheus scraping annotations for the "metrics merge" feature.
// This moves the current prometheus.io annotations into an environment variable and replaces them
// pointing to the agent.
func applyPrometheusMerge(pod *corev1.Pod, mesh *meshconfig.MeshConfig) error {
	if getPrometheusScrape(pod) &&
		enablePrometheusMerge(mesh, pod.ObjectMeta.Annotations) {
		targetPort := strconv.Itoa(int(mesh.GetDefaultConfig().GetStatusPort()))
		if cur, f := getPrometheusPort(pod); f {
			// We have already set the port, assume user is controlling this or, more likely, re-injected
			// the pod.
			if cur == targetPort {
				return nil
			}
		}
		scrape := getPrometheusScrapeConfiguration(pod)
		sidecar := FindSidecar(pod)
		if sidecar != nil && scrape != emptyScrape {
			by, err := json.Marshal(scrape)
			if err != nil {
				return err
			}
			sidecar.Env = append(sidecar.Env, corev1.EnvVar{Name: status.PrometheusScrapingConfig.Name, Value: string(by)})
		}
		if pod.Annotations == nil {
			pod.Annotations = map[string]string{}
		}
		// if a user sets `prometheus/io/path: foo`, then we add `prometheus.io/path: /stats/prometheus`
		// prometheus will pick a random one
		// need to clear out all variants and then set ours
		clearPrometheusAnnotations(pod)
		pod.Annotations["prometheus.io/port"] = targetPort
		pod.Annotations["prometheus.io/path"] = "/stats/prometheus"
		pod.Annotations["prometheus.io/scrape"] = "true"
		return nil
	}

	return nil
}

// getPrometheusScrape respect prometheus scrape config
// not to doing prometheusMerge if this return false
func getPrometheusScrape(pod *corev1.Pod) bool {
	for k, val := range pod.Annotations {
		if strutil.SanitizeLabelName(k) != prometheusScrapeAnnotation {
			continue
		}

		if scrape, err := strconv.ParseBool(val); err == nil {
			return scrape
		}
	}

	return true
}

var prometheusAnnotations = sets.New(
	prometheusPathAnnotation,
	prometheusPortAnnotation,
	prometheusScrapeAnnotation,
)

func clearPrometheusAnnotations(pod *corev1.Pod) {
	needRemovedKeys := make([]string, 0, 2)
	for k := range pod.Annotations {
		anno := strutil.SanitizeLabelName(k)
		if prometheusAnnotations.Contains(anno) {
			needRemovedKeys = append(needRemovedKeys, k)
		}
	}

	for _, k := range needRemovedKeys {
		delete(pod.Annotations, k)
	}
}

func getPrometheusScrapeConfiguration(pod *corev1.Pod) status.PrometheusScrapeConfiguration {
	cfg := status.PrometheusScrapeConfiguration{}

	for k, val := range pod.Annotations {
		anno := strutil.SanitizeLabelName(k)
		switch anno {
		case prometheusPortAnnotation:
			cfg.Port = val
		case prometheusScrapeAnnotation:
			cfg.Scrape = val
		case prometheusPathAnnotation:
			cfg.Path = val
		}
	}

	return cfg
}

func getPrometheusPort(pod *corev1.Pod) (string, bool) {
	for k, val := range pod.Annotations {
		if strutil.SanitizeLabelName(k) != prometheusPortAnnotation {
			continue
		}

		return val, true
	}

	return "", false
}

const (
	// AutoImage is the special image name to indicate to the injector that we should use the injected image, and NOT override it
	// This is necessary because image is a required field on container, so if a user defines an istio-proxy container
	// with customizations they must set an image.
	AutoImage = "auto"
)

// applyContainer merges a container spec on top of the provided pod
func applyContainer(target *corev1.Pod, container corev1.Container) (*corev1.Pod, error) {
	overlay := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{container}}}

	overlayJSON, err := json.Marshal(overlay)
	if err != nil {
		return nil, err
	}

	return applyOverlay(target, overlayJSON)
}

// applyInitContainer merges a container spec on top of the provided pod as an init container
func applyInitContainer(target *corev1.Pod, container corev1.Container) (*corev1.Pod, error) {
	overlay := &corev1.Pod{Spec: corev1.PodSpec{
		// We need to set containers to empty, otherwise it will marshal as "null" and delete all containers
		Containers:     []corev1.Container{},
		InitContainers: []corev1.Container{container},
	}}

	overlayJSON, err := json.Marshal(overlay)
	if err != nil {
		return nil, err
	}

	return applyOverlay(target, overlayJSON)
}

func patchHandleUnmarshal(j []byte, unmarshal func(data []byte, v any) error) (map[string]any, error) {
	if j == nil {
		j = []byte("{}")
	}

	m := map[string]any{}
	err := unmarshal(j, &m)
	if err != nil {
		return nil, mergepatch.ErrBadJSONDoc
	}
	return m, nil
}

// StrategicMergePatchYAML is a small fork of strategicpatch.StrategicMergePatch to allow YAML patches
// This avoids expensive conversion from YAML to JSON
func StrategicMergePatchYAML(originalJSON []byte, patchYAML []byte, dataStruct any) ([]byte, error) {
	schema, err := strategicpatch.NewPatchMetaFromStruct(dataStruct)
	if err != nil {
		return nil, err
	}

	originalMap, err := patchHandleUnmarshal(originalJSON, json.Unmarshal)
	if err != nil {
		return nil, err
	}
	patchMap, err := patchHandleUnmarshal(patchYAML, func(data []byte, v any) error {
		return yaml.Unmarshal(data, v)
	})
	if err != nil {
		return nil, err
	}

	result, err := strategicpatch.StrategicMergeMapPatchUsingLookupPatchMeta(originalMap, patchMap, schema)
	if err != nil {
		return nil, err
	}

	return json.Marshal(result)
}

// applyContainer merges a pod spec, provided as JSON, on top of the provided pod
func applyOverlayYAML(target *corev1.Pod, overlayYAML []byte) (*corev1.Pod, error) {
	currentJSON, err := json.Marshal(target)
	if err != nil {
		return nil, err
	}

	pod := corev1.Pod{}
	// Overlay the injected template onto the original podSpec
	patched, err := StrategicMergePatchYAML(currentJSON, overlayYAML, pod)
	if err != nil {
		return nil, fmt.Errorf("strategic merge: %v", err)
	}

	if err := json.Unmarshal(patched, &pod); err != nil {
		return nil, fmt.Errorf("unmarshal patched pod: %v", err)
	}
	return &pod, nil
}

// applyContainer merges a pod spec, provided as JSON, on top of the provided pod
func applyOverlay(target *corev1.Pod, overlayJSON []byte) (*corev1.Pod, error) {
	currentJSON, err := json.Marshal(target)
	if err != nil {
		return nil, err
	}

	pod := corev1.Pod{}
	// Overlay the injected template onto the original podSpec
	patched, err := strategicpatch.StrategicMergePatch(currentJSON, overlayJSON, pod)
	if err != nil {
		return nil, fmt.Errorf("strategic merge: %v", err)
	}

	if err := json.Unmarshal(patched, &pod); err != nil {
		return nil, fmt.Errorf("unmarshal patched pod: %v", err)
	}
	return &pod, nil
}

func (wh *Webhook) inject(ar *kube.AdmissionReview, path string) *kube.AdmissionResponse {
	log := log.WithLabels("path", path)
	req := ar.Request
	var pod corev1.Pod
	if err := json.Unmarshal(req.Object.Raw, &pod); err != nil {
		handleError(log, fmt.Sprintf("Could not unmarshal raw object: %v %s", err,
			string(req.Object.Raw)))
		return toAdmissionResponse(err)
	}
	// Managed fields is sometimes extremely large, leading to excessive CPU time on patch generation
	// It does not impact the injection output at all, so we can just remove it.
	pod.ManagedFields = nil

	// Deal with potential empty fields, e.g., when the pod is created by a deployment
	podName := potentialPodName(pod.ObjectMeta)
	if pod.ObjectMeta.Namespace == "" {
		pod.ObjectMeta.Namespace = req.Namespace
	}

	log = log.WithLabels("pod", pod.Namespace+"/"+podName)
	log.Infof("Process sidecar injection request")
	log.Debugf("Object: %v", string(req.Object.Raw))
	log.Debugf("OldObject: %v", string(req.OldObject.Raw))

	wh.mu.RLock()
	if !injectRequired(IgnoredNamespaces.UnsortedList(), wh.Config, &pod.Spec, pod.ObjectMeta) {
		log.Infof("Skipping due to policy check")
		totalSkippedInjections.Increment()
		wh.mu.RUnlock()
		return &kube.AdmissionResponse{
			Allowed: true,
		}
	}

	proxyConfig := wh.env.GetProxyConfigOrDefault(pod.Namespace, pod.Labels, pod.Annotations, wh.meshConfig)
	deploy, typeMeta := kube.GetDeployMetaFromPod(&pod)

	params := InjectionParameters{
		pod:                 &pod,
		deployMeta:          deploy,
		typeMeta:            typeMeta,
		templates:           wh.Config.Templates,
		defaultTemplate:     wh.Config.DefaultTemplates,
		aliases:             wh.Config.Aliases,
		meshConfig:          wh.meshConfig,
		proxyConfig:         proxyConfig,
		valuesConfig:        wh.valuesConfig,
		revision:            wh.revision,
		injectedAnnotations: wh.Config.InjectedAnnotations,
		proxyEnvs:           parseInjectEnvs(path),
	}

	clusterID, _ := extractClusterAndNetwork(params)
	if clusterID == "" {
		clusterID = constants.DefaultClusterName
	}

	if platform.IsOpenShift() && wh.namespaces != nil {
		client := wh.namespaces.ForCluster(cluster.ID(clusterID))
		if client != nil {
			params.namespace = client.Get(pod.Namespace, "")
		} else {
			log.Warnf("unable to fetch namespace, failed to get client for %q", clusterID)
		}

		// OpenShift automatically assigns a SecurityContext.RunAsUser to all containers in the Pod, even if the Pod's
		// YAML does not explicitly set this value. Istio treats the values specified in the istio-proxy container as
		// overrides and preserves them in the final Pod yaml as expected. However, the RunAsUser value which is
		// automatically set by OpenShift would be the same for all containers within the Pod, which is a problem.
		// Because the RunAsUser is identical for both the application container and the proxy container, traffic
		// interception fails for the pod. Here, we ignore the RunAsUser value on the sidecar proxy if it matches the
		// application container's value. At the same time, if user explicitly configures a RunAsUser in the istio-proxy
		// container which is different to the application container's value, that setting is still honored.
		if sideCarProxy := FindSidecar(params.pod); sideCarProxy != nil && sideCarProxy.SecurityContext != nil {
			if isSidecarUserMatchingAppUser(params.pod) {
				log.Infof("Resetting the UserID of sideCar proxy as it matches with the app container for Pod %q", params.pod.Name)
				sideCarProxy.SecurityContext.RunAsUser = nil
				sideCarProxy.SecurityContext.RunAsGroup = nil
			}
		}
	}

	var nodes kclient.Client[*corev1.Node]
	if wh.nodes != nil {
		nodes = wh.nodes.ForCluster(cluster.ID(clusterID))
		if nodes == nil {
			log.Warnf("unable to fetch nodes, failed to get client for %q", clusterID)
		}
	} else if features.EnableNativeSidecars {
		log.Warnf("Native sidecars feature is enabled but node client wasn't initialized")
	}
	params.nativeSidecar = detectNativeSidecar(nodes, pod.Spec.NodeName)

	wh.mu.RUnlock()

	patchBytes, err := injectPod(params)
	if err != nil {
		handleError(log, fmt.Sprintf("Pod injection failed: %v", err))
		return toAdmissionResponse(err)
	}

	reviewResponse := kube.AdmissionResponse{
		Allowed: true,
		Patch:   patchBytes,
		PatchType: func() *string {
			pt := "JSONPatch"
			return &pt
		}(),
	}
	return &reviewResponse
}

func isSidecarUserMatchingAppUser(pod *corev1.Pod) bool {
	containers := append([]corev1.Container{}, pod.Spec.Containers...)
	containers = append(containers, pod.Spec.InitContainers...)

	var sideCarUser, appUser int64
	for i := range containers {
		if containers[i].Name == ProxyContainerName {
			if containers[i].SecurityContext != nil && containers[i].SecurityContext.RunAsUser != nil {
				sideCarUser = *containers[i].SecurityContext.RunAsUser
			}
		} else if containers[i].Name != ValidationContainerName && containers[i].Name != InitContainerName {
			if containers[i].SecurityContext != nil && containers[i].SecurityContext.RunAsUser != nil {
				appUser = *containers[i].SecurityContext.RunAsUser
			}
		}
	}

	return sideCarUser == appUser
}

func detectNativeSidecar(nodes kclient.Client[*corev1.Node], podNodeName string) bool {
	if !features.EnableNativeSidecars {
		return false
	}

	if nodes == nil {
		log.Warnf("configured to auto detect native sidecar support, but couldn't find a client")
		return false
	}

	// Native sidecars feature graduated to stable in Kubernetes 1.33
	// See https://github.com/kubernetes/enhancements/blob/master/keps/sig-node/753-sidecar-containers/README.md#implementation-history
	minVersion := 33
	allNodesValid := false

	checkNodeVersion := func(n *corev1.Node) bool {
		nodeKubeletVersion := n.Status.NodeInfo.KubeletVersion
		ver, err := goversion.NewVersion(nodeKubeletVersion)
		if err != nil {
			log.Warnf("could not read node version for %v %v: %v", n.Name, nodeKubeletVersion, err)
			return false
		}
		minor := ver.Segments()[1]
		if minor < minVersion {
			log.Debugf("detected kubelet version 1.%v < 1.%v on node %v; native sidecars disabled",
				minor, minVersion, n.Name)
			return false
		}
		return true
	}

	if podNodeName != "" {
		node := nodes.Get(podNodeName, "")
		if node != nil {
			return checkNodeVersion(node)
		}
		log.Warnf("pod assigned to node %q but node not found in cluster", podNodeName)
	}
	// Check all nodes to see if they are eligible to support native sidecars. If any node is below the minimum version, we disable the feature.
	// This means when an older ineligible node is added to the cluster and the webhook runs
	// NativeSidecar will be disabled since that node will fail the kubelet version check.
	// This avoids issues with mixed clusters where some nodes support native sidecars and others do not.
	for _, n := range nodes.List(metav1.NamespaceAll, klabels.Everything()) {
		if !checkNodeVersion(n) {
			return false
		}
		allNodesValid = true
	}
	return allNodesValid
}

func (wh *Webhook) serveInject(w http.ResponseWriter, r *http.Request) {
	log := log.WithLabels("path", r.URL.Path)
	totalInjections.Increment()
	t0 := time.Now()
	defer func() { injectionTime.Record(time.Since(t0).Seconds()) }()
	var body []byte
	if r.Body != nil {
		if data, err := kube.HTTPConfigReader(r); err == nil {
			body = data
		} else {
			handleError(log, err.Error())
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	if len(body) == 0 {
		handleError(log, "no body found")
		http.Error(w, "no body found", http.StatusBadRequest)
		return
	}

	// verify the content type is accurate
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		handleError(log, fmt.Sprintf("contentType=%s, expect application/json", contentType))
		http.Error(w, "invalid Content-Type, want `application/json`", http.StatusUnsupportedMediaType)
		return
	}

	path := ""
	if r.URL != nil {
		path = r.URL.Path
	}

	var reviewResponse *kube.AdmissionResponse
	var obj runtime.Object
	var ar *kube.AdmissionReview
	if out, _, err := deserializer.Decode(body, nil, obj); err != nil {
		handleError(log, fmt.Sprintf("Could not decode body: %v", err))
		reviewResponse = toAdmissionResponse(err)
	} else {
		log.Debugf("AdmissionRequest for path=%s\n", path)
		ar, err = kube.AdmissionReviewKubeToAdapter(out)
		if err != nil {
			handleError(log, fmt.Sprintf("Could not decode object: %v", err))
			reviewResponse = toAdmissionResponse(err)
		} else {
			reviewResponse = wh.inject(ar, path)
		}
	}

	response := kube.AdmissionReview{}
	response.Response = reviewResponse
	var responseKube runtime.Object
	var apiVersion string
	if ar != nil {
		apiVersion = ar.APIVersion
		response.TypeMeta = ar.TypeMeta
		if response.Response != nil {
			if ar.Request != nil {
				response.Response.UID = ar.Request.UID
			}
		}
	}
	responseKube = kube.AdmissionReviewAdapterToKube(&response, apiVersion)
	resp, err := json.Marshal(responseKube)
	if err != nil {
		handleError(log, fmt.Sprintf("Could not encode response: %v", err))
		http.Error(w, fmt.Sprintf("could not encode response: %v", err), http.StatusInternalServerError)
		return
	}
	if _, err := w.Write(resp); err != nil {
		log.Errorf("Could not write response: %v", err)
		handleError(log, fmt.Sprintf("could not write response: %v", err))
		http.Error(w, fmt.Sprintf("could not write response: %v", err), http.StatusInternalServerError)
		return
	}
	totalSuccessfulInjections.Increment()
}

// parseInjectEnvs parse new envs from inject url path. format: /inject/k1/v1/k2/v2
// slash characters in values must be replaced by --slash-- (e.g. /inject/k1/abc--slash--def/k2/v2).
func parseInjectEnvs(path string) map[string]string {
	path = strings.TrimSuffix(path, "/")
	res := func(path string) []string {
		parts := strings.SplitN(path, "/", 3)
		var newRes []string
		if len(parts) == 3 { // If length is less than 3, then the path is simply "/inject".
			if strings.HasPrefix(parts[2], ":ENV:") {
				// Deprecated, not recommended.
				//    Note that this syntax fails validation when used to set injectionPath (i.e., service.path in mwh).
				//    It doesn't fail validation when used to set injectionURL, however. K8s bug maybe?
				pairs := strings.Split(parts[2], ":ENV:")
				for i := 1; i < len(pairs); i++ { // skip the first part, it is a nil
					pair := strings.SplitN(pairs[i], "=", 2)
					// The first part is the variable name which can not be empty
					// the second part is the variable value which can be empty but has to exist
					// for example, aaa=bbb, aaa= are valid, but =aaa or = are not valid, the
					// invalid ones will be ignored.
					if len(pair[0]) > 0 && len(pair) == 2 {
						newRes = append(newRes, pair...)
					}
				}
				return newRes
			}
			newRes = strings.Split(parts[2], "/")
		}
		for i, value := range newRes {
			if i%2 != 0 {
				// Replace --slash-- with / in values.
				newRes[i] = strings.ReplaceAll(value, "--slash--", "/")
			}
		}
		return newRes
	}(path)
	newEnvs := make(map[string]string)

	for i := 0; i < len(res); i += 2 {
		k := res[i]
		if i == len(res)-1 { // ignore the last key without value
			log.Warnf("Odd number of inject env entries, ignore the last key %s\n", k)
			break
		}

		env, found := URLParameterToEnv[k]
		if !found {
			env = strings.ToUpper(k) // if not found, use the custom env directly
		}
		if env != "" {
			newEnvs[env] = res[i+1]
		}
	}

	return newEnvs
}

func handleError(l *log.Scope, message string) {
	l.Errorf(message)
	totalFailedInjections.Increment()
}
