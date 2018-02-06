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
	"os"
	"reflect"
	"strings"
	"text/template"

	"github.com/ghodss/yaml"
	// TODO(nmittler): Remove this
	_ "github.com/golang/glog"

	"k8s.io/api/batch/v2alpha1"
	"k8s.io/api/core/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	yamlDecoder "k8s.io/apimachinery/pkg/util/yaml"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/log"
)

// per-sidecar policy and status
const (
	istioSidecarAnnotationPolicyKey = "sidecar.istio.io/inject"
	istioSidecarAnnotationStatusKey = "sidecar.istio.io/status"
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

// Defaults values for injecting istio proxy into kubernetes
// resources.
const (
	DefaultSidecarProxyUID = uint64(1337)
	DefaultVerbosity       = 2
	DefaultImagePullPolicy = "IfNotPresent"
)

const (
	// ProxyContainerName is used by e2e integration tests for fetching logs
	ProxyContainerName = "istio-proxy"
)

// SidecarInjectionSpec collects all container types and volumes for
// sidecar mesh injection
type SidecarInjectionSpec struct {
	InitContainers []v1.Container `yaml:"initContainers"`
	Containers     []v1.Container `yaml:"containers"`
	Volumes        []v1.Volume    `yaml:"volumes"`
}

// SidecarTemplateData is the data object to which the templated
// version of `SidecarInjectionSpec` is applied.
type SidecarTemplateData struct {
	ObjectMeta  *metav1.ObjectMeta
	Spec        *v1.PodSpec
	ProxyConfig *meshconfig.ProxyConfig
	MeshConfig  *meshconfig.MeshConfig
}

// InitImageName returns the fully qualified image name for the istio
// init image given a docker hub and tag and debug flag
func InitImageName(hub string, tag string, _ bool) string {
	return hub + "/proxy_init:" + tag
}

// ProxyImageName returns the fully qualified image name for the istio
// proxy image given a docker hub and tag and whether to use debug or not.
func ProxyImageName(hub string, tag string, debug bool) string {
	if debug {
		return hub + "/proxy_debug:" + tag
	}
	return hub + "/proxy:" + tag
}

// Params describes configurable parameters for injecting istio proxy
// into a kubernetes resource.
type Params struct {
	InitImage       string                 `json:"initImage"`
	ProxyImage      string                 `json:"proxyImage"`
	Verbosity       int                    `json:"verbosity"`
	SidecarProxyUID uint64                 `json:"sidecarProxyUID"`
	Version         string                 `json:"version"`
	EnableCoreDump  bool                   `json:"enableCoreDump"`
	DebugMode       bool                   `json:"debugMode"`
	Mesh            *meshconfig.MeshConfig `json:"-"`
	ImagePullPolicy string                 `json:"imagePullPolicy"`
	// Comma separated list of IP ranges in CIDR form. If set, only
	// redirect outbound traffic to Envoy for these IP
	// ranges. Otherwise all outbound traffic is redirected to Envoy.
	IncludeIPRanges string `json:"includeIPRanges"`
}

// Config specifies the sidecar injection configuration This includes
// the sidear template and cluster-side injection policy. It is used
// by kube-inject, sidecar injector, and http endpoint.
type Config struct {
	Policy InjectionPolicy `json:"policy"`

	// Template is the templated version of `SidecarInjectionSpec` prior to
	// expansion over the `SidecarTemplateData`.
	Template string `json:"template"`
}

func injectRequired(ignored []string, namespacePolicy InjectionPolicy, podSpec *corev1.PodSpec, metadata *metav1.ObjectMeta) bool { // nolint: lll
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
	switch strings.ToLower(annotations[istioSidecarAnnotationPolicyKey]) {
	// http://yaml.org/type/bool.html
	case "y", "yes", "true", "on":
		inject = true
	case "":
		useDefault = true
	}

	var required bool
	switch namespacePolicy {
	default: // InjectionPolicyOff
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

	status := annotations[istioSidecarAnnotationStatusKey]

	log.Infof("Sidecar injection policy for %v/%v: namespacePolicy:%v useDefault:%v inject:%v status:%q required:%v",
		metadata.Namespace, metadata.Name, namespacePolicy, useDefault, inject, status, required)

	return required
}

func injectionData(sidecarTemplate, version string, spec *v1.PodSpec, metadata *metav1.ObjectMeta, proxyConfig *meshconfig.ProxyConfig, meshConfig *meshconfig.MeshConfig) (*SidecarInjectionSpec, string, error) { // nolint: lll
	data := SidecarTemplateData{
		ObjectMeta:  metadata,
		Spec:        spec,
		ProxyConfig: proxyConfig,
		MeshConfig:  meshConfig,
	}

	var tmpl bytes.Buffer
	t := template.Must(template.New("inject").Parse(sidecarTemplate))
	if err := t.Execute(&tmpl, &data); err != nil {
		return nil, "", err
	}

	var sic SidecarInjectionSpec
	if err := yaml.Unmarshal(tmpl.Bytes(), &sic); err != nil {
		return nil, "", err
	}

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
	statusAnnotationValue, err := json.Marshal(status)
	if err != nil {
		return nil, "", fmt.Errorf("error encoded injection status: %v", err)
	}
	return &sic, string(statusAnnotationValue), nil
}

// IntoResourceFile injects the istio proxy into the specified
// kubernetes YAML file.
func IntoResourceFile(sidecarTemplate string, meshconfig *meshconfig.MeshConfig, in io.Reader, out io.Writer) error {
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
			outObject, err := intoObject(sidecarTemplate, meshconfig, obj) // nolint: vetshadow
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

func intoObject(sidecarTemplate string, meshconfig *meshconfig.MeshConfig, in runtime.Object) (interface{}, error) {
	out := in.DeepCopyObject()

	var metadata *metav1.ObjectMeta
	var podSpec *v1.PodSpec

	// Handle Lists
	if list, ok := out.(*v1.List); ok {
		result := list

		for i, item := range list.Items {
			obj, err := fromRawToObject(item.Raw)
			if runtime.IsNotRegisteredError(err) {
				continue
			}
			if err != nil {
				return nil, err
			}

			r, err := intoObject(sidecarTemplate, meshconfig, obj) // nolint: vetshadow
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
	if job, ok := out.(*v2alpha1.CronJob); ok {
		metadata = &job.Spec.JobTemplate.ObjectMeta
		podSpec = &job.Spec.JobTemplate.Spec.Template.Spec
	} else {
		// `in` is a pointer to an Object. Dereference it.
		outValue := reflect.ValueOf(out).Elem()

		templateValue := outValue.FieldByName("Spec").FieldByName("Template")
		// `Template` is defined as a pointer in some older API
		// definitions, e.g. ReplicationController
		if templateValue.Kind() == reflect.Ptr {
			templateValue = templateValue.Elem()
		}
		metadata = templateValue.FieldByName("ObjectMeta").Addr().Interface().(*metav1.ObjectMeta)
		podSpec = templateValue.FieldByName("Spec").Addr().Interface().(*v1.PodSpec)
	}

	// Skip injection when host networking is enabled. The problem is
	// that the iptable changes are assumed to be within the pod when,
	// in fact, they are changing the routing at the host level. This
	// often results in routing failures within a node which can
	// affect the network provider within the cluster causing
	// additional pod failures.
	if podSpec.HostNetwork {
		fmt.Fprintf(os.Stderr, "Skipping injection because %q has host networking enabled", metadata.Name)
		return out, nil
	}

	spec, status, err := injectionData(
		sidecarTemplate,
		sidecarTemplateVersionHash(sidecarTemplate),
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

	if metadata.Annotations == nil {
		metadata.Annotations = make(map[string]string)
	}
	metadata.Annotations[istioSidecarAnnotationStatusKey] = status

	return out, nil
}

// GenerateTemplateFromParams generates a sidecar template from the legacy injection parameters
func GenerateTemplateFromParams(params *Params) (string, error) {
	t := template.New("inject").Delims(parameterizedTemplateDelimBegin, parameterizedTemplateDelimEnd)
	var tmp bytes.Buffer
	err := template.Must(t.Parse(parameterizedTemplate)).Execute(&tmp, params)
	return tmp.String(), err
}

// SidecarInjectionStatus contains basic information about the
// injected sidecar. This includes the names of added containers and
// volumes.
type SidecarInjectionStatus struct {
	Version        string   `json:"version"`
	InitContainers []string `json:"initContainers"`
	Containers     []string `json:"containers"`
	Volumes        []string `json:"volumes"`
}

// helper function to generate a template version identifier from a
// hash of the un-executed template contents.
func sidecarTemplateVersionHash(in string) string {
	hash := sha256.Sum256([]byte(in))
	return hex.EncodeToString(hash[:])
}
