// Copyright 2017 Istio Authors
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
	"fmt"
	"strings"

	"istio.io/istio/pilot/pkg/kube/inject"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/log"
	tutil "istio.io/istio/tests/e2e/tests/pilot/util"
)

type zone struct {
	*tutil.Environment
}

func (z *zone) String() string {
	return "zone"
}

func (z *zone) Setup() error {
	return nil
}

func (z *zone) Teardown() {
}

// Ensure that az look up succeeds for both mTLS and http services
func (z *zone) Run() error {

	funcs := make(map[string]func() tutil.Status)
	for _, app := range []string{"a", "b", "d"} {
		name := fmt.Sprintf("Checking log of %s for availability zone", app)
		funcs[name] = (func(app string) func() tutil.Status {
			return func() tutil.Status {
				if len(z.Apps[app]) == 0 {
					return fmt.Errorf("missing pods for app %q", app)
				}
				pod := z.Apps[app][0]
				container := inject.ProxyContainerName
				ns := z.Config.Namespace

				util.CopyPodFiles(container, pod, ns, model.ConfigPathDir, z.Config.CoreFilesDir+"/"+pod+"."+ns)
				logs := util.FetchLogs(z.KubeClient, pod, ns, container)

				if strings.Contains(logs, "Proxy availability zone:") {
					log.Info("Found availability zone look up success in logs")
					return nil
				}
				return tutil.ErrAgain
			}
		})(app)
	}
	return tutil.Parallel(funcs)
}
