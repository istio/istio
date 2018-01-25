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
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"text/template"

	"github.com/ghodss/yaml"
	// TODO(nmittler): Remove this
	_ "github.com/golang/glog"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	meshconfig "istio.io/api/mesh/v1alpha1"
)

// per-sidecar policy and status
const (
	istioSidecarAnnotationPolicyKey = "sidecar.istio.io/inject"
	istioSidecarAnnotationStatusKey = "sidecar.istio.io/status"
)

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

// SidecarInjectionSpec collects all container types and volumes for
// sidecar mesh injection
type SidecarInjectionSpec struct {
	InitContainers []v1.Container `yaml:"initContainers"`
	Containers     []v1.Container `yaml:"containers"`
	Volumes        []v1.Volume    `yaml:"volumes"`
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

// Config specifies the sidecar injection configuration This includes
// the sidear template and cluster-side injection policy. It is used
// by kube-inject, sidecar injector, and http endpoint.
type Config struct {
	Policy InjectionPolicy `json:"policy"`

	// Template is the templated version of `SidecarInjectionSpec` prior to
	// expansion over the `SidecarTemplateData`.
	Template string `json:"template"`
}

// SidecarTemplateData is the data object to which the templated
// version of `SidecarInjectionSpec` is applied.
type SidecarTemplateData struct {
	ObjectMeta  *metav1.ObjectMeta
	Spec        *v1.PodSpec
	ProxyConfig *meshconfig.ProxyConfig
	MeshConfig  *meshconfig.MeshConfig
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
