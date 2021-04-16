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

package cluster

import (
	"context"
	"fmt"
	"strings"

	"github.com/kylelemons/godebug/pretty"

	"istio.io/istio/mdp/controller/pkg/name"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/proxy"
	"istio.io/istio/tools/bug-report/pkg/common"
	"istio.io/pkg/log"
)

func DumpRevisionsAndVersions(resources *Resources) {
	text := ""
	revisions := getIstioRevisions(resources)
	istioVersions, proxyVersions := getIstioVersions(revisions)
	text += "The following Istio control plane revisions/versions were found in the cluster:\n"
	for rev, ver := range istioVersions {
		text += fmt.Sprintf("Revision %s:\n%s\n\n", rev, ver)
	}
	text += "The following proxy revisions/versions were found in the cluster:\n"
	for rev, ver := range proxyVersions {
		text += fmt.Sprintf("Revision %s: Versions {%s}\n", rev, strings.Join(ver, ", "))
	}
	common.LogAndPrintf(text)
}

// getIstioRevisions returns a slice with all Istio revisions detected in the cluster.
func getIstioRevisions(resources *Resources) []string {
	revMap := make(map[string]struct{})
	for _, podLabels := range resources.Labels {
		for label, value := range podLabels {
			if label == name.IstioRevisionLabel {
				revMap[value] = struct{}{}
			}
		}
	}
	var out []string
	for k := range revMap {
		out = append(out, k)
	}
	return out
}

// getIstioVersions returns a mapping of revision to aggregated version string for Istio components and revision to
// slice of versions for proxies. Any errors are embedded in the revision strings.
func getIstioVersions(revisions []string) (map[string]string, map[string][]string) {
	istioVersions := make(map[string]string)
	proxyVersionsMap := make(map[string]map[string]struct{})
	proxyVersions := make(map[string][]string)
	for _, revision := range revisions {
		istioVersions[revision] = getIstioVersion(revision)
		proxyInfo, err := proxy.GetProxyInfo("", "", revision, name.IstioSystemNamespace)
		if err != nil {
			log.Error(err)
			continue
		}
		for _, pi := range *proxyInfo {
			if proxyVersionsMap[revision] == nil {
				proxyVersionsMap[revision] = make(map[string]struct{})
			}
			proxyVersionsMap[revision][pi.IstioVersion] = struct{}{}
		}
	}
	for revision, vmap := range proxyVersionsMap {
		for version := range vmap {
			proxyVersions[revision] = append(proxyVersions[revision], version)
		}
	}
	return istioVersions, proxyVersions
}

func getIstioVersion(revision string) string {
	kubeClient, err := kube.NewExtendedClient(kube.BuildClientCmd("", ""), revision)
	if err != nil {
		return err.Error()
	}

	versions, err := kubeClient.GetIstioVersions(context.TODO(), name.IstioSystemNamespace)
	if err != nil {
		return err.Error()
	}
	return pretty.Sprint(versions)
}
