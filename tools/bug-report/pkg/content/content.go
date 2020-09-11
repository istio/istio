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

	"istio.io/istio/pkg/kube"
	"istio.io/istio/tools/bug-report/pkg/kubectlcmd"
	"istio.io/pkg/log"
)

const (
	coredumpDir = "/var/lib/istio"
)

var (
	istiodURLs = []string{
		"debug/adsz",
		"debug/cdsz",
		"debug/syncz",
		"debug/registryz",
		"debug/endpointz",
		"debug/endpointShardz",
		"debug/configz",
		"debug/resourcesz",
		"debug/authorizationz",
		"debug/push_status",
		"debug/inject",
	}
)

// Params contains parameters for running a kubectl fetch command.
type Params struct {
	Client    kube.ExtendedClient
	DryRun    bool
	Verbose   bool
	Namespace string
	Pod       string
	Container string
}

func (p *Params) SetClient(client kube.ExtendedClient) *Params {
	out := *p
	out.Client = client
	return &out
}

func (p *Params) SetDryRun(dryRun bool) *Params {
	out := *p
	out.DryRun = dryRun
	return &out
}

func (p *Params) SetVerbose(verbose bool) *Params {
	out := *p
	out.Verbose = verbose
	return &out
}

func (p *Params) SetNamespace(namespace string) *Params {
	out := *p
	out.Namespace = namespace
	return &out
}

func (p *Params) SetPod(pod string) *Params {
	out := *p
	out.Pod = pod
	return &out
}

func (p *Params) SetContainer(container string) *Params {
	out := *p
	out.Container = container
	return &out
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
func GetK8sResources(p *Params) (map[string]string, error) {
	out, err := kubectlcmd.RunCmd("get --all-namespaces "+
		"all,jobs,ingresses,endpoints,customresourcedefinitions,configmaps,events "+
		"-o yaml", "", p.DryRun)
	return retMap("k8s-resources", out, err)
}

// GetSecrets returns all k8s secrets. If full is set, the secret contents are also returned.
func GetSecrets(p *Params) (map[string]string, error) {
	cmdStr := "get secrets --all-namespaces"
	if p.Verbose {
		cmdStr += " -o yaml"
	}
	out, err := kubectlcmd.RunCmd(cmdStr, "", p.DryRun)
	return retMap("secrets", out, err)
}

// GetCRs returns CR contents for all CRDs in the cluster.
func GetCRs(p *Params) (map[string]string, error) {
	crds, err := getCRDList(p)
	if err != nil {
		return nil, err
	}
	out, err := kubectlcmd.RunCmd("get --all-namespaces "+strings.Join(crds, ",")+" -o yaml", "", p.DryRun)
	return retMap("crs", out, err)
}

// GetClusterInfo returns the cluster info.
func GetClusterInfo(p *Params) (map[string]string, error) {
	out, err := kubectlcmd.RunCmd("cluster-info dump", "", p.DryRun)
	return retMap("cluster-info", out, err)
}

// GetDescribePods returns describe pods for istioNamespace.
func GetDescribePods(p *Params) (map[string]string, error) {
	if p.Namespace == "" {
		return nil, fmt.Errorf("getDescribePods requires the Istio namespace")
	}
	out, err := kubectlcmd.RunCmd("describe pods", p.Namespace, p.DryRun)
	return retMap("describe-pods", out, err)
}

// GetEvents returns events for all namespaces.
func GetEvents(params *Params) (map[string]string, error) {
	out, err := kubectlcmd.RunCmd("get events --all-namespaces -o wide", "", params.DryRun)
	return retMap("events", out, err)
}

// GetIstiodInfo returns internal Istiod debug info.
func GetIstiodInfo(p *Params) (map[string]string, error) {
	if p.Namespace == "" || p.Pod == "" {
		return nil, fmt.Errorf("getIstiodInfo requires namespace and pod")
	}
	ret := make(map[string]string)
	for _, url := range istiodURLs {

		out, err := kubectlcmd.Exec(p.Client, p.Namespace, p.Pod, "discovery", fmt.Sprintf(`curl localhost:8080/%s`, url), p.DryRun)
		if err != nil {
			return nil, err
		}

		ret[url] = out
	}
	return ret, nil
}

// GetCoredumps returns coredumps for the given namespace/pod/container.
func GetCoredumps(p *Params) (map[string]string, error) {
	if p.Namespace == "" || p.Pod == "" {
		return nil, fmt.Errorf("getCoredumps requires namespace and pod")
	}
	cds, err := getCoredumpList(p)
	if err != nil {
		return nil, err
	}

	ret := make(map[string]string)
	log.Infof("%s/%s/%s has %d coredumps", p.Namespace, p.Pod, p.Container, len(cds))
	for idx, cd := range cds {
		outStr, err := kubectlcmd.Cat(p.Client, p.Namespace, p.Pod, p.Container, cd, p.DryRun)
		if err != nil {
			log.Warna(err)
			continue
		}
		ret[fmt.Sprint(idx)+".core"] = outStr
	}
	return ret, nil
}

func getCoredumpList(p *Params) ([]string, error) {
	out, err := kubectlcmd.Exec(p.Client, p.Namespace, p.Pod, p.Container, fmt.Sprintf("find %s -name core.*", coredumpDir), p.DryRun)
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
