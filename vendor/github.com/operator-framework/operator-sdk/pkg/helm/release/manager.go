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

package release

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	yaml "gopkg.in/yaml.v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	apitypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/cli-runtime/pkg/genericclioptions/resource"
	"k8s.io/client-go/rest"
	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/kube"
	cpb "k8s.io/helm/pkg/proto/hapi/chart"
	rpb "k8s.io/helm/pkg/proto/hapi/release"
	"k8s.io/helm/pkg/proto/hapi/services"
	"k8s.io/helm/pkg/storage"
	"k8s.io/helm/pkg/tiller"

	"github.com/mattbaird/jsonpatch"
	"github.com/operator-framework/operator-sdk/pkg/helm/internal/types"
)

var (
	// ErrNotFound indicates the release was not found.
	ErrNotFound = errors.New("release not found")
)

// Manager manages a Helm release. It can install, update, reconcile,
// and uninstall a release.
type Manager interface {
	ReleaseName() string
	IsInstalled() bool
	IsUpdateRequired() bool
	Sync(context.Context) error
	InstallRelease(context.Context) (*rpb.Release, error)
	UpdateRelease(context.Context) (*rpb.Release, *rpb.Release, error)
	ReconcileRelease(context.Context) (*rpb.Release, error)
	UninstallRelease(context.Context) (*rpb.Release, error)
}

type manager struct {
	storageBackend   *storage.Storage
	tillerKubeClient *kube.Client
	chartDir         string

	tiller      *tiller.ReleaseServer
	releaseName string
	namespace   string

	spec   interface{}
	status *types.HelmAppStatus

	isInstalled      bool
	isUpdateRequired bool
	deployedRelease  *rpb.Release
	chart            *cpb.Chart
	config           *cpb.Config
}

// ReleaseName returns the name of the release.
func (m manager) ReleaseName() string {
	return m.releaseName
}

func (m manager) IsInstalled() bool {
	return m.isInstalled
}

func (m manager) IsUpdateRequired() bool {
	return m.isUpdateRequired
}

// Sync ensures the Helm storage backend is in sync with the status of the
// custom resource.
func (m *manager) Sync(ctx context.Context) error {
	// TODO: We're now persisting releases as secrets. To support seamless upgrades, we
	// need to sync the release status from the CR to the persistent storage backend.
	// Once we release the storage backend migration, this function (and comment)
	// can be removed.
	if err := m.syncReleaseStatus(*m.status); err != nil {
		return fmt.Errorf("failed to sync release status to storage backend: %s", err)
	}

	// Get release history for this release name
	releases, err := m.storageBackend.History(m.releaseName)
	if err != nil && !notFoundErr(err) {
		return fmt.Errorf("failed to retrieve release history: %s", err)
	}

	// Cleanup non-deployed release versions. If all release versions are
	// non-deployed, this will ensure that failed installations are correctly
	// retried.
	for _, rel := range releases {
		if rel.GetInfo().GetStatus().GetCode() != rpb.Status_DEPLOYED {
			_, err := m.storageBackend.Delete(rel.GetName(), rel.GetVersion())
			if err != nil && !notFoundErr(err) {
				return fmt.Errorf("failed to delete stale release version: %s", err)
			}
		}
	}

	// Load the chart and config based on the current state of the custom resource.
	chart, config, err := m.loadChartAndConfig()
	if err != nil {
		return fmt.Errorf("failed to load chart and config: %s", err)
	}
	m.chart = chart
	m.config = config

	// Load the most recently deployed release from the storage backend.
	deployedRelease, err := m.getDeployedRelease()
	if err == ErrNotFound {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get deployed release: %s", err)
	}
	m.deployedRelease = deployedRelease
	m.isInstalled = true

	// Get the next candidate release to determine if an update is necessary.
	candidateRelease, err := m.getCandidateRelease(ctx, m.tiller, m.releaseName, chart, config)
	if err != nil {
		return fmt.Errorf("failed to get candidate release: %s", err)
	}
	if deployedRelease.GetManifest() != candidateRelease.GetManifest() {
		m.isUpdateRequired = true
	}

	return nil
}

func (m manager) syncReleaseStatus(status types.HelmAppStatus) error {
	var release *rpb.Release
	for _, condition := range status.Conditions {
		if condition.Type == types.ConditionDeployed && condition.Status == types.StatusTrue {
			release = condition.Release
			break
		}
	}
	if release == nil {
		return nil
	}

	name := release.GetName()
	version := release.GetVersion()
	_, err := m.storageBackend.Get(name, version)
	if err == nil {
		return nil
	}

	if !notFoundErr(err) {
		return err
	}
	return m.storageBackend.Create(release)
}

func notFoundErr(err error) bool {
	return strings.Contains(err.Error(), "not found")
}

func (m manager) loadChartAndConfig() (*cpb.Chart, *cpb.Config, error) {
	// chart is mutated by the call to processRequirements,
	// so we need to reload it from disk every time.
	chart, err := chartutil.LoadDir(m.chartDir)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load chart: %s", err)
	}

	cr, err := yaml.Marshal(m.spec)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse values: %s", err)
	}
	config := &cpb.Config{Raw: string(cr)}

	err = processRequirements(chart, config)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to process chart requirements: %s", err)
	}
	return chart, config, nil
}

// processRequirements will process the requirements file
// It will disable/enable the charts based on condition in requirements file
// Also imports the specified chart values from child to parent.
func processRequirements(chart *cpb.Chart, values *cpb.Config) error {
	err := chartutil.ProcessRequirementsEnabled(chart, values)
	if err != nil {
		return err
	}
	err = chartutil.ProcessRequirementsImportValues(chart)
	if err != nil {
		return err
	}
	return nil
}

func (m manager) getDeployedRelease() (*rpb.Release, error) {
	deployedRelease, err := m.storageBackend.Deployed(m.releaseName)
	if err != nil {
		if strings.Contains(err.Error(), "has no deployed releases") {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return deployedRelease, nil
}

func (m manager) getCandidateRelease(ctx context.Context, tiller *tiller.ReleaseServer, name string, chart *cpb.Chart, config *cpb.Config) (*rpb.Release, error) {
	dryRunReq := &services.UpdateReleaseRequest{
		Name:   name,
		Chart:  chart,
		Values: config,
		DryRun: true,
	}
	dryRunResponse, err := tiller.UpdateRelease(ctx, dryRunReq)
	if err != nil {
		return nil, err
	}
	return dryRunResponse.GetRelease(), nil
}

// InstallRelease performs a Helm release install.
func (m manager) InstallRelease(ctx context.Context) (*rpb.Release, error) {
	return installRelease(ctx, m.tiller, m.namespace, m.releaseName, m.chart, m.config)
}

func installRelease(ctx context.Context, tiller *tiller.ReleaseServer, namespace, name string, chart *cpb.Chart, config *cpb.Config) (*rpb.Release, error) {
	installReq := &services.InstallReleaseRequest{
		Namespace: namespace,
		Name:      name,
		Chart:     chart,
		Values:    config,
	}

	releaseResponse, err := tiller.InstallRelease(ctx, installReq)
	if err != nil {
		// Workaround for helm/helm#3338
		if releaseResponse.GetRelease() != nil {
			uninstallReq := &services.UninstallReleaseRequest{
				Name:  releaseResponse.GetRelease().GetName(),
				Purge: true,
			}
			_, uninstallErr := tiller.UninstallRelease(ctx, uninstallReq)
			if uninstallErr != nil {
				return nil, fmt.Errorf("failed to roll back failed installation: %s: %s", uninstallErr, err)
			}
		}
		return nil, err
	}
	return releaseResponse.GetRelease(), nil
}

// UpdateRelease performs a Helm release update.
func (m manager) UpdateRelease(ctx context.Context) (*rpb.Release, *rpb.Release, error) {
	updatedRelease, err := updateRelease(ctx, m.tiller, m.releaseName, m.chart, m.config)
	return m.deployedRelease, updatedRelease, err
}

func updateRelease(ctx context.Context, tiller *tiller.ReleaseServer, name string, chart *cpb.Chart, config *cpb.Config) (*rpb.Release, error) {
	updateReq := &services.UpdateReleaseRequest{
		Name:   name,
		Chart:  chart,
		Values: config,
	}

	releaseResponse, err := tiller.UpdateRelease(ctx, updateReq)
	if err != nil {
		// Workaround for helm/helm#3338
		if releaseResponse.GetRelease() != nil {
			rollbackReq := &services.RollbackReleaseRequest{
				Name:  name,
				Force: true,
			}
			_, rollbackErr := tiller.RollbackRelease(ctx, rollbackReq)
			if rollbackErr != nil {
				return nil, fmt.Errorf("failed to roll back failed update: %s: %s", rollbackErr, err)
			}
		}
		return nil, err
	}
	return releaseResponse.GetRelease(), nil
}

// ReconcileRelease creates or patches resources as necessary to match the
// deployed release's manifest.
func (m manager) ReconcileRelease(ctx context.Context) (*rpb.Release, error) {
	err := reconcileRelease(ctx, m.tillerKubeClient, m.namespace, m.deployedRelease.GetManifest())
	return m.deployedRelease, err
}

func reconcileRelease(ctx context.Context, tillerKubeClient *kube.Client, namespace string, expectedManifest string) error {
	expectedInfos, err := tillerKubeClient.BuildUnstructured(namespace, bytes.NewBufferString(expectedManifest))
	if err != nil {
		return err
	}
	return expectedInfos.Visit(func(expected *resource.Info, err error) error {
		if err != nil {
			return err
		}

		expectedClient := resource.NewClientWithOptions(expected.Client, func(r *rest.Request) {
			*r = *r.Context(ctx)
		})
		helper := resource.NewHelper(expectedClient, expected.Mapping)

		existing, err := helper.Get(expected.Namespace, expected.Name, false)
		if apierrors.IsNotFound(err) {
			if _, err := helper.Create(expected.Namespace, true, expected.Object, &metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("create error: %s", err)
			}
			return nil
		} else if err != nil {
			return err
		}

		patch, err := generatePatch(existing, expected.Object)
		if err != nil {
			return fmt.Errorf("failed to marshal JSON patch: %s", err)
		}

		if patch == nil {
			return nil
		}

		_, err = helper.Patch(expected.Namespace, expected.Name, apitypes.JSONPatchType, patch, &metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("patch error: %s", err)
		}
		return nil
	})
}

func generatePatch(existing, expected runtime.Object) ([]byte, error) {
	existingJSON, err := json.Marshal(existing)
	if err != nil {
		return nil, err
	}
	expectedJSON, err := json.Marshal(expected)
	if err != nil {
		return nil, err
	}

	ops, err := jsonpatch.CreatePatch(existingJSON, expectedJSON)
	if err != nil {
		return nil, err
	}

	// We ignore the "remove" operations from the full patch because they are
	// fields added by Kubernetes or by the user after the existing release
	// resource has been applied. The goal for this patch is to make sure that
	// the fields managed by the Helm chart are applied.
	patchOps := make([]jsonpatch.JsonPatchOperation, 0)
	for _, op := range ops {
		if op.Operation != "remove" {
			patchOps = append(patchOps, op)
		}
	}

	// If there are no patch operations, return nil. Callers are expected
	// to check for a nil response and skip the patch operation to avoid
	// unnecessary chatter with the API server.
	if len(patchOps) == 0 {
		return nil, nil
	}

	return json.Marshal(patchOps)
}

// UninstallRelease performs a Helm release uninstall.
func (m manager) UninstallRelease(ctx context.Context) (*rpb.Release, error) {
	return uninstallRelease(ctx, m.storageBackend, m.tiller, m.releaseName)
}

func uninstallRelease(ctx context.Context, storageBackend *storage.Storage, tiller *tiller.ReleaseServer, releaseName string) (*rpb.Release, error) {
	// Get history of this release
	h, err := storageBackend.History(releaseName)
	if err != nil {
		return nil, fmt.Errorf("failed to get release history: %s", err)
	}

	// If there is no history, the release has already been uninstalled,
	// so return ErrNotFound.
	if len(h) == 0 {
		return nil, ErrNotFound
	}

	uninstallResponse, err := tiller.UninstallRelease(ctx, &services.UninstallReleaseRequest{
		Name:  releaseName,
		Purge: true,
	})
	return uninstallResponse.GetRelease(), err
}
