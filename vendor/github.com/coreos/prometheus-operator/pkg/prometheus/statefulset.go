// Copyright 2016 The prometheus-operator Authors
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

package prometheus

import (
	"fmt"
	"net/url"
	"path"
	"strings"

	appsv1 "k8s.io/api/apps/v1beta2"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/blang/semver"
	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/coreos/prometheus-operator/pkg/k8sutil"
	"github.com/pkg/errors"
)

const (
	governingServiceName            = "prometheus-operated"
	DefaultPrometheusVersion        = "v2.7.1"
	DefaultThanosVersion            = "v0.2.1"
	defaultRetention                = "24h"
	defaultReplicaExternalLabelName = "prometheus_replica"
	storageDir                      = "/prometheus"
	confDir                         = "/etc/prometheus/config"
	confOutDir                      = "/etc/prometheus/config_out"
	rulesDir                        = "/etc/prometheus/rules"
	secretsDir                      = "/etc/prometheus/secrets/"
	configmapsDir                   = "/etc/prometheus/configmaps/"
	configFilename                  = "prometheus.yaml.gz"
	configEnvsubstFilename          = "prometheus.env.yaml"
	sSetInputHashName               = "prometheus-operator-input-hash"
)

var (
	minReplicas                 int32 = 1
	defaultMaxConcurrency       int32 = 20
	managedByOperatorLabel            = "managed-by"
	managedByOperatorLabelValue       = "prometheus-operator"
	managedByOperatorLabels           = map[string]string{
		managedByOperatorLabel: managedByOperatorLabelValue,
	}
	probeTimeoutSeconds int32 = 3

	CompatibilityMatrix = []string{
		"v1.4.0",
		"v1.4.1",
		"v1.5.0",
		"v1.5.1",
		"v1.5.2",
		"v1.5.3",
		"v1.6.0",
		"v1.6.1",
		"v1.6.2",
		"v1.6.3",
		"v1.7.0",
		"v1.7.1",
		"v1.7.2",
		"v1.8.0",
		"v2.0.0",
		"v2.2.1",
		"v2.3.1",
		"v2.3.2",
		"v2.4.0",
		"v2.4.1",
		"v2.4.2",
		"v2.4.3",
		"v2.5.0",
		"v2.6.0",
		"v2.6.1",
		"v2.7.0",
		"v2.7.1",
	}
)

func makeStatefulSet(
	p monitoringv1.Prometheus,
	config *Config,
	ruleConfigMapNames []string,
	inputHash string,
) (*appsv1.StatefulSet, error) {
	// p is passed in by value, not by reference. But p contains references like
	// to annotation map, that do not get copied on function invocation. Ensure to
	// prevent side effects before editing p by creating a deep copy. For more
	// details see https://github.com/coreos/prometheus-operator/issues/1659.
	p = *p.DeepCopy()

	// TODO(fabxc): is this the right point to inject defaults?
	// Ideally we would do it before storing but that's currently not possible.
	// Potentially an update handler on first insertion.

	if p.Spec.BaseImage == "" {
		p.Spec.BaseImage = config.PrometheusDefaultBaseImage
	}
	if p.Spec.Version == "" {
		p.Spec.Version = DefaultPrometheusVersion
	}
	if p.Spec.Thanos != nil && p.Spec.Thanos.Version == nil {
		v := DefaultThanosVersion
		p.Spec.Thanos.Version = &v
	}

	versionStr := strings.TrimLeft(p.Spec.Version, "v")

	version, err := semver.Parse(versionStr)
	if err != nil {
		return nil, errors.Wrap(err, "parse version")
	}

	if p.Spec.Replicas == nil {
		p.Spec.Replicas = &minReplicas
	}
	intZero := int32(0)
	if p.Spec.Replicas != nil && *p.Spec.Replicas < 0 {
		p.Spec.Replicas = &intZero
	}
	if p.Spec.Retention == "" {
		p.Spec.Retention = defaultRetention
	}

	if p.Spec.Resources.Requests == nil {
		p.Spec.Resources.Requests = v1.ResourceList{}
	}
	_, memoryRequestFound := p.Spec.Resources.Requests[v1.ResourceMemory]
	memoryLimit, memoryLimitFound := p.Spec.Resources.Limits[v1.ResourceMemory]
	if !memoryRequestFound && version.Major == 1 {
		defaultMemoryRequest := resource.MustParse("2Gi")
		compareResult := memoryLimit.Cmp(defaultMemoryRequest)
		// If limit is given and smaller or equal to 2Gi, then set memory
		// request to the given limit. This is necessary as if limit < request,
		// then a Pod is not schedulable.
		if memoryLimitFound && compareResult <= 0 {
			p.Spec.Resources.Requests[v1.ResourceMemory] = memoryLimit
		} else {
			p.Spec.Resources.Requests[v1.ResourceMemory] = defaultMemoryRequest
		}
	}

	spec, err := makeStatefulSetSpec(p, config, ruleConfigMapNames)
	if err != nil {
		return nil, errors.Wrap(err, "make StatefulSet spec")
	}

	boolTrue := true
	statefulset := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:        prefixedName(p.Name),
			Labels:      config.Labels.Merge(p.ObjectMeta.Labels),
			Annotations: p.ObjectMeta.Annotations,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         p.APIVersion,
					BlockOwnerDeletion: &boolTrue,
					Controller:         &boolTrue,
					Kind:               p.Kind,
					Name:               p.Name,
					UID:                p.UID,
				},
			},
		},
		Spec: *spec,
	}

	if statefulset.ObjectMeta.Annotations == nil {
		statefulset.ObjectMeta.Annotations = map[string]string{
			sSetInputHashName: inputHash,
		}
	} else {
		statefulset.ObjectMeta.Annotations[sSetInputHashName] = inputHash
	}

	if p.Spec.ImagePullSecrets != nil && len(p.Spec.ImagePullSecrets) > 0 {
		statefulset.Spec.Template.Spec.ImagePullSecrets = p.Spec.ImagePullSecrets
	}
	storageSpec := p.Spec.Storage
	if storageSpec == nil {
		statefulset.Spec.Template.Spec.Volumes = append(statefulset.Spec.Template.Spec.Volumes, v1.Volume{
			Name: volumeName(p.Name),
			VolumeSource: v1.VolumeSource{
				EmptyDir: &v1.EmptyDirVolumeSource{},
			},
		})
	} else if storageSpec.EmptyDir != nil {
		emptyDir := storageSpec.EmptyDir
		statefulset.Spec.Template.Spec.Volumes = append(statefulset.Spec.Template.Spec.Volumes, v1.Volume{
			Name: volumeName(p.Name),
			VolumeSource: v1.VolumeSource{
				EmptyDir: emptyDir,
			},
		})
	} else {
		pvcTemplate := storageSpec.VolumeClaimTemplate
		if pvcTemplate.Name == "" {
			pvcTemplate.Name = volumeName(p.Name)
		}
		pvcTemplate.Spec.AccessModes = []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce}
		pvcTemplate.Spec.Resources = storageSpec.VolumeClaimTemplate.Spec.Resources
		pvcTemplate.Spec.Selector = storageSpec.VolumeClaimTemplate.Spec.Selector
		statefulset.Spec.VolumeClaimTemplates = append(statefulset.Spec.VolumeClaimTemplates, pvcTemplate)
	}

	return statefulset, nil
}

func makeEmptyConfigurationSecret(p *monitoringv1.Prometheus, config Config) (*v1.Secret, error) {
	s := makeConfigSecret(p, config)

	s.ObjectMeta.Annotations = map[string]string{
		"empty": "true",
	}

	return s, nil
}

func makeConfigSecret(p *monitoringv1.Prometheus, config Config) *v1.Secret {
	boolTrue := true
	return &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:   configSecretName(p.Name),
			Labels: config.Labels.Merge(managedByOperatorLabels),
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         p.APIVersion,
					BlockOwnerDeletion: &boolTrue,
					Controller:         &boolTrue,
					Kind:               p.Kind,
					Name:               p.Name,
					UID:                p.UID,
				},
			},
		},
		Data: map[string][]byte{
			configFilename: {},
		},
	}
}

func makeStatefulSetService(p *monitoringv1.Prometheus, config Config) *v1.Service {
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: governingServiceName,
			OwnerReferences: []metav1.OwnerReference{
				metav1.OwnerReference{
					Name:       p.GetName(),
					Kind:       p.Kind,
					APIVersion: p.APIVersion,
					UID:        p.GetUID(),
				},
			},
			Labels: config.Labels.Merge(map[string]string{
				"operated-prometheus": "true",
			}),
		},
		Spec: v1.ServiceSpec{
			ClusterIP: "None",
			Ports: []v1.ServicePort{
				{
					Name:       "web",
					Port:       9090,
					TargetPort: intstr.FromString("web"),
				},
			},
			Selector: map[string]string{
				"app": "prometheus",
			},
		},
	}
	return svc
}

func makeStatefulSetSpec(p monitoringv1.Prometheus, c *Config, ruleConfigMapNames []string) (*appsv1.StatefulSetSpec, error) {
	// Prometheus may take quite long to shut down to checkpoint existing data.
	// Allow up to 10 minutes for clean termination.
	terminationGracePeriod := int64(600)

	versionStr := strings.TrimLeft(p.Spec.Version, "v")

	version, err := semver.Parse(versionStr)
	if err != nil {
		return nil, errors.Wrap(err, "parse version")
	}

	promArgs := []string{
		"-web.console.templates=/etc/prometheus/consoles",
		"-web.console.libraries=/etc/prometheus/console_libraries",
	}

	switch version.Major {
	case 1:
		promArgs = append(promArgs,
			"-storage.local.retention="+p.Spec.Retention,
			"-storage.local.num-fingerprint-mutexes=4096",
			fmt.Sprintf("-storage.local.path=%s", storageDir),
			"-storage.local.chunk-encoding-version=2",
			fmt.Sprintf("-config.file=%s", path.Join(confOutDir, configEnvsubstFilename)),
		)
		// We attempt to specify decent storage tuning flags based on how much the
		// requested memory can fit. The user has to specify an appropriate buffering
		// in memory limits to catch increased memory usage during query bursts.
		// More info: https://prometheus.io/docs/operating/storage/.
		reqMem := p.Spec.Resources.Requests[v1.ResourceMemory]

		if version.Minor < 6 {
			// 1024 byte is the fixed chunk size. With increasing number of chunks actually
			// in memory, overhead owed to their management, higher ingestion buffers, etc.
			// increases.
			// We are conservative for now an assume this to be 80% as the Kubernetes environment
			// generally has a very high time series churn.
			memChunks := reqMem.Value() / 1024 / 5

			promArgs = append(promArgs,
				"-storage.local.memory-chunks="+fmt.Sprintf("%d", memChunks),
				"-storage.local.max-chunks-to-persist="+fmt.Sprintf("%d", memChunks/2),
			)
		} else {
			// Leave 1/3 head room for other overhead.
			promArgs = append(promArgs,
				"-storage.local.target-heap-size="+fmt.Sprintf("%d", reqMem.Value()/3*2),
			)
		}
	case 2:
		promArgs = append(promArgs,
			fmt.Sprintf("-config.file=%s", path.Join(confOutDir, configEnvsubstFilename)),
			fmt.Sprintf("-storage.tsdb.path=%s", storageDir),
			"-storage.tsdb.retention="+p.Spec.Retention,
			"-web.enable-lifecycle",
			"-storage.tsdb.no-lockfile",
		)

		if p.Spec.Query != nil && p.Spec.Query.LookbackDelta != nil {
			promArgs = append(promArgs,
				fmt.Sprintf("-query.lookback-delta=%s", *p.Spec.Query.LookbackDelta),
			)
		}

		if version.Minor >= 4 {
			if p.Spec.Rules.Alert.ForOutageTolerance != "" {
				promArgs = append(promArgs, "-rules.alert.for-outage-tolerance="+p.Spec.Rules.Alert.ForOutageTolerance)
			}
			if p.Spec.Rules.Alert.ForGracePeriod != "" {
				promArgs = append(promArgs, "-rules.alert.for-grace-period="+p.Spec.Rules.Alert.ForGracePeriod)
			}
			if p.Spec.Rules.Alert.ResendDelay != "" {
				promArgs = append(promArgs, "-rules.alert.resend-delay="+p.Spec.Rules.Alert.ResendDelay)
			}
		}
	default:
		return nil, errors.Errorf("unsupported Prometheus major version %s", version)
	}

	if p.Spec.Query != nil {
		if p.Spec.Query.MaxConcurrency != nil {
			if *p.Spec.Query.MaxConcurrency < 1 {
				p.Spec.Query.MaxConcurrency = &defaultMaxConcurrency
			}
			promArgs = append(promArgs,
				fmt.Sprintf("-query.max-concurrency=%d", *p.Spec.Query.MaxConcurrency),
			)
		}
		if p.Spec.Query.Timeout != nil {
			promArgs = append(promArgs,
				fmt.Sprintf("-query.timeout=%s", *p.Spec.Query.Timeout),
			)
		}
	}

	var securityContext *v1.PodSecurityContext = nil
	if p.Spec.SecurityContext != nil {
		securityContext = p.Spec.SecurityContext
	}

	if p.Spec.EnableAdminAPI {
		promArgs = append(promArgs, "-web.enable-admin-api")
	}

	if p.Spec.ExternalURL != "" {
		promArgs = append(promArgs, "-web.external-url="+p.Spec.ExternalURL)
	}

	webRoutePrefix := "/"
	if p.Spec.RoutePrefix != "" {
		webRoutePrefix = p.Spec.RoutePrefix
	}
	promArgs = append(promArgs, "-web.route-prefix="+webRoutePrefix)

	if p.Spec.LogLevel != "" && p.Spec.LogLevel != "info" {
		promArgs = append(promArgs, fmt.Sprintf("-log.level=%s", p.Spec.LogLevel))
	}
	if version.GTE(semver.MustParse("2.6.0")) {
		if p.Spec.LogFormat != "" && p.Spec.LogFormat != "logfmt" {
			promArgs = append(promArgs, fmt.Sprintf("-log.format=%s", p.Spec.LogFormat))
		}
	}

	var ports []v1.ContainerPort
	if p.Spec.ListenLocal {
		promArgs = append(promArgs, "-web.listen-address=127.0.0.1:9090")
	} else {
		ports = []v1.ContainerPort{
			{
				Name:          "web",
				ContainerPort: 9090,
				Protocol:      v1.ProtocolTCP,
			},
		}
	}

	if version.Major == 2 {
		for i, a := range promArgs {
			promArgs[i] = "-" + a
		}
	}

	localReloadURL := &url.URL{
		Scheme: "http",
		Host:   c.LocalHost + ":9090",
		Path:   path.Clean(webRoutePrefix + "/-/reload"),
	}

	volumes := []v1.Volume{
		{
			Name: "config",
			VolumeSource: v1.VolumeSource{
				Secret: &v1.SecretVolumeSource{
					SecretName: configSecretName(p.Name),
				},
			},
		},
		{
			Name: "config-out",
			VolumeSource: v1.VolumeSource{
				EmptyDir: &v1.EmptyDirVolumeSource{},
			},
		},
	}

	for _, name := range ruleConfigMapNames {
		volumes = append(volumes, v1.Volume{
			Name: name,
			VolumeSource: v1.VolumeSource{
				ConfigMap: &v1.ConfigMapVolumeSource{
					LocalObjectReference: v1.LocalObjectReference{
						Name: name,
					},
				},
			},
		})
	}

	volName := volumeName(p.Name)
	if p.Spec.Storage != nil {
		if p.Spec.Storage.VolumeClaimTemplate.Name != "" {
			volName = p.Spec.Storage.VolumeClaimTemplate.Name
		}
	}

	promVolumeMounts := []v1.VolumeMount{
		{
			Name:      "config-out",
			ReadOnly:  true,
			MountPath: confOutDir,
		},
		{
			Name:      volName,
			MountPath: storageDir,
			SubPath:   subPathForStorage(p.Spec.Storage),
		},
	}

	for _, name := range ruleConfigMapNames {
		promVolumeMounts = append(promVolumeMounts, v1.VolumeMount{
			Name:      name,
			MountPath: rulesDir + "/" + name,
		})
	}

	for _, s := range p.Spec.Secrets {
		volumes = append(volumes, v1.Volume{
			Name: k8sutil.SanitizeVolumeName("secret-" + s),
			VolumeSource: v1.VolumeSource{
				Secret: &v1.SecretVolumeSource{
					SecretName: s,
				},
			},
		})
		promVolumeMounts = append(promVolumeMounts, v1.VolumeMount{
			Name:      k8sutil.SanitizeVolumeName("secret-" + s),
			ReadOnly:  true,
			MountPath: secretsDir + s,
		})
	}

	for _, c := range p.Spec.ConfigMaps {
		volumes = append(volumes, v1.Volume{
			Name: k8sutil.SanitizeVolumeName("configmap-" + c),
			VolumeSource: v1.VolumeSource{
				ConfigMap: &v1.ConfigMapVolumeSource{
					LocalObjectReference: v1.LocalObjectReference{
						Name: c,
					},
				},
			},
		})
		promVolumeMounts = append(promVolumeMounts, v1.VolumeMount{
			Name:      k8sutil.SanitizeVolumeName("configmap-" + c),
			ReadOnly:  true,
			MountPath: configmapsDir + c,
		})
	}

	configReloadVolumeMounts := []v1.VolumeMount{
		{
			Name:      "config",
			MountPath: confDir,
		},
		{
			Name:      "config-out",
			MountPath: confOutDir,
		},
	}

	configReloadArgs := []string{
		fmt.Sprintf("--log-format=%s", c.LogFormat),
		fmt.Sprintf("--reload-url=%s", localReloadURL),
		fmt.Sprintf("--config-file=%s", path.Join(confDir, configFilename)),
		fmt.Sprintf("--config-envsubst-file=%s", path.Join(confOutDir, configEnvsubstFilename)),
	}

	var livenessProbeHandler v1.Handler
	var readinessProbeHandler v1.Handler
	var livenessFailureThreshold int32
	if (version.Major == 1 && version.Minor >= 8) || version.Major == 2 {
		livenessProbeHandler = v1.Handler{
			HTTPGet: &v1.HTTPGetAction{
				Path: path.Clean(webRoutePrefix + "/-/healthy"),
				Port: intstr.FromString("web"),
			},
		}
		readinessProbeHandler = v1.Handler{
			HTTPGet: &v1.HTTPGetAction{
				Path: path.Clean(webRoutePrefix + "/-/ready"),
				Port: intstr.FromString("web"),
			},
		}
		livenessFailureThreshold = 6
	} else {
		livenessProbeHandler = v1.Handler{
			HTTPGet: &v1.HTTPGetAction{
				Path: path.Clean(webRoutePrefix + "/status"),
				Port: intstr.FromString("web"),
			},
		}
		readinessProbeHandler = livenessProbeHandler
		// For larger servers, restoring a checkpoint on startup may take quite a bit of time.
		// Wait up to 5 minutes (60 fails * 5s per fail)
		livenessFailureThreshold = 60
	}

	var livenessProbe *v1.Probe
	var readinessProbe *v1.Probe
	if !p.Spec.ListenLocal {
		livenessProbe = &v1.Probe{
			Handler:          livenessProbeHandler,
			PeriodSeconds:    5,
			TimeoutSeconds:   probeTimeoutSeconds,
			FailureThreshold: livenessFailureThreshold,
		}
		readinessProbe = &v1.Probe{
			Handler:          readinessProbeHandler,
			TimeoutSeconds:   probeTimeoutSeconds,
			PeriodSeconds:    5,
			FailureThreshold: 120, // Allow up to 10m on startup for data recovery
		}
	}

	podAnnotations := map[string]string{}
	podLabels := map[string]string{}
	if p.Spec.PodMetadata != nil {
		if p.Spec.PodMetadata.Labels != nil {
			for k, v := range p.Spec.PodMetadata.Labels {
				podLabels[k] = v
			}
		}
		if p.Spec.PodMetadata.Annotations != nil {
			for k, v := range p.Spec.PodMetadata.Annotations {
				podAnnotations[k] = v
			}
		}
	}

	podLabels["app"] = "prometheus"
	podLabels["prometheus"] = p.Name

	finalLabels := c.Labels.Merge(podLabels)

	additionalContainers := p.Spec.Containers

	if len(ruleConfigMapNames) != 0 {
		container := v1.Container{
			Name:  "rules-configmap-reloader",
			Image: c.ConfigReloaderImage,
			Args: []string{
				fmt.Sprintf("--webhook-url=%s", localReloadURL),
			},
			VolumeMounts: []v1.VolumeMount{},
			Resources: v1.ResourceRequirements{
				Limits: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse(c.ConfigReloaderCPU),
					v1.ResourceMemory: resource.MustParse(c.ConfigReloaderMemory),
				},
			},
		}

		for _, name := range ruleConfigMapNames {
			mountPath := rulesDir + "/" + name
			container.VolumeMounts = append(container.VolumeMounts, v1.VolumeMount{
				Name:      name,
				MountPath: mountPath,
			})
			container.Args = append(container.Args, fmt.Sprintf("--volume-dir=%s", mountPath))
		}

		additionalContainers = append(additionalContainers, container)
	}

	if p.Spec.Thanos != nil {
		thanosBaseImage := c.ThanosDefaultBaseImage
		if p.Spec.Thanos.BaseImage != nil {
			thanosBaseImage = *p.Spec.Thanos.BaseImage
		}

		// Version is used by default.
		// If the tag is specified, we use the tag to identify the container image.
		// If the sha is specified, we use the sha to identify the container image,
		// as it has even stronger immutable guarantees to identify the image.
		thanosImage := fmt.Sprintf("%s:%s", thanosBaseImage, *p.Spec.Thanos.Version)
		if p.Spec.Thanos.Tag != nil {
			thanosImage = fmt.Sprintf("%s:%s", thanosBaseImage, *p.Spec.Thanos.Tag)
		}
		if p.Spec.Thanos.SHA != nil {
			thanosImage = fmt.Sprintf("%s@sha256:%s", thanosBaseImage, *p.Spec.Thanos.SHA)
		}
		if p.Spec.Thanos.Image != nil && *p.Spec.Thanos.Image != "" {
			thanosImage = *p.Spec.Thanos.Image
		}

		thanosArgs := []string{
			"sidecar",
			fmt.Sprintf("--prometheus.url=http://%s:9090%s", c.LocalHost, path.Clean(webRoutePrefix)),
			fmt.Sprintf("--tsdb.path=%s", storageDir),
			fmt.Sprintf("--cluster.address=[$(POD_IP)]:%d", 10900),
			fmt.Sprintf("--grpc-address=[$(POD_IP)]:%d", 10901),
		}

		if p.Spec.Thanos.Peers != nil {
			thanosArgs = append(thanosArgs, fmt.Sprintf("--cluster.peers=%s", *p.Spec.Thanos.Peers))
		}
		if p.Spec.Thanos.ClusterAdvertiseAddress != nil {
			thanosArgs = append(thanosArgs, fmt.Sprintf("--cluster.advertise-address=%s", *p.Spec.Thanos.ClusterAdvertiseAddress))
		}
		if p.Spec.Thanos.GrpcAdvertiseAddress != nil {
			thanosArgs = append(thanosArgs, fmt.Sprintf("--grpc-advertise-address=%s", *p.Spec.Thanos.GrpcAdvertiseAddress))
		}
		if p.Spec.LogLevel != "" && p.Spec.LogLevel != "info" {
			thanosArgs = append(thanosArgs, fmt.Sprintf("--log.level=%s", p.Spec.LogLevel))
		}
		thanosVersion := semver.MustParse(strings.TrimPrefix(*p.Spec.Thanos.Version, "v"))
		if thanosVersion.GTE(semver.MustParse("0.2.0")) {
			if p.Spec.LogFormat != "" && p.Spec.LogFormat != "logfmt" {
				thanosArgs = append(thanosArgs, fmt.Sprintf("--log.format=%s", p.Spec.LogFormat))
			}
		}

		thanosVolumeMounts := []v1.VolumeMount{
			{
				Name:      volName,
				MountPath: storageDir,
				SubPath:   subPathForStorage(p.Spec.Storage),
			},
		}

		envVars := []v1.EnvVar{
			{
				// Necessary for '--cluster.address', '--grpc-address' flags
				Name: "POD_IP",
				ValueFrom: &v1.EnvVarSource{
					FieldRef: &v1.ObjectFieldSelector{
						FieldPath: "status.podIP",
					},
				},
			},
			{
				Name: "HOST_IP",
				ValueFrom: &v1.EnvVarSource{
					FieldRef: &v1.ObjectFieldSelector{
						FieldPath: "status.hostIP",
					},
				},
			},
		}

		if p.Spec.Thanos.ObjectStorageConfig != nil {
			envVars = append(envVars, v1.EnvVar{
				Name: "OBJSTORE_CONFIG",
				ValueFrom: &v1.EnvVarSource{
					SecretKeyRef: p.Spec.Thanos.ObjectStorageConfig,
				},
			})
			thanosArgs = append(thanosArgs, "--objstore.config=$(OBJSTORE_CONFIG)")
		}

		if p.Spec.Thanos.GCS != nil {
			if p.Spec.Thanos.GCS.Bucket != nil {
				thanosArgs = append(thanosArgs, fmt.Sprintf("--gcs.bucket=%s", *p.Spec.Thanos.GCS.Bucket))
			}
			if p.Spec.Thanos.GCS.SecretKey != nil {
				secretFileName := "service-account.json"
				if p.Spec.Thanos.GCS.SecretKey.Key != "" {
					secretFileName = p.Spec.Thanos.GCS.SecretKey.Key
				}
				secretDir := path.Join("/var/run/secrets/prometheus.io", p.Spec.Thanos.GCS.SecretKey.Name)
				envVars = append(envVars, v1.EnvVar{
					Name:  "GOOGLE_APPLICATION_CREDENTIALS",
					Value: path.Join(secretDir, secretFileName),
				})
				volumeName := k8sutil.SanitizeVolumeName("secret-" + p.Spec.Thanos.GCS.SecretKey.Name)
				volumes = append(volumes, v1.Volume{
					Name: volumeName,
					VolumeSource: v1.VolumeSource{
						Secret: &v1.SecretVolumeSource{
							SecretName: p.Spec.Thanos.GCS.SecretKey.Name,
						},
					},
				})
				thanosVolumeMounts = append(thanosVolumeMounts, v1.VolumeMount{
					Name:      volumeName,
					ReadOnly:  true,
					MountPath: secretDir,
				})
			}
		}

		if p.Spec.Thanos.S3 != nil {
			if p.Spec.Thanos.S3.Bucket != nil {
				thanosArgs = append(thanosArgs, fmt.Sprintf("--s3.bucket=%s", *p.Spec.Thanos.S3.Bucket))
			}
			if p.Spec.Thanos.S3.Endpoint != nil {
				thanosArgs = append(thanosArgs, fmt.Sprintf("--s3.endpoint=%s", *p.Spec.Thanos.S3.Endpoint))
			}
			if p.Spec.Thanos.S3.Insecure != nil && *p.Spec.Thanos.S3.Insecure {
				thanosArgs = append(thanosArgs, "--s3.insecure")
			}
			if p.Spec.Thanos.S3.SignatureVersion2 != nil && *p.Spec.Thanos.S3.SignatureVersion2 {
				thanosArgs = append(thanosArgs, "--s3.signature-version2")
			}
			if p.Spec.Thanos.S3.AccessKey != nil {
				envVars = append(envVars, v1.EnvVar{
					Name: "S3_ACCESS_KEY",
					ValueFrom: &v1.EnvVarSource{
						SecretKeyRef: p.Spec.Thanos.S3.AccessKey,
					},
				})
			}
			if p.Spec.Thanos.S3.EncryptSSE != nil && *p.Spec.Thanos.S3.EncryptSSE {
				thanosArgs = append(thanosArgs, "--s3.encrypt-sse")
			}
			if p.Spec.Thanos.S3.SecretKey != nil {
				envVars = append(envVars, v1.EnvVar{
					Name: "S3_SECRET_KEY",
					ValueFrom: &v1.EnvVarSource{
						SecretKeyRef: p.Spec.Thanos.S3.SecretKey,
					},
				})
			}
		}

		c := v1.Container{
			Name:  "thanos-sidecar",
			Image: thanosImage,
			Args:  thanosArgs,
			Ports: []v1.ContainerPort{
				{
					Name:          "http",
					ContainerPort: 10902,
				},
				{
					Name:          "grpc",
					ContainerPort: 10901,
				},
				{
					Name:          "cluster",
					ContainerPort: 10900,
				},
			},
			Env:          envVars,
			VolumeMounts: thanosVolumeMounts,
			Resources:    p.Spec.Thanos.Resources,
		}

		additionalContainers = append(additionalContainers, c)
		promArgs = append(promArgs, "--storage.tsdb.min-block-duration=2h", "--storage.tsdb.max-block-duration=2h")
	}

	// Version is used by default.
	// If the tag is specified, we use the tag to identify the container image.
	// If the sha is specified, we use the sha to identify the container image,
	// as it has even stronger immutable guarantees to identify the image.
	prometheusImage := fmt.Sprintf("%s:%s", p.Spec.BaseImage, p.Spec.Version)
	if p.Spec.Tag != "" {
		prometheusImage = fmt.Sprintf("%s:%s", p.Spec.BaseImage, p.Spec.Tag)
	}
	if p.Spec.SHA != "" {
		prometheusImage = fmt.Sprintf("%s@sha256:%s", p.Spec.BaseImage, p.Spec.SHA)
	}
	if p.Spec.Image != nil && *p.Spec.Image != "" {
		prometheusImage = *p.Spec.Image
	}

	return &appsv1.StatefulSetSpec{
		ServiceName:         governingServiceName,
		Replicas:            p.Spec.Replicas,
		PodManagementPolicy: appsv1.ParallelPodManagement,
		UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
			Type: appsv1.RollingUpdateStatefulSetStrategyType,
		},
		Selector: &metav1.LabelSelector{
			MatchLabels: finalLabels,
		},
		Template: v1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      finalLabels,
				Annotations: podAnnotations,
			},
			Spec: v1.PodSpec{
				Containers: append([]v1.Container{
					{
						Name:           "prometheus",
						Image:          prometheusImage,
						Ports:          ports,
						Args:           promArgs,
						VolumeMounts:   promVolumeMounts,
						LivenessProbe:  livenessProbe,
						ReadinessProbe: readinessProbe,
						Resources:      p.Spec.Resources,
					}, {
						Name:  "prometheus-config-reloader",
						Image: c.PrometheusConfigReloader,
						Env: []v1.EnvVar{
							{
								Name: "POD_NAME",
								ValueFrom: &v1.EnvVarSource{
									FieldRef: &v1.ObjectFieldSelector{FieldPath: "metadata.name"},
								},
							},
						},
						Command:      []string{"/bin/prometheus-config-reloader"},
						Args:         configReloadArgs,
						VolumeMounts: configReloadVolumeMounts,
						Resources: v1.ResourceRequirements{
							Limits: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("50m"),
								v1.ResourceMemory: resource.MustParse("50Mi"),
							},
						},
					},
				}, additionalContainers...),
				SecurityContext:               securityContext,
				ServiceAccountName:            p.Spec.ServiceAccountName,
				NodeSelector:                  p.Spec.NodeSelector,
				PriorityClassName:             p.Spec.PriorityClassName,
				TerminationGracePeriodSeconds: &terminationGracePeriod,
				Volumes:                       volumes,
				Tolerations:                   p.Spec.Tolerations,
				Affinity:                      p.Spec.Affinity,
			},
		},
	}, nil
}

func configSecretName(name string) string {
	return prefixedName(name)
}

func volumeName(name string) string {
	return fmt.Sprintf("%s-db", prefixedName(name))
}

func prefixedName(name string) string {
	return fmt.Sprintf("prometheus-%s", name)
}

func subPathForStorage(s *monitoringv1.StorageSpec) string {
	if s == nil {
		return ""
	}

	return "prometheus-db"
}
