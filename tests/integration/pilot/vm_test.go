// +build integ
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

package pilot

import (
	"strings"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/label"
)

func GetAdditionVMImages() []string {
	// Note - bionic is not here as its the default
	return []string{"app_sidecar_ubuntu_xenial", "app_sidecar_ubuntu_focal",
		"app_sidecar_debian_9", "app_sidecar_debian_10", "app_sidecar_centos_8"}
}

func TestVmOSPost(t *testing.T) {
	framework.
		NewTest(t).
		Features("traffic.reachability").
		Label(label.Postsubmit).
		Run(func(ctx framework.TestContext) {
			b := echoboot.NewBuilder(ctx)
			images := GetAdditionVMImages()
			instances := make([]echo.Instance, len(images))
			for i, image := range images {
				b = b.With(&instances[i], echo.Config{
					Service:    "vm-" + strings.ReplaceAll(image, "_", "-"),
					Namespace:  apps.namespace,
					Ports:      echoPorts,
					DeployAsVM: true,
					VMImage:    image,
					Subsets:    []echo.SubsetConfig{{}},
				})
			}
			b.BuildOrFail(ctx)

			for i, image := range images {
				i, image := i, image
				ctx.NewSubTest(image).RunParallel(func(ctx framework.TestContext) {
					for _, tt := range vmTestCases(instances[i]) {
						ExecuteTrafficTest(ctx, tt)
					}
				})
			}
		})
}
