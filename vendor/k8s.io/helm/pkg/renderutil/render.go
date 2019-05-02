/*
Copyright The Helm Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package renderutil

import (
	"fmt"

	"github.com/Masterminds/semver"

	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/engine"
	"k8s.io/helm/pkg/proto/hapi/chart"
	tversion "k8s.io/helm/pkg/version"
)

// Options are options for this simple local render
type Options struct {
	ReleaseOptions chartutil.ReleaseOptions
	KubeVersion    string
}

// Render chart templates locally and display the output.
// This does not require the Tiller. Any values that would normally be
// looked up or retrieved in-cluster will be faked locally. Additionally, none
// of the server-side testing of chart validity (e.g. whether an API is supported)
// is done.
//
// Note: a `nil` config passed here means "ignore the chart's default values";
// if you want the normal behavior of merging the defaults with the new config,
// you should pass `&chart.Config{Raw: "{}"},
func Render(c *chart.Chart, config *chart.Config, opts Options) (map[string]string, error) {
	if req, err := chartutil.LoadRequirements(c); err == nil {
		if err := CheckDependencies(c, req); err != nil {
			return nil, err
		}
	} else if err != chartutil.ErrRequirementsNotFound {
		return nil, fmt.Errorf("cannot load requirements: %v", err)
	}

	err := chartutil.ProcessRequirementsEnabled(c, config)
	if err != nil {
		return nil, err
	}
	err = chartutil.ProcessRequirementsImportValues(c)
	if err != nil {
		return nil, err
	}

	// Set up engine.
	renderer := engine.New()

	caps := &chartutil.Capabilities{
		APIVersions:   chartutil.DefaultVersionSet,
		KubeVersion:   chartutil.DefaultKubeVersion,
		TillerVersion: tversion.GetVersionProto(),
	}

	if opts.KubeVersion != "" {
		kv, verErr := semver.NewVersion(opts.KubeVersion)
		if verErr != nil {
			return nil, fmt.Errorf("could not parse a kubernetes version: %v", verErr)
		}
		caps.KubeVersion.Major = fmt.Sprint(kv.Major())
		caps.KubeVersion.Minor = fmt.Sprint(kv.Minor())
		caps.KubeVersion.GitVersion = fmt.Sprintf("v%d.%d.0", kv.Major(), kv.Minor())
	}

	vals, err := chartutil.ToRenderValuesCaps(c, config, opts.ReleaseOptions, caps)
	if err != nil {
		return nil, err
	}

	return renderer.Render(c, vals)
}
