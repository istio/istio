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
	"errors"
	"fmt"

	ctx "golang.org/x/net/context"

	"k8s.io/helm/pkg/proto/hapi/release"
	"k8s.io/helm/pkg/proto/hapi/services"
)

// GetReleaseStatus gets the status information for a named release.
func (s *ReleaseServer) GetReleaseStatus(c ctx.Context, req *services.GetReleaseStatusRequest) (*services.GetReleaseStatusResponse, error) {
	if err := validateReleaseName(req.Name); err != nil {
		s.Log("getStatus: Release name is invalid: %s", req.Name)
		return nil, err
	}

	var rel *release.Release

	if req.Version <= 0 {
		var err error
		rel, err = s.env.Releases.Last(req.Name)
		if err != nil {
			return nil, fmt.Errorf("getting deployed release %q: %s", req.Name, err)
		}
	} else {
		var err error
		if rel, err = s.env.Releases.Get(req.Name, req.Version); err != nil {
			return nil, fmt.Errorf("getting release '%s' (v%d): %s", req.Name, req.Version, err)
		}
	}

	if rel.Info == nil {
		return nil, errors.New("release info is missing")
	}
	if rel.Chart == nil {
		return nil, errors.New("release chart is missing")
	}

	sc := rel.Info.Status.Code
	statusResp := &services.GetReleaseStatusResponse{
		Name:      rel.Name,
		Namespace: rel.Namespace,
		Info:      rel.Info,
	}

	// Ok, we got the status of the release as we had jotted down, now we need to match the
	// manifest we stashed away with reality from the cluster.
	resp, err := s.ReleaseModule.Status(rel, req, s.env)
	if sc == release.Status_DELETED || sc == release.Status_FAILED {
		// Skip errors if this is already deleted or failed.
		return statusResp, nil
	} else if err != nil {
		s.Log("warning: Get for %s failed: %v", rel.Name, err)
		return nil, err
	}
	rel.Info.Status.Resources = resp
	return statusResp, nil
}
