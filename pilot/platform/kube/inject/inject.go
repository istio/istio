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

package inject

// NOTE: This tool only exists because kubernetes does not support
// dynamic/out-of-tree admission controller for transparent proxy
// injection. This file should be removed as soon as a proper kubernetes
// admission controller is written for istio.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/extensions/v1beta1"
	metav1 "k8s.io/client-go/pkg/apis/meta/v1"

	"github.com/ghodss/yaml"
	yamlDecoder "k8s.io/client-go/pkg/util/yaml"
)

// Defaults values for injecting istio proxy into kubernetes
// resources.
const (
	DefaultHub              = "docker.io/istio"
	DefaultTag              = "2017-03-22-17.30.06"
	DefaultManagerAddr      = "istio-manager:8080"
	DefaultMixerAddr        = "istio-mixer:9091"
	DefaultSidecarProxyUID  = int64(1337)
	DefaultSidecarProxyPort = 15001
	DefaultRuntimeVerbosity = 2
)

const (
	istioSidecarAnnotationSidecarKey   = "alpha.istio.io/sidecar"
	istioSidecarAnnotationSidecarValue = "injected"
	istioSidecarAnnotationVersionKey   = "alpha.istio.io/version"
	initContainerName                  = "init"
	runtimeContainerName               = "proxy"
	enableCoreDumpContainerName        = "enable-core-dump"
	enableCoreDumpImage                = "alpine"
)

// InitImageName returns the fully qualified image name for the istio
// init image given a docker hub and tag.
func InitImageName(hub, tag string) string { return hub + "/init:" + tag }

// RuntimeImageName returns the fully qualified image name for the istio
// runtime image given a docker hub and tag.
func RuntimeImageName(hub, tag string) string { return hub + "/runtime:" + tag }

// Params describes configurable parameters for injecting istio proxy
// into kubernetes resource.
type Params struct {
	InitImage        string
	RuntimeImage     string
	RuntimeVerbosity int
	ManagerAddr      string
	MixerAddr        string
	SidecarProxyUID  int64
	SidecarProxyPort int
	Version          string
	EnableCoreDump   bool
}

var enableCoreDumpContainer = map[string]interface{}{
	"name":    enableCoreDumpContainerName,
	"image":   enableCoreDumpImage,
	"command": []string{"/bin/sh"},
	"args": []string{
		"-c",
		"sysctl -w kernel.core_pattern=/tmp/core.%e.%p.%t",
	},
	"imagePullPolicy": "Always",
	"securityContext": map[string]interface{}{
		"privileged": true,
	},
}

func injectIntoPodTemplateSpec(p *Params, t *v1.PodTemplateSpec) error {
	if t.Annotations == nil {
		t.Annotations = make(map[string]string)
	} else if _, ok := t.Annotations[istioSidecarAnnotationSidecarKey]; ok {
		// Return unmodified resource if sidecar is already present or ignored.
		return nil
	}
	t.Annotations[istioSidecarAnnotationSidecarKey] = istioSidecarAnnotationSidecarValue
	t.Annotations[istioSidecarAnnotationVersionKey] = p.Version

	// init-container
	var annotations []interface{}
	if initContainer, ok := t.Annotations["pod.beta.kubernetes.io/init-containers"]; ok {
		if err := json.Unmarshal([]byte(initContainer), &annotations); err != nil {
			return err
		}
	}
	annotations = append(annotations, map[string]interface{}{
		"name":  initContainerName,
		"image": p.InitImage,
		"args": []string{
			"-p", strconv.Itoa(p.SidecarProxyPort),
			"-u", strconv.FormatInt(p.SidecarProxyUID, 10),
		},
		"imagePullPolicy": "Always",
		"securityContext": map[string]interface{}{
			"capabilities": map[string]interface{}{
				"add": []string{
					"NET_ADMIN",
				},
			},
		},
	})

	if p.EnableCoreDump {
		annotations = append(annotations, enableCoreDumpContainer)
	}

	initAnnotationValue, err := json.Marshal(&annotations)
	if err != nil {
		return err
	}
	t.Annotations["pod.beta.kubernetes.io/init-containers"] = string(initAnnotationValue)

	// sidecar proxy container
	t.Spec.Containers = append(t.Spec.Containers,
		v1.Container{
			Name:  runtimeContainerName,
			Image: p.RuntimeImage,
			Args: []string{
				"proxy",
				"sidecar",
				"-s", p.ManagerAddr,
				"-m", p.MixerAddr,
				"-n", "$(POD_NAMESPACE)",
				"-v", strconv.Itoa(p.RuntimeVerbosity),
			},
			Env: []v1.EnvVar{{
				Name: "POD_NAME",
				ValueFrom: &v1.EnvVarSource{
					FieldRef: &v1.ObjectFieldSelector{
						FieldPath: "metadata.name",
					},
				},
			}, {
				Name: "POD_NAMESPACE",
				ValueFrom: &v1.EnvVarSource{
					FieldRef: &v1.ObjectFieldSelector{
						FieldPath: "metadata.namespace",
					},
				},
			}, {
				Name: "POD_IP",
				ValueFrom: &v1.EnvVarSource{
					FieldRef: &v1.ObjectFieldSelector{
						FieldPath: "status.podIP",
					},
				},
			}},
			ImagePullPolicy: v1.PullAlways,
			SecurityContext: &v1.SecurityContext{
				RunAsUser: &p.SidecarProxyUID,
			},
		},
	)
	return nil
}

// IntoResourceFile injects the istio runtime into the specified
// kubernetes YAML file.
func IntoResourceFile(p *Params, in io.Reader, out io.Writer) error {
	reader := yamlDecoder.NewYAMLReader(bufio.NewReaderSize(in, 4096))
	for {
		raw, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		kinds := map[string]struct {
			typ    interface{}
			inject func(typ interface{}) error
		}{
			"Job": {
				typ: &v1beta1.Job{},
				inject: func(typ interface{}) error {
					return injectIntoPodTemplateSpec(p, &((typ.(*v1beta1.Job)).Spec.Template))
				},
			},
			"DaemonSet": {
				typ: &v1beta1.DaemonSet{},
				inject: func(typ interface{}) error {
					return injectIntoPodTemplateSpec(p, &((typ.(*v1beta1.DaemonSet)).Spec.Template))
				},
			},
			"ReplicaSet": {
				typ: &v1beta1.ReplicaSet{},
				inject: func(typ interface{}) error {
					return injectIntoPodTemplateSpec(p, &((typ.(*v1beta1.ReplicaSet)).Spec.Template))
				},
			},
			"Deployment": {
				typ: &v1beta1.Deployment{},
				inject: func(typ interface{}) error {
					return injectIntoPodTemplateSpec(p, &((typ.(*v1beta1.Deployment)).Spec.Template))
				},
			},
		}
		var updated []byte
		var meta metav1.TypeMeta
		if err = yaml.Unmarshal(raw, &meta); err != nil {
			return err
		}
		if kind, ok := kinds[meta.Kind]; ok {
			if err = yaml.Unmarshal(raw, kind.typ); err != nil {
				return err
			}
			if err = kind.inject(kind.typ); err != nil {
				return err
			}
			if updated, err = yaml.Marshal(kind.typ); err != nil {
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
