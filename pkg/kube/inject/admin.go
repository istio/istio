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

package inject

import (
	"fmt"
	"path"
	"reflect"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"istio.io/istio/pkg/envoy/admin"
)

func configureAdmin(pod *corev1.Pod, req InjectionParameters) error {
	proxy := FindSidecar(pod)
	if proxy == nil {
		return nil
	}
	transport, err := admin.Resolve(req.pod.Annotations, req.proxyConfig.ProxyMetadata)
	if err != nil {
		return err
	}
	native := false
	for _, c := range pod.Spec.InitContainers {
		if c.Name == ProxyContainerName && c.RestartPolicy != nil && *c.RestartPolicy == corev1.ContainerRestartPolicyAlways {
			native = true
		}
	}
	if transport == admin.UDS {
		unsupported := func(what string) error {
			return fmt.Errorf("UDS administration does not support %s; remove the override or select TCP", what)
		}
		if !native {
			return fmt.Errorf("UDS administration requires a Kubernetes native sidecar; enable sidecar.istio.io/nativeSidecar or select TCP")
		}
		if req.proxyConfig.CustomConfigFile != "" || req.proxyConfig.ProxyBootstrapTemplatePath != "" {
			return unsupported("custom bootstrap files or templates")
		}
		for _, key := range []string{"sidecar.istio.io/bootstrapOverride", "inject.istio.io/templates"} {
			if _, ok := req.pod.Annotations[key]; ok {
				return unsupported(key)
			}
		}
		if req.valuesConfig.asStruct.GetGlobal().GetProxy().GetLifecycle() != nil {
			return unsupported("custom proxy lifecycle hooks")
		}
		for _, c := range append(append([]corev1.Container{}, req.pod.Spec.Containers...), req.pod.Spec.InitContainers...) {
			if c.Name == ProxyContainerName && c.Lifecycle != nil {
				return unsupported("custom proxy lifecycle hooks")
			}
		}
		for _, e := range proxy.Env {
			if e.Name == "ISTIO_BOOTSTRAP_OVERRIDE" || e.Name == "ISTIO_BOOTSTRAP" {
				return unsupported("bootstrap overrides")
			}
		}
		for _, name := range selectTemplates(req) {
			if name != "sidecar" {
				return unsupported("custom injection templates")
			}
		}
		if len(proxy.Command) != 0 {
			return unsupported("custom proxy commands")
		}
		for _, arg := range proxy.Args {
			if arg == "--templateFile" || strings.HasPrefix(arg, "--templateFile=") {
				return unsupported("custom bootstrap templates")
			}
		}
		if proxy.Lifecycle != nil {
			if hook := proxy.Lifecycle.PostStart; hook != nil && !reflect.DeepEqual(hook, &corev1.LifecycleHandler{Exec: &corev1.ExecAction{Command: []string{"pilot-agent", "wait"}}}) {
				return unsupported("custom proxy postStart hooks")
			}
			if hook := proxy.Lifecycle.PreStop; hook != nil {
				if hook.Exec == nil || len(hook.Exec.Command) != 5 {
					return unsupported("custom proxy preStop hooks")
				}
				cmd := hook.Exec.Command
				if cmd[0] != "pilot-agent" || cmd[1] != "request" || !strings.HasPrefix(cmd[2], "--debug-port=") || cmd[3] != "POST" || cmd[4] != "drain" {
					return unsupported("custom proxy preStop hooks")
				}
			}
		}
		// Find every volume which can contain the private socket, including nested mounts.
		volumes := map[string]bool{}
		for _, m := range proxy.VolumeMounts {
			p := path.Clean(m.MountPath)
			if p == admin.SocketPath || p == "/" || strings.HasPrefix(admin.SocketPath, p+"/") {
				if m.ReadOnly || m.SubPath != "" || m.SubPathExpr != "" {
					return unsupported("read-only or subpath admin mounts")
				}
				volumes[m.Name] = true
			}
		}
		if len(volumes) == 0 {
			return unsupported("a missing writable admin volume")
		}
		for _, c := range append(append([]corev1.Container{}, pod.Spec.Containers...), pod.Spec.InitContainers...) {
			if c.Name == ProxyContainerName {
				continue
			}
			for _, m := range c.VolumeMounts {
				if volumes[m.Name] {
					return unsupported("application access to the admin socket volume " + m.Name)
				}
			}
		}
		for _, c := range pod.Spec.EphemeralContainers {
			for _, m := range c.VolumeMounts {
				if volumes[m.Name] {
					return unsupported("ephemeral container access to the admin socket volume " + m.Name)
				}
			}
		}
		if proxy.Lifecycle == nil {
			proxy.Lifecycle = &corev1.Lifecycle{}
		}
		proxy.Lifecycle.PreStop = &corev1.LifecycleHandler{Exec: &corev1.ExecAction{Command: []string{
			"pilot-agent", "request", "POST", "drain_listeners?inboundonly&graceful&skip_exit",
		}}}
	}
	envs := make([]corev1.EnvVar, 0, len(proxy.Env)+2)
	for _, e := range proxy.Env {
		if e.Name != admin.Env && e.Name != admin.NativeEnv {
			envs = append(envs, e)
		}
	}
	proxy.Env = append(envs, corev1.EnvVar{Name: admin.Env, Value: string(transport)}, corev1.EnvVar{Name: admin.NativeEnv, Value: strconv.FormatBool(native)})
	return nil
}
