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
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/hashicorp/go-multierror"
	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"istio.io/api/annotation"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
)

type wellKnownContainer string

func (n wellKnownContainer) IsContainer(c corev1.Container) bool {
	return c.Name == n.Name()
}

func (n wellKnownContainer) Name() string {
	return string(n)
}

const (
	maxCoreDumpedPods                      = 5
	proxyContainer      wellKnownContainer = "istio-proxy"
	discoveryContainer  wellKnownContainer = "discovery"
	initContainer       wellKnownContainer = "istio-init"
	validationContainer wellKnownContainer = "istio-validation"
)

var coreDumpedPods = atomic.NewInt32(0)

// PodDumper will dump information from all the pods into the given workDir.
// If no pods are provided, client will be used to fetch all the pods in a namespace.
type PodDumper func(ctx resource.Context, cluster cluster.Cluster, workDir string, namespace string, pods ...corev1.Pod)

func podOutputPath(workDir string, cluster cluster.Cluster, pod corev1.Pod, dumpName string) string {
	return outputPath(workDir, cluster, pod.Name, dumpName)
}

// outputPath gives a path in the form of workDir/cluster/<prefix>_<suffix>
func outputPath(workDir string, cluster cluster.Cluster, prefix, suffix string) string {
	dir := path.Join(workDir, cluster.StableName())
	if err := os.MkdirAll(dir, os.ModeDir|0o700); err != nil {
		scopes.Framework.Warnf("failed creating directory: %s", dir)
	}
	return path.Join(dir, fmt.Sprintf("%s_%s", prefix, suffix))
}

func DumpDeployments(ctx resource.Context, workDir, namespace string) {
	errG := multierror.Group{}
	for _, c := range ctx.AllClusters().Kube() {
		deps, err := c.Kube().AppsV1().Deployments(namespace).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			scopes.Framework.Warnf("Error getting deployments for cluster %s: %v", c.Name(), err)
			return
		}
		for _, deployment := range deps.Items {
			deployment := deployment
			errG.Go(func() error {
				out, err := yaml.Marshal(deployment)
				if err != nil {
					return err
				}
				return os.WriteFile(outputPath(workDir, c, deployment.Name, "deployment.yaml"), out, os.ModePerm)
			})
		}
	}
	_ = errG.Wait()
}

func DumpWebhooks(ctx resource.Context, workDir string) {
	errG := multierror.Group{}
	for _, c := range ctx.AllClusters().Kube() {
		mwhs, err := c.Kube().AdmissionregistrationV1().MutatingWebhookConfigurations().List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			scopes.Framework.Warnf("Error getting mutating webhook configurations for cluster %s: %v", c.Name(), err)
			return
		}
		for _, mwh := range mwhs.Items {
			mwh := mwh
			errG.Go(func() error {
				out, err := yaml.Marshal(mwh)
				if err != nil {
					return err
				}
				return os.WriteFile(outputPath(workDir, c, mwh.Name, "mutatingwebhook.yaml"), out, os.ModePerm)
			})
		}
		vwhs, err := c.Kube().AdmissionregistrationV1().ValidatingWebhookConfigurations().List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			scopes.Framework.Warnf("Error getting validating webhook configurations for cluster %s: %v", c.Name(), err)
			return
		}
		for _, vwh := range vwhs.Items {
			vwh := vwh
			errG.Go(func() error {
				out, err := yaml.Marshal(vwh)
				if err != nil {
					return err
				}
				return os.WriteFile(outputPath(workDir, c, vwh.Name, "validatingwebhook.yaml"), out, os.ModePerm)
			})
		}
	}
	_ = errG.Wait()
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
	for _, c := range ctx.AllClusters().Kube() {
		pods, err := c.PodsForSelector(context.TODO(), namespace, selectors...)
		if err != nil {
			scopes.Framework.Warnf("Error getting pods list for cluster %s via kubectl: %v", c.Name(), err)
			return
		}
		if len(pods.Items) == 0 {
			continue
		}
		for _, dump := range dumpers {
			c, dump := c, dump
			wg.Add(1)
			go func() {
				dump(ctx, c, workDir, namespace, pods.Items...)
				wg.Done()
			}()
		}
	}
	wg.Wait()
}

const coredumpDir = "/var/lib/istio"

func DumpCoreDumps(_ resource.Context, c cluster.Cluster, workDir string, namespace string, pods ...corev1.Pod) {
	if coreDumpedPods.Load() >= maxCoreDumpedPods {
		return
	}
	pods = podsOrFetch(c, pods, namespace)
	for _, pod := range pods {
		if coreDumpedPods.Load() >= maxCoreDumpedPods {
			return
		}
		wroteDumpsForPod := false
		containers := append(pod.Spec.Containers, pod.Spec.InitContainers...)
		for _, container := range containers {
			if !proxyContainer.IsContainer(container) {
				continue
			}
			restarts := containerRestarts(pod, proxyContainer.Name())
			crashed, _ := containerCrashed(pod, proxyContainer.Name())
			if !crashed && restarts == 0 {
				// no need to store this dump
				continue
			}

			findDumps := fmt.Sprintf("find %s -name core.*", coredumpDir)
			stdout, _, err := c.PodExec(pod.Name, pod.Namespace, container.Name, findDumps)
			if err != nil {
				scopes.Framework.Warnf("Unable to get core dumps for cluster/pod: %s/%s/%s: %v",
					c.Name(), pod.Namespace, pod.Name, err)
				continue
			}
			for _, cd := range strings.Split(stdout, "\n") {
				if strings.TrimSpace(cd) == "" {
					continue
				}
				stdout, _, err := c.PodExec(pod.Name, pod.Namespace, container.Name, "cat "+cd)
				if err != nil {
					scopes.Framework.Warnf("Unable to get core dumps %v for cluster/pod: %s/%s/%s: %v",
						cd, c.Name(), pod.Namespace, pod.Name, err)
					continue
				}
				fname := podOutputPath(workDir, c, pod, filepath.Base(cd))
				if err = os.WriteFile(fname, []byte(stdout), os.ModePerm); err != nil {
					scopes.Framework.Warnf("Unable to write envoy core dump log for cluster/pod: %s/%s/%s: %v",
						c.Name(), pod.Namespace, pod.Name, err)
				} else {
					wroteDumpsForPod = true
				}
			}
		}
		if wroteDumpsForPod {
			coreDumpedPods.Inc()
		}
	}
}

func podsOrFetch(c cluster.Cluster, pods []corev1.Pod, namespace string) []corev1.Pod {
	if len(pods) == 0 {
		podList, err := c.Kube().CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			scopes.Framework.Warnf("Error getting pods list in cluster %s via kubectl: %v", c.Name(), err)
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
		if err := os.WriteFile(outPath, out, os.ModePerm); err != nil {
			scopes.Framework.Infof("Error writing out pod state to file: %v", err)
		}
	}
}

// DumpPodEvents dumps the pod events for either the provided pods or all pods in the namespace if none are provided.
func DumpPodEvents(_ resource.Context, c cluster.Cluster, workDir, namespace string, pods ...corev1.Pod) {
	pods = podsOrFetch(c, pods, namespace)

	for _, pod := range pods {
		list, err := c.Kube().CoreV1().Events(namespace).List(context.TODO(),
			metav1.ListOptions{
				FieldSelector: "involvedObject.name=" + pod.Name,
			})
		if err != nil {
			scopes.Framework.Warnf("Error getting events list for cluster/pod %s/%s/%s via kubectl: %v",
				c.Name(), namespace, pod.Name, err)
			return
		}

		out, err := yaml.Marshal(list.Items)
		if err != nil {
			scopes.Framework.Warnf("Error marshaling pod event for output: %v", err)
			continue
		}

		outPath := podOutputPath(workDir, c, pod, "pod-events.yaml")
		if err := os.WriteFile(outPath, out, os.ModePerm); err != nil {
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

func containerCrashed(pod corev1.Pod, container string) (bool, *corev1.ContainerStateTerminated) {
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name == container && cs.State.Terminated != nil && cs.State.Terminated.ExitCode != 0 {
			return true, cs.State.Terminated
		}
	}
	return false, nil
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
				scopes.Framework.Warnf("Unable to get logs for cluster/pod/container: %s/%s/%s/%s for: %v",
					c.Name(), pod.Namespace, pod.Name, container.Name, err)
			}

			fname := podOutputPath(workDir, c, pod, fmt.Sprintf("%s.log", container.Name))
			if err = os.WriteFile(fname, []byte(l), os.ModePerm); err != nil {
				scopes.Framework.Warnf("Unable to write logs for cluster/pod/container: %s/%s/%s/%s: %v",
					c.Name(), pod.Namespace, pod.Name, container.Name, err)
			}

			// Get previous container logs, if applicable
			if restarts := containerRestarts(pod, container.Name); restarts > 0 {
				// only care about istio components restart
				if proxyContainer.IsContainer(container) || discoveryContainer.IsContainer(container) || initContainer.IsContainer(container) ||
					validationContainer.IsContainer(container) || strings.HasPrefix(pod.Name, "istio-cni-node") {
					// This is only called if the test failed, so we cannot mark it as "failed" again. Instead, output
					// a log which will get highlighted in the test logs
					// TODO proper analysis of restarts to ensure we do not miss crashes when tests still pass.
					scopes.Framework.Errorf("FAIL: cluster/pod/container %s/%s/%s/%s restarted %d times",
						c.Name(), pod.Namespace, pod.Name, container.Name, restarts)
				}
				l, err := c.PodLogs(context.TODO(), pod.Name, pod.Namespace, container.Name, true /* previousLog */)
				if err != nil {
					scopes.Framework.Warnf("Unable to get previous logs for cluster/pod/container: %s/%s/%s/%s: %v",
						c.Name(), pod.Namespace, pod.Name, container.Name, err)
				}

				fname := podOutputPath(workDir, c, pod, fmt.Sprintf("%s.previous.log", container.Name))
				if err = os.WriteFile(fname, []byte(l), os.ModePerm); err != nil {
					scopes.Framework.Warnf("Unable to write previous logs for cluster/pod/container: %s/%s/%s/%s: %v",
						c.Name(), pod.Namespace, pod.Name, container.Name, err)
				}
			}

			if crashed, terminateState := containerCrashed(pod, container.Name); crashed {
				scopes.Framework.Errorf("FAIL: cluster/pod/container: %s/%s/%s/%s crashed with status: %+v",
					c.Name(), pod.Namespace, pod.Name, container.Name, terminateState)
			}

			// Get envoy logs if the pod is a VM, since kubectl logs only shows the logs from iptables for VMs
			if isVM && proxyContainer.IsContainer(container) {
				if stdout, stderr, err := c.PodExec(pod.Name, pod.Namespace, container.Name, "cat /var/log/istio/istio.err.log"); err == nil {
					fname := podOutputPath(workDir, c, pod, fmt.Sprintf("%s.envoy.err.log", container.Name))
					stdAll := stdout + stderr
					if err = os.WriteFile(fname, []byte(stdAll), os.ModePerm); err != nil {
						scopes.Framework.Warnf("Unable to write envoy err log for VM cluster/pod/container: %s/%s/%s/%s: %v",
							c.Name(), pod.Namespace, pod.Name, container.Name, err)
					}
					if strings.Contains(stdout, "envoy backtrace") {
						scopes.Framework.Errorf("FAIL: VM envoy crashed in cluster/pod/container: %s/%s/%s/%s. See log: %s",
							c.Name(), pod.Namespace, pod.Name, container.Name, fname)

						if strings.Contains(stdAll, "Too many open files") {
							// Run netstat on the container with the crashed proxy to debug socket creation issues.
							if stdout, stderr, err := c.PodExec(pod.Name, pod.Namespace, container.Name, "netstat -at"); err != nil {
								scopes.Framework.Errorf("Unable to run `netstat -at` for crashed VM cluster/pod/container: %s/%s/%s/%s: %v",
									c.Name(), pod.Namespace, pod.Name, container.Name, err)
							} else {
								fname := podOutputPath(workDir, c, pod, fmt.Sprintf("%s.netstat.txt", container.Name))
								if err = os.WriteFile(fname, []byte(stdout+stderr), os.ModePerm); err != nil {
									scopes.Framework.Warnf("Unable to write netstat log for crashed VM cluster/pod/container: %s/%s/%s/%s: %v",
										c.Name(), pod.Namespace, pod.Name, container.Name, err)
								} else {
									scopes.Framework.Errorf("Results of `netstat -at` for crashed VM cluster/pod/container: %s/%s/%s/%s: %s",
										c.Name(), pod.Namespace, pod.Name, container.Name, fname)
								}
							}
						}
					}
				} else {
					scopes.Framework.Warnf("Unable to get envoy err log for VM cluster/pod/container: %s/%s/%s/%s: %v",
						c.Name(), pod.Namespace, pod.Name, container.Name, err)
				}

				if stdout, stderr, err := c.PodExec(pod.Name, pod.Namespace, container.Name, "cat /var/log/istio/istio.log"); err == nil {
					fname := podOutputPath(workDir, c, pod, fmt.Sprintf("%s.envoy.log", container.Name))
					if err = os.WriteFile(fname, []byte(stdout+stderr), os.ModePerm); err != nil {
						scopes.Framework.Warnf("Unable to write envoy log for VM cluster/pod/container: %s/%s/%s/%s: %v",
							c.Name(), pod.Namespace, pod.Name, container.Name, err)
					}
				} else {
					scopes.Framework.Warnf("Unable to get envoy log for VM cluster/pod: %s/%s/%s: %v",
						c.Name(), pod.Namespace, pod.Name, err)
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
		dumpProxyCommand(c, pod, workDir, "proxy-stats.txt", "pilot-agent request GET stats/prometheus")
	}
}

func dumpProxyCommand(c cluster.Cluster, pod corev1.Pod, workDir, filename, command string) {
	containers := append(pod.Spec.Containers, pod.Spec.InitContainers...)
	for _, container := range containers {
		if !proxyContainer.IsContainer(container) {
			// The pilot-agent is only available in the proxy container
			continue
		}

		if cfgDump, _, err := c.PodExec(pod.Name, pod.Namespace, container.Name, command); err == nil {
			fname := podOutputPath(workDir, c, pod, filename)
			if err = os.WriteFile(fname, []byte(cfgDump), os.ModePerm); err != nil {
				scopes.Framework.Errorf("Unable to write output for command %q on cluster/pod/container: %s/%s/%s: %v",
					command, c.Name(), pod.Namespace, pod.Name, container.Name, err)
			}
		} else {
			scopes.Framework.Errorf("Unable to get execute command %q on cluster/pod: %s/%s/%s for: %v",
				command, c.Name(), pod.Namespace, pod.Name, err)
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
		if proxyContainer.IsContainer(c) {
			f = true
			break
		}
	}
	if !f {
		// no proxy container
		return false
	}
	for k, v := range pod.ObjectMeta.Annotations {
		if k == annotation.InjectTemplates.Name && strings.HasPrefix(v, "grpc-") {
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
		scopes.Framework.Warnf("failed dumping %s (cluster %s): %v", endpoint, c.Name(), err)
		return
	}
	args := []string{"x", "internal-debug", "--all", endpoint}
	if ctx.Settings().Revisions.Default() != "" {
		args = append(args, "--revision", ctx.Settings().Revisions.Default())
	}
	scopes.Framework.Debugf("dump %s (cluster %s): %v", endpoint, c.Name(), args)
	stdout, _, err := ik.Invoke(args)
	if err != nil {
		scopes.Framework.Warnf("failed dumping %s (cluster %s): %v", endpoint, c.Name(), err)
		return
	}
	outputs := map[string]string{}
	if err := json.Unmarshal([]byte(stdout), &outputs); err != nil {
		scopes.Framework.Warnf("failed dumping %s (cluster %s): %v", endpoint, c.Name(), err)
		return
	}
	for istiod, out := range outputs {
		outPath := outputPath(workDir, c, istiod, endpoint)
		if err := os.WriteFile(outPath, []byte(out), 0o644); err != nil {
			scopes.Framework.Warnf("failed dumping %s (cluster %s): %v", endpoint, c.Name(), err)
			return
		}
	}
}

func DumpNdsz(_ resource.Context, c cluster.Cluster, workDir string, _ string, pods ...corev1.Pod) {
	for _, pod := range pods {
		dumpProxyCommand(c, pod, workDir, "ndsz.json", "pilot-agent request --debug-port 15020 GET /debug/ndsz")
	}
}
