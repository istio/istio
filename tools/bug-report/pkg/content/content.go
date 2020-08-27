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
	"fmt"
	"strings"

	"istio.io/istio/tools/bug-report/pkg/kubectlcmd"
	"istio.io/pkg/log"
)

const (
	coredumpDir = "/var/lib/istio"
)

var (
	istiodURLs = []string{
		"debug/configz",
		"debug/endpointz",
		"debug/adsz",
		"debug/authenticationz",
		"metrics",
	}
)

// Params contains parameters for running a kubectl fetch command.
type Params struct {
	DryRun    bool
	Verbose   bool
	Namespace string
	Pod       string
	Container string
}

func retMap(filename, text string, err error) (map[string]string, error) {
	if err != nil {
		return nil, err
	}
	return map[string]string{
		filename: text,
	}, nil

}

// GetK8sResources returns all k8s cluster resources.
func GetK8sResources(params *Params) (map[string]string, error) {
	out, err := kubectlcmd.RunCmd("get --all-namespaces "+
		"all,jobs,ingresses,endpoints,customresourcedefinitions,configmaps,events "+
		"-o yaml", "", params.DryRun)
	return retMap("k8s-resources", out, err)
}

// GetSecrets returns all k8s secrets. If full is set, the secret contents are also returned.
func GetSecrets(params *Params) (map[string]string, error) {
	cmdStr := "get secrets --all-namespaces"
	if params.Verbose {
		cmdStr += " -o yaml"
	}
	out, err := kubectlcmd.RunCmd(cmdStr, "", params.DryRun)
	return retMap("secrets", out, err)
}

// GetCRs returns CR contents for all CRDs in the cluster.
func GetCRs(params *Params) (map[string]string, error) {
	crds, err := getCRDList(params)
	if err != nil {
		return nil, err
	}
	out, err := kubectlcmd.RunCmd("get --all-namespaces "+strings.Join(crds, ",")+" -o yaml", "", params.DryRun)
	return retMap("crs", out, err)
}

// GetClusterInfo returns the cluster info.
func GetClusterInfo(params *Params) (map[string]string, error) {
	out, err := kubectlcmd.RunCmd("cluster-info dump", "", params.DryRun)
	return retMap("cluster-info", out, err)
}

// GetDescribePods returns describe pods for istioNamespace.
func GetDescribePods(params *Params) (map[string]string, error) {
	if params.Namespace == "" {
		return nil, fmt.Errorf("getDescribePods requires the Istio namespace")
	}
	out, err := kubectlcmd.RunCmd("describe pods", params.Namespace, params.DryRun)
	return retMap("describe-pods", out, err)
}

// GetEvents returns events for all namespaces.
func GetEvents(params *Params) (map[string]string, error) {
	out, err := kubectlcmd.RunCmd("get events --all-namespaces -o wide", "", params.DryRun)
	return retMap("events", out, err)
}

// GetIstiodInfo returns internal Istiod debug info.
func GetIstiodInfo(params *Params) (map[string]string, error) {
	if params.Namespace == "" || params.Pod == "" {
		return nil, fmt.Errorf("getIstiodInfo requires namespace and pod")
	}
	ret := make(map[string]string)
	for _, url := range istiodURLs {
		out, err := kubectlcmd.Exec(params.Namespace, params.Pod, "discovery", params.DryRun, "curl", `http://localhost:8080/`+url+``)
		if err != nil {
			return nil, err
		}

		ret[url] = out
	}
	return ret, nil
}

// GetCoredumps returns coredumps for the given namespace/pod/container.
func GetCoredumps(params *Params) (map[string]string, error) {
	if params.Namespace == "" || params.Pod == "" {
		return nil, fmt.Errorf("getCoredumps requires namespace and pod")
	}
	cds, err := getCoredumpList(params)
	if err != nil {
		return nil, err
	}

	ret := make(map[string]string)
	log.Infof("%s/%s/%s has %d coredumps", params.Namespace, params.Pod, params.Container, len(cds))
	for idx, cd := range cds {
		outStr, err := kubectlcmd.Cat(params.Namespace, params.Pod, params.Container, cd, params.DryRun)
		if err != nil {
			log.Warna(err)
			continue
		}
		ret[fmt.Sprint(idx)+".core"] = outStr
	}
	return ret, nil
}

func getCoredumpList(params *Params) ([]string, error) {
	out, err := kubectlcmd.Exec(params.Namespace, params.Pod, params.Container, params.DryRun, "find", coredumpDir, "-name", "core.*")
	if err != nil {
		return nil, err
	}
	var cds []string
	for _, cd := range strings.Split(out, "\n") {
		if strings.TrimSpace(cd) != "" {
			cds = append(cds, cd)
		}
	}
	return cds, nil
}

func getCRDList(params *Params) ([]string, error) {
	crdStr, err := kubectlcmd.RunCmd("get customresourcedefinitions --no-headers", "", params.DryRun)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, crd := range strings.Split(crdStr, "\n") {
		if strings.TrimSpace(crd) == "" {
			continue
		}
		out = append(out, strings.Split(crd, " ")[0])
	}
	return out, nil
}
