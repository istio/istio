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

package helm

import (
	"k8s.io/helm/pkg/proto/hapi/chart"
	rls "k8s.io/helm/pkg/proto/hapi/services"
)

// Interface for helm client for mocking in tests
type Interface interface {
	ListReleases(opts ...ReleaseListOption) (*rls.ListReleasesResponse, error)
	InstallRelease(chStr, namespace string, opts ...InstallOption) (*rls.InstallReleaseResponse, error)
	InstallReleaseFromChart(chart *chart.Chart, namespace string, opts ...InstallOption) (*rls.InstallReleaseResponse, error)
	DeleteRelease(rlsName string, opts ...DeleteOption) (*rls.UninstallReleaseResponse, error)
	ReleaseStatus(rlsName string, opts ...StatusOption) (*rls.GetReleaseStatusResponse, error)
	UpdateRelease(rlsName, chStr string, opts ...UpdateOption) (*rls.UpdateReleaseResponse, error)
	UpdateReleaseFromChart(rlsName string, chart *chart.Chart, opts ...UpdateOption) (*rls.UpdateReleaseResponse, error)
	RollbackRelease(rlsName string, opts ...RollbackOption) (*rls.RollbackReleaseResponse, error)
	ReleaseContent(rlsName string, opts ...ContentOption) (*rls.GetReleaseContentResponse, error)
	ReleaseHistory(rlsName string, opts ...HistoryOption) (*rls.GetHistoryResponse, error)
	GetVersion(opts ...VersionOption) (*rls.GetVersionResponse, error)
	RunReleaseTest(rlsName string, opts ...ReleaseTestOption) (<-chan *rls.TestReleaseResponse, <-chan error)
	PingTiller() error
}
