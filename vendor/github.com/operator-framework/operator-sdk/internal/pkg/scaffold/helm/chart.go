// Copyright 2018 The Operator-SDK Authors
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

package helm

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold"

	"github.com/iancoleman/strcase"
	log "github.com/sirupsen/logrus"
	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/downloader"
	"k8s.io/helm/pkg/getter"
	"k8s.io/helm/pkg/helm/environment"
	"k8s.io/helm/pkg/helm/helmpath"
	"k8s.io/helm/pkg/proto/hapi/chart"
	"k8s.io/helm/pkg/repo"
)

const (

	// HelmChartsDir is the relative directory within an SDK project where Helm
	// charts are stored.
	HelmChartsDir string = "helm-charts"

	// DefaultAPIVersion is the Kubernetes CRD API Version used for fetched
	// charts when the --api-version flag is not specified
	DefaultAPIVersion string = "charts.helm.k8s.io/v1alpha1"
)

// CreateChartOptions is used to configure how a Helm chart is scaffolded
// for a new Helm operator project.
type CreateChartOptions struct {
	// ResourceAPIVersion defines the Kubernetes GroupVersion to be associated
	// with the created chart.
	ResourceAPIVersion string

	// ResourceKind defines the Kubernetes Kind to be associated with the
	// created chart.
	ResourceKind string

	// Chart is a chart reference for a local or remote chart.
	Chart string

	// Repo is a URL to a custom chart repository.
	Repo string

	// Version is the version of the chart to fetch.
	Version string
}

// CreateChart scaffolds a new helm chart for the project rooted in projectDir
// based on the passed opts.
//
// It returns a scaffold.Resource that can be used by the caller to create
// other related files. opts.ResourceAPIVersion and opts.ResourceKind are
// used to create the resource and must be specified if opts.Chart is empty.
//
// If opts.Chart is not empty, opts.ResourceAPIVersion and opts.Kind can be
// left unset: opts.ResourceAPIVersion defaults to "charts.helm.k8s.io/v1alpha1"
// and opts.ResourceKind is deduced from the specified opts.Chart.
//
// CreateChart also returns a chart.Chart that references the newly created
// chart.
//
// If opts.Chart is empty, CreateChart scaffolds the default chart from helm's
// default template.
//
// If opts.Chart is a local file, CreateChart verifies that it is a valid helm
// chart archive and unpacks it into the project's helm charts directory.
//
// If opts.Chart is a local directory, CreateChart verifies that it is a valid
// helm chart directory and copies it into the project's helm charts directory.
//
// For any other value of opts.Chart, CreateChart attempts to fetch the helm chart
// from a remote repository.
//
// If opts.Repo is not specified, the following chart reference formats are supported:
//
//   - <repoName>/<chartName>: Fetch the helm chart named chartName from the helm
//                             chart repository named repoName, as specified in the
//                             $HELM_HOME/repositories/repositories.yaml file.
//
//   - <url>: Fetch the helm chart archive at the specified URL.
//
// If opts.Repo is specified, only one chart reference format is supported:
//
//   - <chartName>: Fetch the helm chart named chartName in the helm chart repository
//                  specified by opts.Repo
//
// If opts.Version is not set, CreateChart will fetch the latest available version of
// the helm chart. Otherwise, CreateChart will fetch the specified version.
// opts.Version is not used when opts.Chart itself refers to a specific version, for
// example when it is a local path or a URL.
//
// CreateChart returns an error if an error occurs creating the scaffold.Resource or
// creating the chart.
func CreateChart(projectDir string, opts CreateChartOptions) (*scaffold.Resource, *chart.Chart, error) {
	chartsDir := filepath.Join(projectDir, HelmChartsDir)
	err := os.MkdirAll(chartsDir, 0755)
	if err != nil {
		return nil, nil, err
	}

	var (
		r *scaffold.Resource
		c *chart.Chart
	)

	// If we don't have a helm chart reference, scaffold the default chart
	// from Helm's default template. Otherwise, fetch it.
	if len(opts.Chart) == 0 {
		r, c, err = scaffoldChart(chartsDir, opts.ResourceAPIVersion, opts.ResourceKind)
	} else {
		r, c, err = fetchChart(chartsDir, opts)
	}
	if err != nil {
		return nil, nil, err
	}
	log.Infof("Created %s/%s/", HelmChartsDir, c.GetMetadata().GetName())
	return r, c, nil
}

func scaffoldChart(destDir, apiVersion, kind string) (*scaffold.Resource, *chart.Chart, error) {
	r, err := scaffold.NewResource(apiVersion, kind)
	if err != nil {
		return nil, nil, err
	}

	chartfile := &chart.Metadata{
		// Many helm charts use hyphenated names, but we chose not to because
		// of the issues related to how hyphens are interpreted in templates.
		// See https://github.com/helm/helm/issues/2192
		Name:        r.LowerKind,
		Description: "A Helm chart for Kubernetes",
		Version:     "0.1.0",
		AppVersion:  "1.0",
		ApiVersion:  chartutil.ApiVersionV1,
	}
	chartPath, err := chartutil.Create(chartfile, destDir)
	if err != nil {
		return nil, nil, err
	}

	chart, err := chartutil.LoadDir(chartPath)
	if err != nil {
		return nil, nil, err
	}
	return r, chart, nil
}

func fetchChart(destDir string, opts CreateChartOptions) (*scaffold.Resource, *chart.Chart, error) {
	var (
		stat  os.FileInfo
		chart *chart.Chart
		err   error
	)

	if stat, err = os.Stat(opts.Chart); err == nil {
		chart, err = createChartFromDisk(destDir, opts.Chart, stat.IsDir())
	} else {
		chart, err = createChartFromRemote(destDir, opts)
	}
	if err != nil {
		return nil, nil, err
	}

	chartName := chart.GetMetadata().GetName()
	if len(opts.ResourceAPIVersion) == 0 {
		opts.ResourceAPIVersion = DefaultAPIVersion
	}
	if len(opts.ResourceKind) == 0 {
		opts.ResourceKind = strcase.ToCamel(chartName)
	}

	r, err := scaffold.NewResource(opts.ResourceAPIVersion, opts.ResourceKind)
	if err != nil {
		return nil, nil, err
	}
	return r, chart, nil
}

func createChartFromDisk(destDir, source string, isDir bool) (*chart.Chart, error) {
	var (
		chart *chart.Chart
		err   error
	)

	// If source is a file or directory, attempt to load it
	if isDir {
		chart, err = chartutil.LoadDir(source)
	} else {
		chart, err = chartutil.LoadFile(source)
	}
	if err != nil {
		return nil, err
	}

	// Save it into our project's helm-charts directory.
	if err := chartutil.SaveDir(chart, destDir); err != nil {
		return nil, err
	}
	return chart, nil
}

func createChartFromRemote(destDir string, opts CreateChartOptions) (*chart.Chart, error) {
	helmHome, ok := os.LookupEnv(environment.HomeEnvVar)
	if !ok {
		helmHome = environment.DefaultHelmHome
	}
	getters := getter.All(environment.EnvSettings{})
	c := downloader.ChartDownloader{
		HelmHome: helmpath.Home(helmHome),
		Out:      os.Stderr,
		Getters:  getters,
	}

	if opts.Repo != "" {
		chartURL, err := repo.FindChartInRepoURL(opts.Repo, opts.Chart, opts.Version, "", "", "", getters)
		if err != nil {
			return nil, err
		}
		opts.Chart = chartURL
	}

	tmpDir, err := ioutil.TempDir("", "osdk-helm-chart")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			log.Errorf("Failed to remove temporary directory %s: %s", tmpDir, err)
		}
	}()

	chartArchive, _, err := c.DownloadTo(opts.Chart, opts.Version, tmpDir)
	if err != nil {
		// One of Helm's error messages directs users to run `helm init`, which
		// installs tiller in a remote cluster. Since that's unnecessary and
		// unhelpful, modify the error message to be relevant for operator-sdk.
		if strings.Contains(err.Error(), "Couldn't load repositories file") {
			return nil, fmt.Errorf("failed to load repositories file %s "+
				"(you might need to run `helm init --client-only` "+
				"to create and initialize it)", c.HelmHome.RepositoryFile())
		}
		return nil, err
	}

	return createChartFromDisk(destDir, chartArchive, false)
}
