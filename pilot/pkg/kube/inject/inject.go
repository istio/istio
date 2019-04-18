// Copyright 2018 Istio Authors
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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"reflect"
	"strconv"
	"strings"
	"text/template"

	"istio.io/istio/pkg/annotations"

	"github.com/ghodss/yaml"
	"github.com/gogo/protobuf/types"
	"github.com/hashicorp/go-multierror"
	"k8s.io/api/batch/v2alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	yamlDecoder "k8s.io/apimachinery/pkg/util/yaml"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
)

type annotationValidationFunc func(value string) error

const (
	annotationPolicy                = "sidecar.istio.io/inject"
	annotationStatus                = "sidecar.istio.io/status"
	annotationRewriteAppHTTPProbers = "sidecar.istio.io/rewriteAppHTTPProbers"
)

// per-sidecar policy and status
var (
	alwaysValidFunc = func(value string) error {
		return nil
	}

	annotationRegistry = map[string]annotationValidationFunc{
		annotations.Register(annotationPolicy, "").Name: alwaysValidFunc,
		annotations.Register(annotationStatus, "").Name: alwaysValidFunc,
		annotations.Register(annotationRewriteAppHTTPProbers,
			"Rewrite HTTP readiness and liveness probes to be redirected to istio-proxy sidecar").Name: alwaysValidFunc,
		annotations.Register("sidecar.istio.io/proxyImage", "").Name:                           alwaysValidFunc,
		annotations.Register("sidecar.istio.io/interceptionMode", "").Name:                     validateInterceptionMode,
		annotations.Register("status.sidecar.istio.io/port", "").Name:                          validateStatusPort,
		annotations.Register("readiness.status.sidecar.istio.io/initialDelaySeconds", "").Name: validateUInt32,
		annotations.Register("readiness.status.sidecar.istio.io/periodSeconds", "").Name:       validateUInt32,
		annotations.Register("readiness.status.sidecar.istio.io/failureThreshold", "").Name:    validateUInt32,
		annotations.Register("readiness.status.sidecar.istio.io/applicationPorts", "").Name:    validateReadinessApplicationPorts,
		annotations.Register("traffic.sidecar.istio.io/includeOutboundIPRanges", "").Name:      ValidateIncludeIPRanges,
		annotations.Register("traffic.sidecar.istio.io/excludeOutboundIPRanges", "").Name:      ValidateExcludeIPRanges,
		annotations.Register("traffic.sidecar.istio.io/includeInboundPorts", "").Name:          ValidateIncludeInboundPorts,
		annotations.Register("traffic.sidecar.istio.io/excludeInboundPorts", "").Name:          ValidateExcludeInboundPorts,
		annotations.Register("traffic.sidecar.istio.io/kubevirtInterfaces", "").Name:           alwaysValidFunc,
	}
)

func validateAnnotations(annotations map[string]string) (err error) {
	for name, value := range annotations {
		if v, ok := annotationRegistry[name]; ok {
			if e := v(value); e != nil {
				err = multierror.Append(err, fmt.Errorf("invalid value '%s' for annotation '%s': %v", value, name, e))
			}
		} else if strings.Contains(name, "istio") {
			log.Warnf("Potentially misspelled annotation '%s' with value '%s' encountered", name, value)
		}
	}
	return
}

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

// Defaults values for injecting istio proxy into kubernetes
// resources.
const (
	DefaultSidecarProxyUID              = uint64(1337)
	DefaultVerbosity                    = 2
	DefaultImagePullPolicy              = "IfNotPresent"
	DefaultStatusPort                   = 15020
	DefaultReadinessInitialDelaySeconds = 1
	DefaultReadinessPeriodSeconds       = 2
	DefaultReadinessFailureThreshold    = 30
	DefaultIncludeIPRanges              = "*"
	DefaultIncludeInboundPorts          = "*"
	DefaultkubevirtInterfaces           = ""
)

const (
	// ProxyContainerName is used by e2e integration tests for fetching logs
	ProxyContainerName = "istio-proxy"
)

// SidecarInjectionSpec collects all container types and volumes for
// sidecar mesh injection
type SidecarInjectionSpec struct {
	// RewriteHTTPProbe indicates whether Kubernetes HTTP prober in the PodSpec
	// will be rewritten to be redirected by pilot agent.
	RewriteAppHTTPProbe bool                          `yaml:"rewriteAppHTTPProbe"`
	InitContainers      []corev1.Container            `yaml:"initContainers"`
	Containers          []corev1.Container            `yaml:"containers"`
	Volumes             []corev1.Volume               `yaml:"volumes"`
	DNSConfig           *corev1.PodDNSConfig          `yaml:"dnsConfig"`
	ImagePullSecrets    []corev1.LocalObjectReference `yaml:"imagePullSecrets"`
}

// SidecarTemplateData is the data object to which the templated
// version of `SidecarInjectionSpec` is applied.
type SidecarTemplateData struct {
	DeploymentMeta *metav1.ObjectMeta
	ObjectMeta     *metav1.ObjectMeta
	Spec           *corev1.PodSpec
	ProxyConfig    *meshconfig.ProxyConfig
	MeshConfig     *meshconfig.MeshConfig
	Values         map[string]interface{}
}

// InitImageName returns the fully qualified image name for the istio
// init image given a docker hub and tag and debug flag
func InitImageName(hub string, tag string, _ bool) string {
	return hub + "/proxy_init:" + tag
}

// ProxyImageName returns the fully qualified image name for the istio
// proxy image given a docker hub and tag and whether to use debug or not.
func ProxyImageName(hub string, tag string, debug bool) string {
	// Allow overriding the proxy image.
	if debug {
		return hub + "/proxy_debug:" + tag
	}
	return hub + "/proxyv2:" + tag
}

// Params describes configurable parameters for injecting istio proxy
// into a kubernetes resource.
type Params struct {
	InitImage       string `json:"initImage"`
	ProxyImage      string `json:"proxyImage"`
	Version         string `json:"version"`
	ImagePullPolicy string `json:"imagePullPolicy"`
	Tracer          string `json:"tracer"`
	// Comma separated list of IP ranges in CIDR form. If set, only redirect outbound traffic to Envoy for these IP
	// ranges. All outbound traffic can be redirected with the wildcard character "*". Defaults to "*".
	IncludeIPRanges string `json:"includeIPRanges"`
	// Comma separated list of IP ranges in CIDR form. If set, outbound traffic will not be redirected for
	// these IP ranges. Exclusions are only applied if configured to redirect all outbound traffic. By default,
	// no IP ranges are excluded.
	ExcludeIPRanges string `json:"excludeIPRanges"`
	// Comma separated list of inbound ports for which traffic is to be redirected to Envoy. All ports can be
	// redirected with the wildcard character "*". Defaults to "*".
	IncludeInboundPorts string `json:"includeInboundPorts"`
	// Comma separated list of inbound ports. If set, inbound traffic will not be redirected for those ports.
	// Exclusions are only applied if configured to redirect all inbound traffic. By default, no ports are excluded.
	ExcludeInboundPorts string `json:"excludeInboundPorts"`
	// Comma separated list of virtual interfaces whose inbound traffic (from VM) will be treated as outbound
	// By default, no interfaces are configured.
	KubevirtInterfaces           string                 `json:"kubevirtInterfaces"`
	Verbosity                    int                    `json:"verbosity"`
	SidecarProxyUID              uint64                 `json:"sidecarProxyUID"`
	Mesh                         *meshconfig.MeshConfig `json:"-"`
	StatusPort                   int                    `json:"statusPort"`
	ReadinessInitialDelaySeconds uint32                 `json:"readinessInitialDelaySeconds"`
	ReadinessPeriodSeconds       uint32                 `json:"readinessPeriodSeconds"`
	ReadinessFailureThreshold    uint32                 `json:"readinessFailureThreshold"`
	RewriteAppHTTPProbe          bool                   `json:"rewriteAppHTTPProbe"`
	EnableCoreDump               bool                   `json:"enableCoreDump"`
	DebugMode                    bool                   `json:"debugMode"`
	Privileged                   bool                   `json:"privileged"`
	SDSEnabled                   bool                   `json:"sdsEnabled"`
	EnableSdsTokenMount          bool                   `json:"enableSdsTokenMount"`
}

// Validate validates the parameters and returns an error if there is configuration issue.
func (p *Params) Validate() error {
	if err := ValidateIncludeIPRanges(p.IncludeIPRanges); err != nil {
		return err
	}
	if err := ValidateExcludeIPRanges(p.ExcludeIPRanges); err != nil {
		return err
	}
	if err := ValidateIncludeInboundPorts(p.IncludeInboundPorts); err != nil {
		return err
	}
	return ValidateExcludeInboundPorts(p.ExcludeInboundPorts)
}

// intoHelmValues returns a map of the traversed path in helm values YAML to the param value.
func (p *Params) intoHelmValues() map[string]string {
	vals := map[string]string{
		"global.proxy_init.image":                    p.InitImage,
		"global.proxy.image":                         p.ProxyImage,
		"global.proxy.enableCoreDump":                strconv.FormatBool(p.EnableCoreDump),
		"global.proxy.privileged":                    strconv.FormatBool(p.Privileged),
		"global.imagePullPolicy":                     p.ImagePullPolicy,
		"global.proxy.statusPort":                    strconv.Itoa(p.StatusPort),
		"global.proxy.tracer":                        p.Tracer,
		"global.proxy.readinessInitialDelaySeconds":  strconv.Itoa(int(p.ReadinessInitialDelaySeconds)),
		"global.proxy.readinessPeriodSeconds":        strconv.Itoa(int(p.ReadinessPeriodSeconds)),
		"global.proxy.readinessFailureThreshold":     strconv.Itoa(int(p.ReadinessFailureThreshold)),
		"global.sds.enabled":                         strconv.FormatBool(p.SDSEnabled),
		"global.sds.useTrustworthyJwt":               strconv.FormatBool(p.EnableSdsTokenMount),
		"global.proxy.includeIPRanges":               p.IncludeIPRanges,
		"global.proxy.excludeIPRanges":               p.ExcludeIPRanges,
		"global.proxy.includeInboundPorts":           p.IncludeInboundPorts,
		"global.proxy.excludeInboundPorts":           p.ExcludeInboundPorts,
		"sidecarInjectorWebhook.rewriteAppHTTPProbe": strconv.FormatBool(p.RewriteAppHTTPProbe),
	}
	return vals
}

// Config specifies the sidecar injection configuration This includes
// the sidecar template and cluster-side injection policy. It is used
// by kube-inject, sidecar injector, and http endpoint.
type Config struct {
	Policy InjectionPolicy `json:"policy"`

	// Template is the templated version of `SidecarInjectionSpec` prior to
	// expansion over the `SidecarTemplateData`.
	Template string `json:"template"`

	// NeverInjectSelector: Refuses the injection on pods whose labels match this selector.
	// It's an array of label selectors, that will be OR'ed, meaning we will iterate
	// over it and stop at the first match
	// Takes precedence over AlwaysInjectSelector.
	NeverInjectSelector []metav1.LabelSelector `json:"neverInjectSelector"`

	// AlwaysInjectSelector: Forces the injection on pods whose labels match this selector.
	// It's an array of label selectors, that will be OR'ed, meaning we will iterate
	// over it and stop at the first match
	AlwaysInjectSelector []metav1.LabelSelector `json:"alwaysInjectSelector"`
}

func validateCIDRList(cidrs string) error {
	if len(cidrs) > 0 {
		for _, cidr := range strings.Split(cidrs, ",") {
			if _, _, err := net.ParseCIDR(cidr); err != nil {
				return fmt.Errorf("failed parsing cidr '%s': %v", cidr, err)
			}
		}
	}
	return nil
}

func splitPorts(portsString string) []string {
	return strings.Split(portsString, ",")
}

func parsePort(portStr string) (int, error) {
	port, err := strconv.ParseUint(strings.TrimSpace(portStr), 10, 16)
	if err != nil {
		return 0, fmt.Errorf("failed parsing port '%d': %v", port, err)
	}
	return int(port), nil
}

func parsePorts(portsString string) ([]int, error) {
	portsString = strings.TrimSpace(portsString)
	ports := make([]int, 0)
	if len(portsString) > 0 {
		for _, portStr := range splitPorts(portsString) {
			port, err := parsePort(portStr)
			if err != nil {
				return nil, fmt.Errorf("failed parsing port '%d': %v", port, err)
			}
			ports = append(ports, port)
		}
	}
	return ports, nil
}

func validatePortList(parameterName, ports string) error {
	if _, err := parsePorts(ports); err != nil {
		return fmt.Errorf("%s invalid: %v", parameterName, err)
	}
	return nil
}

// validateInterceptionMode validates the interceptionMode annotation
func validateInterceptionMode(mode string) error {
	switch mode {
	case meshconfig.ProxyConfig_REDIRECT.String():
	case meshconfig.ProxyConfig_TPROXY.String():
	case string(model.InterceptionNone): // not a global mesh config - must be enabled for each sidecar
	default:
		return fmt.Errorf("interceptionMode invalid, use REDIRECT,TPROXY,NONE: %v", mode)
	}
	return nil
}

// ValidateIncludeIPRanges validates the includeIPRanges parameter
func ValidateIncludeIPRanges(ipRanges string) error {
	if ipRanges != "*" {
		if e := validateCIDRList(ipRanges); e != nil {
			return fmt.Errorf("includeIPRanges invalid: %v", e)
		}
	}
	return nil
}

// ValidateExcludeIPRanges validates the excludeIPRanges parameter
func ValidateExcludeIPRanges(ipRanges string) error {
	if e := validateCIDRList(ipRanges); e != nil {
		return fmt.Errorf("excludeIPRanges invalid: %v", e)
	}
	return nil
}

func validateReadinessApplicationPorts(ports string) error {
	if ports != "*" {
		return validatePortList("readinessApplicationPorts", ports)
	}
	return nil
}

// ValidateIncludeInboundPorts validates the includeInboundPorts parameter
func ValidateIncludeInboundPorts(ports string) error {
	if ports != "*" {
		return validatePortList("includeInboundPorts", ports)
	}
	return nil
}

// ValidateExcludeInboundPorts validates the excludeInboundPorts parameter
func ValidateExcludeInboundPorts(ports string) error {
	return validatePortList("excludeInboundPorts", ports)
}

// validateStatusPort validates the statusPort parameter
func validateStatusPort(port string) error {
	if _, e := parsePort(port); e != nil {
		return fmt.Errorf("excludeInboundPorts invalid: %v", e)
	}
	return nil
}

// validateUInt32 validates that the given annotation value is a positive integer.
func validateUInt32(value string) error {
	_, err := strconv.ParseUint(value, 10, 32)
	return err
}

func injectRequired(ignored []string, config *Config, podSpec *corev1.PodSpec, metadata *metav1.ObjectMeta) bool { // nolint: lll
	// Skip injection when host networking is enabled. The problem is
	// that the iptable changes are assumed to be within the pod when,
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

	annotations := metadata.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}

	var useDefault bool
	var inject bool
	switch strings.ToLower(annotations[annotationPolicy]) {
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
		for name := range annotationRegistry {
			value, ok := annotations[name]
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

func formatDuration(in *types.Duration) string {
	dur, err := types.DurationFromProto(in)
	if err != nil {
		return "1s"
	}
	return dur.String()
}

func isset(m map[string]string, key string) bool {
	_, ok := m[key]
	return ok
}

func directory(filepath string) string {
	dir, _ := path.Split(filepath)
	return dir
}

func flippedContains(needle, haystack string) bool {
	return strings.Contains(haystack, needle)
}

func InjectionData(sidecarTemplate, valuesConfig, version string, deploymentMetadata *metav1.ObjectMeta, spec *corev1.PodSpec,
	metadata *metav1.ObjectMeta, proxyConfig *meshconfig.ProxyConfig, meshConfig *meshconfig.MeshConfig) (
	*SidecarInjectionSpec, string, error) {
	if err := validateAnnotations(metadata.GetAnnotations()); err != nil {
		log.Errorf("Injection failed due to invalid annotations: %v", err)
		return nil, "", err
	}

	values := map[string]interface{}{}
	if err := yaml.Unmarshal([]byte(valuesConfig), &values); err != nil {
		log.Infof("Failed to parse values config: %v [%v]\n", err, valuesConfig)
		return nil, "", err
	}

	data := SidecarTemplateData{
		DeploymentMeta: deploymentMetadata,
		ObjectMeta:     metadata,
		Spec:           spec,
		ProxyConfig:    proxyConfig,
		MeshConfig:     meshConfig,
		Values:         values,
	}

	funcMap := template.FuncMap{
		"formatDuration":      formatDuration,
		"isset":               isset,
		"excludeInboundPort":  excludeInboundPort,
		"includeInboundPorts": includeInboundPorts,
		"kubevirtInterfaces":  kubevirtInterfaces,
		"applicationPorts":    applicationPorts,
		"annotation":          annotation,
		"valueOrDefault":      valueOrDefault,
		"toJSON":              toJSON,
		"toJson":              toJSON, // Used by, e.g. Istio 1.0.5 template sidecar-injector-configmap.yaml
		"fromJSON":            fromJSON,
		"toYaml":              toYaml,
		"indent":              indent,
		"directory":           directory,
		"contains":            flippedContains,
	}

	var tmpl bytes.Buffer
	temp := template.New("inject")
	t, err := temp.Funcs(funcMap).Parse(sidecarTemplate)
	if err != nil {
		log.Infof("Failed to parse template: %v %v\n", err, sidecarTemplate)
		return nil, "", err
	}
	if err := t.Execute(&tmpl, &data); err != nil {
		log.Infof("Invalid template: %v %v\n", err, sidecarTemplate)
		return nil, "", err
	}

	var sic SidecarInjectionSpec
	if err := yaml.Unmarshal(tmpl.Bytes(), &sic); err != nil {
		log.Warnf("Failed to unmarshall template %v %s", err, tmpl.String())
		return nil, "", err
	}

	// set sidecar --concurrency
	applyConcurrency(sic.Containers)

	status := &SidecarInjectionStatus{Version: version}
	for _, c := range sic.InitContainers {
		status.InitContainers = append(status.InitContainers, c.Name)
	}
	for _, c := range sic.Containers {
		status.Containers = append(status.Containers, c.Name)
	}
	for _, c := range sic.Volumes {
		status.Volumes = append(status.Volumes, c.Name)
	}
	for _, c := range sic.ImagePullSecrets {
		status.ImagePullSecrets = append(status.ImagePullSecrets, c.Name)
	}
	statusAnnotationValue, err := json.Marshal(status)
	if err != nil {
		return nil, "", fmt.Errorf("error encoded injection status: %v", err)
	}
	return &sic, string(statusAnnotationValue), nil
}

// IntoResourceFile injects the istio proxy into the specified
// kubernetes YAML file.
func IntoResourceFile(sidecarTemplate string, valuesConfig string, meshconfig *meshconfig.MeshConfig, in io.Reader, out io.Writer) error {
	reader := yamlDecoder.NewYAMLReader(bufio.NewReaderSize(in, 4096))
	for {
		raw, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		obj, err := fromRawToObject(raw)
		if err != nil && !runtime.IsNotRegisteredError(err) {
			return err
		}

		var updated []byte
		if err == nil {
			outObject, err := intoObject(sidecarTemplate, valuesConfig, meshconfig, obj) // nolint: vetshadow
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

func fromRawToObject(raw []byte) (runtime.Object, error) {
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

func intoObject(sidecarTemplate string, valuesConfig string, meshconfig *meshconfig.MeshConfig, in runtime.Object) (interface{}, error) {
	out := in.DeepCopyObject()

	var deploymentMetadata *metav1.ObjectMeta
	var metadata *metav1.ObjectMeta
	var podSpec *corev1.PodSpec

	// Handle Lists
	if list, ok := out.(*corev1.List); ok {
		result := list

		for i, item := range list.Items {
			obj, err := fromRawToObject(item.Raw)
			if runtime.IsNotRegisteredError(err) {
				continue
			}
			if err != nil {
				return nil, err
			}

			r, err := intoObject(sidecarTemplate, valuesConfig, meshconfig, obj) // nolint: vetshadow
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
		metadata = &job.Spec.JobTemplate.ObjectMeta
		deploymentMetadata = &job.ObjectMeta
		podSpec = &job.Spec.JobTemplate.Spec.Template.Spec
	case *corev1.Pod:
		pod := v
		metadata = &pod.ObjectMeta
		deploymentMetadata = &pod.ObjectMeta
		podSpec = &pod.Spec
	default:
		// `in` is a pointer to an Object. Dereference it.
		outValue := reflect.ValueOf(out).Elem()

		deploymentMetadata = outValue.FieldByName("ObjectMeta").Addr().Interface().(*metav1.ObjectMeta)

		templateValue := outValue.FieldByName("Spec").FieldByName("Template")
		// `Template` is defined as a pointer in some older API
		// definitions, e.g. ReplicationController
		if templateValue.Kind() == reflect.Ptr {
			templateValue = templateValue.Elem()
		}
		metadata = templateValue.FieldByName("ObjectMeta").Addr().Interface().(*metav1.ObjectMeta)
		podSpec = templateValue.FieldByName("Spec").Addr().Interface().(*corev1.PodSpec)
	}

	// Skip injection when host networking is enabled. The problem is
	// that the iptable changes are assumed to be within the pod when,
	// in fact, they are changing the routing at the host level. This
	// often results in routing failures within a node which can
	// affect the network provider within the cluster causing
	// additional pod failures.
	if podSpec.HostNetwork {
		fmt.Fprintf(os.Stderr, "Skipping injection because %q has host networking enabled\n", metadata.Name) //nolint: errcheck
		return out, nil
	}

	spec, status, err := InjectionData(
		sidecarTemplate,
		valuesConfig,
		sidecarTemplateVersionHash(sidecarTemplate),
		deploymentMetadata,
		podSpec,
		metadata,
		meshconfig.DefaultConfig,
		meshconfig)
	if err != nil {
		return nil, err
	}

	podSpec.InitContainers = append(podSpec.InitContainers, spec.InitContainers...)

	podSpec.Containers = append(podSpec.Containers, spec.Containers...)
	podSpec.Volumes = append(podSpec.Volumes, spec.Volumes...)

	podSpec.DNSConfig = spec.DNSConfig

	// Modify application containers' HTTP probe after appending injected containers.
	// Because we need to extract istio-proxy's statusPort.
	rewriteAppHTTPProbe(metadata.Annotations, podSpec, spec)

	// due to bug https://github.com/kubernetes/kubernetes/issues/57923,
	// k8s sa jwt token volume mount file is only accessible to root user, not istio-proxy(the user that istio proxy runs as).
	// workaround by https://kubernetes.io/docs/tasks/configure-pod-container/security-context/#set-the-security-context-for-a-pod
	if meshconfig.EnableSdsTokenMount && meshconfig.SdsUdsPath != "" {
		var grp = int64(1337)
		podSpec.SecurityContext = &corev1.PodSecurityContext{
			FSGroup: &grp,
		}
	}

	if metadata.Annotations == nil {
		metadata.Annotations = make(map[string]string)
	}
	metadata.Annotations[annotationStatus] = status

	return out, nil
}

func getPortsForContainer(container corev1.Container) []string {
	parts := make([]string, 0)
	for _, p := range container.Ports {
		parts = append(parts, strconv.Itoa(int(p.ContainerPort)))
	}
	return parts
}

func getContainerPorts(containers []corev1.Container, shouldIncludePorts func(corev1.Container) bool) string {
	parts := make([]string, 0)
	for _, c := range containers {
		if shouldIncludePorts(c) {
			parts = append(parts, getPortsForContainer(c)...)
		}
	}

	return strings.Join(parts, ",")
}

func applicationPorts(containers []corev1.Container) string {
	return getContainerPorts(containers, func(c corev1.Container) bool {
		return c.Name != ProxyContainerName
	})
}

func includeInboundPorts(containers []corev1.Container) string {
	// Include the ports from all containers in the deployment.
	return getContainerPorts(containers, func(corev1.Container) bool { return true })
}

func kubevirtInterfaces(s string) string {
	return s
}

func toJSON(m map[string]string) string {
	if m == nil {
		return "{}"
	}

	ba, err := json.Marshal(m)
	if err != nil {
		log.Warnf("Unable to marshal %v", m)
		return "{}"
	}

	return string(ba)
}

func fromJSON(j string) interface{} {
	var m interface{}
	err := json.Unmarshal([]byte(j), &m)
	if err != nil {
		log.Warnf("Unable to unmarshal %s", j)
		return "{}"
	}

	log.Warnf("%v", m)
	return m
}

func indent(spaces int, source string) string {
	res := strings.Split(source, "\n")
	for i, line := range res {
		if i > 0 {
			res[i] = fmt.Sprintf(fmt.Sprintf("%% %ds%%s", spaces), "", line)
		}
	}
	return strings.Join(res, "\n")
}

func toYaml(value interface{}) string {
	y, err := yaml.Marshal(value)
	if err != nil {
		log.Warnf("Unable to marshal %v", value)
		return ""
	}

	return string(y)
}

func annotation(meta metav1.ObjectMeta, name string, defaultValue interface{}) string {
	value, ok := meta.Annotations[name]
	if !ok {
		value = fmt.Sprint(defaultValue)
	}
	return value
}

func excludeInboundPort(port interface{}, excludedInboundPorts string) string {
	portStr := strings.TrimSpace(fmt.Sprint(port))
	if len(portStr) == 0 || portStr == "0" {
		// Nothing to do.
		return excludedInboundPorts
	}

	// Exclude the readiness port if not already excluded.
	ports := splitPorts(excludedInboundPorts)
	outPorts := make([]string, 0, len(ports))
	for _, port := range ports {
		if port == portStr {
			// The port is already excluded.
			return excludedInboundPorts
		}
		port = strings.TrimSpace(port)
		if len(port) > 0 {
			outPorts = append(outPorts, port)
		}
	}

	// The port was not already excluded - exclude it now.
	outPorts = append(outPorts, portStr)
	return strings.Join(outPorts, ",")
}

func valueOrDefault(value interface{}, defaultValue interface{}) interface{} {
	if value == "" || value == nil {
		return defaultValue
	}
	return value
}

// SidecarInjectionStatus contains basic information about the
// injected sidecar. This includes the names of added containers and
// volumes.
type SidecarInjectionStatus struct {
	Version          string   `json:"version"`
	InitContainers   []string `json:"initContainers"`
	Containers       []string `json:"containers"`
	Volumes          []string `json:"volumes"`
	ImagePullSecrets []string `json:"imagePullSecrets"`
}

// helper function to generate a template version identifier from a
// hash of the un-executed template contents.
func sidecarTemplateVersionHash(in string) string {
	hash := sha256.Sum256([]byte(in))
	return hex.EncodeToString(hash[:])
}

func potentialPodName(metadata *metav1.ObjectMeta) string {
	if metadata.Name != "" {
		return metadata.Name
	}
	if metadata.GenerateName != "" {
		return metadata.GenerateName + "***** (actual name not yet known)"
	}
	return ""
}
