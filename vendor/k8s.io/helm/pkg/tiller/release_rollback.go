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

package tiller

import (
	"fmt"
	"k8s.io/helm/pkg/storage"
	"strings"

	ctx "golang.org/x/net/context"

	"k8s.io/helm/pkg/hooks"
	"k8s.io/helm/pkg/proto/hapi/release"
	"k8s.io/helm/pkg/proto/hapi/services"
	"k8s.io/helm/pkg/timeconv"
)

// RollbackRelease rolls back to a previous version of the given release.
func (s *ReleaseServer) RollbackRelease(c ctx.Context, req *services.RollbackReleaseRequest) (*services.RollbackReleaseResponse, error) {
	s.Log("preparing rollback of %s", req.Name)
	currentRelease, targetRelease, err := s.prepareRollback(req)
	if err != nil {
		return nil, err
	}

	if !req.DryRun {
		s.Log("creating rolled back release for %s", req.Name)
		if err := s.env.Releases.Create(targetRelease); err != nil {
			return nil, err
		}
	}
	s.Log("performing rollback of %s", req.Name)
	res, err := s.performRollback(currentRelease, targetRelease, req)
	if err != nil {
		return res, err
	}

	if !req.DryRun {
		s.Log("updating status for rolled back release for %s", req.Name)
		if err := s.env.Releases.Update(targetRelease); err != nil {
			return res, err
		}
	}

	return res, nil
}

// prepareRollback finds the previous release and prepares a new release object with
// the previous release's configuration
func (s *ReleaseServer) prepareRollback(req *services.RollbackReleaseRequest) (*release.Release, *release.Release, error) {
	if err := validateReleaseName(req.Name); err != nil {
		s.Log("prepareRollback: Release name is invalid: %s", req.Name)
		return nil, nil, err
	}

	if req.Version < 0 {
		return nil, nil, errInvalidRevision
	}

	currentRelease, err := s.env.Releases.Last(req.Name)
	if err != nil {
		return nil, nil, err
	}

	previousVersion := req.Version
	if req.Version == 0 {
		previousVersion = currentRelease.Version - 1
	}

	s.Log("rolling back %s (current: v%d, target: v%d)", req.Name, currentRelease.Version, previousVersion)

	previousRelease, err := s.env.Releases.Get(req.Name, previousVersion)
	if err != nil {
		return nil, nil, err
	}

	description := req.Description
	if req.Description == "" {
		description = fmt.Sprintf("Rollback to %d", previousVersion)
	}

	// Store a new release object with previous release's configuration
	targetRelease := &release.Release{
		Name:      req.Name,
		Namespace: currentRelease.Namespace,
		Chart:     previousRelease.Chart,
		Config:    previousRelease.Config,
		Info: &release.Info{
			FirstDeployed: currentRelease.Info.FirstDeployed,
			LastDeployed:  timeconv.Now(),
			Status: &release.Status{
				Code:  release.Status_PENDING_ROLLBACK,
				Notes: previousRelease.Info.Status.Notes,
			},
			// Because we lose the reference to previous version elsewhere, we set the
			// message here, and only override it later if we experience failure.
			Description: description,
		},
		Version:  currentRelease.Version + 1,
		Manifest: previousRelease.Manifest,
		Hooks:    previousRelease.Hooks,
	}

	return currentRelease, targetRelease, nil
}

func (s *ReleaseServer) performRollback(currentRelease, targetRelease *release.Release, req *services.RollbackReleaseRequest) (*services.RollbackReleaseResponse, error) {
	res := &services.RollbackReleaseResponse{Release: targetRelease}

	if req.DryRun {
		s.Log("dry run for %s", targetRelease.Name)
		return res, nil
	}

	// pre-rollback hooks
	if !req.DisableHooks {
		if err := s.execHook(targetRelease.Hooks, targetRelease.Name, targetRelease.Namespace, hooks.PreRollback, req.Timeout); err != nil {
			return res, err
		}
	} else {
		s.Log("rollback hooks disabled for %s", req.Name)
	}

	if err := s.ReleaseModule.Rollback(currentRelease, targetRelease, req, s.env); err != nil {
		msg := fmt.Sprintf("Rollback %q failed: %s", targetRelease.Name, err)
		s.Log("warning: %s", msg)
		currentRelease.Info.Status.Code = release.Status_SUPERSEDED
		targetRelease.Info.Status.Code = release.Status_FAILED
		targetRelease.Info.Description = msg
		s.recordRelease(currentRelease, true)
		s.recordRelease(targetRelease, true)
		return res, err
	}

	// post-rollback hooks
	if !req.DisableHooks {
		if err := s.execHook(targetRelease.Hooks, targetRelease.Name, targetRelease.Namespace, hooks.PostRollback, req.Timeout); err != nil {
			return res, err
		}
	}

	// update the current release
	s.Log("superseding previous deployment %d", currentRelease.Version)
	currentRelease.Info.Status.Code = release.Status_SUPERSEDED
	s.recordRelease(currentRelease, true)

	// Supersede all previous deployments, see issue #2941.
	deployed, err := s.env.Releases.DeployedAll(currentRelease.Name)
	if err != nil && !strings.Contains(err.Error(), storage.NoReleasesErr) {
		return nil, err
	}
	for _, r := range deployed {
		s.Log("superseding previous deployment %d", r.Version)
		r.Info.Status.Code = release.Status_SUPERSEDED
		s.recordRelease(r, true)
	}

	targetRelease.Info.Status.Code = release.Status_DEPLOYED

	return res, nil
}
