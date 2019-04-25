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
	ctx "golang.org/x/net/context"

	"k8s.io/helm/pkg/proto/hapi/services"
)

// GetReleaseContent gets all of the stored information for the given release.
func (s *ReleaseServer) GetReleaseContent(c ctx.Context, req *services.GetReleaseContentRequest) (*services.GetReleaseContentResponse, error) {
	if err := validateReleaseName(req.Name); err != nil {
		s.Log("releaseContent: Release name is invalid: %s", req.Name)
		return nil, err
	}

	if req.Version <= 0 {
		rel, err := s.env.Releases.Last(req.Name)
		return &services.GetReleaseContentResponse{Release: rel}, err
	}

	rel, err := s.env.Releases.Get(req.Name, req.Version)
	return &services.GetReleaseContentResponse{Release: rel}, err
}
