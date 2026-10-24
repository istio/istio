//go:build integ

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

package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/rest"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/tools/bug-report/pkg/kubectlcmd"

	"istio.io/istio/pkg/envoy/admin"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/components/namespace"
	testkube "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/util/retry"
)

// This suite runs unchanged in the init-networking, CNI, IPv4, and IPv6 jobs.
// Each job must use the proxy and control-plane images built from this checkout.
func TestAdminTransport(t *testing.T) {
	framework.NewTest(t).Run(func(t framework.TestContext) {
		for _, c := range t.Clusters() {
			t.NewSubTest(c.Name()).Run(func(t framework.TestContext) {
				if !c.MinKubeVersion(33) {
					t.Skip("requires Linux Kubernetes 1.33+ with stable native sidecars")
				}
				nodes, err := c.Kube().CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
				if err != nil {
					t.Fatal(err)
				}
				for _, n := range nodes.Items {
					if n.Status.NodeInfo.OperatingSystem != "linux" {
						t.Skip("private admin socket tests require Linux nodes")
					}
				}
				ns := namespace.NewOrFail(t, namespace.Config{Prefix: "admin-transport", Inject: true})
				t.Cleanup(func() {
					if t.Failed() {
						testkube.DumpPods(t, t.CreateDirectoryOrFail("admin-failure"), ns.Name(), nil)
					}
				})

				role := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "diagnostics-without-exec", Namespace: ns.Name()}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"get", "list"}}, {APIGroups: []string{""}, Resources: []string{"pods/portforward"}, Verbs: []string{"create", "get"}}}}
				if _, err := c.Kube().RbacV1().Roles(ns.Name()).Create(context.Background(), role, metav1.CreateOptions{}); err != nil {
					t.Fatal(err)
				}
				user := "diagnostics-no-exec-" + ns.Name()
				binding := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: role.Name, Namespace: ns.Name()}, RoleRef: rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "Role", Name: role.Name}, Subjects: []rbacv1.Subject{{Kind: "User", Name: user}}}
				if _, err := c.Kube().RbacV1().RoleBindings(ns.Name()).Create(context.Background(), binding, metav1.CreateOptions{}); err != nil {
					t.Fatal(err)
				}
				limitedConfig := rest.CopyConfig(c.RESTConfig())
				limitedConfig.Impersonate = rest.ImpersonationConfig{UserName: user}
				limited, err := kube.NewCLIClient(kube.NewClientConfigForRestConfig(limitedConfig))
				if err != nil {
					t.Fatal(err)
				}
				cases := []struct {
					name, annotation, metadata, transport string
					native                                bool
				}{
					{name: "traditional-default", transport: "TCP"},
					{name: "native-tcp", native: true, transport: "TCP"},
					{name: "native-uds", native: true, annotation: "UDS", transport: "UDS"},
					{name: "metadata-uds", native: true, metadata: "UDS", transport: "UDS"},
					{name: "pod-opt-in", native: true, metadata: "TCP", annotation: "UDS", transport: "UDS"},
					{name: "pod-opt-out", native: true, metadata: "UDS", annotation: "TCP", transport: "TCP"},
					{name: "custom-identity", native: true, annotation: "UDS", transport: "UDS"},
					{name: "root-tproxy", native: true, annotation: "UDS", transport: "UDS"},
					{name: "distroless", native: true, annotation: "UDS", transport: "UDS"},
				}
				for _, tc := range cases {
					t.NewSubTest(tc.name).Run(func(t framework.TestContext) {
						pod := fixture(tc.name, ns.Name(), tc.native, tc.annotation, tc.metadata)
						if tc.name == "custom-identity" {
							uid, gid := int64(2000), int64(2001)
							pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{Name: "istio-proxy", Image: "auto", ImagePullPolicy: corev1.PullIfNotPresent, SecurityContext: &corev1.SecurityContext{RunAsUser: &uid, RunAsGroup: &gid}})
						}

						if tc.name == "root-tproxy" {
							pod.Annotations["sidecar.istio.io/interceptionMode"] = "TPROXY"
						}

						if tc.name == "distroless" {
							pod.Annotations["sidecar.istio.io/proxyImageType"] = "distroless"
						}

						created, err := c.Kube().CoreV1().Pods(ns.Name()).Create(context.Background(), pod, metav1.CreateOptions{})
						if err != nil {
							t.Fatal(err)
						}
						t.Cleanup(func() {
							if t.Failed() {
								testkube.DumpPods(t, t.CreateDirectoryOrFail("pod-failure"), ns.Name(), nil)
							}
							_ = c.Kube().CoreV1().Pods(ns.Name()).Delete(context.Background(), pod.Name, metav1.DeleteOptions{})
						})
						proxy := inject.FindSidecar(created)
						if proxy == nil {
							t.Fatal("proxy was not injected")
						}
						count := 0
						for _, e := range proxy.Env {
							if e.Name == admin.Env {
								count++
								if e.Value != tc.transport {
									t.Fatalf("rendered transport %s, want %s", e.Value, tc.transport)
								}
							}
						}
						if count != 1 {
							t.Fatalf("got %d resolved transport variables", count)
						}
						readyPod := waitReady(t, c, ns.Name(), pod.Name)
						loopback := "127.0.0.1"
						if net.ParseIP(readyPod.Status.PodIP).To4() == nil {
							loopback = "::1"
						}
						localURL := func(port int, path string) string {
							return "http://" + net.JoinHostPort(loopback, strconv.Itoa(port)) + path
						}

						restricted := tc.transport == "UDS"
						for _, host := range []string{loopback, readyPod.Status.PodIP} {
							for _, method := range []string{"GET", "POST"} {
								_, _, err := c.PodExecCommands(pod.Name, ns.Name(), "app", []string{"curl", "--noproxy", "*", "--max-time", "3", "-fsS", "-X", method, "http://" + net.JoinHostPort(host, "15000") + "/server_info"})
								if restricted && err == nil {
									t.Fatalf("application reached UDS admin over TCP through %s", host)
								}
								if !restricted && host == loopback && method == "GET" && err != nil {
									t.Fatalf("TCP control cannot reach admin: %v", err)
								}
							}
						}
						if restricted {
							for _, endpoint := range []string{"quitquitquit", "drain"} {
								for _, method := range []string{"GET", "POST"} {
									out, _, err := c.PodExecCommands(pod.Name, ns.Name(), "app", []string{"curl", "--noproxy", "*", "--max-time", "3", "-sS", "-o", "/dev/null", "-w", "%{http_code}", "-X", method, localURL(15020, "/"+endpoint)})
									if err != nil || out != "404" {
										t.Fatalf("%s %s: %s, %v", method, endpoint, out, err)
									}
								}
							}
							if _, _, err := c.PodExecCommands(pod.Name, ns.Name(), "app", []string{"sh", "-c", "test ! -e /etc/istio/proxy/admin/admin.sock"}); err != nil {
								t.Fatalf("application can see socket: %v", err)
							}
						}
						for _, endpoint := range []string{localURL(15021, "/healthz/ready"), localURL(15090, "/stats/prometheus"), localURL(15020, "/stats/prometheus"), localURL(8080, "/get")} {
							if _, _, err := c.PodExecCommands(pod.Name, ns.Name(), "app", []string{"curl", "--noproxy", "*", "--max-time", "5", "-fsS", endpoint}); err != nil {
								t.Fatalf("health/metrics/traffic %s: %v", endpoint, err)
							}
						}
						for _, endpoint := range []string{"server_info", "stats", "config_dump", "logging"} {
							method := "GET"
							if endpoint == "logging" {
								method = "POST"
							}
							out, err := c.EnvoyDoWithPort(context.Background(), pod.Name, ns.Name(), method, endpoint, 15000)
							if err != nil || len(out) == 0 {
								t.Fatalf("diagnostic %s: %v", endpoint, err)
							}
							if endpoint == "config_dump" && !json.Valid(out) {
								t.Fatalf("diagnostic stdout is not JSON: %s", out)
							}
							if endpoint == "config_dump" && restricted {
								if !strings.Contains(string(out), admin.SocketPath) {
									t.Fatal("bootstrap does not contain private socket")
								}
							}
						}

						if _, err := limited.EnvoyDoWithPort(context.Background(), pod.Name, ns.Name(), "GET", "server_info", 15000); restricted {
							if err == nil || !strings.Contains(strings.ToLower(err.Error()), "forbidden") {
								t.Fatalf("expected exec authorization denial: %v", err)
							}
						} else if err != nil {
							t.Fatalf("TCP port forwarding without exec failed: %v", err)
						}
						runner := kubectlcmd.NewRunner(1)
						runner.Client = c
						if out, err := runner.EnvoyGet(ns.Name(), pod.Name, "config_dump", false, 15000); err != nil || !json.Valid([]byte(out)) {
							t.Fatalf("bug-report admin collection failed: %v", err)
						}
						ctl := istioctl.NewOrFail(t, istioctl.Config{Cluster: c})
						for _, args := range [][]string{{"proxy-config", "bootstrap", pod.Name, "-n", ns.Name()}, {"proxy-config", "log", pod.Name, "-n", ns.Name()}} {
							if _, _, err := ctl.Invoke(args); err != nil {
								t.Fatalf("istioctl %v: %v", args, err)
							}
						}
						if restricted {

							if tc.name == "native-uds" {
								if _, _, err := c.PodExecCommands(pod.Name, ns.Name(), "istio-proxy", []string{"mv", admin.SocketPath, admin.SocketPath + ".hidden"}); err != nil {
									t.Fatal(err)
								}
								// Always restore the pathname, including on assertion failure.
								func() {
									defer func() {
										_, _, err := c.PodExecCommands(pod.Name, ns.Name(), "istio-proxy", []string{"mv", admin.SocketPath + ".hidden", admin.SocketPath})
										if err != nil {
											t.Errorf("restore socket: %v", err)
										}
									}()
									if _, err := c.EnvoyDoWithPort(context.Background(), pod.Name, ns.Name(), "GET", "server_info", 15000); err == nil {
										t.Fatal("unavailable socket request succeeded")
									}
									out, _, err := c.PodExecCommands(pod.Name, ns.Name(), "app", []string{"curl", "--noproxy", "*", "--max-time", "5", "-sS", "-o", "/dev/null", "-w", "%{http_code}", localURL(15021, "/healthz/ready")})
									if err != nil || out != "503" {
										t.Fatalf("unavailable admin readiness: %s, %v", out, err)
									}
									if _, _, err := c.PodExecCommands(pod.Name, ns.Name(), "app", []string{"curl", "--noproxy", "*", "--max-time", "3", "-fsS", localURL(15000, "/server_info")}); err == nil {
										t.Fatal("TCP fallback listener appeared")
									}
								}()
							}
							if tc.name != "distroless" {
								out, _, err := c.PodExecCommands(pod.Name, ns.Name(), "istio-proxy", []string{"stat", "-c", "%a %u %g", "/etc/istio/proxy/admin", admin.SocketPath})
								uid, gid := int64(1337), int64(1337)
								if proxy.SecurityContext != nil {
									if proxy.SecurityContext.RunAsUser != nil {
										uid = *proxy.SecurityContext.RunAsUser
									}
									if proxy.SecurityContext.RunAsGroup != nil {
										gid = *proxy.SecurityContext.RunAsGroup
									}
								}
								expected := fmt.Sprintf("700 %d %d\n600 %d %d\n", uid, gid, uid, gid)
								if err != nil || out != expected {
									t.Fatalf("socket permissions: %q, %v; want %q", out, err, expected)
								}
							}
							before := readyPod.Status.InitContainerStatuses
							if _, err := c.EnvoyDoWithPort(context.Background(), pod.Name, ns.Name(), "POST", "quitquitquit", 15000); err != nil {
								t.Fatal(err)
							}
							retry.UntilSuccessOrFail(t, func() error {
								p, err := c.Kube().CoreV1().Pods(ns.Name()).Get(context.Background(), pod.Name, metav1.GetOptions{})
								if err != nil {
									return err
								}
								for _, now := range p.Status.InitContainerStatuses {
									for _, old := range before {
										if now.Name == "istio-proxy" && old.Name == now.Name && now.RestartCount > old.RestartCount && now.Ready {
											return nil
										}
									}
								}
								return fmt.Errorf("waiting for proxy restart")
							}, retry.Timeout(90*time.Second))
							if _, err := c.EnvoyDoWithPort(context.Background(), pod.Name, ns.Name(), "GET", "server_info", 15000); err != nil {
								t.Fatalf("admin unavailable after restart: %v", err)
							}
						}
					})
				}
				t.NewSubTest("invalid-configurations").Run(func(t framework.TestContext) {
					for _, kind := range []string{"traditional", "empty", "unknown", "bootstrap", "lifecycle", "shared-volume"} {
						p := fixture("invalid-"+kind, ns.Name(), true, "UDS", "")
						switch kind {
						case "traditional":
							p.Annotations["sidecar.istio.io/nativeSidecar"] = "false"
						case "empty":
							p.Annotations[admin.Annotation] = ""
						case "unknown":
							p.Annotations[admin.Annotation] = "invalid"
						case "bootstrap":
							p.Annotations["sidecar.istio.io/bootstrapOverride"] = "custom"
						case "lifecycle":
							p.Spec.Containers = append(p.Spec.Containers, corev1.Container{Name: "istio-proxy", Image: "auto", Lifecycle: &corev1.Lifecycle{PreStop: &corev1.LifecycleHandler{Exec: &corev1.ExecAction{Command: []string{"false"}}}}})
						case "shared-volume":
							p.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{Name: "istio-envoy", MountPath: "/shared"}}
						}
						if _, err := c.Kube().CoreV1().Pods(ns.Name()).Create(context.Background(), p, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}}); err == nil {
							t.Errorf("accepted %s", kind)
						}
					}
				})
				t.NewSubTest("job-completion").Run(func(t framework.TestContext) {
					p := fixture("job", ns.Name(), true, "UDS", "")
					p.Spec.RestartPolicy = corev1.RestartPolicyNever
					p.Spec.Containers = p.Spec.Containers[:1]
					p.Spec.Containers[0].Command = []string{"sh", "-c", "echo application-complete"}
					job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "native-uds-job", Namespace: ns.Name()}, Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{ObjectMeta: p.ObjectMeta, Spec: p.Spec}}}
					job.Spec.Template.Name = ""
					start := time.Now()
					if _, err := c.Kube().BatchV1().Jobs(ns.Name()).Create(context.Background(), job, metav1.CreateOptions{}); err != nil {
						t.Fatal(err)
					}
					retry.UntilSuccessOrFail(t, func() error {
						j, err := c.Kube().BatchV1().Jobs(ns.Name()).Get(context.Background(), job.Name, metav1.GetOptions{})
						if err != nil {
							return err
						}
						if j.Status.Succeeded == 1 {
							pods, err := c.Kube().CoreV1().Pods(ns.Name()).List(context.Background(), metav1.ListOptions{LabelSelector: "batch.kubernetes.io/job-name=" + job.Name})
							if err != nil {
								return err
							}
							if len(pods.Items) != 1 {
								return fmt.Errorf("expected one Job pod, got %d", len(pods.Items))
							}
							p := pods.Items[0]
							if len(p.Status.ContainerStatuses) != 1 || p.Status.ContainerStatuses[0].State.Terminated == nil {
								return fmt.Errorf("missing application termination timestamp")
							}
							appEnd := p.Status.ContainerStatuses[0].State.Terminated.FinishedAt.Time
							for _, s := range p.Status.InitContainerStatuses {
								if s.Name == "istio-proxy" && s.State.Terminated != nil {
									elapsed := s.State.Terminated.FinishedAt.Sub(appEnd)
									if elapsed < 0 || elapsed > 10*time.Second {
										t.Fatalf("Job proxy termination lag %v", elapsed)
									}
									return nil
								}
							}
							return fmt.Errorf("missing proxy termination timestamp")
						}
						return fmt.Errorf("job not complete after %v", time.Since(start))
					}, retry.Timeout(90*time.Second))
				})
			})
		}
	})
}

func fixture(name, ns string, native bool, transport, metadata string) *corev1.Pod {
	grace := int64(30)
	annotations := map[string]string{"sidecar.istio.io/nativeSidecar": strconv.FormatBool(native)}
	if transport != "" {
		annotations[admin.Annotation] = transport
	}
	if metadata != "" {
		annotations["proxy.istio.io/config"] = "proxyMetadata:\n  ISTIO_ENVOY_ADMIN_TRANSPORT: " + metadata
	}
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Annotations: annotations, Labels: map[string]string{"app": name}}, Spec: corev1.PodSpec{
		TerminationGracePeriodSeconds: &grace,
		Containers: []corev1.Container{
			{Name: "app", Image: "docker.io/curlimages/curl:8.16.0", Command: []string{"sh", "-c", "trap 'exit 0' TERM; while :; do sleep 1 & wait $!; done"}},
			{Name: "httpbin", Image: "docker.io/mccutchen/go-httpbin:v2.15.0", Ports: []corev1.ContainerPort{{Name: "http", ContainerPort: 8080}}, ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/get", Port: intstr.FromInt32(8080)}}, PeriodSeconds: 1}},
		},
	}}
}

func waitReady(t framework.TestContext, c cluster.Cluster, ns, name string) *corev1.Pod {
	t.Helper()
	var pod *corev1.Pod
	retry.UntilSuccessOrFail(t, func() error {
		var err error
		pod, err = c.Kube().CoreV1().Pods(ns).Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
				return nil
			}
		}
		return fmt.Errorf("pod %s not ready", name)
	}, retry.Timeout(120*time.Second))
	return pod
}

func TestNativeTermination(t *testing.T) {
	framework.NewTest(t).Run(func(t framework.TestContext) {
		for _, c := range t.Clusters() {
			t.NewSubTest(c.Name()).Run(func(t framework.TestContext) {
				if !c.MinKubeVersion(33) {
					t.Skip("requires stable Kubernetes native sidecars")
				}
				ns := namespace.NewOrFail(t, namespace.Config{Prefix: "admin-termination", Inject: true})
				t.Cleanup(func() {
					if t.Failed() {
						testkube.DumpPods(t, t.CreateDirectoryOrFail("termination-failure"), ns.Name(), nil)
					}
				})
				control := fixture("control", ns.Name(), true, "TCP", "")
				if _, err := c.Kube().CoreV1().Pods(ns.Name()).Create(context.Background(), control, metav1.CreateOptions{}); err != nil {
					t.Fatal(err)
				}
				control = waitReady(t, c, ns.Name(), control.Name)
				for _, hook := range []string{"normal", "skipped", "failed"} {
					t.NewSubTest(hook).Run(func(t framework.TestContext) {
						p := fixture("termination-"+hook, ns.Name(), true, "UDS", "")
						p.Annotations["sidecar.istio.io/statsInclusionSuffixes"] = "downstream_rq_active"
						service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: p.Name, Namespace: ns.Name()}, Spec: corev1.ServiceSpec{Selector: map[string]string{"app": p.Name}, Ports: []corev1.ServicePort{{Name: "http", Port: 8080, TargetPort: intstr.FromInt32(8080)}}}}
						if _, err := c.Kube().CoreV1().Services(ns.Name()).Create(context.Background(), service, metav1.CreateOptions{}); err != nil {
							t.Fatal(err)
						}

						p.Finalizers = []string{"test.istio.io/observe-termination"}
						grace := int64(45)
						p.Spec.TerminationGracePeriodSeconds = &grace
						p.Annotations["proxy.istio.io/config"] = "terminationDrainDuration: 20s"
						// Record successful outbound traffic after SIGTERM in the application's termination message.
						outbound := "http://" + net.JoinHostPort(control.Status.PodIP, "8080") + "/delay/2"
						p.Spec.Containers[0].Command = []string{"sh", "-c", "trap 'curl --noproxy \"*\" --max-time 8 -fsS " + outbound + " >/dev/null && echo outbound-complete >/dev/termination-log; exit 0' TERM; while :; do sleep 1 & wait $!; done"}
						if hook != "normal" {
							// Test-only mutation of an already injected fixture. Product injection continues
							// to reject custom proxy hooks, and the agent sees the original resolved UDS env.
							injected, err := c.Kube().CoreV1().Pods(ns.Name()).Create(context.Background(), p, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
							if err != nil {
								t.Fatal(err)
							}
							p = injected
							p.ResourceVersion = ""
							p.UID = ""
							p.CreationTimestamp = metav1.Time{}
							p.Annotations["sidecar.istio.io/inject"] = "false"
							proxy := inject.FindSidecar(p)
							if hook == "skipped" {
								proxy.Lifecycle = nil
							} else {
								proxy.Lifecycle = &corev1.Lifecycle{PreStop: &corev1.LifecycleHandler{Exec: &corev1.ExecAction{Command: []string{"pilot-agent", "request", "POST", "does-not-exist"}}}}
							}
						}
						if _, err := c.Kube().CoreV1().Pods(ns.Name()).Create(context.Background(), p, metav1.CreateOptions{}); err != nil {
							t.Fatal(err)
						}
						t.Cleanup(func() {
							if t.Failed() {
								testkube.DumpPods(t, t.CreateDirectoryOrFail("pod-failure"), ns.Name(), nil)
							}
							_, _ = c.Kube().CoreV1().Pods(ns.Name()).Patch(context.Background(), p.Name, types.MergePatchType, []byte(`{"metadata":{"finalizers":[]}}`), metav1.PatchOptions{})
							_ = c.Kube().CoreV1().Pods(ns.Name()).Delete(context.Background(), p.Name, metav1.DeleteOptions{})
						})
						p = waitReady(t, c, ns.Name(), p.Name)
						incomingDone := make(chan error, 1)
						go func() {
							_, _, err := c.PodExecCommands(control.Name, ns.Name(), "app", []string{"curl", "--noproxy", "*", "--max-time", "15", "-fsS", "http://" + net.JoinHostPort(p.Status.PodIP, "8080") + "/delay/5"})
							incomingDone <- err
						}()
						retry.UntilSuccessOrFail(t, func() error {
							select {
							case err := <-incomingDone:
								return fmt.Errorf("inbound request completed before observation: %v", err)
							default:
							}
							out, err := c.EnvoyDoWithPort(context.Background(), p.Name, ns.Name(), "GET", "stats?filter=downstream_rq_active", 15000)
							if err != nil {
								return err
							}
							for _, line := range strings.Split(string(out), "\n") {
								parts := strings.SplitN(line, ":", 2)
								if len(parts) == 2 && strings.Contains(strings.ToLower(parts[0]), "inbound") {
									n, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
									if n > 0 {
										return nil
									}
								}
							}
							return fmt.Errorf("waiting for in-flight inbound request: %s", out)
						}, retry.Timeout(10*time.Second))
						if err := c.Kube().CoreV1().Pods(ns.Name()).Delete(context.Background(), p.Name, metav1.DeleteOptions{}); err != nil {
							t.Fatal(err)
						}
						select {
						case err := <-incomingDone:
							if err != nil {
								t.Fatalf("in-flight request failed: %v", err)
							}
						case <-time.After(20 * time.Second):
							t.Fatal("in-flight request did not finish")
						}
						retry.UntilSuccessOrFail(t, func() error {
							ended, err := c.Kube().CoreV1().Pods(ns.Name()).Get(context.Background(), p.Name, metav1.GetOptions{})
							if err != nil {
								return err
							}
							var appEnd, proxyEnd time.Time
							for _, s := range ended.Status.ContainerStatuses {
								if s.State.Terminated == nil {
									return fmt.Errorf("application %s still running", s.Name)
								}
								if s.State.Terminated.FinishedAt.Time.After(appEnd) {
									appEnd = s.State.Terminated.FinishedAt.Time
								}
								if s.Name == "app" && !strings.Contains(s.State.Terminated.Message, "outbound-complete") {
									return fmt.Errorf("outbound shutdown call did not complete: %s", s.State.Terminated.Message)
								}
							}
							for _, s := range ended.Status.InitContainerStatuses {
								if s.Name == "istio-proxy" && s.State.Terminated != nil {
									proxyEnd = s.State.Terminated.FinishedAt.Time
								}
							}
							if proxyEnd.IsZero() {
								return fmt.Errorf("proxy still running")
							}
							if proxyEnd.Before(appEnd) {
								t.Fatal("proxy terminated before applications")
							}
							if proxyEnd.Sub(appEnd) > 10*time.Second {
								t.Fatalf("proxy waited %v after applications, expected no extra 20s drain", proxyEnd.Sub(appEnd))
							}
							return nil
						}, retry.Timeout(35*time.Second))
					})
				}
			})
		}
	})
}
