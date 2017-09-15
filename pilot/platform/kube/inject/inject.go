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
	"strings"
	"time"

	"github.com/ghodss/yaml"
	"github.com/golang/glog"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/duration"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	yamlDecoder "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/pilot/proxy"
	"istio.io/pilot/tools/version"
)

// per-sidecar policy and status (deployment, job, statefulset, pod, etc)
const (
	istioSidecarAnnotationPolicyKey = "sidecar.istio.io/inject"
	istioSidecarAnnotationStatusKey = "sidecar.istio.io/status"
)

// InjectionPolicy determines the policy for injecting the
// sidecar proxy into the watched namespace(s).
type InjectionPolicy string

const (
	// InjectionPolicyOff disables the initializer from modifying
	// resources. The pending 'status.sidecar.istio.io initializer'
	// initializer is still removed to avoid blocking creation of
	// resources.
	InjectionPolicyOff InjectionPolicy = "off"

	// InjectionPolicyDisabled specifies that the initializer will not
	// inject the sidecar into resources by default for the
	// namespace(s) being watched. Resources can enable injection
	// using the "sidecar.istio.io/inject" annotation with value of
	// true.
	InjectionPolicyDisabled InjectionPolicy = "disabled"

	// InjectionPolicyEnabled specifies that the initializer will
	// inject the sidecar into resources by default for the
	// namespace(s) being watched. Resources can disable injection
	// using the "sidecar.istio.io/inject" annotation with value of
	// false.
	InjectionPolicyEnabled InjectionPolicy = "enabled"

	// DefaultInjectionPolicy is the default injection policy.
	DefaultInjectionPolicy = InjectionPolicyEnabled
)

// Defaults values for injecting istio proxy into kubernetes
// resources.
const (
	DefaultSidecarProxyUID = int64(1337)
	DefaultVerbosity       = 2
	DefaultHub             = "docker.io/istio"
	DefaultImagePullPolicy = "IfNotPresent"
)

const (
	// InitContainerName is the name for init container
	InitContainerName = "istio-init"

	// ProxyContainerName is the name for sidecar proxy container
	ProxyContainerName = "istio-proxy"

	enableCoreDumpContainerName = "enable-core-dump"
	enableCoreDumpImage         = "alpine"

	istioCertSecretPrefix = "istio."

	istioCertVolumeName        = "istio-certs"
	istioEnvoyConfigVolumeName = "istio-envoy"

	// ConfigMapKey should match the expected MeshConfig file name
	ConfigMapKey = "mesh"

	// InitializerConfigMapKey is the key into the initailizer ConfigMap data.
	InitializerConfigMapKey = "config"

	// DefaultResyncPeriod specifies how frequently to retrieve the
	// full list of watched resources for initialization.
	DefaultResyncPeriod = 30 * time.Second

	// DefaultInitializerName specifies the name of the initializer.
	DefaultInitializerName = "sidecar.initializer.istio.io"
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
	InitImage       string                  `json:"initImage"`
	ProxyImage      string                  `json:"proxyImage"`
	Verbosity       int                     `json:"verbosity"`
	SidecarProxyUID int64                   `json:"sidecarProxyUID"`
	Version         string                  `json:"version"`
	EnableCoreDump  bool                    `json:"enableCoreDump"`
	DebugMode       bool                    `json:"debugMode"`
	Mesh            *proxyconfig.MeshConfig `json:"-"`
	ImagePullPolicy string                  `json:"imagePullPolicy"`
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

	// InitializerName specifies the name of the initializer.
	InitializerName string `json:"initializerName"`
}

// GetInitializerConfig fetches the initializer configuration from a Kubernetes ConfigMap.
func GetInitializerConfig(kube kubernetes.Interface, namespace, injectConfigName string) (*Config, error) {
	var configMap *v1.ConfigMap
	var err error
	if errPoll := wait.Poll(500*time.Millisecond, 60*time.Second, func() (bool, error) {
		if configMap, err = kube.CoreV1().ConfigMaps(namespace).Get(injectConfigName, metav1.GetOptions{}); err != nil {
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
	case InjectionPolicyOff, InjectionPolicyDisabled, InjectionPolicyEnabled:
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
	if c.Params.ImagePullPolicy == "" {
		c.Params.ImagePullPolicy = DefaultImagePullPolicy
	}
	if c.InitializerName == "" {
		c.InitializerName = DefaultInitializerName
	}

	return &c, nil
}

func injectRequired(namespacePolicy InjectionPolicy, obj metav1.Object) bool {
	var useDefault bool
	var inject bool

	annotations := obj.GetAnnotations()
	if annotations == nil {
		useDefault = true
	} else {
		if value, ok := annotations[istioSidecarAnnotationPolicyKey]; !ok {
			useDefault = true
		} else {
			// http://yaml.org/type/bool.html
			switch strings.ToLower(value) {
			case "y", "yes", "true", "on":
				inject = true
			}
		}
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

	status, ok := annotations[istioSidecarAnnotationStatusKey]

	glog.V(2).Infof("Sidecar injection policy for %v/%v: namespacePolicy:%v useDefault:%v inject:%v status:%q required:%v",
		obj.GetNamespace(), obj.GetName(), namespacePolicy, useDefault, inject, status, required)

	if !required {
		return false
	}

	// TODO - add version check for sidecar upgrade

	return !ok
}

func timeString(dur *duration.Duration) string {
	out, err := ptypes.Duration(dur)
	if err != nil {
		glog.Warning(err)
	}
	return out.String()
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
			fmt.Sprintf("sysctl -w kernel.core_pattern=%s/core.%%e.%%p.%%t && ulimit -c unlimited",
				p.Mesh.DefaultConfig.ConfigPath),
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

	// set all proxy config flags
	args = append(args, "--configPath", p.Mesh.DefaultConfig.ConfigPath)
	args = append(args, "--binaryPath", p.Mesh.DefaultConfig.BinaryPath)
	args = append(args, "--serviceCluster", p.Mesh.DefaultConfig.ServiceCluster)
	args = append(args, "--drainDuration", timeString(p.Mesh.DefaultConfig.DrainDuration))
	args = append(args, "--parentShutdownDuration", timeString(p.Mesh.DefaultConfig.ParentShutdownDuration))
	args = append(args, "--discoveryAddress", p.Mesh.DefaultConfig.DiscoveryAddress)
	args = append(args, "--discoveryRefreshDelay", timeString(p.Mesh.DefaultConfig.DiscoveryRefreshDelay))
	args = append(args, "--zipkinAddress", p.Mesh.DefaultConfig.ZipkinAddress)
	args = append(args, "--connectTimeout", timeString(p.Mesh.DefaultConfig.ConnectTimeout))
	args = append(args, "--statsdUdpAddress", p.Mesh.DefaultConfig.StatsdUdpAddress)
	args = append(args, "--proxyAdminPort", fmt.Sprintf("%d", p.Mesh.DefaultConfig.ProxyAdminPort))

	volumeMounts := []v1.VolumeMount{
		{
			Name:      istioEnvoyConfigVolumeName,
			MountPath: p.Mesh.DefaultConfig.ConfigPath,
		},
	}

	spec.Volumes = append(spec.Volumes,
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
		MountPath: proxy.AuthCertsPath,
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
