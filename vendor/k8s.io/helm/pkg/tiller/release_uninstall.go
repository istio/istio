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

	"k8s.io/helm/pkg/hooks"
	"k8s.io/helm/pkg/proto/hapi/release"
	"k8s.io/helm/pkg/proto/hapi/services"
	relutil "k8s.io/helm/pkg/releaseutil"
	"k8s.io/helm/pkg/timeconv"
)

// UninstallRelease deletes all of the resources associated with this release, and marks the release DELETED.
func (s *ReleaseServer) UninstallRelease(c ctx.Context, req *services.UninstallReleaseRequest) (*services.UninstallReleaseResponse, error) {
	if err := validateReleaseName(req.Name); err != nil {
		s.Log("uninstallRelease: Release name is invalid: %s", req.Name)
		return nil, err
	}

	rels, err := s.env.Releases.History(req.Name)
	if err != nil {
		s.Log("uninstall: Release not loaded: %s", req.Name)
		return nil, err
	}
	if len(rels) < 1 {
		return nil, errMissingRelease
	}

	relutil.SortByRevision(rels)
	rel := rels[len(rels)-1]

	// TODO: Are there any cases where we want to force a delete even if it's
	// already marked deleted?
	if rel.Info.Status.Code == release.Status_DELETED {
		if req.Purge {
			if err := s.purgeReleases(rels...); err != nil {
				s.Log("uninstall: Failed to purge the release: %s", err)
				return nil, err
			}
			return &services.UninstallReleaseResponse{Release: rel}, nil
		}
		return nil, fmt.Errorf("the release named %q is already deleted", req.Name)
	}

	s.Log("uninstall: Deleting %s", req.Name)
	rel.Info.Status.Code = release.Status_DELETING
	rel.Info.Deleted = timeconv.Now()
	rel.Info.Description = "Deletion in progress (or silently failed)"
	res := &services.UninstallReleaseResponse{Release: rel}

	if !req.DisableHooks {
		if err := s.execHook(rel.Hooks, rel.Name, rel.Namespace, hooks.PreDelete, req.Timeout); err != nil {
			return res, err
		}
	} else {
		s.Log("delete hooks disabled for %s", req.Name)
	}

	// From here on out, the release is currently considered to be in Status_DELETING
	// state.
	if err := s.env.Releases.Update(rel); err != nil {
		s.Log("uninstall: Failed to store updated release: %s", err)
	}

	kept, errs := s.ReleaseModule.Delete(rel, req, s.env)
	res.Info = kept

	es := make([]string, 0, len(errs))
	for _, e := range errs {
		s.Log("error: %v", e)
		es = append(es, e.Error())
	}

	if !req.DisableHooks {
		if err := s.execHook(rel.Hooks, rel.Name, rel.Namespace, hooks.PostDelete, req.Timeout); err != nil {
			es = append(es, err.Error())
		}
	}

	rel.Info.Status.Code = release.Status_DELETED
	if req.Description == "" {
		rel.Info.Description = "Deletion complete"
	} else {
		rel.Info.Description = req.Description
	}

	if req.Purge {
		s.Log("purge requested for %s", req.Name)
		err := s.purgeReleases(rels...)
		if err != nil {
			s.Log("uninstall: Failed to purge the release: %s", err)
		}
		return res, err
	}

	if err := s.env.Releases.Update(rel); err != nil {
		s.Log("uninstall: Failed to store updated release: %s", err)
	}

	if len(es) > 0 {
		return res, fmt.Errorf("deletion completed with %d error(s): %s", len(es), strings.Join(es, "; "))
	}
	return res, nil
}

func (s *ReleaseServer) purgeReleases(rels ...*release.Release) error {
	for _, rel := range rels {
		if _, err := s.env.Releases.Delete(rel.Name, rel.Version); err != nil {
			return err
		}
	}
	return nil
}
