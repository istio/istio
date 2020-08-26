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

// GetK8sResources returns all k8s cluster resources.
func GetK8sResources(dryRun bool) (string, error) {
	return kubectlcmd.RunCmd("get --all-namespaces "+
		"all,jobs,ingresses,endpoints,customresourcedefinitions,configmaps,events "+
		"-o yaml", "", dryRun)
}

// GetSecrets returns all k8s secrets. If full is set, the secret contents are also returned.
func GetSecrets(full, dryRun bool) (string, error) {
	cmdStr := "get secrets --all-namespaces"
	if full {
		cmdStr += " -o yaml"
	}
	return kubectlcmd.RunCmd(cmdStr, "", dryRun)
}

// GetCRs returns CR contents for all CRDs in the cluster.
func GetCRs(dryRun bool) (string, error) {
	crds, err := getCRDList(dryRun)
	if err != nil {
		return "", err
	}
	return kubectlcmd.RunCmd("get --all-namespaces "+strings.Join(crds, ",")+" -o yaml", "", dryRun)
}

// GetClusterInfo returns the cluster info.
func GetClusterInfo(dryRun bool) (string, error) {
	return kubectlcmd.RunCmd("cluster-info dump", "", dryRun)
}

// GetDescribePods returns describe pods for istioNamespace.
func GetDescribePods(istioNamespace string, dryRun bool) (string, error) {
	return kubectlcmd.RunCmd("describe pods", istioNamespace, dryRun)
}

// GetEvents returns events for all namespaces.
func GetEvents(dryRun bool) (string, error) {
	return kubectlcmd.RunCmd("get events --all-namespaces -o wide", "", dryRun)
}

// GetIstiodInfo returns internal Istiod debug info.
func GetIstiodInfo(namespace, pod string, dryRun bool) (string, error) {
	var sb strings.Builder
	for _, url := range istiodURLs {
		out, err := kubectlcmd.Exec(namespace, pod, "discovery", `curl "http://localhost:8080/`+url+`"`, dryRun)
		if err != nil {
			return "", err
		}
		sb.WriteString("============== " + url + " ==============\n\n")
		sb.WriteString(out)
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

// GetCoredumps returns coredumps for the given namespace/pod/container.
func GetCoredumps(namespace, pod, container string, dryRun bool) ([]string, error) {
	log.Infof("Getting coredumps for %s/%s/%s...", namespace, pod, container)
	cds, err := getCoredumpList(namespace, pod, container, dryRun)
	if err != nil {
		return nil, err
	}

	var out []string
	log.Infof("%s/%s/%s has %d coredumps", namespace, pod, container, len(cds))
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

func getCRDList(dryRun bool) ([]string, error) {
	crdStr, err := kubectlcmd.RunCmd("get customresourcedefinitions --no-headers", "", dryRun)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, crd := range strings.Split(crdStr, "\n") {
		out = append(out, strings.Split(crd, " ")[0])
	}
	return out, nil
}
