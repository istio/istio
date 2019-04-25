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

package storage // import "k8s.io/helm/pkg/storage"

import (
	"fmt"
	"strings"

	rspb "k8s.io/helm/pkg/proto/hapi/release"
	relutil "k8s.io/helm/pkg/releaseutil"
	"k8s.io/helm/pkg/storage/driver"
)

// NoReleasesErr indicates that a given release cannot be found
const NoReleasesErr = "has no deployed releases"

// Storage represents a storage engine for a Release.
type Storage struct {
	driver.Driver

	// MaxHistory specifies the maximum number of historical releases that will
	// be retained, including the most recent release. Values of 0 or less are
	// ignored (meaning no limits are imposed).
	MaxHistory int

	Log func(string, ...interface{})
}

// Get retrieves the release from storage. An error is returned
// if the storage driver failed to fetch the release, or the
// release identified by the key, version pair does not exist.
func (s *Storage) Get(name string, version int32) (*rspb.Release, error) {
	s.Log("getting release %q", makeKey(name, version))
	return s.Driver.Get(makeKey(name, version))
}

// Create creates a new storage entry holding the release. An
// error is returned if the storage driver failed to store the
// release, or a release with identical an key already exists.
func (s *Storage) Create(rls *rspb.Release) error {
	s.Log("creating release %q", makeKey(rls.Name, rls.Version))
	if s.MaxHistory > 0 {
		// Want to make space for one more release.
		s.removeLeastRecent(rls.Name, s.MaxHistory-1)
	}
	return s.Driver.Create(makeKey(rls.Name, rls.Version), rls)
}

// Update update the release in storage. An error is returned if the
// storage backend fails to update the release or if the release
// does not exist.
func (s *Storage) Update(rls *rspb.Release) error {
	s.Log("updating release %q", makeKey(rls.Name, rls.Version))
	return s.Driver.Update(makeKey(rls.Name, rls.Version), rls)
}

// Delete deletes the release from storage. An error is returned if
// the storage backend fails to delete the release or if the release
// does not exist.
func (s *Storage) Delete(name string, version int32) (*rspb.Release, error) {
	s.Log("deleting release %q", makeKey(name, version))
	return s.Driver.Delete(makeKey(name, version))
}

// ListReleases returns all releases from storage. An error is returned if the
// storage backend fails to retrieve the releases.
func (s *Storage) ListReleases() ([]*rspb.Release, error) {
	s.Log("listing all releases in storage")
	return s.Driver.List(func(_ *rspb.Release) bool { return true })
}

// ListDeleted returns all releases with Status == DELETED. An error is returned
// if the storage backend fails to retrieve the releases.
func (s *Storage) ListDeleted() ([]*rspb.Release, error) {
	s.Log("listing deleted releases in storage")
	return s.Driver.List(func(rls *rspb.Release) bool {
		return relutil.StatusFilter(rspb.Status_DELETED).Check(rls)
	})
}

// ListDeployed returns all releases with Status == DEPLOYED. An error is returned
// if the storage backend fails to retrieve the releases.
func (s *Storage) ListDeployed() ([]*rspb.Release, error) {
	s.Log("listing all deployed releases in storage")
	return s.Driver.List(func(rls *rspb.Release) bool {
		return relutil.StatusFilter(rspb.Status_DEPLOYED).Check(rls)
	})
}

// ListFilterAll returns the set of releases satisfying the predicate
// (filter0 && filter1 && ... && filterN), i.e. a Release is included in the results
// if and only if all filters return true.
func (s *Storage) ListFilterAll(fns ...relutil.FilterFunc) ([]*rspb.Release, error) {
	s.Log("listing all releases with filter")
	return s.Driver.List(func(rls *rspb.Release) bool {
		return relutil.All(fns...).Check(rls)
	})
}

// ListFilterAny returns the set of releases satisfying the predicate
// (filter0 || filter1 || ... || filterN), i.e. a Release is included in the results
// if at least one of the filters returns true.
func (s *Storage) ListFilterAny(fns ...relutil.FilterFunc) ([]*rspb.Release, error) {
	s.Log("listing any releases with filter")
	return s.Driver.List(func(rls *rspb.Release) bool {
		return relutil.Any(fns...).Check(rls)
	})
}

// Deployed returns the last deployed release with the provided release name, or
// returns ErrReleaseNotFound if not found.
func (s *Storage) Deployed(name string) (*rspb.Release, error) {
	ls, err := s.DeployedAll(name)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, fmt.Errorf("%q %s", name, NoReleasesErr)
		}
		return nil, err
	}

	if len(ls) == 0 {
		return nil, fmt.Errorf("%q %s", name, NoReleasesErr)
	}

	return ls[0], err
}

// DeployedAll returns all deployed releases with the provided name, or
// returns ErrReleaseNotFound if not found.
func (s *Storage) DeployedAll(name string) ([]*rspb.Release, error) {
	s.Log("getting deployed releases from %q history", name)

	ls, err := s.Driver.Query(map[string]string{
		"NAME":   name,
		"OWNER":  "TILLER",
		"STATUS": "DEPLOYED",
	})
	if err == nil {
		return ls, nil
	}
	if strings.Contains(err.Error(), "not found") {
		return nil, fmt.Errorf("%q %s", name, NoReleasesErr)
	}
	return nil, err
}

// History returns the revision history for the release with the provided name, or
// returns ErrReleaseNotFound if no such release name exists.
func (s *Storage) History(name string) ([]*rspb.Release, error) {
	s.Log("getting release history for %q", name)

	return s.Driver.Query(map[string]string{"NAME": name, "OWNER": "TILLER"})
}

// removeLeastRecent removes items from history until the length number of releases
// does not exceed max.
//
// We allow max to be set explicitly so that calling functions can "make space"
// for the new records they are going to write.
func (s *Storage) removeLeastRecent(name string, max int) error {
	if max < 0 {
		return nil
	}
	h, err := s.History(name)
	if err != nil {
		return err
	}
	if len(h) <= max {
		return nil
	}

	// We want oldest to newest
	relutil.SortByRevision(h)

	lastDeployed, err := s.Deployed(name)
	if err != nil {
		return err
	}

	var toDelete []*rspb.Release
	for _, rel := range h {
		// once we have enough releases to delete to reach the max, stop
		if len(h)-len(toDelete) == max {
			break
		}
		if lastDeployed != nil {
			if rel.GetVersion() != lastDeployed.GetVersion() {
				toDelete = append(toDelete, rel)
			}
		} else {
			toDelete = append(toDelete, rel)
		}
	}

	// Delete as many as possible. In the case of API throughput limitations,
	// multiple invocations of this function will eventually delete them all.
	errors := []error{}
	for _, rel := range toDelete {
		err = s.deleteReleaseVersion(name, rel.GetVersion())
		if err != nil {
			errors = append(errors, err)
		}
	}

	s.Log("Pruned %d record(s) from %s with %d error(s)", len(toDelete), name, len(errors))
	switch c := len(errors); c {
	case 0:
		return nil
	case 1:
		return errors[0]
	default:
		return fmt.Errorf("encountered %d deletion errors. First is: %s", c, errors[0])
	}
}

func (s *Storage) deleteReleaseVersion(name string, version int32) error {
	key := makeKey(name, version)
	_, err := s.Delete(name, version)
	if err != nil {
		s.Log("error pruning %s from release history: %s", key, err)
		return err
	}
	return nil
}

// Last fetches the last revision of the named release.
func (s *Storage) Last(name string) (*rspb.Release, error) {
	s.Log("getting last revision of %q", name)
	h, err := s.History(name)
	if err != nil {
		return nil, err
	}
	if len(h) == 0 {
		return nil, fmt.Errorf("no revision for release %q", name)
	}

	relutil.Reverse(h, relutil.SortByRevision)
	return h[0], nil
}

// makeKey concatenates a release name and version into
// a string with format ```<release_name>#v<version>```.
// This key is used to uniquely identify storage objects.
func makeKey(rlsname string, version int32) string {
	return fmt.Sprintf("%s.v%d", rlsname, version)
}

// Init initializes a new storage backend with the driver d.
// If d is nil, the default in-memory driver is used.
func Init(d driver.Driver) *Storage {
	// default driver is in memory
	if d == nil {
		d = driver.NewMemory()
	}
	return &Storage{
		Driver: d,
		Log:    func(_ string, _ ...interface{}) {},
	}
}
