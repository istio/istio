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
	"fmt"
	"io"
	"reflect"
	"strconv"
	"time"

	"github.com/ghodss/yaml"
	"github.com/golang/glog"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	yamlDecoder "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/pilot/tools/version"
)

// TODO - Temporally retain the deprecated alpha annotations to ease
// migration to k8s sidecar initializer.
var (
	insertDeprecatedAlphaAnnotation = true
	checkDeprecatedAlphaAnnotation  = true
)

const istioSidecarAnnotationStatusKey = "status.sidecar.istio.io"

// per-sidecar policy (deployment, job, statefulset, pod, etc)
const (
	istioSidecarAnnotationPolicyKey           = "policy.sidecar.istio.io"
	istioSidecarAnnotationPolicyValueDefault  = "policy.sidecar.istio.io/default"
	istioSidecarAnnotationPolicyValueForceOn  = "policy.sidecar.istio.io/force-on"
	istioSidecarAnnotationPolicyValueForceOff = "policy.sidecar.istio.io/force-off"
)

// InjectionPolicy determines the policy for injecting the sidecar
// proxy into the watched namespace(s).
type InjectionPolicy string

const (
	// InjectionPolicyOff disables the initializer from modifying
	// resources. The pending 'status.sidecar.istio.io initializer'
	// initializer is still removed to avoid blocking creation of
	// resources.
	InjectionPolicyOff InjectionPolicy = "off"

	// InjectionPolicyOptIn specifies that the initializer will not
	// inject the sidecar into resources by default for the
	// namespace(s) being watched. Resources can opt-in using the
	// "policy.sidecar.istio.io" annotation with value of
	// policy.sidecar.istio.io/force-on.
	InjectionPolicyOptIn InjectionPolicy = "opt-in"

	// InjectionPolicyOptOut specifies that the initializer will
	// inject the sidecar into resources by default for the
	// namespace(s) being watched. Resources can opt-out using the
	// "policy.sidecar.istio.io" annotation with value of
	// policy.sidecar.istio.io/force-off.
	InjectionPolicyOptOut InjectionPolicy = "opt-out"

	// DefaultInjectionPolicy is the default injection policy.
	DefaultInjectionPolicy = InjectionPolicyOptOut
)

// Defaults values for injecting istio proxy into kubernetes
// resources.
const (
	DefaultSidecarProxyUID   = int64(1337)
	DefaultVerbosity         = 2
	DefaultHub               = "docker.io/istio"
	DefaultMeshConfigMapName = "meshConfig"
	DefaultImagePullPolicy   = "IfNotPresent"
)

const (
	// deprecated - remove after istio/istio is updated to use new templates
	deprecatedIstioSidecarAnnotationSidecarKey   = "alpha.istio.io/sidecar"
	deprecatedIstioSidecarAnnotationSidecarValue = "injected(deprecated)"

	// InitContainerName is the name for init container
	InitContainerName = "istio-init"

	// ProxyContainerName is the name for sidecar proxy container
	ProxyContainerName = "istio-proxy"

	enableCoreDumpContainerName = "enable-core-dump"
	enableCoreDumpImage         = "alpine"

	istioCertSecretPrefix = "istio."

	istioCertVolumeName        = "istio-certs"
	istioConfigVolumeName      = "istio-config"
	istioEnvoyConfigVolumeName = "istio-envoy"

	// ConfigMapKey should match the expected MeshConfig file name
	ConfigMapKey = "mesh"

	// InitializerConfigMapKey is the key into the initailizer ConfigMap data.
	InitializerConfigMapKey = "config"

	// EnvoyConfigPath is the temporary directory for storing configuration
	EnvoyConfigPath = "/etc/istio/proxy"

	// DefaultResyncPeriod specifies how frequently to retrieve the
	// full list of watched resources for initialization.
	DefaultResyncPeriod = 30 * time.Second
)

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
// into kubernetes resource.
type Params struct {
	InitImage         string                       `json:"initImage"`
	ProxyImage        string                       `json:"proxyImage"`
	Verbosity         int                          `json:"verbosity"`
	SidecarProxyUID   int64                        `json:"sidecarProxyUID"`
	Version           string                       `json:"version"`
	EnableCoreDump    bool                         `json:"enableCoreDump"`
	DebugMode         bool                         `json:"debugMode"`
	Mesh              *proxyconfig.ProxyMeshConfig `json:"-"`
	MeshConfigMapName string                       `json:"meshConfigMapName"`
	ImagePullPolicy   string                       `json:"imagePullPolicy"`
	// Comma separated list of IP ranges in CIDR form. If set, only
	// redirect outbound traffic to Envoy for these IP
	// ranges. Otherwise all outbound traffic is redirected to Envoy.
	IncludeIPRanges string `json:"includeIPRanges"`
}

// Config specifies the initializer configuration for sidecar
// injection. This includes the sidear template and cluster-side
// injection policy. It is used by kube-inject, initializer, and http
// endpoint.
type Config struct {
	Policy InjectionPolicy `json:"policy"`

	// deprecate if InitializerConfiguration becomes namespace aware
	Namespaces []string `json:"namespaces"`

	// Params specifies the parameters of the injected sidcar template
	Params Params `json:"params"`
}

// GetInitializerConfig fetches the initializer configuration from a Kubernetes ConfigMap.
func GetInitializerConfig(kube kubernetes.Interface, namespace, name string) (*Config, error) {
	var configMap *v1.ConfigMap
	var err error
	if errPoll := wait.Poll(500*time.Millisecond, 60*time.Second, func() (bool, error) {
		if configMap, err = kube.CoreV1().ConfigMaps(namespace).Get(name, metav1.GetOptions{}); err != nil {
			return false, err
		}
		return true, nil
	}); errPoll != nil {
		return nil, errPoll
	}
	data, exists := configMap.Data[InitializerConfigMapKey]
	if !exists {
		return nil, fmt.Errorf("missing configuration map key %q", InitializerConfigMapKey)
	}

	var c Config
	if err := yaml.Unmarshal([]byte(data), &c); err != nil {
		return nil, err
	}

	// apply safe defaults if not specified
	switch c.Policy {
	case InjectionPolicyOff, InjectionPolicyOptIn, InjectionPolicyOptOut:
	default:
		c.Policy = DefaultInjectionPolicy
	}
	if c.Params.InitImage == "" {
		c.Params.InitImage = InitImageName(DefaultHub, version.Info.Version, c.Params.DebugMode)
	}
	if c.Params.ProxyImage == "" {
		c.Params.ProxyImage = ProxyImageName(DefaultHub, version.Info.Version, c.Params.DebugMode)
	}
	if c.Params.SidecarProxyUID == 0 {
		c.Params.SidecarProxyUID = DefaultSidecarProxyUID
	}
	if c.Params.MeshConfigMapName == "" {
		c.Params.MeshConfigMapName = DefaultMeshConfigMapName
	}
	if c.Params.ImagePullPolicy == "" {
		c.Params.ImagePullPolicy = DefaultImagePullPolicy
	}

	return &c, nil
}

func injectRequired(namespacePolicy InjectionPolicy, obj metav1.Object) bool {
	var resourcePolicy string

	annotations := obj.GetAnnotations()
	if annotations == nil {
		resourcePolicy = istioSidecarAnnotationPolicyValueDefault
	} else {
		var ok bool
		if resourcePolicy, ok = annotations[istioSidecarAnnotationPolicyKey]; !ok {
			resourcePolicy = istioSidecarAnnotationPolicyValueDefault
		}
	}

	var required bool
	switch namespacePolicy {
	case InjectionPolicyOptIn:
		if resourcePolicy == istioSidecarAnnotationPolicyValueForceOn {
			required = true
		}
	case InjectionPolicyOptOut:
		if resourcePolicy != istioSidecarAnnotationPolicyValueForceOff {
			required = true
		}
	}

	status, ok := annotations[istioSidecarAnnotationStatusKey]

	// avoid injecting sidecar to resources previously modified with kube-inject
	if annotations != nil && checkDeprecatedAlphaAnnotation {
		if _, ok = annotations[deprecatedIstioSidecarAnnotationSidecarKey]; ok {
			required = false
		}
	}

	glog.V(2).Infof("Sidecar injection policy for %v/%v: namespace:%v resource:%v status:%q required:%v",
		obj.GetNamespace(), obj.GetName(), namespacePolicy, resourcePolicy, status, required)

	if !required {
		return false
	}

	// TODO - add version check for sidecar upgrade

	return !ok
}

func injectIntoSpec(p *Params, spec *v1.PodSpec) {
	// proxy initContainer 1.6 spec
	initArgs := []string{
		"-p", fmt.Sprintf("%d", p.Mesh.ProxyListenPort),
		"-u", strconv.FormatInt(p.SidecarProxyUID, 10),
	}
	if p.IncludeIPRanges != "" {
		initArgs = append(initArgs, "-i", p.IncludeIPRanges)
	}

	var pullPolicy v1.PullPolicy
	switch p.ImagePullPolicy {
	case "Always":
		pullPolicy = v1.PullAlways
	case "IfNotPresent":
		pullPolicy = v1.PullIfNotPresent
	case "Never":
		pullPolicy = v1.PullNever
	default:
		pullPolicy = v1.PullIfNotPresent
	}

	privTrue := true

	initContainer := v1.Container{
		Name:            InitContainerName,
		Image:           p.InitImage,
		Args:            initArgs,
		ImagePullPolicy: pullPolicy,
		SecurityContext: &v1.SecurityContext{
			Capabilities: &v1.Capabilities{
				Add: []v1.Capability{"NET_ADMIN"},
			},
			// TODO: Determine SELINUX options needed to remove privileged
			Privileged: &privTrue,
		},
	}

	enableCoreDumpContainer := v1.Container{
		Name:    enableCoreDumpContainerName,
		Image:   enableCoreDumpImage,
		Command: []string{"/bin/sh"},
		Args: []string{
			"-c",
			fmt.Sprintf("sysctl -w kernel.core_pattern=%s/core.%%e.%%p.%%t && ulimit -c unlimited", EnvoyConfigPath),
		},
		ImagePullPolicy: pullPolicy,
		SecurityContext: &v1.SecurityContext{
			// TODO: Determine SELINUX options needed to remove privileged
			Privileged: &privTrue,
		},
	}

	spec.InitContainers = append(spec.InitContainers, initContainer)

	if p.EnableCoreDump {
		spec.InitContainers = append(spec.InitContainers, enableCoreDumpContainer)
	}

	// sidecar proxy container
	args := []string{"proxy", "sidecar"}

	if p.Verbosity > 0 {
		args = append(args, "-v", strconv.Itoa(p.Verbosity))
	}

	volumeMounts := []v1.VolumeMount{
		{
			Name:      istioConfigVolumeName,
			ReadOnly:  true,
			MountPath: "/etc/istio/config",
		},
		{
			Name:      istioEnvoyConfigVolumeName,
			MountPath: EnvoyConfigPath,
		},
	}

	spec.Volumes = append(spec.Volumes,
		v1.Volume{
			Name: istioConfigVolumeName,
			VolumeSource: v1.VolumeSource{
				ConfigMap: &v1.ConfigMapVolumeSource{
					LocalObjectReference: v1.LocalObjectReference{
						Name: p.MeshConfigMapName,
					},
				},
			},
		},
		v1.Volume{
			Name: istioEnvoyConfigVolumeName,
			VolumeSource: v1.VolumeSource{
				EmptyDir: &v1.EmptyDirVolumeSource{
					Medium: v1.StorageMediumMemory,
				},
			},
		})

	volumeMounts = append(volumeMounts, v1.VolumeMount{
		Name:      istioCertVolumeName,
		ReadOnly:  true,
		MountPath: p.Mesh.AuthCertsPath,
	})

	sa := spec.ServiceAccountName
	if sa == "" {
		sa = "default"
	}
	spec.Volumes = append(spec.Volumes, v1.Volume{
		Name: istioCertVolumeName,
		VolumeSource: v1.VolumeSource{
			Secret: &v1.SecretVolumeSource{
				SecretName: istioCertSecretPrefix + sa,
				Optional:   (func(b bool) *bool { return &b })(true),
			},
		},
	})

	// In debug mode we need to be able to write in the proxy container
	// and change the iptables.
	readOnly := !p.DebugMode
	priviledged := p.DebugMode

	sidecar := v1.Container{
		Name:  ProxyContainerName,
		Image: p.ProxyImage,
		Args:  args,
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
			Name: "INSTANCE_IP",
			ValueFrom: &v1.EnvVarSource{
				FieldRef: &v1.ObjectFieldSelector{
					FieldPath: "status.podIP",
				},
			},
		}},
		ImagePullPolicy: pullPolicy,
		SecurityContext: &v1.SecurityContext{
			RunAsUser:              &p.SidecarProxyUID,
			ReadOnlyRootFilesystem: &readOnly,
			Privileged:             &priviledged,
		},
		VolumeMounts: volumeMounts,
	}

	spec.Containers = append(spec.Containers, sidecar)
}

func intoObject(c *Config, in interface{}) (interface{}, error) {
	obj, err := meta.Accessor(in)
	if err != nil {
		return nil, err
	}

	out, err := injectScheme.DeepCopy(in)
	if err != nil {
		return nil, err
	}

	if !injectRequired(c.Policy, obj) {
		glog.V(2).Infof("Skipping %s/%s due to policy check", obj.GetNamespace(), obj.GetName())
		return out, nil
	}

	// `in` is a pointer to an Object. Dereference it.
	outValue := reflect.ValueOf(out).Elem()

	templateValue := outValue.FieldByName("Spec").FieldByName("Template")
	// `Template` is defined as a pointer in some older API
	// definitions, e.g. ReplicationController
	if templateValue.Kind() == reflect.Ptr {
		templateValue = templateValue.Elem()
	}

	objectMeta := outValue.FieldByName("ObjectMeta").Addr().Interface().(*metav1.ObjectMeta)
	templateObjectMeta := templateValue.FieldByName("ObjectMeta").Addr().Interface().(*metav1.ObjectMeta)
	templatePodSpec := templateValue.FieldByName("Spec").Addr().Interface().(*v1.PodSpec)

	for _, m := range []*metav1.ObjectMeta{objectMeta, templateObjectMeta} {
		if m.Annotations == nil {
			m.Annotations = make(map[string]string)
		}
		m.Annotations[istioSidecarAnnotationStatusKey] = "injected-version-" + c.Params.Version

		if insertDeprecatedAlphaAnnotation {
			m.Annotations[deprecatedIstioSidecarAnnotationSidecarKey] =
				deprecatedIstioSidecarAnnotationSidecarValue
		}
	}

	injectIntoSpec(&c.Params, templatePodSpec)

	return out, nil
}

// IntoResourceFile injects the istio proxy into the specified
// kubernetes YAML file.
func IntoResourceFile(c *Config, in io.Reader, out io.Writer) error {
	reader := yamlDecoder.NewYAMLReader(bufio.NewReaderSize(in, 4096))
	for {
		raw, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		var typeMeta metav1.TypeMeta
		if err = yaml.Unmarshal(raw, &typeMeta); err != nil {
			return err
		}

		gvk := schema.FromAPIVersionAndKind(typeMeta.APIVersion, typeMeta.Kind)
		obj, err := injectScheme.New(gvk)
		var updated []byte
		if err == nil {
			if err = yaml.Unmarshal(raw, obj); err != nil {
				return err
			}
			out, err := intoObject(c, obj) // nolint: vetshadow
			if err != nil {
				return err
			}
			if updated, err = yaml.Marshal(out); err != nil {
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
