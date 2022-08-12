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

package cmd

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"regexp"
	"strings"

	"github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
	batch "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	yamlDecoder "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"

	istioStatus "istio.io/istio/pilot/cmd/pilot-agent/status"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/pkg/log"
)

const (
	annotationPolicy            = "sidecar.istio.io/inject"
	certVolumeName              = "istio-certs"
	dataVolumeName              = "istio-data"
	enableCoreDumpContainerName = "enable-core-dump"
	envoyVolumeName             = "istio-envoy"
	initContainerName           = "istio-init"
	initValidationContainerName = "istio-validation"
	jwtTokenVolumeName          = "istio-token"
	pilotCertVolumeName         = "istiod-ca-cert"
	podInfoVolumeName           = "istio-podinfo"
	proxyContainerName          = "istio-proxy"
	sidecarAnnotationPrefix     = "sidecar.istio.io"
)

func validateUninjectFlags() error {
	var err error

	if uninjectInFilename == "" {
		err = multierror.Append(err, errors.New("filename not specified (see --filename or -f)"))
	}
	return err
}

// extractResourceFile uninjects the istio proxy from the specified
// kubernetes YAML file.
func extractResourceFile(in io.Reader, out io.Writer) error {
	reader := yamlDecoder.NewYAMLReader(bufio.NewReaderSize(in, 4096))
	for {
		raw, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		obj, err := inject.FromRawToObject(raw)
		if err != nil && !runtime.IsNotRegisteredError(err) {
			return multierror.Append(err, fmt.Errorf("cannot parse YAML input"))
		}

		var updated []byte
		if err == nil {
			outObject, err := extractObject(obj)
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

// removeInjectedContainers removes the injected container name - istio-proxy and istio-init
func removeInjectedContainers(containers []corev1.Container, injectedContainerName string) []corev1.Container {
	for index, c := range containers {
		if c.Name == injectedContainerName {
			if index < len(containers)-1 {
				containers = append(containers[:index], containers[index+1:]...)
			} else {
				containers = containers[:index]
			}
			break
		}
	}
	return containers
}

func restoreAppProbes(containers []corev1.Container, probers map[string]*inject.Prober) []corev1.Container {
	re := regexp.MustCompile("/app-health/([a-z]+)/(readyz|livez|startupz)")
	for name, prober := range probers {
		matches := re.FindStringSubmatch(name)
		if len(matches) == 0 {
			continue
		}
		containerName := matches[1]
		probeType := matches[2]
		for i, c := range containers {
			if c.Name == containerName {
				container := c.DeepCopy()
				switch probeType {
				case "readyz":
					container.ReadinessProbe = &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: prober.HTTPGet,
						},
						TimeoutSeconds: prober.TimeoutSeconds,
					}
				case "livez":
					container.LivenessProbe = &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: prober.HTTPGet,
						},
						TimeoutSeconds: prober.TimeoutSeconds,
					}
				case "startupz":
					container.StartupProbe = &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: prober.HTTPGet,
						},
						TimeoutSeconds: prober.TimeoutSeconds,
					}
				}
				containers[i] = *container
			}
		}
	}
	return containers
}

func retrieveAppProbe(containers []corev1.Container) string {
	for _, c := range containers {
		if c.Name != proxyContainerName {
			continue
		}

		for _, env := range c.Env {
			if env.Name == istioStatus.KubeAppProberEnvName {
				return env.Value
			}
		}
	}

	return ""
}

// removeInjectedVolumes removes the injected volumes if exists.
// for example, istio-envoy, istio-certs, and istio-token
func removeInjectedVolumes(volumes []corev1.Volume, injectedVolume string) []corev1.Volume {
	for index, v := range volumes {
		if v.Name == injectedVolume {
			if index < len(volumes)-1 {
				volumes = append(volumes[:index], volumes[index+1:]...)
			} else {
				volumes = volumes[:index]
			}
			break
		}
	}
	return volumes
}

func removeDNSConfig(podDNSConfig *corev1.PodDNSConfig) {
	if podDNSConfig == nil {
		return
	}

	l := len(podDNSConfig.Searches)
	index := 0
	for index < l {
		s := podDNSConfig.Searches[index]
		if strings.Contains(s, "global") {
			if index < len(podDNSConfig.Searches)-1 {
				podDNSConfig.Searches = append(podDNSConfig.Searches[:index],
					podDNSConfig.Searches[index+1:]...)
			} else {
				podDNSConfig.Searches = podDNSConfig.Searches[:index]
			}
			// reset to 0
			index = 0
			l = len(podDNSConfig.Searches)
		} else {
			index++
		}
	}
}

// handleAnnotations removes the injected annotations which contains sidecar.istio.io
// it adds sidecar.istio.io/inject: false
func handleAnnotations(annotations map[string]string) map[string]string {
	if annotations == nil {
		annotations = make(map[string]string)
	}

	for key := range annotations {
		if strings.Contains(key, sidecarAnnotationPrefix) {
			delete(annotations, key)
		}
	}
	// sidecar.istio.io/inject: false to default the auto-injector in case it is present.
	annotations[annotationPolicy] = "false"
	return annotations
}

// extractObject extras the sidecar injection and return the uninjected object.
func extractObject(in runtime.Object) (any, error) {
	out := in.DeepCopyObject()

	var metadata *metav1.ObjectMeta
	var podSpec *corev1.PodSpec

	// Handle Lists
	if list, ok := out.(*corev1.List); ok {
		result := list

		for i, item := range list.Items {
			obj, err := inject.FromRawToObject(item.Raw)
			if runtime.IsNotRegisteredError(err) {
				continue
			}
			if err != nil {
				return nil, err
			}

			r, err := extractObject(obj)
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
		metadata = &job.Spec.JobTemplate.ObjectMeta
		podSpec = &job.Spec.JobTemplate.Spec.Template.Spec
	case *corev1.Pod:
		pod := v
		metadata = &pod.ObjectMeta
		podSpec = &pod.Spec
	default:
		// `in` is a pointer to an Object. Dereference it.
		outValue := reflect.ValueOf(out).Elem()

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

	metadata.Annotations = handleAnnotations(metadata.Annotations)
	// skip uninjection for pods
	sidecarInjected := false
	for _, c := range podSpec.Containers {
		if c.Name == proxyContainerName {
			sidecarInjected = true
		}
	}
	if !sidecarInjected {
		return out, nil
	}

	var appProbe inject.KubeAppProbers
	appProbeStr := retrieveAppProbe(podSpec.Containers)
	if appProbeStr != "" {
		if err := json.Unmarshal([]byte(appProbeStr), &appProbe); err != nil {
			return nil, err
		}
	}
	if appProbe != nil {
		podSpec.Containers = restoreAppProbes(podSpec.Containers, appProbe)
	}

	podSpec.InitContainers = removeInjectedContainers(podSpec.InitContainers, initContainerName)
	podSpec.InitContainers = removeInjectedContainers(podSpec.InitContainers, initValidationContainerName)
	podSpec.InitContainers = removeInjectedContainers(podSpec.InitContainers, enableCoreDumpContainerName)
	podSpec.Containers = removeInjectedContainers(podSpec.Containers, proxyContainerName)
	podSpec.Volumes = removeInjectedVolumes(podSpec.Volumes, certVolumeName)
	podSpec.Volumes = removeInjectedVolumes(podSpec.Volumes, dataVolumeName)
	podSpec.Volumes = removeInjectedVolumes(podSpec.Volumes, envoyVolumeName)
	podSpec.Volumes = removeInjectedVolumes(podSpec.Volumes, jwtTokenVolumeName)
	podSpec.Volumes = removeInjectedVolumes(podSpec.Volumes, pilotCertVolumeName)
	podSpec.Volumes = removeInjectedVolumes(podSpec.Volumes, podInfoVolumeName)
	removeDNSConfig(podSpec.DNSConfig)

	return out, nil
}

var (
	uninjectInFilename  string
	uninjectOutFilename string
)

func uninjectCommand() *cobra.Command {
	uninjectCmd := &cobra.Command{
		Use:   "kube-uninject",
		Short: "Uninject Envoy sidecar from Kubernetes pod resources",
		Long: `
kube-uninject is used to prevent Istio from adding a sidecar and
also provides the inverse of "istioctl kube-inject -f".
`,
		Example: `  # Update resources before applying.
  kubectl apply -f <(istioctl experimental kube-uninject -f <resource.yaml>)

  # Create a persistent version of the deployment by removing Envoy sidecar.
  istioctl experimental kube-uninject -f deployment.yaml -o deployment-uninjected.yaml

  # Update an existing deployment.
  kubectl get deployment -o yaml | istioctl experimental kube-uninject -f - | kubectl apply -f -`,
		RunE: func(c *cobra.Command, _ []string) (err error) {
			if err = validateUninjectFlags(); err != nil {
				return err
			}
			// get the resource content
			var reader io.Reader
			if uninjectInFilename == "-" {
				reader = os.Stdin
			} else {
				var in *os.File
				if in, err = os.Open(uninjectInFilename); err != nil {
					log.Errorf("Error: close file from %s, %s", uninjectInFilename, err)
					return err
				}
				reader = in
				defer func() {
					if errClose := in.Close(); errClose != nil {
						log.Errorf("Error: close file from %s, %s", uninjectInFilename, errClose)

						// don't overwrite the previous error
						if err == nil {
							err = errClose
						}
					}
				}()
			}

			var writer io.Writer
			if uninjectOutFilename == "" {
				writer = c.OutOrStdout()
			} else {
				var out *os.File
				if out, err = os.Create(uninjectOutFilename); err != nil {
					return err
				}
				writer = out
				defer func() {
					if errClose := out.Close(); errClose != nil {
						log.Errorf("Error: close file from %s, %s", uninjectOutFilename, errClose)

						// don't overwrite the previous error
						if err == nil {
							err = errClose
						}
					}
				}()
			}

			return extractResourceFile(reader, writer)
		},
	}

	uninjectCmd.PersistentFlags().StringVarP(&uninjectInFilename, "filename", "f",
		"", "Input Kubernetes resource filename")
	uninjectCmd.PersistentFlags().StringVarP(&uninjectOutFilename, "output", "o",
		"", "Modified output Kubernetes resource filename")

	return uninjectCmd
}
