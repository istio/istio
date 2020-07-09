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

package mesh

import (
	"fmt"
	"strings"

	"github.com/ghodss/yaml"

	"istio.io/api/operator/v1alpha1"
	"istio.io/istio/operator/pkg/helm"
	"istio.io/istio/operator/pkg/tpath"
	"istio.io/istio/operator/pkg/util"
)

// makeTreeFromSetList creates a YAML tree from a string slice containing key-value pairs in the format key=value.
func makeTreeFromSetList(setOverlay []string) (string, error) {
	if len(setOverlay) == 0 {
		return "", nil
	}
	tree := make(map[string]interface{})
	for _, kv := range setOverlay {
		kvv := strings.Split(kv, "=")
		if len(kvv) != 2 {
			return "", fmt.Errorf("bad argument %s: expect format key=value", kv)
		}
		k := kvv[0]
		v := util.ParseValue(kvv[1])
		if err := tpath.WriteNode(tree, util.PathFromString(k), v); err != nil {
			return "", err
		}
		// To make errors more user friendly, test the path and error out immediately if we cannot unmarshal.
		testTree, err := yaml.Marshal(tree)
		if err != nil {
			return "", err
		}
		iops := &v1alpha1.IstioOperatorSpec{}
		if err := util.UnmarshalWithJSONPB(string(testTree), iops, false); err != nil {
			return "", fmt.Errorf("bad path=value: %s", kv)
		}
	}
	out, err := yaml.Marshal(tree)
	if err != nil {
		return "", err
	}
	return tpath.AddSpecRoot(string(out))
}

// fetchExtractInstallPackageHTTP downloads installation tar from the URL specified and extracts it to a local
// filesystem dir. If successful, it returns the path to the filesystem path where the charts were extracted.
func fetchExtractInstallPackageHTTP(releaseTarURL string) (string, error) {
	uf := helm.NewURLFetcher(releaseTarURL, "")
	if err := uf.Fetch(); err != nil {
		return "", err
	}
	return uf.DestDir(), nil
}

// --manifests is an alias for --set installPackagePath=
// --revision is an alias for --set revision=
func applyFlagAliases(flags []string, manifestsPath, revision string) []string {
	if manifestsPath != "" {
		flags = append(flags, fmt.Sprintf("installPackagePath=%s", manifestsPath))
	}
	if revision != "" {
		flags = append(flags, fmt.Sprintf("revision=%s", revision))
	}
	return flags
}
