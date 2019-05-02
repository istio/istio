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
	"crypto/tls"
	"time"

	"github.com/golang/protobuf/proto"
	"golang.org/x/net/context"
	"google.golang.org/grpc/metadata"

	cpb "k8s.io/helm/pkg/proto/hapi/chart"
	"k8s.io/helm/pkg/proto/hapi/release"
	rls "k8s.io/helm/pkg/proto/hapi/services"
	"k8s.io/helm/pkg/version"
)

// Option allows specifying various settings configurable by
// the helm client user for overriding the defaults used when
// issuing rpc's to the Tiller release server.
type Option func(*options)

// options specify optional settings used by the helm client.
type options struct {
	// value of helm home override
	host string
	// if set dry-run helm client calls
	dryRun bool
	// if set enable TLS on helm client calls
	useTLS bool
	// if set, re-use an existing name
	reuseName bool
	// if set, performs pod restart during upgrade/rollback
	recreate bool
	// if set, force resource update through delete/recreate if needed
	force bool
	// if set, skip running hooks
	disableHooks bool
	// if set, skip CRD hook only
	disableCRDHook bool
	// name of release
	releaseName string
	// tls.Config to use for rpc if tls enabled
	tlsConfig *tls.Config
	// release list options are applied directly to the list releases request
	listReq rls.ListReleasesRequest
	// release install options are applied directly to the install release request
	instReq rls.InstallReleaseRequest
	// release update options are applied directly to the update release request
	updateReq rls.UpdateReleaseRequest
	// release uninstall options are applied directly to the uninstall release request
	uninstallReq rls.UninstallReleaseRequest
	// release get status options are applied directly to the get release status request
	statusReq rls.GetReleaseStatusRequest
	// release get content options are applied directly to the get release content request
	contentReq rls.GetReleaseContentRequest
	// release rollback options are applied directly to the rollback release request
	rollbackReq rls.RollbackReleaseRequest
	// before intercepts client calls before sending
	before func(context.Context, proto.Message) error
	// release history options are applied directly to the get release history request
	histReq rls.GetHistoryRequest
	// resetValues instructs Tiller to reset values to their defaults.
	resetValues bool
	// reuseValues instructs Tiller to reuse the values from the last release.
	reuseValues bool
	// release test options are applied directly to the test release history request
	testReq rls.TestReleaseRequest
	// connectTimeout specifies the time duration Helm will wait to establish a connection to tiller
	connectTimeout time.Duration
}

// Host specifies the host address of the Tiller release server, (default = ":44134").
func Host(host string) Option {
	return func(opts *options) {
		opts.host = host
	}
}

// WithTLS specifies the tls configuration if the helm client is enabled to use TLS.
func WithTLS(cfg *tls.Config) Option {
	return func(opts *options) {
		opts.useTLS = true
		opts.tlsConfig = cfg
	}
}

// BeforeCall returns an option that allows intercepting a helm client rpc
// before being sent OTA to tiller. The intercepting function should return
// an error to indicate that the call should not proceed or nil otherwise.
func BeforeCall(fn func(context.Context, proto.Message) error) Option {
	return func(opts *options) {
		opts.before = fn
	}
}

// ReleaseListOption allows specifying various settings
// configurable by the helm client user for overriding
// the defaults used when running the `helm list` command.
type ReleaseListOption func(*options)

// ReleaseListOffset specifies the offset into a list of releases.
func ReleaseListOffset(offset string) ReleaseListOption {
	return func(opts *options) {
		opts.listReq.Offset = offset
	}
}

// ReleaseListFilter specifies a filter to apply a list of releases.
func ReleaseListFilter(filter string) ReleaseListOption {
	return func(opts *options) {
		opts.listReq.Filter = filter
	}
}

// ReleaseListLimit set an upper bound on the number of releases returned.
func ReleaseListLimit(limit int) ReleaseListOption {
	return func(opts *options) {
		opts.listReq.Limit = int64(limit)
	}
}

// ReleaseListOrder specifies how to order a list of releases.
func ReleaseListOrder(order int32) ReleaseListOption {
	return func(opts *options) {
		opts.listReq.SortOrder = rls.ListSort_SortOrder(order)
	}
}

// ReleaseListSort specifies how to sort a release list.
func ReleaseListSort(sort int32) ReleaseListOption {
	return func(opts *options) {
		opts.listReq.SortBy = rls.ListSort_SortBy(sort)
	}
}

// ReleaseListStatuses specifies which status codes should be returned.
func ReleaseListStatuses(statuses []release.Status_Code) ReleaseListOption {
	return func(opts *options) {
		if len(statuses) == 0 {
			statuses = []release.Status_Code{release.Status_DEPLOYED}
		}
		opts.listReq.StatusCodes = statuses
	}
}

// ReleaseListNamespace specifies the namespace to list releases from
func ReleaseListNamespace(namespace string) ReleaseListOption {
	return func(opts *options) {
		opts.listReq.Namespace = namespace
	}
}

// InstallOption allows specifying various settings
// configurable by the helm client user for overriding
// the defaults used when running the `helm install` command.
type InstallOption func(*options)

// ValueOverrides specifies a list of values to include when installing.
func ValueOverrides(raw []byte) InstallOption {
	return func(opts *options) {
		opts.instReq.Values = &cpb.Config{Raw: string(raw)}
	}
}

// ReleaseName specifies the name of the release when installing.
func ReleaseName(name string) InstallOption {
	return func(opts *options) {
		opts.instReq.Name = name
	}
}

// ConnectTimeout specifies the duration (in seconds) Helm will wait to establish a connection to tiller
func ConnectTimeout(timeout int64) Option {
	return func(opts *options) {
		opts.connectTimeout = time.Duration(timeout) * time.Second
	}
}

// InstallTimeout specifies the number of seconds before kubernetes calls timeout
func InstallTimeout(timeout int64) InstallOption {
	return func(opts *options) {
		opts.instReq.Timeout = timeout
	}
}

// UpgradeTimeout specifies the number of seconds before kubernetes calls timeout
func UpgradeTimeout(timeout int64) UpdateOption {
	return func(opts *options) {
		opts.updateReq.Timeout = timeout
	}
}

// DeleteTimeout specifies the number of seconds before kubernetes calls timeout
func DeleteTimeout(timeout int64) DeleteOption {
	return func(opts *options) {
		opts.uninstallReq.Timeout = timeout
	}
}

// ReleaseTestTimeout specifies the number of seconds before kubernetes calls timeout
func ReleaseTestTimeout(timeout int64) ReleaseTestOption {
	return func(opts *options) {
		opts.testReq.Timeout = timeout
	}
}

// ReleaseTestCleanup is a boolean value representing whether to cleanup test pods
func ReleaseTestCleanup(cleanup bool) ReleaseTestOption {
	return func(opts *options) {
		opts.testReq.Cleanup = cleanup
	}
}

// ReleaseTestParallel is a boolean value representing whether to run test pods in parallel
func ReleaseTestParallel(parallel bool) ReleaseTestOption {
	return func(opts *options) {
		opts.testReq.Parallel = parallel
	}
}

// RollbackTimeout specifies the number of seconds before kubernetes calls timeout
func RollbackTimeout(timeout int64) RollbackOption {
	return func(opts *options) {
		opts.rollbackReq.Timeout = timeout
	}
}

// InstallWait specifies whether or not to wait for all resources to be ready
func InstallWait(wait bool) InstallOption {
	return func(opts *options) {
		opts.instReq.Wait = wait
	}
}

// UpgradeWait specifies whether or not to wait for all resources to be ready
func UpgradeWait(wait bool) UpdateOption {
	return func(opts *options) {
		opts.updateReq.Wait = wait
	}
}

// RollbackWait specifies whether or not to wait for all resources to be ready
func RollbackWait(wait bool) RollbackOption {
	return func(opts *options) {
		opts.rollbackReq.Wait = wait
	}
}

// UpdateValueOverrides specifies a list of values to include when upgrading
func UpdateValueOverrides(raw []byte) UpdateOption {
	return func(opts *options) {
		opts.updateReq.Values = &cpb.Config{Raw: string(raw)}
	}
}

// InstallDescription specifies the description for the release
func InstallDescription(description string) InstallOption {
	return func(opts *options) {
		opts.instReq.Description = description
	}
}

// UpgradeDescription specifies the description for the update
func UpgradeDescription(description string) UpdateOption {
	return func(opts *options) {
		opts.updateReq.Description = description
	}
}

// RollbackDescription specifies the description for the release
func RollbackDescription(description string) RollbackOption {
	return func(opts *options) {
		opts.rollbackReq.Description = description
	}
}

// DeleteDescription specifies the description for the release
func DeleteDescription(description string) DeleteOption {
	return func(opts *options) {
		opts.uninstallReq.Description = description
	}
}

// DeleteDisableHooks will disable hooks for a deletion operation.
func DeleteDisableHooks(disable bool) DeleteOption {
	return func(opts *options) {
		opts.disableHooks = disable
	}
}

// DeleteDryRun will (if true) execute a deletion as a dry run.
func DeleteDryRun(dry bool) DeleteOption {
	return func(opts *options) {
		opts.dryRun = dry
	}
}

// DeletePurge removes the release from the store and make its name free for later use.
func DeletePurge(purge bool) DeleteOption {
	return func(opts *options) {
		opts.uninstallReq.Purge = purge
	}
}

// InstallDryRun will (if true) execute an installation as a dry run.
func InstallDryRun(dry bool) InstallOption {
	return func(opts *options) {
		opts.dryRun = dry
	}
}

// InstallDisableHooks disables hooks during installation.
func InstallDisableHooks(disable bool) InstallOption {
	return func(opts *options) {
		opts.disableHooks = disable
	}
}

// InstallDisableCRDHook disables CRD hook during installation.
func InstallDisableCRDHook(disable bool) InstallOption {
	return func(opts *options) {
		opts.disableCRDHook = disable
	}
}

// InstallReuseName will (if true) instruct Tiller to re-use an existing name.
func InstallReuseName(reuse bool) InstallOption {
	return func(opts *options) {
		opts.reuseName = reuse
	}
}

// InstallSubNotes will (if true) instruct Tiller to render SubChart Notes
func InstallSubNotes(enable bool) InstallOption {
	return func(opts *options) {
		opts.instReq.SubNotes = enable
	}
}

// UpgradeSubNotes will (if true) instruct Tiller to render SubChart Notes
func UpgradeSubNotes(enable bool) UpdateOption {
	return func(opts *options) {
		opts.updateReq.SubNotes = enable
	}
}

// RollbackDisableHooks will disable hooks for a rollback operation
func RollbackDisableHooks(disable bool) RollbackOption {
	return func(opts *options) {
		opts.disableHooks = disable
	}
}

// RollbackDryRun will (if true) execute a rollback as a dry run.
func RollbackDryRun(dry bool) RollbackOption {
	return func(opts *options) {
		opts.dryRun = dry
	}
}

// RollbackRecreate will (if true) recreate pods after rollback.
func RollbackRecreate(recreate bool) RollbackOption {
	return func(opts *options) {
		opts.recreate = recreate
	}
}

// RollbackForce will (if true) force resource update through delete/recreate if needed
func RollbackForce(force bool) RollbackOption {
	return func(opts *options) {
		opts.force = force
	}
}

// RollbackVersion sets the version of the release to deploy.
func RollbackVersion(ver int32) RollbackOption {
	return func(opts *options) {
		opts.rollbackReq.Version = ver
	}
}

// UpgradeDisableHooks will disable hooks for an upgrade operation.
func UpgradeDisableHooks(disable bool) UpdateOption {
	return func(opts *options) {
		opts.disableHooks = disable
	}
}

// UpgradeDryRun will (if true) execute an upgrade as a dry run.
func UpgradeDryRun(dry bool) UpdateOption {
	return func(opts *options) {
		opts.dryRun = dry
	}
}

// ResetValues will (if true) trigger resetting the values to their original state.
func ResetValues(reset bool) UpdateOption {
	return func(opts *options) {
		opts.resetValues = reset
	}
}

// ReuseValues will cause Tiller to reuse the values from the last release.
// This is ignored if ResetValues is true.
func ReuseValues(reuse bool) UpdateOption {
	return func(opts *options) {
		opts.reuseValues = reuse
	}
}

// UpgradeRecreate will (if true) recreate pods after upgrade.
func UpgradeRecreate(recreate bool) UpdateOption {
	return func(opts *options) {
		opts.recreate = recreate
	}
}

// UpgradeForce will (if true) force resource update through delete/recreate if needed
func UpgradeForce(force bool) UpdateOption {
	return func(opts *options) {
		opts.force = force
	}
}

// ContentOption allows setting optional attributes when
// performing a GetReleaseContent tiller rpc.
type ContentOption func(*options)

// ContentReleaseVersion will instruct Tiller to retrieve the content
// of a particular version of a release.
func ContentReleaseVersion(version int32) ContentOption {
	return func(opts *options) {
		opts.contentReq.Version = version
	}
}

// StatusOption allows setting optional attributes when
// performing a GetReleaseStatus tiller rpc.
type StatusOption func(*options)

// StatusReleaseVersion will instruct Tiller to retrieve the status
// of a particular version of a release.
func StatusReleaseVersion(version int32) StatusOption {
	return func(opts *options) {
		opts.statusReq.Version = version
	}
}

// DeleteOption allows setting optional attributes when
// performing a UninstallRelease tiller rpc.
type DeleteOption func(*options)

// VersionOption -- TODO
type VersionOption func(*options)

// UpdateOption allows specifying various settings
// configurable by the helm client user for overriding
// the defaults used when running the `helm upgrade` command.
type UpdateOption func(*options)

// RollbackOption allows specifying various settings configurable
// by the helm client user for overriding the defaults used when
// running the `helm rollback` command.
type RollbackOption func(*options)

// HistoryOption allows configuring optional request data for
// issuing a GetHistory rpc.
type HistoryOption func(*options)

// WithMaxHistory sets the max number of releases to return
// in a release history query.
func WithMaxHistory(max int32) HistoryOption {
	return func(opts *options) {
		opts.histReq.Max = max
	}
}

// NewContext creates a versioned context.
func NewContext() context.Context {
	md := metadata.Pairs("x-helm-api-client", version.GetVersion())
	return metadata.NewOutgoingContext(context.TODO(), md)
}

// ReleaseTestOption allows configuring optional request data for
// issuing a TestRelease rpc.
type ReleaseTestOption func(*options)
