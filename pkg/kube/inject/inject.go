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
	jsonpatch "github.com/evanphx/json-patch"
	"github.com/ghodss/yaml"
	"github.com/hashicorp/go-multierror"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/api/batch/v2alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	yamlDecoder "k8s.io/apimachinery/pkg/util/yaml"

	"istio.io/api/annotation"
	"istio.io/api/label"
	meshconfig "istio.io/api/mesh/v1alpha1"
	opconfig "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/util/gogoprotomarshal"
	"istio.io/pkg/log"
)

// InjectionPolicy determines the policy for injecting the
// sidecar proxy into the watched namespace(s).
type InjectionPolicy string

const (
	// InjectionPolicyDisabled specifies that the sidecar injector
	// will not inject the sidecar into resources by default for the
	// namespace(s) being watched. Resources can enable injection
	// using the "sidecar.istio.io/inject" annotation with value of
	// true.
	InjectionPolicyDisabled InjectionPolicy = "disabled"

	// InjectionPolicyEnabled specifies that the sidecar injector will
	// inject the sidecar into resources by default for the
	// namespace(s) being watched. Resources can disable injection
	// using the "sidecar.istio.io/inject" annotation with value of
	// false.
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

// SidecarTemplateData is the data object to which the templated
// version of `SidecarInjectionSpec` is applied.
type SidecarTemplateData struct {
	TypeMeta       *metav1.TypeMeta
	DeploymentMeta *metav1.ObjectMeta
	ObjectMeta     metav1.ObjectMeta
	Spec           corev1.PodSpec
	ProxyConfig    *meshconfig.ProxyConfig
	MeshConfig     *meshconfig.MeshConfig
	Values         map[string]interface{}
	Revision       string
}

type (
	Template  *corev1.Pod
	Templates map[string]string
)

// Config specifies the sidecar injection configuration This includes
// the sidecar template and cluster-side injection policy. It is used
// by kube-inject, sidecar injector, and http endpoint.
type Config struct {
	Policy InjectionPolicy `json:"policy"`

	// DefaultTemplates defines the default template to use for pods that do not explicitly specify a template
	DefaultTemplates []string `json:"defaultTemplates"`

	// Templates defines a set of templates to be used. The specified template will be run, provided with
	// SidecarTemplateData, and merged with the original pod spec using a strategic merge patch.
	Templates Templates `json:"templates"`

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
}

const (
	SidecarTemplateName = "sidecar"
)

// UnmarshalConfig unmarshals the provided YAML configuration, while normalizing the resulting configuration
// nolint: staticcheck
func UnmarshalConfig(yml []byte) (Config, error) {
	var injectConfig Config
	if err := yaml.Unmarshal(yml, &injectConfig); err != nil {
		return injectConfig, fmt.Errorf("failed to unmarshal injection template: %v", err)
	}
	if injectConfig.Templates == nil {
		injectConfig.Templates = make(map[string]string)
	}
	if len(injectConfig.DefaultTemplates) == 0 {
		injectConfig.DefaultTemplates = []string{SidecarTemplateName}
	}
	if len(injectConfig.Templates) == 0 {
		log.Warnf("injection templates are empty." +
			" This may be caused by using an injection template from an older version of Istio." +
			" Please ensure the template is correct; mismatch template versions can lead to unexpected results, including pods not being injected.")
	}
	return injectConfig, nil
}

func injectRequired(ignored []string, config *Config, podSpec *corev1.PodSpec, metadata metav1.ObjectMeta) bool { // nolint: lll
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
	if annos == nil {
		annos = map[string]string{}
	}

	var useDefault bool
	var inject bool
	switch strings.ToLower(annos[annotation.SidecarInject.Name]) {
	// http://yaml.org/type/bool.html
	case "y", "yes", "true", "on":
		inject = true
	case "":
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

// RunTemplate renders the sidecar template
// Returns the raw string template, as well as the parse pod form
func RunTemplate(params InjectionParameters) (mergedPod *corev1.Pod, templatePod *corev1.Pod, err error) {
	metadata := &params.pod.ObjectMeta
	meshConfig := params.meshConfig

	if err := validateAnnotations(metadata.GetAnnotations()); err != nil {
		log.Errorf("Injection failed due to invalid annotations: %v", err)
		return nil, nil, err
	}

	if pca, f := metadata.GetAnnotations()[annotation.ProxyConfig.Name]; f {
		var merr error
		meshConfig, merr = mesh.ApplyProxyConfig(pca, *meshConfig)
		if merr != nil {
			return nil, nil, merr
		}
	}

	valuesStruct := &opconfig.Values{}
	if err := gogoprotomarshal.ApplyYAML(params.valuesConfig, valuesStruct); err != nil {
		log.Infof("Failed to parse values config: %v [%v]\n", err, params.valuesConfig)
		return nil, nil, multierror.Prefix(err, "could not parse configuration values:")
	}

	cluster := valuesStruct.GetGlobal().GetMultiCluster().GetClusterName()
	// TODO allow overriding the values.global network in injection with the system namespace label
	network := valuesStruct.GetGlobal().GetNetwork()
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

	// use network in values for template, and proxy env variables
	if cluster != "" {
		params.proxyEnvs["ISTIO_META_CLUSTER_ID"] = cluster
	}
	if network != "" {
		params.proxyEnvs["ISTIO_META_NETWORK"] = network
	}

	values := map[string]interface{}{}
	if err := yaml.Unmarshal([]byte(params.valuesConfig), &values); err != nil {
		log.Infof("Failed to parse values config: %v [%v]\n", err, params.valuesConfig)
		return nil, nil, multierror.Prefix(err, "could not parse configuration values:")
	}

	strippedPod, err := reinsertOverrides(stripPod(params))
	if err != nil {
		return nil, nil, err
	}

	data := SidecarTemplateData{
		TypeMeta:       params.typeMeta,
		DeploymentMeta: params.deployMeta,
		ObjectMeta:     strippedPod.ObjectMeta,
		Spec:           strippedPod.Spec,
		ProxyConfig:    meshConfig.GetDefaultConfig(),
		MeshConfig:     meshConfig,
		Values:         values,
		Revision:       params.revision,
	}
	funcMap := CreateInjectionFuncmap()

	// Need to use FuncMap and SidecarTemplateData context
	funcMap["render"] = func(template string) string {
		bbuf, err := parseTemplate(template, funcMap, data)
		if err != nil {
			return ""
		}

		return bbuf.String()
	}

	mergedPod = params.pod
	templatePod = &corev1.Pod{}
	for _, templateName := range selectTemplates(params) {
		templateYAML, f := params.templates[templateName]
		if !f {
			return nil, nil, fmt.Errorf("requested template %q not found; have %v",
				templateName, strings.Join(knownTemplates(params.templates), ", "))
		}
		bbuf, err := parseTemplate(templateYAML, funcMap, data)
		if err != nil {
			return nil, nil, err
		}

		templateJSON, err := yaml.YAMLToJSON(bbuf.Bytes())
		if err != nil {
			return nil, nil, fmt.Errorf("yaml to json: %v", err)
		}

		mergedPod, err = applyOverlay(mergedPod, templateJSON)
		if err != nil {
			return nil, nil, fmt.Errorf("failed parsing generated injected YAML (check Istio sidecar injector configuration): %v", err)
		}
		templatePod, err = applyOverlay(templatePod, templateJSON)
		if err != nil {
			return nil, nil, fmt.Errorf("failed applying injection overlay: %v", err)
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
	// TODO move annotation to istio/api
	if a, f := params.pod.Annotations[TemplatesAnnotation]; f {
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
	if cur, f := pod.Annotations["prometheus.io/port"]; f {
		// We have already set the port, assume user is controlling this or, more likely, re-injected
		// the pod.
		if cur == targetPort {
			delete(pod.Annotations, "prometheus.io/scrape")
			delete(pod.Annotations, "prometheus.io/path")
			delete(pod.Annotations, "prometheus.io/port")
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
	if err := json.Unmarshal(statusBytes, &iStatus); err == nil {
		return &iStatus
	}
	return nil
}

func parseTemplate(tmplStr string, funcMap map[string]interface{}, data SidecarTemplateData) (bytes.Buffer, error) {
	var tmpl bytes.Buffer
	temp := template.New("inject")
	t, err := temp.Funcs(sprig.TxtFuncMap()).Funcs(funcMap).Parse(tmplStr)
	if err != nil {
		log.Infof("Failed to parse template: %v %v\n", err, tmplStr)
		return bytes.Buffer{}, err
	}
	if err := t.Execute(&tmpl, &data); err != nil {
		log.Infof("Invalid template: %v %v\n", err, tmplStr)
		return bytes.Buffer{}, err
	}

	return tmpl, nil
}

// IntoResourceFile injects the istio proxy into the specified
// kubernetes YAML file.
// nolint: lll
func IntoResourceFile(sidecarTemplate Templates, valuesConfig string, revision string, meshconfig *meshconfig.MeshConfig, in io.Reader, out io.Writer, warningHandler func(string)) error {
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
			outObject, err := IntoObject(sidecarTemplate, valuesConfig, revision, meshconfig, obj, warningHandler) // nolint: vetshadow
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
func IntoObject(sidecarTemplate Templates, valuesConfig string, revision string, meshconfig *meshconfig.MeshConfig, in runtime.Object, warningHandler func(string)) (interface{}, error) {
	out := in.DeepCopyObject()

	var deploymentMetadata *metav1.ObjectMeta
	var metadata *metav1.ObjectMeta
	var podSpec *corev1.PodSpec
	var typeMeta *metav1.TypeMeta

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

			r, err := IntoObject(sidecarTemplate, valuesConfig, revision, meshconfig, obj, warningHandler) // nolint: vetshadow
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
	case *v2alpha1.CronJob:
		job := v
		typeMeta = &job.TypeMeta
		metadata = &job.Spec.JobTemplate.ObjectMeta
		deploymentMetadata = &job.ObjectMeta
		podSpec = &job.Spec.JobTemplate.Spec.Template.Spec
	case *corev1.Pod:
		pod := v
		typeMeta = &pod.TypeMeta
		metadata = &pod.ObjectMeta
		deploymentMetadata = &pod.ObjectMeta
		podSpec = &pod.Spec
	case *appsv1.Deployment: // Added to be explicit about the most expected case
		deploy := v
		typeMeta = &deploy.TypeMeta
		deploymentMetadata = &deploy.ObjectMeta
		metadata = &deploy.Spec.Template.ObjectMeta
		podSpec = &deploy.Spec.Template.Spec
	default:
		// `in` is a pointer to an Object. Dereference it.
		outValue := reflect.ValueOf(out).Elem()

		typeMeta = outValue.FieldByName("TypeMeta").Addr().Interface().(*metav1.TypeMeta)

		deploymentMetadata = outValue.FieldByName("ObjectMeta").Addr().Interface().(*metav1.ObjectMeta)

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
	// Skip injection when host networking is enabled. The problem is
	// that the iptable changes are assumed to be within the pod when,
	// in fact, they are changing the routing at the host level. This
	// often results in routing failures within a node which can
	// affect the network provider within the cluster causing
	// additional pod failures.
	if podSpec.HostNetwork {
		warningHandler(fmt.Sprintf("===> Skipping injection because %q has host networking enabled\n",
			name))
		return out, nil
	}

	// skip injection for injected pods
	if len(podSpec.Containers) > 1 {
		_, hasStatus := metadata.Annotations[annotation.SidecarStatus.Name]
		for _, c := range podSpec.Containers {
			if c.Name == ProxyContainerName && hasStatus {
				warningHandler(fmt.Sprintf("===> Skipping injection because %q has injected %q sidecar already\n",
					name, ProxyContainerName))
				return out, nil
			}
		}
	}

	pod := &corev1.Pod{
		ObjectMeta: *metadata,
		Spec:       *podSpec,
	}
	if !injectRequired(ignoredNamespaces, &Config{Policy: InjectionPolicyEnabled}, &pod.Spec, pod.ObjectMeta) {
		warningHandler(fmt.Sprintf("===> Skipping injection because %q has sidecar injection disabled\n", name))
		return out, nil
	}
	params := InjectionParameters{
		pod:        pod,
		deployMeta: deploymentMetadata,
		typeMeta:   typeMeta,
		// Todo replace with some template resolver abstraction
		templates:           sidecarTemplate,
		defaultTemplate:     []string{SidecarTemplateName},
		meshConfig:          meshconfig,
		valuesConfig:        valuesConfig,
		revision:            revision,
		proxyEnvs:           map[string]string{},
		injectedAnnotations: nil,
	}
	patchBytes, err := injectPod(params)
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
func overwriteClusterInfo(containers []corev1.Container, params InjectionParameters) {
	if len(params.proxyEnvs) > 0 {
		log.Debugf("Updating cluster envs based on inject url: %s\n", params.proxyEnvs)
		for i, c := range containers {
			if c.Name == ProxyContainerName {
				updateClusterEnvs(&containers[i], params.proxyEnvs)
				break
			}
		}
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
