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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/hashicorp/go-multierror"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
)

// PodDumper will dump information from all the pods into the given workDir.
// If no pods are provided, client will be used to fetch all the pods in a namespace.
type PodDumper func(ctx resource.Context, cluster cluster.Cluster, workDir string, namespace string, pods ...corev1.Pod)

func podOutputPath(workDir string, cluster cluster.Cluster, pod corev1.Pod, dumpName string) string {
	return outputPath(workDir, cluster, pod.Name, dumpName)
}

// outputPath gives a path in the form of workDir/cluster/<prefix>_<suffix>
func outputPath(workDir string, cluster cluster.Cluster, prefix, suffix string) string {
	dir := path.Join(workDir, cluster.Name())
	if err := os.MkdirAll(dir, os.ModeDir|0o700); err != nil {
		scopes.Framework.Warnf("failed creating directory: %s", dir)
	}
	return path.Join(dir, fmt.Sprintf("%s_%s", prefix, suffix))
}

func DumpDeployments(ctx resource.Context, workDir, namespace string) {
	errG := multierror.Group{}
	for _, cluster := range ctx.Clusters().Kube() {
		deps, err := cluster.AppsV1().Deployments(namespace).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			scopes.Framework.Warnf("Error getting deployments: %v", err)
			return
		}
		for _, deployment := range deps.Items {
			deployment := deployment
			errG.Go(func() error {
				out, err := yaml.Marshal(deployment)
				if err != nil {
					return err
				}
				return ioutil.WriteFile(outputPath(workDir, cluster, deployment.Name, "deployment.yaml"), out, os.ModePerm)
			})
		}
	}
}

// DumpPods runs each dumper with the selected pods in the given namespace.
// If selectors is empty, all pods in the namespace will be dumpped.
// If no dumpers are provided, their resource state, events, container logs and Envoy information will be dumped.
func DumpPods(ctx resource.Context, workDir, namespace string, selectors []string, dumpers ...PodDumper) {
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
		pods, err := cluster.PodsForSelector(context.TODO(), namespace, selectors...)
		if err != nil {
			scopes.Framework.Warnf("Error getting pods list via kubectl: %v", err)
			return
		}
		if len(pods.Items) == 0 {
			continue
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
				fname := podOutputPath(workDir, c, pod, filepath.Base(cd))
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

	for _, pod := range pods {
		out, err := yaml.Marshal(&pod)
		if err != nil {
			scopes.Framework.Warnf("Error marshaling pod state for output: %v", err)
			continue
		}

		outPath := podOutputPath(workDir, c, pod, "pod-state.yaml")
		if err := ioutil.WriteFile(outPath, out, os.ModePerm); err != nil {
			scopes.Framework.Infof("Error writing out pod state to file: %v", err)
		}
	}
}

// DumpPodEvents dumps the pod events for either the provided pods or all pods in the namespace if none are provided.
func DumpPodEvents(_ resource.Context, c cluster.Cluster, workDir, namespace string, pods ...corev1.Pod) {
	pods = podsOrFetch(c, pods, namespace)

	for _, pod := range pods {
		list, err := c.CoreV1().Events(namespace).List(context.TODO(),
			metav1.ListOptions{
				FieldSelector: "involvedObject.name=" + pod.Name,
			})
		if err != nil {
			scopes.Framework.Warnf("Error getting events list for pod %s/%s via kubectl: %v", namespace, pod.Name, err)
			return
		}

		out, err := yaml.Marshal(list.Items)
		if err != nil {
			scopes.Framework.Warnf("Error marshaling pod event for output: %v", err)
			continue
		}

		outPath := podOutputPath(workDir, c, pod, "pod-events.yaml")
		if err := ioutil.WriteFile(outPath, out, os.ModePerm); err != nil {
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

func containerCrashed(pod corev1.Pod, container string) bool {
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name == container {
			return cs.State.Terminated != nil
		}
	}
	return false
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

			fname := podOutputPath(workDir, c, pod, fmt.Sprintf("%s.log", container.Name))
			if err = ioutil.WriteFile(fname, []byte(l), os.ModePerm); err != nil {
				scopes.Framework.Warnf("Unable to write logs for pod/container: %s/%s/%s", pod.Namespace, pod.Name, container.Name)
			}

			// Get previous container logs, if applicable
			if restarts := containerRestarts(pod, container.Name); restarts > 0 {
				// This is only called if the test failed, so we cannot mark it as "failed" again. Instead, output
				// a log which will get highlighted in the test logs
				// TODO proper analysis of restarts to ensure we do not miss crashes when tests still pass.
				scopes.Framework.Errorf("FAIL: pod %v/%v container %v restarted %d times", pod.Name, pod.Namespace, container.Name, restarts)
				l, err := c.PodLogs(context.TODO(), pod.Name, pod.Namespace, container.Name, true /* previousLog */)
				if err != nil {
					scopes.Framework.Warnf("Unable to get previous logs for pod/container: %s/%s/%s", pod.Namespace, pod.Name, container.Name)
				}

				fname := podOutputPath(workDir, c, pod, fmt.Sprintf("%s.previous.log", container.Name))
				if err = ioutil.WriteFile(fname, []byte(l), os.ModePerm); err != nil {
					scopes.Framework.Warnf("Unable to write previous logs for pod/container: %s/%s/%s", pod.Namespace, pod.Name, container.Name)
				}
			}

			if containerCrashed(pod, container.Name) {
				scopes.Framework.Errorf("FAIL: pod %v/%v crashed with status: terminated", pod.Name, container.Name)
			}

			// Get envoy logs if the pod is a VM, since kubectl logs only shows the logs from iptables for VMs
			if isVM && container.Name == "istio-proxy" {
				if stdout, stderr, err := c.PodExec(pod.Name, pod.Namespace, container.Name, "cat /var/log/istio/istio.err.log"); err == nil {
					fname := podOutputPath(workDir, c, pod, fmt.Sprintf("%s.envoy.err.log", container.Name))
					if err = ioutil.WriteFile(fname, []byte(stdout+stderr), os.ModePerm); err != nil {
						scopes.Framework.Warnf("Unable to write envoy err log for pod/container: %s/%s/%s", pod.Namespace, pod.Name, container.Name)
					}
					if strings.Contains(stdout, "envoy backtrace") {
						scopes.Framework.Errorf("FAIL: VM %v/%v crashed", pod.Name, container.Name)
					}
				} else {
					scopes.Framework.Warnf("Unable to get envoy err log for pod: %s/%s", pod.Namespace, pod.Name)
				}

				if stdout, stderr, err := c.PodExec(pod.Name, pod.Namespace, container.Name, "cat /var/log/istio/istio.log"); err == nil {
					fname := podOutputPath(workDir, c, pod, fmt.Sprintf("%s.envoy.log", container.Name))
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
		if !hasEnvoy(pod) {
			continue
		}
		dumpProxyCommand(c, pod, workDir, "proxy-config.json", "pilot-agent request GET config_dump?include_eds=true")
		dumpProxyCommand(c, pod, workDir, "proxy-clusters.txt", "pilot-agent request GET clusters")
	}
}

func dumpProxyCommand(c cluster.Cluster, pod corev1.Pod, workDir, filename, command string) {
	isVM := checkIfVM(pod)
	containers := append(pod.Spec.Containers, pod.Spec.InitContainers...)
	for _, container := range containers {
		if container.Name != "istio-proxy" && !isVM {
			// if we don't have istio-proxy container, and we're not running as a VM, agent isn't running
			continue
		}

		if cfgDump, _, err := c.PodExec(pod.Name, pod.Namespace, container.Name, command); err == nil {
			fname := podOutputPath(workDir, c, pod, filename)
			if err = ioutil.WriteFile(fname, []byte(cfgDump), os.ModePerm); err != nil {
				scopes.Framework.Errorf("Unable to write output for command %q on pod/container: %s/%s/%s", command, pod.Namespace, pod.Name, container.Name)
			}
		} else {
			scopes.Framework.Errorf("Unable to get execute command %q on pod: %s/%s for: %v", command, pod.Namespace, pod.Name, err)
		}
	}
}

func hasEnvoy(pod corev1.Pod) bool {
	if checkIfVM(pod) {
		// assume VMs run Envoy
		return true
	}
	f := false
	for _, c := range pod.Spec.Containers {
		if c.Name == "istio-proxy" {
			f = true
			break
		}
	}
	if !f {
		// no proxy container
		return false
	}
	for k, v := range pod.ObjectMeta.Annotations {
		if k == inject.TemplatesAnnotation && strings.HasPrefix(v, "grpc-") {
			// proxy container may run only agent for proxyless gRPC
			return false
		}
	}
	return true
}

func checkIfVM(pod corev1.Pod) bool {
	for k := range pod.ObjectMeta.Labels {
		if strings.Contains(k, "test-vm") {
			return true
		}
	}
	return false
}

func DumpDebug(ctx resource.Context, c cluster.Cluster, workDir string, endpoint string) {
	ik, err := istioctl.New(ctx, istioctl.Config{Cluster: c})
	if err != nil {
		scopes.Framework.Warnf("failed dumping %q: %v", endpoint, err)
		return
	}
	args := []string{"x", "internal-debug", "--all", endpoint}
	if ctx.Settings().Revisions.Default() != "" {
		args = append(args, "--revision", ctx.Settings().Revisions.Default())
	}
	scopes.Framework.Debugf("dump %v: %v", endpoint, args)
	stdout, _, err := ik.Invoke(args)
	if err != nil {
		scopes.Framework.Warnf("failed dumping %q: %v", endpoint, err)
		return
	}
	outputs := map[string]string{}
	if err := json.Unmarshal([]byte(stdout), &outputs); err != nil {
		scopes.Framework.Warnf("failed dumping %q: %v", endpoint, err)
		return
	}
	for istiod, out := range outputs {
		outPath := outputPath(workDir, c, istiod, endpoint)
		if err := ioutil.WriteFile(outPath, []byte(out), 0o644); err != nil {
			scopes.Framework.Warnf("failed dumping %q: %v", endpoint, err)
			return
		}
	}
}

func DumpNdsz(_ resource.Context, c cluster.Cluster, workDir string, _ string, pods ...corev1.Pod) {
	for _, pod := range pods {
		dumpProxyCommand(c, pod, workDir, "ndsz.json", "pilot-agent request --debug-port 15020 GET /debug/ndsz")
	}
}
