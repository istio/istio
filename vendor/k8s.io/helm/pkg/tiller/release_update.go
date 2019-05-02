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
	"strings"

	ctx "golang.org/x/net/context"

	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/hooks"
	"k8s.io/helm/pkg/proto/hapi/release"
	"k8s.io/helm/pkg/proto/hapi/services"
	"k8s.io/helm/pkg/timeconv"
)

// UpdateRelease takes an existing release and new information, and upgrades the release.
func (s *ReleaseServer) UpdateRelease(c ctx.Context, req *services.UpdateReleaseRequest) (*services.UpdateReleaseResponse, error) {
	if err := validateReleaseName(req.Name); err != nil {
		s.Log("updateRelease: Release name is invalid: %s", req.Name)
		return nil, err
	}
	s.Log("preparing update for %s", req.Name)
	currentRelease, updatedRelease, err := s.prepareUpdate(req)
	if err != nil {
		if req.Force {
			// Use the --force, Luke.
			return s.performUpdateForce(req)
		}
		return nil, err
	}

	if !req.DryRun {
		s.Log("creating updated release for %s", req.Name)
		if err := s.env.Releases.Create(updatedRelease); err != nil {
			return nil, err
		}
	}

	s.Log("performing update for %s", req.Name)
	res, err := s.performUpdate(currentRelease, updatedRelease, req)
	if err != nil {
		return res, err
	}

	if !req.DryRun {
		s.Log("updating status for updated release for %s", req.Name)
		if err := s.env.Releases.Update(updatedRelease); err != nil {
			return res, err
		}
	}

	return res, nil
}

// prepareUpdate builds an updated release for an update operation.
func (s *ReleaseServer) prepareUpdate(req *services.UpdateReleaseRequest) (*release.Release, *release.Release, error) {
	if req.Chart == nil {
		return nil, nil, errMissingChart
	}

	// finds the deployed release with the given name
	currentRelease, err := s.env.Releases.Deployed(req.Name)
	if err != nil {
		return nil, nil, err
	}

	// determine if values will be reused
	if err := s.reuseValues(req, currentRelease); err != nil {
		return nil, nil, err
	}

	// finds the non-deleted release with the given name
	lastRelease, err := s.env.Releases.Last(req.Name)
	if err != nil {
		return nil, nil, err
	}

	// Increment revision count. This is passed to templates, and also stored on
	// the release object.
	revision := lastRelease.Version + 1

	ts := timeconv.Now()
	options := chartutil.ReleaseOptions{
		Name:      req.Name,
		Time:      ts,
		Namespace: currentRelease.Namespace,
		IsUpgrade: true,
		Revision:  int(revision),
	}

	caps, err := capabilities(s.clientset.Discovery())
	if err != nil {
		return nil, nil, err
	}
	valuesToRender, err := chartutil.ToRenderValuesCaps(req.Chart, req.Values, options, caps)
	if err != nil {
		return nil, nil, err
	}

	hooks, manifestDoc, notesTxt, err := s.renderResources(req.Chart, valuesToRender, req.SubNotes, caps.APIVersions)
	if err != nil {
		return nil, nil, err
	}

	// Store an updated release.
	updatedRelease := &release.Release{
		Name:      req.Name,
		Namespace: currentRelease.Namespace,
		Chart:     req.Chart,
		Config:    req.Values,
		Info: &release.Info{
			FirstDeployed: currentRelease.Info.FirstDeployed,
			LastDeployed:  ts,
			Status:        &release.Status{Code: release.Status_PENDING_UPGRADE},
			Description:   "Preparing upgrade", // This should be overwritten later.
		},
		Version:  revision,
		Manifest: manifestDoc.String(),
		Hooks:    hooks,
	}

	if len(notesTxt) > 0 {
		updatedRelease.Info.Status.Notes = notesTxt
	}
	err = validateManifest(s.env.KubeClient, currentRelease.Namespace, manifestDoc.Bytes())
	return currentRelease, updatedRelease, err
}

// performUpdateForce performs the same action as a `helm delete && helm install --replace`.
func (s *ReleaseServer) performUpdateForce(req *services.UpdateReleaseRequest) (*services.UpdateReleaseResponse, error) {
	// find the last release with the given name
	oldRelease, err := s.env.Releases.Last(req.Name)
	if err != nil {
		return nil, err
	}

	res := &services.UpdateReleaseResponse{}

	newRelease, err := s.prepareRelease(&services.InstallReleaseRequest{
		Chart:        req.Chart,
		Values:       req.Values,
		DryRun:       req.DryRun,
		Name:         req.Name,
		DisableHooks: req.DisableHooks,
		Namespace:    oldRelease.Namespace,
		ReuseName:    true,
		Timeout:      req.Timeout,
		Wait:         req.Wait,
	})
	if err != nil {
		s.Log("failed update prepare step: %s", err)
		// On dry run, append the manifest contents to a failed release. This is
		// a stop-gap until we can revisit an error backchannel post-2.0.
		if req.DryRun && strings.HasPrefix(err.Error(), "YAML parse error") {
			err = fmt.Errorf("%s\n%s", err, newRelease.Manifest)
		}
		return res, err
	}

	// update new release with next revision number so as to append to the old release's history
	newRelease.Version = oldRelease.Version + 1
	res.Release = newRelease

	if req.DryRun {
		s.Log("dry run for %s", newRelease.Name)
		res.Release.Info.Description = "Dry run complete"
		return res, nil
	}

	// From here on out, the release is considered to be in Status_DELETING or Status_DELETED
	// state. There is no turning back.
	oldRelease.Info.Status.Code = release.Status_DELETING
	oldRelease.Info.Deleted = timeconv.Now()
	oldRelease.Info.Description = "Deletion in progress (or silently failed)"
	s.recordRelease(oldRelease, true)

	// pre-delete hooks
	if !req.DisableHooks {
		if err := s.execHook(oldRelease.Hooks, oldRelease.Name, oldRelease.Namespace, hooks.PreDelete, req.Timeout); err != nil {
			return res, err
		}
	} else {
		s.Log("hooks disabled for %s", req.Name)
	}

	// delete manifests from the old release
	_, errs := s.ReleaseModule.Delete(oldRelease, nil, s.env)

	oldRelease.Info.Status.Code = release.Status_DELETED
	oldRelease.Info.Description = "Deletion complete"
	s.recordRelease(oldRelease, true)

	if len(errs) > 0 {
		es := make([]string, 0, len(errs))
		for _, e := range errs {
			s.Log("error: %v", e)
			es = append(es, e.Error())
		}
		return res, fmt.Errorf("Upgrade --force successfully deleted the previous release, but encountered %d error(s) and cannot continue: %s", len(es), strings.Join(es, "; "))
	}

	// post-delete hooks
	if !req.DisableHooks {
		if err := s.execHook(oldRelease.Hooks, oldRelease.Name, oldRelease.Namespace, hooks.PostDelete, req.Timeout); err != nil {
			return res, err
		}
	}

	// pre-install hooks
	if !req.DisableHooks {
		if err := s.execHook(newRelease.Hooks, newRelease.Name, newRelease.Namespace, hooks.PreInstall, req.Timeout); err != nil {
			return res, err
		}
	}

	s.recordRelease(newRelease, false)
	if err := s.ReleaseModule.Update(oldRelease, newRelease, req, s.env); err != nil {
		msg := fmt.Sprintf("Upgrade %q failed: %s", newRelease.Name, err)
		s.Log("warning: %s", msg)
		newRelease.Info.Status.Code = release.Status_FAILED
		newRelease.Info.Description = msg
		s.recordRelease(newRelease, true)
		return res, err
	}

	// post-install hooks
	if !req.DisableHooks {
		if err := s.execHook(newRelease.Hooks, newRelease.Name, newRelease.Namespace, hooks.PostInstall, req.Timeout); err != nil {
			msg := fmt.Sprintf("Release %q failed post-install: %s", newRelease.Name, err)
			s.Log("warning: %s", msg)
			newRelease.Info.Status.Code = release.Status_FAILED
			newRelease.Info.Description = msg
			s.recordRelease(newRelease, true)
			return res, err
		}
	}

	newRelease.Info.Status.Code = release.Status_DEPLOYED
	if req.Description == "" {
		newRelease.Info.Description = "Upgrade complete"
	} else {
		newRelease.Info.Description = req.Description
	}
	s.recordRelease(newRelease, true)

	return res, nil
}

func (s *ReleaseServer) performUpdate(originalRelease, updatedRelease *release.Release, req *services.UpdateReleaseRequest) (*services.UpdateReleaseResponse, error) {
	res := &services.UpdateReleaseResponse{Release: updatedRelease}

	if req.DryRun {
		s.Log("dry run for %s", updatedRelease.Name)
		res.Release.Info.Description = "Dry run complete"
		return res, nil
	}

	// pre-upgrade hooks
	if !req.DisableHooks {
		if err := s.execHook(updatedRelease.Hooks, updatedRelease.Name, updatedRelease.Namespace, hooks.PreUpgrade, req.Timeout); err != nil {
			return res, err
		}
	} else {
		s.Log("update hooks disabled for %s", req.Name)
	}
	if err := s.ReleaseModule.Update(originalRelease, updatedRelease, req, s.env); err != nil {
		msg := fmt.Sprintf("Upgrade %q failed: %s", updatedRelease.Name, err)
		s.Log("warning: %s", msg)
		updatedRelease.Info.Status.Code = release.Status_FAILED
		updatedRelease.Info.Description = msg
		s.recordRelease(originalRelease, true)
		s.recordRelease(updatedRelease, true)
		return res, err
	}

	// post-upgrade hooks
	if !req.DisableHooks {
		if err := s.execHook(updatedRelease.Hooks, updatedRelease.Name, updatedRelease.Namespace, hooks.PostUpgrade, req.Timeout); err != nil {
			return res, err
		}
	}

	originalRelease.Info.Status.Code = release.Status_SUPERSEDED
	s.recordRelease(originalRelease, true)

	updatedRelease.Info.Status.Code = release.Status_DEPLOYED
	if req.Description == "" {
		updatedRelease.Info.Description = "Upgrade complete"
	} else {
		updatedRelease.Info.Description = req.Description
	}

	return res, nil
}
