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
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	jsonpatch "github.com/evanphx/json-patch/v5"
	appsv1 "k8s.io/api/apps/v1"
	batch "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	yamlDecoder "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"

	"istio.io/api/annotation"
	"istio.io/api/label"
	meshconfig "istio.io/api/mesh/v1alpha1"
	proxyConfig "istio.io/api/networking/v1beta1"
	opconfig "istio.io/istio/operator/pkg/apis"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/mesh"
	common_features "istio.io/istio/pkg/features"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/log"
	"istio.io/istio/tools/istio-iptables/pkg/constants"
)

// InjectionPolicy determines the policy for injecting the
// sidecar proxy into the watched namespace(s).
type InjectionPolicy string

const (
	// InjectionPolicyDisabled specifies that the sidecar injector
	// will not inject the sidecar into resources by default for the
	// namespace(s) being watched. Resources can enable injection
	// using the "sidecar.istio.io/inject" label with value of
	// "true".
	InjectionPolicyDisabled InjectionPolicy = "disabled"

	// InjectionPolicyEnabled specifies that the sidecar injector will
	// inject the sidecar into resources by default for the
	// namespace(s) being watched. Resources can disable injection
	// using the "sidecar.istio.io/inject" label with value of
	// "false".
	InjectionPolicyEnabled InjectionPolicy = "enabled"
)

const (
	// ProxyContainerName is used by e2e integration tests for fetching logs
	ProxyContainerName = "istio-proxy"

	// ValidationContainerName is the name of the init container that validates
	// if CNI has made the necessary changes to iptables
	ValidationContainerName = "istio-validation"

	// InitContainerName is the name of the init container that deploys iptables
	InitContainerName = "istio-init"

	// EnableCoreDumpName is the name of the init container that allows core dumps
	EnableCoreDumpName = "enable-core-dump"
)

const (
	// ImageTypeDebug is the suffix of the debug image.
	ImageTypeDebug = "debug"
	// ImageTypeDistroless is the suffix of the distroless image.
	ImageTypeDistroless = "distroless"
	// ImageTypeDefault is the type name of the default image, sufix is elided.
	ImageTypeDefault = "default"
)

// SidecarTemplateData is the data object to which the templated
// version of `SidecarInjectionSpec` is applied.
type SidecarTemplateData struct {
	TypeMeta                 metav1.TypeMeta
	DeploymentMeta           types.NamespacedName
	ObjectMeta               metav1.ObjectMeta
	Spec                     corev1.PodSpec
	ProxyConfig              *meshconfig.ProxyConfig
	MeshConfig               *meshconfig.MeshConfig
	Values                   map[string]any
	Revision                 string
	NativeSidecars           bool
	ProxyImage               string
	ProxyUID                 int64
	ProxyGID                 int64
	InboundTrafficPolicyMode string
	CompliancePolicy         string
}

type (
	Template     *corev1.Pod
	RawTemplates map[string]string
	Templates    map[string]*template.Template
)

type Injector interface {
	Inject(pod *corev1.Pod, namespace string) ([]byte, error)
	GetKubeClient() kube.Client
}

// Config specifies the sidecar injection configuration This includes
// the sidecar template and cluster-side injection policy. It is used
// by kube-inject, sidecar injector, and http endpoint.
type Config struct {
	Policy InjectionPolicy `json:"policy"`

	// DefaultTemplates defines the default template to use for pods that do not explicitly specify a template
	DefaultTemplates []string `json:"defaultTemplates"`

	// RawTemplates defines a set of templates to be used. The specified template will be run, provided with
	// SidecarTemplateData, and merged with the original pod spec using a strategic merge patch.
	RawTemplates RawTemplates `json:"templates"`

	// Aliases defines a translation of a name to inject template. For example, `sidecar: [proxy,init]` could allow
	// referencing two templates, "proxy" and "init" by a single name, "sidecar".
	// Expansion is not recursive.
	Aliases map[string][]string `json:"aliases"`

	// NeverInjectSelector: Refuses the injection on pods whose labels match this selector.
	// It's an array of label selectors, that will be OR'ed, meaning we will iterate
	// over it and stop at the first match
	// Takes precedence over AlwaysInjectSelector.
	NeverInjectSelector []metav1.LabelSelector `json:"neverInjectSelector"`

	// AlwaysInjectSelector: Forces the injection on pods whose labels match this selector.
	// It's an array of label selectors, that will be OR'ed, meaning we will iterate
	// over it and stop at the first match
	AlwaysInjectSelector []metav1.LabelSelector `json:"alwaysInjectSelector"`

	// InjectedAnnotations are additional annotations that will be added to the pod spec after injection
	// This is primarily to support PSP annotations.
	InjectedAnnotations map[string]string `json:"injectedAnnotations"`

	// Templates is a pre-parsed copy of RawTemplates
	Templates Templates `json:"-"`
}

const (
	SidecarTemplateName           = "sidecar"
	AmbientValidationTemplateName = "ambient-validation"
)

// UnmarshalConfig unmarshals the provided YAML configuration, while normalizing the resulting configuration
func UnmarshalConfig(yml []byte) (Config, error) {
	var injectConfig Config
	if err := yaml.Unmarshal(yml, &injectConfig); err != nil {
		return injectConfig, fmt.Errorf("failed to unmarshal injection template: %v", err)
	}
	if injectConfig.RawTemplates == nil {
		injectConfig.RawTemplates = make(map[string]string)
	}
	if len(injectConfig.DefaultTemplates) == 0 {
		injectConfig.DefaultTemplates = []string{SidecarTemplateName}
	}
	if len(injectConfig.RawTemplates) == 0 {
		log.Warnf("injection templates are empty." +
			" This may be caused by using an injection template from an older version of Istio." +
			" Please ensure the template is correct; mismatch template versions can lead to unexpected results, including pods not being injected.")
	}

	var err error
	injectConfig.Templates, err = ParseTemplates(injectConfig.RawTemplates)
	if err != nil {
		return injectConfig, err
	}

	return injectConfig, nil
}

func injectRequired(ignored []string, config *Config, podSpec *corev1.PodSpec, metadata metav1.ObjectMeta) bool { // nolint: lll
	log := log.WithLabels("pod", metadata.Namespace+"/"+potentialPodName(metadata))
	// Skip injection when host networking is enabled. The problem is
	// that the iptables changes are assumed to be within the pod when,
	// in fact, they are changing the routing at the host level. This
	// often results in routing failures within a node which can
	// affect the network provider within the cluster causing
	// additional pod failures.
	if podSpec.HostNetwork {
		return false
	}

	// skip special kubernetes system namespaces
	for _, namespace := range ignored {
		if metadata.Namespace == namespace {
			return false
		}
	}

	annos := metadata.GetAnnotations()

	var useDefault bool
	var inject bool

	objectSelector := annos[annotation.SidecarInject.Name]
	if lbl, labelPresent := metadata.GetLabels()[label.SidecarInject.Name]; labelPresent {
		// The label is the new API; if both are present we prefer the label
		objectSelector = lbl
	}
	switch objectSelector {
	case "true":
		inject = true
	case "false":
		inject = false
	case "":
		useDefault = true
	default:
		log.Warnf("Invalid value for %s: %q. Only 'true' and 'false' are accepted. Falling back to default injection policy.",
			label.SidecarInject.Name, objectSelector)
		useDefault = true
	}

	// If an annotation is not explicitly given, check the LabelSelectors, starting with NeverInject
	if useDefault {
		for _, neverSelector := range config.NeverInjectSelector {
			selector, err := metav1.LabelSelectorAsSelector(&neverSelector)
			if err != nil {
				log.Warnf("Invalid selector for NeverInjectSelector: %v (%v)", neverSelector, err)
			} else if !selector.Empty() && selector.Matches(labels.Set(metadata.Labels)) {
				log.Debugf("Explicitly disabling injection for pod %s/%s due to pod labels matching NeverInjectSelector config map entry.",
					metadata.Namespace, potentialPodName(metadata))
				inject = false
				useDefault = false
				break
			}
		}
	}

	// If there's no annotation nor a NeverInjectSelector, check the AlwaysInject one
	if useDefault {
		for _, alwaysSelector := range config.AlwaysInjectSelector {
			selector, err := metav1.LabelSelectorAsSelector(&alwaysSelector)
			if err != nil {
				log.Warnf("Invalid selector for AlwaysInjectSelector: %v (%v)", alwaysSelector, err)
			} else if !selector.Empty() && selector.Matches(labels.Set(metadata.Labels)) {
				log.Debugf("Explicitly enabling injection for pod %s/%s due to pod labels matching AlwaysInjectSelector config map entry.",
					metadata.Namespace, potentialPodName(metadata))
				inject = true
				useDefault = false
				break
			}
		}
	}

	var required bool
	switch config.Policy {
	default: // InjectionPolicyOff
		log.Errorf("Illegal value for autoInject:%s, must be one of [%s,%s]. Auto injection disabled!",
			config.Policy, InjectionPolicyDisabled, InjectionPolicyEnabled)
		required = false
	case InjectionPolicyDisabled:
		if useDefault {
			required = false
		} else {
			required = inject
		}
	case InjectionPolicyEnabled:
		if useDefault {
			required = true
		} else {
			required = inject
		}
	}

	if log.DebugEnabled() {
		// Build a log message for the annotations.
		annotationStr := ""
		for name := range AnnotationValidation {
			value, ok := annos[name]
			if !ok {
				value = "(unset)"
			}
			annotationStr += fmt.Sprintf("%s:%s ", name, value)
		}

		log.Debugf("Sidecar injection policy for %v/%v: namespacePolicy:%v useDefault:%v inject:%v required:%v %s",
			metadata.Namespace,
			potentialPodName(metadata),
			config.Policy,
			useDefault,
			inject,
			required,
			annotationStr)
	}

	return required
}

// TODO(jaellio): generate label
var ambientInjectInit = label.Instance{
	Name:          "ambient.istio.io/init",
	Description:   "Specifies whether or not an init sidecar should be " +
		"injected into the workload.",
	FeatureStatus: label.Alpha,
	Hidden:        false,
	Deprecated:    false,
	Resources: []label.ResourceTypes{
		label.Pod,
	},
}

func ambientInjectRequired(ignored []string, config *Config, podSpec *corev1.PodSpec, metadata metav1.ObjectMeta) bool {
	log := log.WithLabels("pod", metadata.Namespace+"/"+potentialPodName(metadata))
	// Skip injection when host networking is enabled. The problem is
	// that the iptables changes are assumed to be within the pod when,
	// in fact, they are changing the routing at the host level. This
	// often results in routing failures within a node which can
	// affect the network provider within the cluster causing
	// additional pod failures.
	if podSpec.HostNetwork {
		return false
	}

	// skip special kubernetes system namespaces
	for _, namespace := range ignored {
		if metadata.Namespace == namespace {
			return false
		}
	}

	if !features.EnableAmbient {
		log.Debug("Control plane is not in ambient mode, skipping ambient injection")
		return false
	}

	var inject bool

	lbl, labelPresent := metadata.GetLabels()[ambientInjectInit.Name]
	if !labelPresent {
		log.Debugf("%s label not present", ambientInjectInit.Name)
		return false
	}
	switch lbl {
	case "true":
		inject = true
	case "false", "":
		inject = false
	default:
		log.Warnf("Invalid value for %s: %q. Only 'true' and 'false' are accepted. Ambient injections must be explicitly enabled.",
			ambientInjectInit.Name, lbl)
		inject = false
	}

	return inject
}

// ProxyImage constructs image url in a backwards compatible way.
// values based name => {{ .Values.global.hub }}/{{ .Values.global.proxy.image }}:{{ .Values.global.tag }}
func ProxyImage(values *opconfig.Values, image *proxyConfig.ProxyImage, annotations map[string]string) string {
	imageName := "proxyv2"
	global := values.GetGlobal()

	tag := ""
	if global.GetTag() != nil { // Tag is an interface but we need the string form.
		tag = fmt.Sprintf("%v", global.GetTag().AsInterface())
	}

	imageType := global.GetVariant()
	if image != nil {
		imageType = image.ImageType
	}

	if global.GetProxy() != nil && global.GetProxy().GetImage() != "" {
		imageName = global.GetProxy().GetImage()
	}

	if it, ok := annotations[annotation.SidecarProxyImageType.Name]; ok {
		imageType = it
	}

	return imageURL(global.GetHub(), imageName, tag, imageType)
}

func InboundTrafficPolicyMode(meshConfig *meshconfig.MeshConfig) string {
	switch meshConfig.GetInboundTrafficPolicy().GetMode() {
	case meshconfig.MeshConfig_InboundTrafficPolicy_LOCALHOST:
		return "localhost"
	case meshconfig.MeshConfig_InboundTrafficPolicy_PASSTHROUGH:
		return "passthrough"
	}
	return "passthrough"
}

// imageURL creates url from parts.
// imageType is appended if not empty
// if imageType is already present in the tag, then it is replaced.
// docker.io/istio/proxyv2:1.12-distroless
// gcr.io/gke-release/asm/proxyv2:1.11.2-asm.17-distroless
// docker.io/istio/proxyv2:1.12
func imageURL(hub, imageName, tag, imageType string) string {
	return hub + "/" + imageName + ":" + updateImageTypeIfPresent(tag, imageType)
}

// KnownImageTypes are image types that istio pubishes.
var KnownImageTypes = []string{ImageTypeDistroless, ImageTypeDebug}

func updateImageTypeIfPresent(tag string, imageType string) string {
	if imageType == "" {
		return tag
	}

	for _, i := range KnownImageTypes {
		if strings.HasSuffix(tag, "-"+i) {
			tag = tag[:len(tag)-(len(i)+1)]
			break
		}
	}

	if imageType == ImageTypeDefault {
		return tag
	}

	return tag + "-" + imageType
}

func extractClusterAndNetwork(params InjectionParameters) (string, string) {
	metadata := &params.pod.ObjectMeta
	cluster := params.valuesConfig.asStruct.GetGlobal().GetMultiCluster().GetClusterName()
	// TODO allow overriding the values.global network in injection with the system namespace label
	network := params.valuesConfig.asStruct.GetGlobal().GetNetwork()
	// params may be set from webhook URL, take priority over values yaml
	if params.proxyEnvs["ISTIO_META_CLUSTER_ID"] != "" {
		cluster = params.proxyEnvs["ISTIO_META_CLUSTER_ID"]
	}
	if params.proxyEnvs["ISTIO_META_NETWORK"] != "" {
		network = params.proxyEnvs["ISTIO_META_NETWORK"]
	}
	// explicit label takes highest precedence
	if n, ok := metadata.Labels[label.TopologyNetwork.Name]; ok {
		network = n
	}
	return cluster, network
}

// RunTemplate renders the sidecar template
// Returns the raw string template, as well as the parse pod form
func RunTemplate(params InjectionParameters) (mergedPod *corev1.Pod, templatePod *corev1.Pod, err error) {
	metadata := &params.pod.ObjectMeta
	meshConfig := params.meshConfig

	if err := validateAnnotations(metadata.GetAnnotations()); err != nil {
		log.Errorf("Injection failed due to invalid annotations: %v", err)
		return nil, nil, err
	}

	cluster, network := extractClusterAndNetwork(params)

	// use network in values for template, and proxy env variables
	if cluster != "" {
		params.proxyEnvs["ISTIO_META_CLUSTER_ID"] = cluster
	}
	if network != "" {
		params.proxyEnvs["ISTIO_META_NETWORK"] = network
	}

	strippedPod, err := reinsertOverrides(stripPod(params))
	if err != nil {
		return nil, nil, err
	}

	proxyUID, proxyGID := GetProxyIDs(params.namespace)

	// When changing this, make sure to change TemplateInput in deploymentcontroller.go
	data := SidecarTemplateData{
		TypeMeta:                 params.typeMeta,
		DeploymentMeta:           params.deployMeta,
		ObjectMeta:               strippedPod.ObjectMeta,
		Spec:                     strippedPod.Spec,
		ProxyConfig:              params.proxyConfig,
		MeshConfig:               meshConfig,
		Values:                   params.valuesConfig.asMap,
		Revision:                 params.revision,
		ProxyImage:               ProxyImage(params.valuesConfig.asStruct, params.proxyConfig.Image, strippedPod.Annotations),
		NativeSidecars:           params.nativeSidecar,
		ProxyUID:                 proxyUID,
		ProxyGID:                 proxyGID,
		InboundTrafficPolicyMode: InboundTrafficPolicyMode(meshConfig),
		CompliancePolicy:         common_features.CompliancePolicy,
	}
	if params.valuesConfig.asMap == nil {
		return nil, nil, fmt.Errorf("failed to parse values.yaml; check Istiod logs for errors")
	}

	mergedPod = params.pod
	templatePod = &corev1.Pod{}
	for _, templateName := range selectTemplates(params) {
		parsedTemplate, f := params.templates[templateName]
		if !f {
			return nil, nil, fmt.Errorf("requested template %q not found; have %v",
				templateName, strings.Join(knownTemplates(params.templates), ", "))
		}
		bbuf, err := runTemplate(parsedTemplate, data)
		if err != nil {
			return nil, nil, err
		}

		templatePod, err = applyOverlayYAML(templatePod, bbuf.Bytes())
		if err != nil {
			return nil, nil, fmt.Errorf("failed applying injection overlay: %v", err)
		}
		// This is a bit of a weird hack. With NativeSidecars, the container will be under initContainers in the template pod.
		// But we may have injection customizations (https://istio.io/latest/docs/setup/additional-setup/sidecar-injection/#customizing-injection);
		// these will be in the `containers` field.
		// So if we see the proxy container in `containers` in the original pod, and in `initContainers` in the template pod,
		// move the container.
		// The sidecar.istio.io/nativeSidecar annotation takes precedence over the global feature flag.
		native := params.nativeSidecar
		if mergedPod.Annotations["sidecar.istio.io/nativeSidecar"] == "true" {
			native = true
		} else if mergedPod.Annotations["sidecar.istio.io/nativeSidecar"] == "false" {
			native = false
		}
		if native &&
			FindContainer(ProxyContainerName, templatePod.Spec.InitContainers) != nil &&
			FindContainer(ProxyContainerName, mergedPod.Spec.Containers) != nil {
			mergedPod = mergedPod.DeepCopy()
			mergedPod.Spec.Containers, mergedPod.Spec.InitContainers = moveContainer(mergedPod.Spec.Containers, mergedPod.Spec.InitContainers, ProxyContainerName)
		}
		mergedPod, err = applyOverlayYAML(mergedPod, bbuf.Bytes())
		if err != nil {
			return nil, nil, fmt.Errorf("failed parsing generated injected YAML (check Istio sidecar injector configuration): %v", err)
		}
	}

	return mergedPod, templatePod, nil
}

func knownTemplates(t Templates) []string {
	keys := make([]string, 0, len(t))
	for k := range t {
		keys = append(keys, k)
	}
	return keys
}

func selectTemplates(params InjectionParameters) []string {
	if a, f := params.pod.Annotations[annotation.InjectTemplates.Name]; f {
		names := []string{}
		for _, tmplName := range strings.Split(a, ",") {
			name := strings.TrimSpace(tmplName)
			names = append(names, name)
		}
		return resolveAliases(params, names)
	}
	return resolveAliases(params, params.defaultTemplate)
}

func resolveAliases(params InjectionParameters, names []string) []string {
	ret := []string{}
	for _, name := range names {
		if al, f := params.aliases[name]; f {
			ret = append(ret, al...)
		} else {
			ret = append(ret, name)
		}
	}
	return ret
}

func stripPod(req InjectionParameters) *corev1.Pod {
	pod := req.pod.DeepCopy()
	prevStatus := injectionStatus(pod)
	if prevStatus == nil {
		return req.pod
	}
	// We found a previous status annotation. Possibly we are re-injecting the pod
	// To ensure idempotency, remove our injected containers first
	for _, c := range prevStatus.Containers {
		pod.Spec.Containers = modifyContainers(pod.Spec.Containers, c, Remove)
	}
	for _, c := range prevStatus.InitContainers {
		pod.Spec.InitContainers = modifyContainers(pod.Spec.InitContainers, c, Remove)
	}

	targetPort := strconv.Itoa(int(req.meshConfig.GetDefaultConfig().GetStatusPort()))
	if cur, f := getPrometheusPort(pod); f {
		// We have already set the port, assume user is controlling this or, more likely, re-injected
		// the pod.
		if cur == targetPort {
			clearPrometheusAnnotations(pod)
		}
	}
	delete(pod.Annotations, annotation.SidecarStatus.Name)

	return pod
}

func injectionStatus(pod *corev1.Pod) *SidecarInjectionStatus {
	var statusBytes []byte
	if pod.ObjectMeta.Annotations != nil {
		if value, ok := pod.ObjectMeta.Annotations[annotation.SidecarStatus.Name]; ok {
			statusBytes = []byte(value)
		}
	}
	if statusBytes == nil {
		return nil
	}

	// default case when injected pod has explicit status
	var iStatus SidecarInjectionStatus
	if err := json.Unmarshal(statusBytes, &iStatus); err != nil {
		return nil
	}
	return &iStatus
}

func parseDryTemplate(tmplStr string, funcMap map[string]any) (*template.Template, error) {
	temp := template.New("inject")
	t, err := temp.Funcs(sprig.TxtFuncMap()).Funcs(funcMap).Parse(tmplStr)
	if err != nil {
		log.Infof("Failed to parse template: %v %v\n", err, tmplStr)
		return nil, err
	}

	return t, nil
}

func runTemplate(tmpl *template.Template, data SidecarTemplateData) (bytes.Buffer, error) {
	var res bytes.Buffer
	if err := tmpl.Execute(&res, &data); err != nil {
		log.Errorf("Invalid template: %v", err)
		return bytes.Buffer{}, err
	}

	return res, nil
}

// IntoResourceFile injects the istio proxy into the specified
// kubernetes YAML file.
// nolint: lll
func IntoResourceFile(injector Injector, sidecarTemplate Templates,
	valuesConfig ValuesConfig, revision string, meshconfig *meshconfig.MeshConfig, in io.Reader, out io.Writer, warningHandler func(string),
) error {
	reader := yamlDecoder.NewYAMLReader(bufio.NewReaderSize(in, 4096))
	for {
		raw, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		obj, err := FromRawToObject(raw)
		if err != nil && !runtime.IsNotRegisteredError(err) {
			return err
		}

		var updated []byte
		if err == nil {
			outObject, err := IntoObject(injector, sidecarTemplate, valuesConfig, revision, meshconfig, obj, warningHandler) // nolint: vetshadow
			if err != nil {
				return err
			}
			if updated, err = yaml.Marshal(outObject); err != nil {
				return err
			}
		} else {
			updated = raw // unchanged
		}

		if _, err = out.Write(updated); err != nil {
			return err
		}
		if _, err = fmt.Fprint(out, "---\n"); err != nil {
			return err
		}
	}
	return nil
}

// FromRawToObject is used to convert from raw to the runtime object
func FromRawToObject(raw []byte) (runtime.Object, error) {
	var typeMeta metav1.TypeMeta
	if err := yaml.Unmarshal(raw, &typeMeta); err != nil {
		return nil, err
	}

	gvk := schema.FromAPIVersionAndKind(typeMeta.APIVersion, typeMeta.Kind)
	obj, err := injectScheme.New(gvk)
	if err != nil {
		return nil, err
	}
	if err = yaml.Unmarshal(raw, obj); err != nil {
		return nil, err
	}

	return obj, nil
}

// IntoObject convert the incoming resources into Injected resources
// nolint: lll
func IntoObject(injector Injector, sidecarTemplate Templates, valuesConfig ValuesConfig,
	revision string, meshconfig *meshconfig.MeshConfig, in runtime.Object, warningHandler func(string),
) (any, error) {
	out := in.DeepCopyObject()

	var deploymentMetadata types.NamespacedName
	var metadata *metav1.ObjectMeta
	var podSpec *corev1.PodSpec
	var typeMeta metav1.TypeMeta

	// Handle Lists
	if list, ok := out.(*corev1.List); ok {
		result := list

		for i, item := range list.Items {
			obj, err := FromRawToObject(item.Raw)
			if runtime.IsNotRegisteredError(err) {
				continue
			}
			if err != nil {
				return nil, err
			}

			r, err := IntoObject(injector, sidecarTemplate, valuesConfig, revision, meshconfig, obj, warningHandler) // nolint: vetshadow
			if err != nil {
				return nil, err
			}

			re := runtime.RawExtension{}
			re.Object = r.(runtime.Object)
			result.Items[i] = re
		}
		return result, nil
	}

	// CronJobs have JobTemplates in them, instead of Templates, so we
	// special case them.
	switch v := out.(type) {
	case *batch.CronJob:
		job := v
		typeMeta = job.TypeMeta
		metadata = &job.Spec.JobTemplate.ObjectMeta
		deploymentMetadata = config.NamespacedName(job)
		podSpec = &job.Spec.JobTemplate.Spec.Template.Spec
	case *corev1.Pod:
		pod := v
		metadata = &pod.ObjectMeta
		// sync from webhook inject
		deploymentMetadata, typeMeta = kube.GetDeployMetaFromPod(pod)
		podSpec = &pod.Spec
	case *appsv1.Deployment: // Added to be explicit about the most expected case
		deploy := v
		typeMeta = deploy.TypeMeta
		deploymentMetadata = config.NamespacedName(deploy)
		metadata = &deploy.Spec.Template.ObjectMeta
		podSpec = &deploy.Spec.Template.Spec
	default:
		// `in` is a pointer to an Object. Dereference it.
		outValue := reflect.ValueOf(out).Elem()

		typeMeta = outValue.FieldByName("TypeMeta").Interface().(metav1.TypeMeta)

		om := outValue.FieldByName("ObjectMeta").Interface().(metav1.ObjectMeta)
		deploymentMetadata = types.NamespacedName{Name: om.GetName(), Namespace: om.GetNamespace()}

		templateValue := outValue.FieldByName("Spec").FieldByName("Template")
		// `Template` is defined as a pointer in some older API
		// definitions, e.g. ReplicationController
		if templateValue.Kind() == reflect.Ptr {
			if templateValue.IsNil() {
				return out, fmt.Errorf("spec.template is required value")
			}
			templateValue = templateValue.Elem()
		}
		metadata = templateValue.FieldByName("ObjectMeta").Addr().Interface().(*metav1.ObjectMeta)
		podSpec = templateValue.FieldByName("Spec").Addr().Interface().(*corev1.PodSpec)
	}

	name := metadata.Name
	if name == "" {
		name = deploymentMetadata.Name
	}
	namespace := metadata.Namespace
	if namespace == "" {
		namespace = deploymentMetadata.Namespace
	}

	var fullName string
	if deploymentMetadata.Namespace != "" {
		fullName = fmt.Sprintf("%s/%s", deploymentMetadata.Namespace, name)
	} else {
		fullName = name
	}

	kind := typeMeta.Kind

	// Skip injection when host networking is enabled. The problem is
	// that the iptable changes are assumed to be within the pod when,
	// in fact, they are changing the routing at the host level. This
	// often results in routing failures within a node which can
	// affect the network provider within the cluster causing
	// additional pod failures.
	if podSpec.HostNetwork {
		warningStr := fmt.Sprintf("===> Skipping injection because %q has host networking enabled\n",
			fullName)
		if kind != "" {
			warningStr = fmt.Sprintf("===> Skipping injection because %s %q has host networking enabled\n",
				kind, fullName)
		}
		warningHandler(warningStr)
		return out, nil
	}

	pod := &corev1.Pod{
		ObjectMeta: *metadata,
		Spec:       *podSpec,
	}

	var patchBytes []byte
	var err error
	if injector != nil {
		patchBytes, err = injector.Inject(pod, namespace)
	}
	if err != nil {
		return nil, err
	}
	// TODO(Monkeyanator) istioctl injection still applies just the pod annotation since we don't have
	// the ProxyConfig CRs here.
	if pca, f := metadata.GetAnnotations()[annotation.ProxyConfig.Name]; f {
		var merr error
		meshconfig, merr = mesh.ApplyProxyConfig(pca, meshconfig)
		if merr != nil {
			return nil, merr
		}
	}

	if patchBytes == nil {
		if !injectRequired(IgnoredNamespaces.UnsortedList(), &Config{Policy: InjectionPolicyEnabled}, &pod.Spec, pod.ObjectMeta) {
			warningStr := fmt.Sprintf("===> Skipping injection because %q has sidecar injection disabled\n", fullName)
			if kind != "" {
				warningStr = fmt.Sprintf("===> Skipping injection because %s %q has sidecar injection disabled\n",
					kind, fullName)
			}
			warningHandler(warningStr)
			return out, nil
		}

		var nativeSidecar bool
		if injector != nil && injector.GetKubeClient() != nil {
			nodes := kclient.New[*corev1.Node](injector.GetKubeClient())
			nativeSidecar = DetectNativeSidecar(nodes, pod.Spec.NodeName)
		} else {
			// if injector or client is nil, enable native sidecars if the feature is explicitly enabled
			nativeSidecar = (features.EnableNativeSidecars == features.NativeSidecarModeEnabled)
		}

		params := InjectionParameters{
			pod:        pod,
			deployMeta: deploymentMetadata,
			typeMeta:   typeMeta,
			// Todo replace with some template resolver abstraction
			templates:           sidecarTemplate,
			defaultTemplate:     []string{SidecarTemplateName},
			meshConfig:          meshconfig,
			proxyConfig:         meshconfig.GetDefaultConfig(),
			valuesConfig:        valuesConfig,
			nativeSidecar:       nativeSidecar,
			revision:            revision,
			proxyEnvs:           map[string]string{},
			injectedAnnotations: nil,
		}
		patchBytes, err = injectPod(params)
	}
	if err != nil {
		return nil, err
	}
	patched, err := applyJSONPatchToPod(pod, patchBytes)
	if err != nil {
		return nil, err
	}
	patchedObject, _, err := jsonSerializer.Decode(patched, nil, &corev1.Pod{})
	if err != nil {
		return nil, err
	}
	patchedPod := patchedObject.(*corev1.Pod)
	*metadata = patchedPod.ObjectMeta
	*podSpec = patchedPod.Spec
	return out, nil
}

func applyJSONPatchToPod(input *corev1.Pod, patch []byte) ([]byte, error) {
	objJS, err := runtime.Encode(jsonSerializer, input)
	if err != nil {
		return nil, err
	}

	p, err := jsonpatch.DecodePatch(patch)
	if err != nil {
		return nil, err
	}

	patchedJSON, err := p.Apply(objJS)
	if err != nil {
		return nil, err
	}
	return patchedJSON, nil
}

// SidecarInjectionStatus contains basic information about the
// injected sidecar. This includes the names of added containers and
// volumes.
type SidecarInjectionStatus struct {
	InitContainers   []string `json:"initContainers"`
	Containers       []string `json:"containers"`
	Volumes          []string `json:"volumes"`
	ImagePullSecrets []string `json:"imagePullSecrets"`
	Revision         string   `json:"revision"`
}

func potentialPodName(metadata metav1.ObjectMeta) string {
	if metadata.Name != "" {
		return metadata.Name
	}
	if metadata.GenerateName != "" {
		return metadata.GenerateName + "***** (actual name not yet known)"
	}
	return ""
}

// overwriteClusterInfo updates cluster name and network from url path
// This is needed when webconfig config runs on a different cluster than webhook
func overwriteClusterInfo(pod *corev1.Pod, params InjectionParameters) {
	c := FindSidecar(pod)
	if c == nil {
		return
	}
	if len(params.proxyEnvs) > 0 {
		log.Debugf("Updating cluster envs based on inject url: %s\n", params.proxyEnvs)
		updateClusterEnvs(c, params.proxyEnvs)
	}
}

func updateClusterEnvs(container *corev1.Container, newKVs map[string]string) {
	envVars := make([]corev1.EnvVar, 0)

	for _, env := range container.Env {
		if _, found := newKVs[env.Name]; !found {
			envVars = append(envVars, env)
		}
	}

	keys := make([]string, 0, len(newKVs))
	for key := range newKVs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		val := newKVs[key]
		envVars = append(envVars, corev1.EnvVar{Name: key, Value: val, ValueFrom: nil})
	}
	container.Env = envVars
}

// GetProxyIDs returns the UID and GID to be used in the RunAsUser and RunAsGroup fields in the template
// Inspects the namespace metadata for hints and fallbacks to the usual value of 1337.
func GetProxyIDs(namespace *corev1.Namespace) (uid int64, gid int64) {
	uid = constants.DefaultProxyUIDInt
	gid = constants.DefaultProxyUIDInt

	if namespace == nil {
		return
	}

	// Check for OpenShift specifics and returns the max number in the range specified in the namespace annotation
	if _, uidMax, err := getPreallocatedUIDRange(namespace); err == nil {
		uid = *uidMax
	}
	if groups, err := getPreallocatedSupplementalGroups(namespace); err == nil && len(groups) > 0 {
		gid = groups[0].Max
	}

	return
}
