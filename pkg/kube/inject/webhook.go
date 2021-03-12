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
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"gomodules.xyz/jsonpatch/v3"
	kubeApiAdmissionv1 "k8s.io/api/admission/v1"
	kubeApiAdmissionv1beta1 "k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	kjson "k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apimachinery/pkg/util/strategicpatch"

	"istio.io/api/annotation"
	"istio.io/api/label"
	meshconfig "istio.io/api/mesh/v1alpha1"
	opconfig "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/pilot/cmd/pilot-agent/status"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/util/gogoprotomarshal"
	"istio.io/pkg/log"
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
	_ = kubeApiAdmissionv1.AddToScheme(runtimeScheme)
	_ = kubeApiAdmissionv1beta1.AddToScheme(runtimeScheme)
}

const (
	watchDebounceDelay = 100 * time.Millisecond
)

// Webhook implements a mutating webhook for automatic proxy injection.
type Webhook struct {
	mu           sync.RWMutex
	Config       *Config
	meshConfig   *meshconfig.MeshConfig
	valuesConfig string

	watcher Watcher

	env      *model.Environment
	revision string
}

// nolint directives: interfacer
func loadConfig(injectFile, valuesFile string) (*Config, string, error) {
	data, err := ioutil.ReadFile(injectFile)
	if err != nil {
		return nil, "", err
	}
	var c *Config
	if c, err = unmarshalConfig(data); err != nil {
		log.Warnf("Failed to parse injectFile %s", string(data))
		return nil, "", err
	}

	valuesConfig, err := ioutil.ReadFile(valuesFile)
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
	log.Debugf("Templates: |\n  %v", c.Templates, "\n", "\n  ", -1)
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

	p.Watcher.SetHandler(wh.updateConfig)
	sidecarConfig, valuesConfig, err := p.Watcher.Get()
	if err != nil {
		return nil, err
	}
	wh.updateConfig(sidecarConfig, valuesConfig)

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

func (wh *Webhook) updateConfig(sidecarConfig *Config, valuesConfig string) {
	wh.mu.Lock()
	wh.Config = sidecarConfig
	wh.valuesConfig = valuesConfig
	wh.mu.Unlock()
}

type ContainerReorder int

const (
	MoveFirst ContainerReorder = iota
	MoveLast
	Remove
)

func modifyContainers(cl []corev1.Container, name string, modifier ContainerReorder) []corev1.Container {
	containers := []corev1.Container{}
	var match *corev1.Container
	for _, c := range cl {
		c := c
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

func ExtractCanonicalServiceLabels(podLabels map[string]string, workloadName string) (string, string) {
	return extractCanonicalServiceLabel(podLabels, workloadName), extractCanonicalServiceRevision(podLabels)
}

func extractCanonicalServiceRevision(podLabels map[string]string) string {
	if rev, ok := podLabels[model.IstioCanonicalServiceRevisionLabelName]; ok {
		return rev
	}

	if rev, ok := podLabels["app.kubernetes.io/version"]; ok {
		return rev
	}

	if rev, ok := podLabels["version"]; ok {
		return rev
	}

	return "latest"
}

func extractCanonicalServiceLabel(podLabels map[string]string, workloadName string) string {
	if svc, ok := podLabels[model.IstioCanonicalServiceLabelName]; ok {
		return svc
	}

	if svc, ok := podLabels["app.kubernetes.io/name"]; ok {
		return svc
	}

	if svc, ok := podLabels["app"]; ok {
		return svc
	}

	return workloadName
}

func toAdmissionResponse(err error) *kube.AdmissionResponse {
	return &kube.AdmissionResponse{Result: &metav1.Status{Message: err.Error()}}
}

type InjectionParameters struct {
	pod                 *corev1.Pod
	deployMeta          *metav1.ObjectMeta
	typeMeta            *metav1.TypeMeta
	templates           Templates
	defaultTemplate     []string
	aliases             map[string][]string
	meshConfig          *meshconfig.MeshConfig
	valuesConfig        string
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

func getInjectionStatus(podSpec corev1.PodSpec) string {
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

	mergedPod, err = reapplyOverwrittenContainers(mergedPod, req.pod, injectedPodData)
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

// OverrideAnnotation is used to store the overrides for injected containers
// TODO move this to api repo
const OverrideAnnotation = "proxy.istio.io/overrides"

// TemplatesAnnotation declares the set of templates to use for injection. If not specified, DefaultTemplates
// will take precedence, which will inject a standard sidecar.
// The format is a comma separated list. For example, `inject.istio.io/templates: sidecar,debug`.
// TODO move this to api repo
const TemplatesAnnotation = "inject.istio.io/templates"

// reapplyOverwrittenContainers enables users to provide container level overrides for settings in the injection template
// * originalPod: the pod before injection. If needed, we will apply some configurations from this pod on top of the final pod
// * templatePod: the rendered injection template. This is needed only to see what containers we injected
// * finalPod: the current result of injection, roughly equivalent to the merging of originalPod and templatePod
// There are essentially three cases we cover here:
// 1. There is no overlap in containers in original and template pod. We will do nothing.
// 2. There is an overlap (ie, both define istio-proxy), but that is because the pod is being re-injected.
//    In this case we do nothing, since we want to apply the new settings
// 3. There is an overlap. We will re-apply the original container.
// Where "overlap" is a container defined in both the original and template pod. Typically, this would mean
// the user has defined an `istio-proxy` container in their own pod spec.
func reapplyOverwrittenContainers(finalPod *corev1.Pod, originalPod *corev1.Pod, templatePod *corev1.Pod) (*corev1.Pod, error) {
	type podOverrides struct {
		Containers     []corev1.Container `json:"containers,omitempty"`
		InitContainers []corev1.Container `json:"initContainers,omitempty"`
	}

	overrides := podOverrides{}
	existingOverrides := podOverrides{}
	if annotationOverrides, f := originalPod.Annotations[OverrideAnnotation]; f {
		if err := json.Unmarshal([]byte(annotationOverrides), &existingOverrides); err != nil {
			return nil, err
		}
	}

	for _, c := range templatePod.Spec.Containers {
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
		match := FindContainer(c.Name, existingOverrides.InitContainers)
		if match == nil {
			match = FindContainer(c.Name, originalPod.Spec.InitContainers)
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

	_, alreadyInjected := originalPod.Annotations[annotation.SidecarStatus.Name]
	if !alreadyInjected && (len(overrides.Containers) > 0 || len(overrides.InitContainers) > 0) {
		// We found any overrides. Put them in the pod annotation so we can re-apply them on re-injection
		js, err := json.Marshal(overrides)
		if err != nil {
			return nil, err
		}
		if finalPod.Annotations == nil {
			finalPod.Annotations = map[string]string{}
		}
		finalPod.Annotations[OverrideAnnotation] = string(js)
	}

	return finalPod, nil
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
	if annotationOverrides, f := pod.Annotations[OverrideAnnotation]; f {
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

	overwriteClusterInfo(pod.Spec.Containers, req)

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
	pod.Annotations[annotation.SidecarStatus.Name] = getInjectionStatus(injectedPodData.Spec)

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
		mc, merr = mesh.ApplyProxyConfig(pca, *req.meshConfig)
		if merr != nil {
			return merr
		}
	}

	valuesStruct := &opconfig.Values{}
	if err := gogoprotomarshal.ApplyYAML(req.valuesConfig, valuesStruct); err != nil {
		return fmt.Errorf("could not parse configuration values: %v", err)
	}
	// nolint: staticcheck
	holdPod := mc.GetDefaultConfig().GetHoldApplicationUntilProxyStarts().GetValue() ||
		valuesStruct.GetGlobal().GetProxy().GetHoldApplicationUntilProxyStarts().GetValue()

	proxyLocation := MoveLast
	// If HoldApplicationUntilProxyStarts is set, reorder the proxy location
	if holdPod {
		proxyLocation = MoveFirst
	}

	// Proxy container should be last, unless HoldApplicationUntilProxyStarts is set
	// This is to ensure `kubectl exec` and similar commands continue to default to the user's container
	pod.Spec.Containers = modifyContainers(pod.Spec.Containers, ProxyContainerName, proxyLocation)
	// Validation container must be first to block any user containers
	pod.Spec.InitContainers = modifyContainers(pod.Spec.InitContainers, ValidationContainerName, MoveFirst)
	// Init container must be last to allow any traffic to pass before iptables is setup
	pod.Spec.InitContainers = modifyContainers(pod.Spec.InitContainers, InitContainerName, MoveLast)
	pod.Spec.InitContainers = modifyContainers(pod.Spec.InitContainers, EnableCoreDumpName, MoveLast)

	return nil
}

func applyRewrite(pod *corev1.Pod, req InjectionParameters) error {
	valuesStruct := &opconfig.Values{}
	if err := gogoprotomarshal.ApplyYAML(req.valuesConfig, valuesStruct); err != nil {
		log.Infof("Failed to parse values config: %v [%v]\n", err, req.valuesConfig)
		return fmt.Errorf("could not parse configuration values: %v", err)
	}

	rewrite := ShouldRewriteAppHTTPProbers(pod.Annotations, valuesStruct.GetSidecarInjectorWebhook().GetRewriteAppHTTPProbe())
	sidecar := FindSidecar(pod.Spec.Containers)

	// We don't have to escape json encoding here when using golang libraries.
	if rewrite && sidecar != nil {
		if prober := DumpAppProbers(&pod.Spec, req.meshConfig.GetDefaultConfig().GetStatusPort()); prober != "" {
			sidecar.Env = append(sidecar.Env, corev1.EnvVar{Name: status.KubeAppProberEnvName, Value: prober})
		}
		patchRewriteProbe(pod.Annotations, pod, req.meshConfig.GetDefaultConfig().GetStatusPort())
	}
	return nil
}

// applyPrometheusMerge configures prometheus scraping annotations for the "metrics merge" feature.
// This moves the current prometheus.io annotations into an environment variable and replaces them
// pointing to the agent.
func applyPrometheusMerge(pod *corev1.Pod, mesh *meshconfig.MeshConfig) error {
	sidecar := FindSidecar(pod.Spec.Containers)
	if enablePrometheusMerge(mesh, pod.ObjectMeta.Annotations) {
		targetPort := strconv.Itoa(int(mesh.GetDefaultConfig().GetStatusPort()))
		if cur, f := pod.Annotations["prometheus.io/port"]; f {
			// We have already set the port, assume user is controlling this or, more likely, re-injected
			// the pod.
			if cur == targetPort {
				return nil
			}
		}
		scrape := status.PrometheusScrapeConfiguration{
			Scrape: pod.Annotations["prometheus.io/scrape"],
			Path:   pod.Annotations["prometheus.io/path"],
			Port:   pod.Annotations["prometheus.io/port"],
		}
		empty := status.PrometheusScrapeConfiguration{}
		if sidecar != nil && scrape != empty {
			by, err := json.Marshal(scrape)
			if err != nil {
				return err
			}
			sidecar.Env = append(sidecar.Env, corev1.EnvVar{Name: status.PrometheusScrapingConfig.Name, Value: string(by)})
		}
		if pod.Annotations == nil {
			pod.Annotations = map[string]string{}
		}
		pod.Annotations["prometheus.io/port"] = targetPort
		pod.Annotations["prometheus.io/path"] = "/stats/prometheus"
		pod.Annotations["prometheus.io/scrape"] = "true"
	}
	return nil
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
	req := ar.Request
	var pod corev1.Pod
	if err := json.Unmarshal(req.Object.Raw, &pod); err != nil {
		handleError(fmt.Sprintf("Could not unmarshal raw object: %v %s", err,
			string(req.Object.Raw)))
		return toAdmissionResponse(err)
	}

	// Deal with potential empty fields, e.g., when the pod is created by a deployment
	podName := potentialPodName(pod.ObjectMeta)
	if pod.ObjectMeta.Namespace == "" {
		pod.ObjectMeta.Namespace = req.Namespace
	}
	log.Infof("Sidecar injection request for %v/%v", req.Namespace, podName)
	log.Debugf("Object: %v", string(req.Object.Raw))
	log.Debugf("OldObject: %v", string(req.OldObject.Raw))

	wh.mu.RLock()
	if !injectRequired(ignoredNamespaces, wh.Config, &pod.Spec, pod.ObjectMeta) {
		log.Infof("Skipping %s/%s due to policy check", pod.ObjectMeta.Namespace, podName)
		totalSkippedInjections.Increment()
		wh.mu.RUnlock()
		return &kube.AdmissionResponse{
			Allowed: true,
		}
	}

	deploy, typeMeta := kube.GetDeployMetaFromPod(&pod)
	params := InjectionParameters{
		pod:                 &pod,
		deployMeta:          deploy,
		typeMeta:            typeMeta,
		templates:           wh.Config.Templates,
		defaultTemplate:     wh.Config.DefaultTemplates,
		aliases:             wh.Config.Aliases,
		meshConfig:          wh.meshConfig,
		valuesConfig:        wh.valuesConfig,
		revision:            wh.revision,
		injectedAnnotations: wh.Config.InjectedAnnotations,
		proxyEnvs:           parseInjectEnvs(path),
	}
	wh.mu.RUnlock()

	patchBytes, err := injectPod(params)
	if err != nil {
		handleError(fmt.Sprintf("Pod injection failed: %v", err))
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
	totalSuccessfulInjections.Increment()
	return &reviewResponse
}

func (wh *Webhook) serveInject(w http.ResponseWriter, r *http.Request) {
	totalInjections.Increment()
	var body []byte
	if r.Body != nil {
		if data, err := ioutil.ReadAll(r.Body); err == nil {
			body = data
		}
	}
	if len(body) == 0 {
		handleError("no body found")
		http.Error(w, "no body found", http.StatusBadRequest)
		return
	}

	// verify the content type is accurate
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		handleError(fmt.Sprintf("contentType=%s, expect application/json", contentType))
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
		handleError(fmt.Sprintf("Could not decode body: %v", err))
		reviewResponse = toAdmissionResponse(err)
	} else {
		log.Debugf("AdmissionRequest for path=%s\n", path)
		ar, err = kube.AdmissionReviewKubeToAdapter(out)
		if err != nil {
			handleError(fmt.Sprintf("Could not decode object: %v", err))
		}
		reviewResponse = wh.inject(ar, path)
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
		log.Errorf("Could not encode response: %v", err)
		http.Error(w, fmt.Sprintf("could not encode response: %v", err), http.StatusInternalServerError)
	}
	if _, err := w.Write(resp); err != nil {
		log.Errorf("Could not write response: %v", err)
		http.Error(w, fmt.Sprintf("could not write response: %v", err), http.StatusInternalServerError)
	}
}

// parseInjectEnvs parse new envs from inject url path
// follow format: /inject/k1/v1/k2/v2, any kv order works
// eg. "/inject/cluster/cluster1", "/inject/net/network1/cluster/cluster1"
func parseInjectEnvs(path string) map[string]string {
	path = strings.TrimSuffix(path, "/")
	res := strings.Split(path, "/")
	newEnvs := make(map[string]string)

	for i := 2; i < len(res); i += 2 { // skip '/inject'
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

func handleError(message string) {
	log.Errorf(message)
	totalFailedInjections.Increment()
}
