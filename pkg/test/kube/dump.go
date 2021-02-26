//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package kube

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gogo/protobuf/jsonpb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
)

// PodDumper will dump information from all the pods into the given workDir.
// If no pods are provided, client will be used to fetch all the pods in a namespace.
type PodDumper func(ctx resource.Context, cluster cluster.Cluster, workDir string, namespace string, pods ...corev1.Pod)

func outputPath(workDir string, cluster cluster.Cluster, pod corev1.Pod, name string) string {
	dir := path.Join(workDir, cluster.Name())
	if err := os.MkdirAll(dir, os.ModeDir|0o700); err != nil {
		scopes.Framework.Warnf("failed creating directory: %s", dir)
	}
	return path.Join(dir, fmt.Sprintf("%s_%s", pod.Name, name))
}

// DumpPods runs each dumper with all the pods in the given namespace.
// If no dumpers are provided, their resource state, events, container logs and Envoy information will be dumped.
func DumpPods(ctx resource.Context, workDir, namespace string, dumpers ...PodDumper) {
	if len(dumpers) == 0 {
		dumpers = []PodDumper{
			DumpPodState,
			DumpPodEvents,
			DumpPodLogs,
			DumpPodProxies,
			DumpNdsz,
			DumpCoreDumps,
		}
	}

	wg := sync.WaitGroup{}
	for _, cluster := range ctx.Clusters().Kube() {
		pods, err := cluster.PodsForSelector(context.TODO(), namespace)
		if err != nil {
			scopes.Framework.Warnf("Error getting pods list via kubectl: %v", err)
			return
		}
		for _, dump := range dumpers {
			cluster, dump := cluster, dump
			wg.Add(1)
			go func() {
				dump(ctx, cluster, workDir, namespace, pods.Items...)
				wg.Done()
			}()
		}
	}
	wg.Wait()
}

const coredumpDir = "/var/lib/istio"

func DumpCoreDumps(ctx resource.Context, c cluster.Cluster, workDir string, namespace string, pods ...corev1.Pod) {
	pods = podsOrFetch(c, pods, namespace)
	for _, pod := range pods {
		containers := append(pod.Spec.Containers, pod.Spec.InitContainers...)
		for _, container := range containers {
			if container.Name != "istio-proxy" {
				continue
			}
			findDumps := fmt.Sprintf("find %s -name core.*", coredumpDir)
			stdout, _, err := c.PodExec(pod.Name, pod.Namespace, container.Name, findDumps)
			if err != nil {
				scopes.Framework.Warnf("Unable to get core dumps for pod: %s/%s", pod.Namespace, pod.Name)
				continue
			}
			for _, cd := range strings.Split(stdout, "\n") {
				if strings.TrimSpace(cd) == "" {
					continue
				}
				stdout, _, err := c.PodExec(pod.Name, pod.Namespace, container.Name, "cat "+cd)
				if err != nil {
					scopes.Framework.Warnf("Unable to get core dumps %v for pod: %s/%s", cd, pod.Namespace, pod.Name)
					continue
				}
				fname := outputPath(workDir, c, pod, filepath.Base(cd))
				if err = ioutil.WriteFile(fname, []byte(stdout), os.ModePerm); err != nil {
					scopes.Framework.Warnf("Unable to write envoy core dump log for pod: %s/%s: %v", pod.Namespace, pod.Name, err)
				}
			}
		}
	}
}

func podsOrFetch(a cluster.Cluster, pods []corev1.Pod, namespace string) []corev1.Pod {
	if len(pods) == 0 {
		podList, err := a.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			scopes.Framework.Warnf("Error getting pods list via kubectl: %v", err)
			return nil
		}
		pods = podList.Items
	}
	return pods
}

// DumpPodState dumps the pod state for either the provided pods or all pods in the namespace if none are provided.
func DumpPodState(_ resource.Context, c cluster.Cluster, workDir string, namespace string, pods ...corev1.Pod) {
	pods = podsOrFetch(c, pods, namespace)

	marshaler := jsonpb.Marshaler{
		Indent: "  ",
	}

	for _, pod := range pods {
		str, err := marshaler.MarshalToString(&pod)
		if err != nil {
			scopes.Framework.Warnf("Error marshaling pod state for output: %v", err)
			continue
		}

		outPath := outputPath(workDir, c, pod, "pod-state.yaml")
		if err := ioutil.WriteFile(outPath, []byte(str), os.ModePerm); err != nil {
			scopes.Framework.Infof("Error writing out pod state to file: %v", err)
		}
	}
}

// DumpPodEvents dumps the pod events for either the provided pods or all pods in the namespace if none are provided.
func DumpPodEvents(_ resource.Context, c cluster.Cluster, workDir, namespace string, pods ...corev1.Pod) {
	pods = podsOrFetch(c, pods, namespace)

	marshaler := jsonpb.Marshaler{
		Indent: "  ",
	}

	for _, pod := range pods {
		list, err := c.CoreV1().Events(namespace).List(context.TODO(),
			metav1.ListOptions{
				FieldSelector: "involvedObject.name=" + pod.Name,
			})
		if err != nil {
			scopes.Framework.Warnf("Error getting events list for pod %s/%s via kubectl: %v", namespace, pod.Name, err)
			return
		}

		eventsStr := ""
		for _, event := range list.Items {
			eventStr, err := marshaler.MarshalToString(&event)
			if err != nil {
				scopes.Framework.Warnf("Error marshaling pod event for output: %v", err)
				continue
			}

			eventsStr += eventStr
			eventsStr += "\n"
		}

		outPath := outputPath(workDir, c, pod, "pod-events.yaml")
		if err := ioutil.WriteFile(outPath, []byte(eventsStr), os.ModePerm); err != nil {
			scopes.Framework.Infof("Error writing out pod events to file: %v", err)
		}
	}
}

// containerRestarts checks how many times container has ever restarted
func containerRestarts(pod corev1.Pod, container string) int {
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name == container {
			return int(cs.RestartCount)
		}
	}
	// No match - assume that means no restart
	return 0
}

// DumpPodLogs will dump logs from each container in each of the provided pods
// or all pods in the namespace if none are provided.
func DumpPodLogs(_ resource.Context, c cluster.Cluster, workDir, namespace string, pods ...corev1.Pod) {
	pods = podsOrFetch(c, pods, namespace)

	for _, pod := range pods {
		isVM := checkIfVM(pod)
		containers := append(pod.Spec.Containers, pod.Spec.InitContainers...)
		for _, container := range containers {
			l, err := c.PodLogs(context.TODO(), pod.Name, pod.Namespace, container.Name, false /* previousLog */)
			if err != nil {
				scopes.Framework.Warnf("Unable to get logs for pod/container: %s/%s/%s for: %v", pod.Namespace, pod.Name, container.Name, err)
			}

			fname := outputPath(workDir, c, pod, fmt.Sprintf("%s.log", container.Name))
			if err = ioutil.WriteFile(fname, []byte(l), os.ModePerm); err != nil {
				scopes.Framework.Warnf("Unable to write logs for pod/container: %s/%s/%s", pod.Namespace, pod.Name, container.Name)
			}

			// Get previous container logs, if applicable
			if restarts := containerRestarts(pod, container.Name); restarts > 0 {
				// This is only called if the test failed, so we cannot mark it as "failed" again. Instead, output
				// a log which will get highlighted in the test logs
				// TODO proper analysis of restarts to ensure we do not miss crashes when tests still pass.
				scopes.Framework.Errorf("FAIL: pod %v/%v restarted %d times", pod.Name, pod.Namespace, restarts)
				l, err := c.PodLogs(context.TODO(), pod.Name, pod.Namespace, container.Name, true /* previousLog */)
				if err != nil {
					scopes.Framework.Warnf("Unable to get previous logs for pod/container: %s/%s/%s", pod.Namespace, pod.Name, container.Name)
				}

				fname := outputPath(workDir, c, pod, fmt.Sprintf("%s.previous.log", container.Name))
				if err = ioutil.WriteFile(fname, []byte(l), os.ModePerm); err != nil {
					scopes.Framework.Warnf("Unable to write previous logs for pod/container: %s/%s/%s", pod.Namespace, pod.Name, container.Name)
				}
			}

			// Get envoy logs if the pod is a VM, since kubectl logs only shows the logs from iptables for VMs
			if isVM && container.Name == "istio-proxy" {
				if stdout, stderr, err := c.PodExec(pod.Name, pod.Namespace, container.Name, "cat /var/log/istio/istio.err.log"); err == nil {
					fname := outputPath(workDir, c, pod, fmt.Sprintf("%s.envoy.err.log", container.Name))
					if err = ioutil.WriteFile(fname, []byte(stdout+stderr), os.ModePerm); err != nil {
						scopes.Framework.Warnf("Unable to write envoy err log for pod/container: %s/%s/%s", pod.Namespace, pod.Name, container.Name)
					}
				} else {
					scopes.Framework.Warnf("Unable to get envoy err log for pod: %s/%s", pod.Namespace, pod.Name)
				}

				if stdout, stderr, err := c.PodExec(pod.Name, pod.Namespace, container.Name, "cat /var/log/istio/istio.log"); err == nil {
					fname := outputPath(workDir, c, pod, fmt.Sprintf("%s.envoy.log", container.Name))
					if err = ioutil.WriteFile(fname, []byte(stdout+stderr), os.ModePerm); err != nil {
						scopes.Framework.Warnf("Unable to write envoy log for pod/container: %s/%s/%s", pod.Namespace, pod.Name, container.Name)
					}
				} else {
					scopes.Framework.Warnf("Unable to get envoy log for pod: %s/%s", pod.Namespace, pod.Name)
				}
			}
		}
	}
}

// DumpPodProxies will dump Envoy proxy config and clusters in each of the provided pods
// or all pods in the namespace if none are provided.
func DumpPodProxies(_ resource.Context, c cluster.Cluster, workDir, namespace string, pods ...corev1.Pod) {
	pods = podsOrFetch(c, pods, namespace)

	for _, pod := range pods {
		containers := append(pod.Spec.Containers, pod.Spec.InitContainers...)
		for _, container := range containers {
			if container.Name != "istio-proxy" {
				continue
			}

			if cfgDump, _, err := c.PodExec(pod.Name, pod.Namespace, container.Name, "pilot-agent request GET config_dump"); err == nil {
				fname := outputPath(workDir, c, pod, "proxy-config.json")
				if err = ioutil.WriteFile(fname, []byte(cfgDump), os.ModePerm); err != nil {
					scopes.Framework.Errorf("Unable to write config dump for pod/container: %s/%s/%s", pod.Namespace, pod.Name, container.Name)
				}
			} else {
				scopes.Framework.Errorf("Unable to get istio-proxy config dump for pod: %s/%s for: %v", pod.Namespace, pod.Name, err)
			}

			if cfgDump, _, err := c.PodExec(pod.Name, pod.Namespace, container.Name, "pilot-agent request GET clusters"); err == nil {
				fname := outputPath(workDir, c, pod, "proxy-clusters.txt")
				if err = ioutil.WriteFile(fname, []byte(cfgDump), os.ModePerm); err != nil {
					scopes.Framework.Errorf("Unable to write clusters for pod/container: %s/%s/%s", pod.Namespace, pod.Name, container.Name)
				}
			} else {
				scopes.Framework.Errorf("Unable to get istio-proxy clusters for pod: %s/%s", pod.Namespace, pod.Name)
			}
		}
	}
}

func checkIfVM(pod corev1.Pod) bool {
	for k := range pod.ObjectMeta.Labels {
		if strings.Contains(k, "test-vm") {
			return true
		}
	}
	return false
}

func DumpDebug(c cluster.Cluster, workDir string, endpoint string) {
	cp, istiod, err := getControlPlane(c)
	if err != nil {
		scopes.Framework.Warnf("failed dumping %q: %v", endpoint, err)
		return
	}
	outPath := outputPath(workDir, c, istiod, endpoint)
	out, err := dumpDebug(cp, istiod, fmt.Sprintf("/debug/%s", endpoint))
	if err != nil {
		scopes.Framework.Warnf("failed dumping %q: %v", endpoint, err)
		return
	}
	if err := ioutil.WriteFile(outPath, []byte(out), 0o644); err != nil {
		scopes.Framework.Warnf("failed dumping %q: %v", endpoint, err)
		return
	}
}

func DumpNdsz(_ resource.Context, c cluster.Cluster, workDir string, _ string, pods ...corev1.Pod) {
	cp, istiod, err := getControlPlane(c)
	if err != nil {
		scopes.Framework.Warnf("failed dumping ndsz: %v", err)
		return
	}
	for _, p := range pods {
		endpoint := fmt.Sprintf("/debug/ndsz?proxyID=%s.%s", p.Name, p.Namespace)
		out, err := dumpDebug(cp, istiod, endpoint)
		if err != nil {
			scopes.Framework.Warnf("failed dumping ndsz: %v", err)
			continue
		}
		// dump to the cluster directory for the proxy
		outPath := outputPath(workDir, c, p, "ndsz.json")
		if err := ioutil.WriteFile(outPath, []byte(out), 0o644); err != nil {
			scopes.Framework.Warnf("failed dumping ndsz: %v", err)
		}
	}
}

func dumpDebug(cp cluster.Cluster, istiodPod corev1.Pod, endpoint string) (string, error) {
	// exec to the control plane to run nds gen
	cmd := []string{"pilot-discovery", "request", "GET", endpoint}

	out, _, err := cp.PodExec(istiodPod.Name, istiodPod.Namespace, "discovery", strings.Join(cmd, " "))
	if err != nil {
		return "", err
	}
	return out, nil
}

func getControlPlane(cluster cluster.Cluster) (cluster.Cluster, corev1.Pod, error) {
	// fetch istiod from the control-plane cluster
	cp := cluster.Primary()
	// TODO use namespace from framework
	istiodPods, err := cp.PodsForSelector(context.TODO(), "istio-system", "istio=pilot")
	if err != nil {
		return nil, corev1.Pod{}, err
	}
	if len(istiodPods.Items) == 0 {
		return nil, corev1.Pod{}, fmt.Errorf("0 pods found for istio=pilot in %s", cp.Name())
	}
	return cp, istiodPods.Items[0], nil
}
