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

package gendeployment

import (
	"bytes"
	"fmt"
	"strings"

	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/engine"
	"k8s.io/helm/pkg/proto/hapi/chart"
	"k8s.io/helm/pkg/timeconv"
)

// TODO: add tests based on golden files. Need to fix up helm charts first, since they're not correct atm.
// Today they spit out multiple auth deployments, we need to fix that then we can build golden outputs.

func yamlFromInstallation(values, namespace, helmChartDirectory string) (string, error) {
	c, err := chartutil.Load(helmChartDirectory)
	if err != nil {
		return "", err
	}

	config := &chart.Config{Raw: values, Values: map[string]*chart.Value{}}
	options := chartutil.ReleaseOptions{
		Name:      "istio",
		Time:      timeconv.Now(),
		Namespace: namespace,
	}

	vals, err := chartutil.ToRenderValues(c, config, options)
	if err != nil {
		return "", err
	}

	files, err := engine.New().Render(c, vals)
	if err != nil {
		return "", err
	}

	out := &bytes.Buffer{}
	for name, data := range files {
		if len(strings.TrimSpace(data)) == 0 {
			continue
		}
		if _, err = fmt.Fprintf(out, "---\n# Source: %q\n", name); err != nil {
			return "", err
		}
		if _, err = fmt.Fprintln(out, data); err != nil {
			return "", err
		}
	}
	return out.String(), nil
}
