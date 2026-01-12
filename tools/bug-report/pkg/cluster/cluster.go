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

package cluster

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/tools/bug-report/pkg/common"
	config2 "istio.io/istio/tools/bug-report/pkg/config"
	"istio.io/istio/tools/bug-report/pkg/util/path"
)

var versionRegex = regexp.MustCompile(`.*(\d\.\d\.\d).*`)

// ParsePath parses path into its components. Input must have the form namespace/deployment/pod/container.
func ParsePath(path string) (namespace string, deployment, pod string, container string, err error) {
	pv := strings.Split(path, "/")
	if len(pv) != 4 {
		return "", "", "", "", fmt.Errorf("bad path %s, must be namespace/deployment/pod/container", path)
	}
	return pv[0], pv[1], pv[2], pv[3], nil
}

// shouldSkipPod means that current pod should be skip or not based on given --include and --exclude
func shouldSkipPod(pod *corev1.Pod, config *config2.BugReportConfig) bool {
	for _, eld := range config.Exclude {
		if len(eld.Namespaces) > 0 {
			if isIncludeOrExcludeEntriesMatched(eld.Namespaces, pod.Namespace) {
				return true
			}
		}
		if len(eld.Pods) > 0 {
			if isIncludeOrExcludeEntriesMatched(eld.Pods, pod.Name) {
				return true
			}
		}
		if len(eld.Containers) > 0 {
			for _, c := range pod.Spec.Containers {
				if isIncludeOrExcludeEntriesMatched(eld.Containers, c.Name) {
					return true
				}
			}
		}
		if len(eld.Labels) > 0 {
			for key, val := range eld.Labels {
				if evLabel, exists := pod.Labels[key]; exists {
					if isExactMatchedOrPatternMatched(val, evLabel) {
						return true
					}
				}
			}
		}
		if len(eld.Annotations) > 0 {
			for key, val := range eld.Annotations {
				if evAnnotation, exists := pod.Annotations[key]; exists {
					if isExactMatchedOrPatternMatched(val, evAnnotation) {
						return true
					}
				}
			}
		}
	}

	for _, ild := range config.Include {
		if len(ild.Namespaces) > 0 {
			if !isIncludeOrExcludeEntriesMatched(ild.Namespaces, pod.Namespace) {
				continue
			}
		}
		if len(ild.Pods) > 0 {
			if !isIncludeOrExcludeEntriesMatched(ild.Pods, pod.Name) {
				continue
			}
		}

		if len(ild.Containers) > 0 {
			isContainerMatch := false
			for _, c := range pod.Spec.Containers {
				if isIncludeOrExcludeEntriesMatched(ild.Containers, c.Name) {
					isContainerMatch = true
				}
			}
			if !isContainerMatch {
				continue
			}
		}

		if len(ild.Labels) > 0 {
			isLabelsMatch := false
			for key, val := range ild.Labels {
				if evLabel, exists := pod.Labels[key]; exists {
					if isExactMatchedOrPatternMatched(val, evLabel) {
						isLabelsMatch = true
						break
					}
				}
			}
			if !isLabelsMatch {
				continue
			}
		}

		if len(ild.Annotations) > 0 {
			isAnnotationMatch := false
			for key, val := range ild.Annotations {
				if evAnnotation, exists := pod.Annotations[key]; exists {
					if isExactMatchedOrPatternMatched(val, evAnnotation) {
						isAnnotationMatch = true
						break
					}
				}
			}
			if !isAnnotationMatch {
				continue
			}
		}
		// If we reach here, it means that all include entries are matched.
		return false
	}
	// If we reach here, it means that no include entries are matched.
	return true
}

func shouldSkipDeployment(deployment string, config *config2.BugReportConfig) bool {
	for _, eld := range config.Exclude {
		if len(eld.Deployments) > 0 {
			if isIncludeOrExcludeEntriesMatched(eld.Deployments, deployment) {
				return true
			}
		}
	}

	for _, ild := range config.Include {
		if len(ild.Deployments) > 0 {
			if !isIncludeOrExcludeEntriesMatched(ild.Deployments, deployment) {
				return true
			}
		}
	}

	return false
}

func shouldSkipDaemonSet(daemonSet string, config *config2.BugReportConfig) bool {
	for _, eld := range config.Exclude {
		if len(eld.Daemonsets) > 0 {
			if isIncludeOrExcludeEntriesMatched(eld.Daemonsets, daemonSet) {
				return true
			}
		}
	}

	for _, ild := range config.Include {
		if len(ild.Daemonsets) > 0 {
			if !isIncludeOrExcludeEntriesMatched(ild.Daemonsets, daemonSet) {
				return true
			}
		}
	}
	return false
}

func isExactMatchedOrPatternMatched(pattern string, term string) bool {
	result, _ := regexp.MatchString(entryPatternToRegexp(pattern), term)
	return result
}

func isIncludeOrExcludeEntriesMatched(entries []string, term string) bool {
	for _, entry := range entries {
		if isExactMatchedOrPatternMatched(entry, term) {
			return true
		}
	}
	return false
}

func entryPatternToRegexp(pattern string) string {
	var reg string
	for i, literal := range strings.Split(pattern, "*") {
		if i > 0 {
			reg += ".*"
		}
		reg += regexp.QuoteMeta(literal)
	}
	return reg
}

// GetClusterResources returns cluster resources for the given REST config and k8s Clientset.
func GetClusterResources(ctx context.Context, clientset *kubernetes.Clientset, config *config2.BugReportConfig) (*Resources, error) {
	out := &Resources{
		Labels:      make(map[string]map[string]string),
		Annotations: make(map[string]map[string]string),
		Pod:         make(map[string]*corev1.Pod),
		CniPod:      make(map[string]*corev1.Pod),
	}

	pods, err := clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	replicasets, err := clientset.AppsV1().ReplicaSets("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	daemonsets, err := clientset.AppsV1().DaemonSets("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	for i, p := range pods.Items {
		if p.Labels["k8s-app"] == "istio-cni-node" {
			out.CniPod[PodKey(p.Namespace, p.Name)] = &pods.Items[i]
		}

		if inject.IgnoredNamespaces.Contains(p.Namespace) {
			continue
		}
		if skip := shouldSkipPod(&p, config); skip {
			continue
		}

		deployment := getOwnerDeployment(&p, replicasets.Items)
		if skip := shouldSkipDeployment(deployment, config); skip {
			continue
		}
		daemonset := getOwnerDaemonSet(&p, daemonsets.Items)
		if skip := shouldSkipDaemonSet(daemonset, config); skip {
			continue
		}

		if deployment != "" {
			for _, c := range p.Spec.Containers {
				out.insertContainer(p.Namespace, deployment, p.Name, c.Name)
			}
			for _, c := range p.Spec.InitContainers {
				if c.Name == inject.ProxyContainerName {
					out.insertContainer(p.Namespace, deployment, p.Name, c.Name)
				}
			}
		} else if daemonset != "" {
			for _, c := range p.Spec.Containers {
				out.insertContainer(p.Namespace, daemonset, p.Name, c.Name)
			}
			for _, c := range p.Spec.InitContainers {
				if c.Name == inject.ProxyContainerName {
					out.insertContainer(p.Namespace, deployment, p.Name, c.Name)
				}
			}
		}

		out.Labels[PodKey(p.Namespace, p.Name)] = p.Labels
		out.Annotations[PodKey(p.Namespace, p.Name)] = p.Annotations
		out.Pod[PodKey(p.Namespace, p.Name)] = &pods.Items[i]
	}

	return out, nil
}

// Resources defines a tree of cluster resource names.
type Resources struct {
	// Root is the first level in the cluster resource hierarchy.
	// Each level in the hierarchy is a map[string]interface{} to the next level.
	// The levels are: namespaces/deployments/pods/containers.
	Root map[string]any
	// Labels maps a pod name to a map of labels key-values.
	Labels map[string]map[string]string
	// Annotations maps a pod name to a map of annotation key-values.
	Annotations map[string]map[string]string
	// Pod maps a pod name to its Pod info. The key is namespace/pod-name.
	Pod map[string]*corev1.Pod
	// CniPod
	CniPod map[string]*corev1.Pod
}

func (r *Resources) insertContainer(namespace, deployment, pod, container string) {
	if r.Root == nil {
		r.Root = make(map[string]any)
	}
	if r.Root[namespace] == nil {
		r.Root[namespace] = make(map[string]any)
	}
	d := r.Root[namespace].(map[string]any)
	if d[deployment] == nil {
		d[deployment] = make(map[string]any)
	}
	p := d[deployment].(map[string]any)
	if p[pod] == nil {
		p[pod] = make(map[string]any)
	}
	c := p[pod].(map[string]any)
	c[container] = nil
}

// ContainerRestarts returns the number of container restarts for the given container.
func (r *Resources) ContainerRestarts(namespace, pod, container string, isCniPod bool) int {
	var podItem *corev1.Pod
	if isCniPod {
		podItem = r.CniPod[PodKey(namespace, pod)]
	} else {
		podItem = r.Pod[PodKey(namespace, pod)]
	}
	for _, cs := range podItem.Status.ContainerStatuses {
		if cs.Name == container {
			return int(cs.RestartCount)
		}
	}
	return 0
}

// IsDiscoveryContainer reports whether the given container is the Istio discovery container.
func (r *Resources) IsDiscoveryContainer(clusterVersion, namespace, pod, container string) bool {
	return common.IsDiscoveryContainer(clusterVersion, container, r.Labels[PodKey(namespace, pod)])
}

// PodIstioVersion returns the Istio version for the given pod, if either the proxy or discovery are one of its
// containers and the tag is in a parseable format.
func (r *Resources) PodIstioVersion(namespace, pod string) string {
	p := r.Pod[PodKey(namespace, pod)]
	if p == nil {
		return ""
	}

	for _, c := range p.Spec.Containers {
		if c.Name == common.ProxyContainerName || c.Name == common.DiscoveryContainerName {
			return imageToVersion(c.Image)
		}
	}
	return ""
}

// String implements the Stringer interface.
func (r *Resources) String() string {
	return resourcesStringImpl(r.Root, "")
}

func resourcesStringImpl(node any, prefix string) string {
	out := ""
	if node == nil {
		return ""
	}
	nv := node.(map[string]any)
	for k, n := range nv {
		out += prefix + k + "\n"
		out += resourcesStringImpl(n, prefix+"  ")
	}

	return out
}

// PodKey returns a unique key based on the namespace and pod name.
func PodKey(namespace, pod string) string {
	return path.Path{namespace, pod}.String()
}

func getOwnerDeployment(pod *corev1.Pod, replicasets []appsv1.ReplicaSet) string {
	for _, o := range pod.OwnerReferences {
		if o.Kind == "ReplicaSet" {
			for _, rs := range replicasets {
				if rs.Name == o.Name {
					for _, oo := range rs.OwnerReferences {
						if oo.Kind == gvk.Deployment.Kind {
							return oo.Name
						}
					}
				}
			}
		}
	}
	return ""
}

func getOwnerDaemonSet(pod *corev1.Pod, daemonsets []appsv1.DaemonSet) string {
	for _, o := range pod.OwnerReferences {
		if o.Kind == gvk.DaemonSet.Kind {
			for _, ds := range daemonsets {
				if ds.Name == o.Name {
					return ds.Name
				}
			}
		}
	}
	return ""
}

func imageToVersion(imageStr string) string {
	vs := versionRegex.FindStringSubmatch(imageStr)
	if len(vs) != 2 {
		return ""
	}
	return vs[0]
}
