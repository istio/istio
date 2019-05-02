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
	"regexp"

	"github.com/golang/protobuf/proto"
	"k8s.io/helm/pkg/proto/hapi/release"
	"k8s.io/helm/pkg/proto/hapi/services"
	relutil "k8s.io/helm/pkg/releaseutil"
)

// ListReleases lists the releases found by the server.
func (s *ReleaseServer) ListReleases(req *services.ListReleasesRequest, stream services.ReleaseService_ListReleasesServer) error {
	if len(req.StatusCodes) == 0 {
		req.StatusCodes = []release.Status_Code{release.Status_DEPLOYED}
	}

	//rels, err := s.env.Releases.ListDeployed()
	rels, err := s.env.Releases.ListFilterAll(func(r *release.Release) bool {
		for _, sc := range req.StatusCodes {
			if sc == r.Info.Status.Code {
				return true
			}
		}
		return false
	})
	if err != nil {
		return err
	}

	if req.Namespace != "" {
		rels, err = filterByNamespace(req.Namespace, rels)
		if err != nil {
			return err
		}
	}

	if len(req.Filter) != 0 {
		rels, err = filterReleases(req.Filter, rels)
		if err != nil {
			return err
		}
	}

	total := int64(len(rels))

	switch req.SortBy {
	case services.ListSort_NAME:
		relutil.SortByName(rels)
	case services.ListSort_LAST_RELEASED:
		relutil.SortByDate(rels)
	case services.ListSort_CHART_NAME:
		relutil.SortByChartName(rels)
	}

	if req.SortOrder == services.ListSort_DESC {
		ll := len(rels)
		rr := make([]*release.Release, ll)
		for i, item := range rels {
			rr[ll-i-1] = item
		}
		rels = rr
	}

	l := int64(len(rels))
	if req.Offset != "" {

		i := -1
		for ii, cur := range rels {
			if cur.Name == req.Offset {
				i = ii
			}
		}
		if i == -1 {
			return fmt.Errorf("offset %q not found", req.Offset)
		}

		if len(rels) < i {
			return fmt.Errorf("no items after %q", req.Offset)
		}

		rels = rels[i:]
		l = int64(len(rels))
	}

	if req.Limit == 0 {
		req.Limit = ListDefaultLimit
	}

	next := ""
	if l > req.Limit {
		next = rels[req.Limit].Name
		rels = rels[0:req.Limit]
		l = int64(len(rels))
	}
	res := &services.ListReleasesResponse{
		Next:  next,
		Count: l,
		Total: total,
	}
	chunks := s.partition(rels[:min(len(rels), int(req.Limit))], maxMsgSize-proto.Size(res))
	for res.Releases = range chunks {
		if err := stream.Send(res); err != nil {
			for range chunks { // drain
			}
			return err
		}
	}
	return nil
}

// partition packs releases into slices upto the capacity cap in bytes.
func (s *ReleaseServer) partition(rels []*release.Release, cap int) <-chan []*release.Release {
	chunks := make(chan []*release.Release, 1)
	go func() {
		var (
			fill = 0 // fill is space available to fill
			size int // size is size of a release
		)
		var chunk []*release.Release
		for _, rls := range rels {
			if size = proto.Size(rls); size+fill > cap {
				// Over-cap, push chunk onto channel to send over gRPC stream
				s.Log("partitioned at %d with %d releases (cap=%d)", fill, len(chunk), cap)
				chunks <- chunk
				// reset partitioning state
				chunk = nil
				fill = 0
			}
			chunk = append(chunk, rls)
			fill += size
		}
		if len(chunk) > 0 {
			// send remaining if any
			chunks <- chunk
		}
		close(chunks)
	}()
	return chunks
}

func filterByNamespace(namespace string, rels []*release.Release) ([]*release.Release, error) {
	matches := []*release.Release{}
	for _, r := range rels {
		if namespace == r.Namespace {
			matches = append(matches, r)
		}
	}
	return matches, nil
}

func filterReleases(filter string, rels []*release.Release) ([]*release.Release, error) {
	preg, err := regexp.Compile(filter)
	if err != nil {
		return rels, err
	}
	matches := []*release.Release{}
	for _, r := range rels {
		if preg.MatchString(r.Name) {
			matches = append(matches, r)
		}
	}
	return matches, nil
}
