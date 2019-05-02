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
	relutil "k8s.io/helm/pkg/releaseutil"
	"k8s.io/helm/pkg/timeconv"
)

// InstallRelease installs a release and stores the release record.
func (s *ReleaseServer) InstallRelease(c ctx.Context, req *services.InstallReleaseRequest) (*services.InstallReleaseResponse, error) {
	s.Log("preparing install for %s", req.Name)
	rel, err := s.prepareRelease(req)
	if err != nil {
		s.Log("failed install prepare step: %s", err)
		res := &services.InstallReleaseResponse{Release: rel}

		// On dry run, append the manifest contents to a failed release. This is
		// a stop-gap until we can revisit an error backchannel post-2.0.
		if req.DryRun && strings.HasPrefix(err.Error(), "YAML parse error") {
			err = fmt.Errorf("%s\n%s", err, rel.Manifest)
		}
		return res, err
	}

	s.Log("performing install for %s", req.Name)
	res, err := s.performRelease(rel, req)
	if err != nil {
		s.Log("failed install perform step: %s", err)
	}
	return res, err
}

// prepareRelease builds a release for an install operation.
func (s *ReleaseServer) prepareRelease(req *services.InstallReleaseRequest) (*release.Release, error) {
	if req.Chart == nil {
		return nil, errMissingChart
	}

	name, err := s.uniqName(req.Name, req.ReuseName)
	if err != nil {
		return nil, err
	}

	caps, err := capabilities(s.clientset.Discovery())
	if err != nil {
		return nil, err
	}

	revision := 1
	ts := timeconv.Now()
	options := chartutil.ReleaseOptions{
		Name:      name,
		Time:      ts,
		Namespace: req.Namespace,
		Revision:  revision,
		IsInstall: true,
	}
	valuesToRender, err := chartutil.ToRenderValuesCaps(req.Chart, req.Values, options, caps)
	if err != nil {
		return nil, err
	}

	hooks, manifestDoc, notesTxt, err := s.renderResources(req.Chart, valuesToRender, req.SubNotes, caps.APIVersions)
	if err != nil {
		// Return a release with partial data so that client can show debugging
		// information.
		rel := &release.Release{
			Name:      name,
			Namespace: req.Namespace,
			Chart:     req.Chart,
			Config:    req.Values,
			Info: &release.Info{
				FirstDeployed: ts,
				LastDeployed:  ts,
				Status:        &release.Status{Code: release.Status_UNKNOWN},
				Description:   fmt.Sprintf("Install failed: %s", err),
			},
			Version: 0,
		}
		if manifestDoc != nil {
			rel.Manifest = manifestDoc.String()
		}
		return rel, err
	}

	// Store a release.
	rel := &release.Release{
		Name:      name,
		Namespace: req.Namespace,
		Chart:     req.Chart,
		Config:    req.Values,
		Info: &release.Info{
			FirstDeployed: ts,
			LastDeployed:  ts,
			Status:        &release.Status{Code: release.Status_PENDING_INSTALL},
			Description:   "Initial install underway", // Will be overwritten.
		},
		Manifest: manifestDoc.String(),
		Hooks:    hooks,
		Version:  int32(revision),
	}
	if len(notesTxt) > 0 {
		rel.Info.Status.Notes = notesTxt
	}

	return rel, nil
}

func hasCRDHook(hs []*release.Hook) bool {
	for _, h := range hs {
		for _, e := range h.Events {
			if e == events[hooks.CRDInstall] {
				return true
			}
		}
	}
	return false
}

// performRelease runs a release.
func (s *ReleaseServer) performRelease(r *release.Release, req *services.InstallReleaseRequest) (*services.InstallReleaseResponse, error) {
	res := &services.InstallReleaseResponse{Release: r}
	manifestDoc := []byte(r.Manifest)

	if req.DryRun {
		s.Log("dry run for %s", r.Name)

		if !req.DisableCrdHook && hasCRDHook(r.Hooks) {
			s.Log("validation skipped because CRD hook is present")
			res.Release.Info.Description = "Validation skipped because CRDs are not installed"
			return res, nil
		}

		// Here's the problem with dry runs and CRDs: We can't install a CRD
		// during a dry run, which means it cannot be validated.
		if err := validateManifest(s.env.KubeClient, req.Namespace, manifestDoc); err != nil {
			return res, err
		}

		res.Release.Info.Description = "Dry run complete"
		return res, nil
	}

	// crd-install hooks
	if !req.DisableHooks && !req.DisableCrdHook {
		if err := s.execHook(r.Hooks, r.Name, r.Namespace, hooks.CRDInstall, req.Timeout); err != nil {
			fmt.Printf("Finished installing CRD: %s", err)
			return res, err
		}
	} else {
		s.Log("CRD install hooks disabled for %s", req.Name)
	}

	// Because the CRDs are installed, they are used for validation during this step.
	if err := validateManifest(s.env.KubeClient, req.Namespace, manifestDoc); err != nil {
		return res, fmt.Errorf("validation failed: %s", err)
	}

	// pre-install hooks
	if !req.DisableHooks {
		if err := s.execHook(r.Hooks, r.Name, r.Namespace, hooks.PreInstall, req.Timeout); err != nil {
			return res, err
		}
	} else {
		s.Log("install hooks disabled for %s", req.Name)
	}

	switch h, err := s.env.Releases.History(req.Name); {
	// if this is a replace operation, append to the release history
	case req.ReuseName && err == nil && len(h) >= 1:
		s.Log("name reuse for %s requested, replacing release", req.Name)
		// get latest release revision
		relutil.Reverse(h, relutil.SortByRevision)

		// old release
		old := h[0]

		// update old release status
		old.Info.Status.Code = release.Status_SUPERSEDED
		s.recordRelease(old, true)

		// update new release with next revision number
		// so as to append to the old release's history
		r.Version = old.Version + 1
		updateReq := &services.UpdateReleaseRequest{
			Wait:     req.Wait,
			Recreate: false,
			Timeout:  req.Timeout,
		}
		s.recordRelease(r, false)
		if err := s.ReleaseModule.Update(old, r, updateReq, s.env); err != nil {
			msg := fmt.Sprintf("Release replace %q failed: %s", r.Name, err)
			s.Log("warning: %s", msg)
			old.Info.Status.Code = release.Status_SUPERSEDED
			r.Info.Status.Code = release.Status_FAILED
			r.Info.Description = msg
			s.recordRelease(old, true)
			s.recordRelease(r, true)
			return res, err
		}

	default:
		// nothing to replace, create as normal
		// regular manifests
		s.recordRelease(r, false)
		if err := s.ReleaseModule.Create(r, req, s.env); err != nil {
			msg := fmt.Sprintf("Release %q failed: %s", r.Name, err)
			s.Log("warning: %s", msg)
			r.Info.Status.Code = release.Status_FAILED
			r.Info.Description = msg
			s.recordRelease(r, true)
			return res, fmt.Errorf("release %s failed: %s", r.Name, err)
		}
	}

	// post-install hooks
	if !req.DisableHooks {
		if err := s.execHook(r.Hooks, r.Name, r.Namespace, hooks.PostInstall, req.Timeout); err != nil {
			msg := fmt.Sprintf("Release %q failed post-install: %s", r.Name, err)
			s.Log("warning: %s", msg)
			r.Info.Status.Code = release.Status_FAILED
			r.Info.Description = msg
			s.recordRelease(r, true)
			return res, err
		}
	}

	r.Info.Status.Code = release.Status_DEPLOYED
	if req.Description == "" {
		r.Info.Description = "Install complete"
	} else {
		r.Info.Description = req.Description
	}
	// This is a tricky case. The release has been created, but the result
	// cannot be recorded. The truest thing to tell the user is that the
	// release was created. However, the user will not be able to do anything
	// further with this release.
	//
	// One possible strategy would be to do a timed retry to see if we can get
	// this stored in the future.
	s.recordRelease(r, true)

	return res, nil
}
