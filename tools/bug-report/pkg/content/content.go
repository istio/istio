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

package content

import (
	"strings"

	"istio.io/istio/tools/bug-report/pkg/kubectlcmd"
)

const (
	coredumpDir = "/var/lib/istio"
)

func GetCoredumps(namespace, pod, container string, dryRun bool) ([]string, error) {
	cds, err := getCoredumpList(namespace, pod, container, dryRun)
	if err != nil {
		return nil, err
	}

	var out []string
	for _, cd := range cds {
		outStr, err := kubectlcmd.Cat(namespace, pod, container, cd, dryRun)
		if err != nil {
			return nil, err
		}
		out = append(out, outStr)
	}
	return out, nil
}

func getCoredumpList(namespace, pod, container string, dryRun bool) ([]string, error) {
	out, err := kubectlcmd.Exec(namespace, pod, container, `find `+coredumpDir+` -name 'core.*'`, dryRun)
	if err != nil {
		return nil, err
	}
	return strings.Split(out, "\n"), nil
}
