//go:build linux
// +build linux

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

package repair

import (
	"fmt"

	"istio.io/istio/cni/pkg/plugin"
	corev1 "k8s.io/api/core/v1"
)

// redirectRunningPod dynamically enters the provided pod, that is already running, and programs it's networking configuration.
func redirectRunningPod(pod *corev1.Pod, netns string) error {
	pi := plugin.ExtractPodInfo(pod)
	redirect, err := plugin.NewRedirect(pi)
	if err != nil {
		return fmt.Errorf("setup redirect: %v", err)
	}
	rulesMgr := plugin.IptablesInterceptRuleMgr()
	if err := rulesMgr.Program(pod.Name, netns, redirect); err != nil {
		return fmt.Errorf("program redirection: %v", err)
	}
	return nil
}
