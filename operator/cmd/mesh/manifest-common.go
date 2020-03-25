// Copyright 2019 Istio Authors
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

	"istio.io/istio/operator/pkg/helm"
	"istio.io/istio/operator/pkg/tpath"
	"istio.io/istio/operator/pkg/validate"
)

var (
	ignoreStdErrList = []string{
		// TODO: remove when https://github.com/kubernetes/kubernetes/issues/82154 is fixed.
		"Warning: kubectl apply should be used on resource created by either kubectl create --save-config or kubectl apply",
	}
)

func ignoreError(stderr string) bool {
	trimmedStdErr := strings.TrimSpace(stderr)
	for _, ignore := range ignoreStdErrList {
		if strings.HasPrefix(trimmedStdErr, ignore) {
			return true
		}
	}
	return trimmedStdErr == ""
}

// yamlFromSetFlags takes a slice of --set flag key-value pairs and returns a YAML tree representation.
// If force is set, validation errors cause warning messages to be written to logger rather than causing error.
func yamlFromSetFlags(setOverlay []string, force bool, l *Logger) (string, error) {
	out, err := tpath.MakeTreeFromSetList(setOverlay)
	if err != nil {
		return "", fmt.Errorf("failed to generate tree from the set overlay, error: %v", err)
	}
	if err := validate.ValidIOPYAML(out); err != nil {
		if !force {
			return "", fmt.Errorf("validation errors (use --force to override): \n%s", err)
		}
		l.logAndErrorf("Validation errors (continuing because of --force):\n%s", err)
	}
	return out, nil
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
