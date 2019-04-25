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

package helm // import "k8s.io/helm/pkg/helm"

import (
	"bytes"
	"errors"
	"math/rand"
	"strings"
	"sync"

	"github.com/golang/protobuf/ptypes/timestamp"
	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/manifest"
	"k8s.io/helm/pkg/proto/hapi/chart"
	"k8s.io/helm/pkg/proto/hapi/release"
	rls "k8s.io/helm/pkg/proto/hapi/services"
	"k8s.io/helm/pkg/proto/hapi/version"
	"k8s.io/helm/pkg/renderutil"
	storageerrors "k8s.io/helm/pkg/storage/errors"
)

// FakeClient implements Interface
type FakeClient struct {
	Rels            []*release.Release
	Responses       map[string]release.TestRun_Status
	Opts            options
	RenderManifests bool
}

// Option returns the fake release client
func (c *FakeClient) Option(opts ...Option) Interface {
	for _, opt := range opts {
		opt(&c.Opts)
	}
	return c
}

var _ Interface = &FakeClient{}
var _ Interface = (*FakeClient)(nil)

// ListReleases lists the current releases
func (c *FakeClient) ListReleases(opts ...ReleaseListOption) (*rls.ListReleasesResponse, error) {
	reqOpts := c.Opts
	for _, opt := range opts {
		opt(&reqOpts)
	}
	req := &reqOpts.listReq
	rels := c.Rels
	count := int64(len(c.Rels))
	var next string
	limit := req.GetLimit()
	// TODO: Handle all other options.
	if limit != 0 && limit < count {
		rels = rels[:limit]
		count = limit
		next = c.Rels[limit].GetName()
	}

	resp := &rls.ListReleasesResponse{
		Count:    count,
		Releases: rels,
	}
	if next != "" {
		resp.Next = next
	}
	return resp, nil
}

// InstallRelease creates a new release and returns a InstallReleaseResponse containing that release
func (c *FakeClient) InstallRelease(chStr, ns string, opts ...InstallOption) (*rls.InstallReleaseResponse, error) {
	chart := &chart.Chart{}
	return c.InstallReleaseFromChart(chart, ns, opts...)
}

// InstallReleaseFromChart adds a new MockRelease to the fake client and returns a InstallReleaseResponse containing that release
func (c *FakeClient) InstallReleaseFromChart(chart *chart.Chart, ns string, opts ...InstallOption) (*rls.InstallReleaseResponse, error) {
	for _, opt := range opts {
		opt(&c.Opts)
	}

	releaseName := c.Opts.instReq.Name
	releaseDescription := c.Opts.instReq.Description

	// Check to see if the release already exists.
	rel, err := c.ReleaseStatus(releaseName, nil)
	if err == nil && rel != nil {
		return nil, errors.New("cannot re-use a name that is still in use")
	}

	mockOpts := &MockReleaseOptions{
		Name:        releaseName,
		Chart:       chart,
		Config:      c.Opts.instReq.Values,
		Namespace:   ns,
		Description: releaseDescription,
	}

	release := ReleaseMock(mockOpts)

	if c.RenderManifests {
		if err := RenderReleaseMock(release, false); err != nil {
			return nil, err
		}
	}

	if !c.Opts.dryRun {
		c.Rels = append(c.Rels, release)
	}

	return &rls.InstallReleaseResponse{
		Release: release,
	}, nil
}

// DeleteRelease deletes a release from the FakeClient
func (c *FakeClient) DeleteRelease(rlsName string, opts ...DeleteOption) (*rls.UninstallReleaseResponse, error) {
	for i, rel := range c.Rels {
		if rel.Name == rlsName {
			c.Rels = append(c.Rels[:i], c.Rels[i+1:]...)
			return &rls.UninstallReleaseResponse{
				Release: rel,
			}, nil
		}
	}

	return nil, storageerrors.ErrReleaseNotFound(rlsName)
}

// GetVersion returns a fake version
func (c *FakeClient) GetVersion(opts ...VersionOption) (*rls.GetVersionResponse, error) {
	return &rls.GetVersionResponse{
		Version: &version.Version{
			SemVer: "1.2.3-fakeclient+testonly",
		},
	}, nil
}

// UpdateRelease returns an UpdateReleaseResponse containing the updated release, if it exists
func (c *FakeClient) UpdateRelease(rlsName string, chStr string, opts ...UpdateOption) (*rls.UpdateReleaseResponse, error) {
	return c.UpdateReleaseFromChart(rlsName, &chart.Chart{}, opts...)
}

// UpdateReleaseFromChart returns an UpdateReleaseResponse containing the updated release, if it exists
func (c *FakeClient) UpdateReleaseFromChart(rlsName string, newChart *chart.Chart, opts ...UpdateOption) (*rls.UpdateReleaseResponse, error) {
	for _, opt := range opts {
		opt(&c.Opts)
	}
	// Check to see if the release already exists.
	rel, err := c.ReleaseContent(rlsName, nil)
	if err != nil {
		return nil, err
	}

	mockOpts := &MockReleaseOptions{
		Name:        rel.Release.Name,
		Version:     rel.Release.Version + 1,
		Chart:       newChart,
		Config:      c.Opts.updateReq.Values,
		Namespace:   rel.Release.Namespace,
		Description: c.Opts.updateReq.Description,
	}

	newRelease := ReleaseMock(mockOpts)

	if c.Opts.updateReq.ResetValues {
		newRelease.Config = &chart.Config{Raw: "{}"}
	} else if c.Opts.updateReq.ReuseValues {
		// TODO: This should merge old and new values but does not.
	}

	if c.RenderManifests {
		if err := RenderReleaseMock(newRelease, true); err != nil {
			return nil, err
		}
	}

	if !c.Opts.dryRun {
		*rel.Release = *newRelease
	}

	return &rls.UpdateReleaseResponse{Release: newRelease}, nil
}

// RollbackRelease returns nil, nil
func (c *FakeClient) RollbackRelease(rlsName string, opts ...RollbackOption) (*rls.RollbackReleaseResponse, error) {
	return nil, nil
}

// ReleaseStatus returns a release status response with info from the matching release name.
func (c *FakeClient) ReleaseStatus(rlsName string, opts ...StatusOption) (*rls.GetReleaseStatusResponse, error) {
	for _, rel := range c.Rels {
		if rel.Name == rlsName {
			return &rls.GetReleaseStatusResponse{
				Name:      rel.Name,
				Info:      rel.Info,
				Namespace: rel.Namespace,
			}, nil
		}
	}
	return nil, storageerrors.ErrReleaseNotFound(rlsName)
}

// ReleaseContent returns the configuration for the matching release name in the fake release client.
func (c *FakeClient) ReleaseContent(rlsName string, opts ...ContentOption) (resp *rls.GetReleaseContentResponse, err error) {
	for _, rel := range c.Rels {
		if rel.Name == rlsName {
			return &rls.GetReleaseContentResponse{
				Release: rel,
			}, nil
		}
	}
	return resp, storageerrors.ErrReleaseNotFound(rlsName)
}

// ReleaseHistory returns a release's revision history.
func (c *FakeClient) ReleaseHistory(rlsName string, opts ...HistoryOption) (*rls.GetHistoryResponse, error) {
	return &rls.GetHistoryResponse{Releases: c.Rels}, nil
}

// RunReleaseTest executes a pre-defined tests on a release
func (c *FakeClient) RunReleaseTest(rlsName string, opts ...ReleaseTestOption) (<-chan *rls.TestReleaseResponse, <-chan error) {

	results := make(chan *rls.TestReleaseResponse)
	errc := make(chan error, 1)

	go func() {
		var wg sync.WaitGroup
		for m, s := range c.Responses {
			wg.Add(1)

			go func(msg string, status release.TestRun_Status) {
				defer wg.Done()
				results <- &rls.TestReleaseResponse{Msg: msg, Status: status}
			}(m, s)
		}

		wg.Wait()
		close(results)
		close(errc)
	}()

	return results, errc
}

// PingTiller pings the Tiller pod and ensures that it is up and running
func (c *FakeClient) PingTiller() error {
	return nil
}

// MockHookTemplate is the hook template used for all mock release objects.
var MockHookTemplate = `apiVersion: v1
kind: Job
metadata:
  annotations:
    "helm.sh/hook": pre-install
`

// MockManifest is the manifest used for all mock release objects.
var MockManifest = `apiVersion: v1
kind: Secret
metadata:
  name: fixture
`

// MockReleaseOptions allows for user-configurable options on mock release objects.
type MockReleaseOptions struct {
	Name        string
	Version     int32
	Chart       *chart.Chart
	Config      *chart.Config
	StatusCode  release.Status_Code
	Namespace   string
	Description string
}

// ReleaseMock creates a mock release object based on options set by
// MockReleaseOptions. This function should typically not be used outside of
// testing.
func ReleaseMock(opts *MockReleaseOptions) *release.Release {
	date := timestamp.Timestamp{Seconds: 242085845, Nanos: 0}

	name := opts.Name
	if name == "" {
		name = "testrelease-" + string(rand.Intn(100))
	}

	var version int32 = 1
	if opts.Version != 0 {
		version = opts.Version
	}

	namespace := opts.Namespace
	if namespace == "" {
		namespace = "default"
	}

	description := opts.Description
	if description == "" {
		description = "Release mock"
	}

	ch := opts.Chart
	if opts.Chart == nil {
		ch = &chart.Chart{
			Metadata: &chart.Metadata{
				Name:    "foo",
				Version: "0.1.0-beta.1",
			},
			Templates: []*chart.Template{
				{Name: "templates/foo.tpl", Data: []byte(MockManifest)},
			},
		}
	}

	config := opts.Config
	if config == nil {
		config = &chart.Config{Raw: `name: "value"`}
	}

	scode := release.Status_DEPLOYED
	if opts.StatusCode > 0 {
		scode = opts.StatusCode
	}

	return &release.Release{
		Name: name,
		Info: &release.Info{
			FirstDeployed: &date,
			LastDeployed:  &date,
			Status:        &release.Status{Code: scode},
			Description:   description,
		},
		Chart:     ch,
		Config:    config,
		Version:   version,
		Namespace: namespace,
		Hooks: []*release.Hook{
			{
				Name:     "pre-install-hook",
				Kind:     "Job",
				Path:     "pre-install-hook.yaml",
				Manifest: MockHookTemplate,
				LastRun:  &date,
				Events:   []release.Hook_Event{release.Hook_PRE_INSTALL},
			},
		},
		Manifest: MockManifest,
	}
}

// RenderReleaseMock will take a release (usually produced by helm.ReleaseMock)
// and will render the Manifest inside using the local mechanism (no tiller).
// (Compare to renderResources in pkg/tiller)
func RenderReleaseMock(r *release.Release, asUpgrade bool) error {
	if r == nil || r.Chart == nil || r.Chart.Metadata == nil {
		return errors.New("a release with a chart with metadata must be provided to render the manifests")
	}

	renderOpts := renderutil.Options{
		ReleaseOptions: chartutil.ReleaseOptions{
			Name:      r.Name,
			Namespace: r.Namespace,
			Time:      r.Info.LastDeployed,
			Revision:  int(r.Version),
			IsUpgrade: asUpgrade,
			IsInstall: !asUpgrade,
		},
	}
	rendered, err := renderutil.Render(r.Chart, r.Config, renderOpts)
	if err != nil {
		return err
	}

	b := bytes.NewBuffer(nil)
	for _, m := range manifest.SplitManifests(rendered) {
		// Remove empty manifests
		if len(strings.TrimSpace(m.Content)) == 0 {
			continue
		}
		b.WriteString("\n---\n# Source: " + m.Name + "\n")
		b.WriteString(m.Content)
	}
	r.Manifest = b.String()
	return nil
}
