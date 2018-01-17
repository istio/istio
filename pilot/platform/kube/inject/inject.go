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
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	// TODO(nmittler): Remove this
	_ "github.com/golang/glog"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/duration"
	appsv1beta1 "k8s.io/api/apps/v1beta1"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/api/batch/v2alpha1"
	"k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/model"
	"istio.io/istio/pilot/tools/version"
	"istio.io/istio/pkg/log"
)

var (
	kinds = []struct {
		groupVersion schema.GroupVersion
		obj          runtime.Object
		resource     string
		apiPath      string
	}{
		{v1.SchemeGroupVersion, &v1.ReplicationController{}, "replicationcontrollers", "/api"},

		{v1beta1.SchemeGroupVersion, &v1beta1.Deployment{}, "deployments", "/apis"},
		{v1beta1.SchemeGroupVersion, &v1beta1.DaemonSet{}, "daemonsets", "/apis"},
		{v1beta1.SchemeGroupVersion, &v1beta1.ReplicaSet{}, "replicasets", "/apis"},

		{batchv1.SchemeGroupVersion, &batchv1.Job{}, "jobs", "/apis"},
		{v2alpha1.SchemeGroupVersion, &v2alpha1.CronJob{}, "cronjobs", "/apis"},
		// TODO JobTemplate requires different reflection logic to populate the PodTemplateSpec

		{appsv1beta1.SchemeGroupVersion, &appsv1beta1.StatefulSet{}, "statefulsets", "/apis"},
	}
	injectScheme = runtime.NewScheme()
)

func init() {
	for _, kind := range kinds {
		injectScheme.AddKnownTypes(kind.groupVersion, kind.obj)
		injectScheme.AddUnversionedTypes(kind.groupVersion, kind.obj)
	}
}

var ignoredNamespaces = []string{
	metav1.NamespaceSystem,
	metav1.NamespacePublic,
}

// per-sidecar policy and status
const (
	istioSidecarAnnotationPolicyKey = "sidecar.istio.io/inject"
	istioSidecarAnnotationStatusKey = "sidecar.istio.io/status"
)

// TODO - include hash of sidecar templated configuration file when its merged
type sidecarStatus struct {
	Version        string   `json:"version"`
	InitContainers []string `json:"initContainers"`
	Containers     []string `json:"containers"`
	Volumes        []string `json:"volumes"`
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
	InitImage       string                 `json:"initImage"`
	ProxyImage      string                 `json:"proxyImage"`
	Verbosity       int                    `json:"verbosity"`
	SidecarProxyUID int64                  `json:"sidecarProxyUID"`
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

func applyDefaultConfig(c *Config) {
	switch c.Policy {
	case InjectionPolicyDisabled, InjectionPolicyEnabled:
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
}

func timeString(dur *duration.Duration) string {
	out, err := ptypes.Duration(dur)
	if err != nil {
		log.Warna(err)
	}
	return out.String()
}

func injectionData(p *Params, spec *v1.PodSpec, metadata *metav1.ObjectMeta) (*SidecarConfig, string, error) {
	var sc SidecarConfig

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

	sc.InitContainers = append(sc.InitContainers, v1.Container{
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
	})

	if p.EnableCoreDump {
		sc.InitContainers = append(sc.InitContainers, v1.Container{
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
		})
	}

	// sidecar proxy container
	args := []string{"proxy", "sidecar"}

	if p.Verbosity > 0 {
		args = append(args, "-v", strconv.Itoa(p.Verbosity))
	}

	serviceCluster := p.Mesh.DefaultConfig.ServiceCluster

	// If 'app' label is available, use it as the default service cluster
	if val, ok := metadata.GetLabels()["app"]; ok {
		serviceCluster = val
	}

	// set all proxy config flags
	args = append(args, "--configPath", p.Mesh.DefaultConfig.ConfigPath)
	args = append(args, "--binaryPath", p.Mesh.DefaultConfig.BinaryPath)
	args = append(args, "--serviceCluster", serviceCluster)
	args = append(args, "--drainDuration", timeString(p.Mesh.DefaultConfig.DrainDuration))
	args = append(args, "--parentShutdownDuration", timeString(p.Mesh.DefaultConfig.ParentShutdownDuration))
	args = append(args, "--discoveryAddress", p.Mesh.DefaultConfig.DiscoveryAddress)
	args = append(args, "--discoveryRefreshDelay", timeString(p.Mesh.DefaultConfig.DiscoveryRefreshDelay))
	args = append(args, "--zipkinAddress", p.Mesh.DefaultConfig.ZipkinAddress)
	args = append(args, "--connectTimeout", timeString(p.Mesh.DefaultConfig.ConnectTimeout))
	args = append(args, "--statsdUdpAddress", p.Mesh.DefaultConfig.StatsdUdpAddress)
	args = append(args, "--proxyAdminPort", fmt.Sprintf("%d", p.Mesh.DefaultConfig.ProxyAdminPort))
	args = append(args, "--controlPlaneAuthPolicy", p.Mesh.DefaultConfig.ControlPlaneAuthPolicy.String())

	volumeMounts := []v1.VolumeMount{
		{
			Name:      istioEnvoyConfigVolumeName,
			MountPath: p.Mesh.DefaultConfig.ConfigPath,
		},
	}

	sc.Volumes = append(sc.Volumes, v1.Volume{
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
		MountPath: model.AuthCertsPath,
	})

	sa := spec.ServiceAccountName
	if sa == "" {
		sa = "default"
	}
	sc.Volumes = append(sc.Volumes, v1.Volume{
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

	// https://github.com/kubernetes/kubernetes/pull/42944
	sc.Containers = append(sc.Containers, v1.Container{
		Name:  ProxyContainerName,
		Image: p.ProxyImage,
		Args:  args,
		Env: []v1.EnvVar{{
			Name: "POD_NAME",
			ValueFrom: &v1.EnvVarSource{
				FieldRef: &v1.ObjectFieldSelector{
					// APIVersion: "v1",
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
	})

	status := &sidecarStatus{Version: p.Version}
	for _, c := range sc.InitContainers {
		status.InitContainers = append(status.InitContainers, c.Name)
	}
	for _, c := range sc.Containers {
		status.Containers = append(status.Containers, c.Name)
	}
	for _, c := range sc.Volumes {
		status.Volumes = append(status.Volumes, c.Name)
	}
	statusAnnotationValue, err := json.Marshal(status)
	if err != nil {
		return nil, "", fmt.Errorf("error encoded injection status: %v", err)
	}

	return &sc, string(statusAnnotationValue), nil
}

// Config specifies the sidecar injection configuration This includes
// the sidear template and cluster-side injection policy. It is used
// by kube-inject, sidecar injector, and http endpoint.
type Config struct {
	Policy InjectionPolicy `json:"policy"`

	// Params specifies the parameters of the injected sidcar template
	Params Params `json:"params"`
}

func injectRequired(ignored []string, namespacePolicy InjectionPolicy, podSpec *v1.PodSpec, metadata *metav1.ObjectMeta) bool { // nolint: lll
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

type SidecarConfig struct {
	InitContainers []v1.Container `yaml:"initContainers"`
	Containers     []v1.Container `yaml:"containers"`
	Volumes        []v1.Volume    `yaml:"volumes"`
}
