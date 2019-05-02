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
	"golang.org/x/net/context"

	tpb "k8s.io/helm/pkg/proto/hapi/services"
	relutil "k8s.io/helm/pkg/releaseutil"
)

// GetHistory gets the history for a given release.
func (s *ReleaseServer) GetHistory(ctx context.Context, req *tpb.GetHistoryRequest) (*tpb.GetHistoryResponse, error) {
	if err := validateReleaseName(req.Name); err != nil {
		s.Log("getHistory: Release name is invalid: %s", req.Name)
		return nil, err
	}

	s.Log("getting history for release %s", req.Name)
	h, err := s.env.Releases.History(req.Name)
	if err != nil {
		return nil, err
	}

	relutil.Reverse(h, relutil.SortByRevision)

	var resp tpb.GetHistoryResponse
	for i := 0; i < min(len(h), int(req.Max)); i++ {
		resp.Releases = append(resp.Releases, h[i])
	}

	return &resp, nil
}

func min(x, y int) int {
	if x < y {
		return x
	}
	return y
}
