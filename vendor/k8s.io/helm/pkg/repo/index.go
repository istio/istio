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

package repo

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Masterminds/semver"
	"github.com/ghodss/yaml"

	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/proto/hapi/chart"
	"k8s.io/helm/pkg/provenance"
	"k8s.io/helm/pkg/urlutil"
)

var indexPath = "index.yaml"

// APIVersionV1 is the v1 API version for index and repository files.
const APIVersionV1 = "v1"

var (
	// ErrNoAPIVersion indicates that an API version was not specified.
	ErrNoAPIVersion = errors.New("no API version specified")
	// ErrNoChartVersion indicates that a chart with the given version is not found.
	ErrNoChartVersion = errors.New("no chart version found")
	// ErrNoChartName indicates that a chart with the given name is not found.
	ErrNoChartName = errors.New("no chart name found")
)

// ChartVersions is a list of versioned chart references.
// Implements a sorter on Version.
type ChartVersions []*ChartVersion

// Len returns the length.
func (c ChartVersions) Len() int { return len(c) }

// Swap swaps the position of two items in the versions slice.
func (c ChartVersions) Swap(i, j int) { c[i], c[j] = c[j], c[i] }

// Less returns true if the version of entry a is less than the version of entry b.
func (c ChartVersions) Less(a, b int) bool {
	// Failed parse pushes to the back.
	i, err := semver.NewVersion(c[a].Version)
	if err != nil {
		return true
	}
	j, err := semver.NewVersion(c[b].Version)
	if err != nil {
		return false
	}
	return i.LessThan(j)
}

// IndexFile represents the index file in a chart repository
type IndexFile struct {
	APIVersion string                   `json:"apiVersion"`
	Generated  time.Time                `json:"generated"`
	Entries    map[string]ChartVersions `json:"entries"`
	PublicKeys []string                 `json:"publicKeys,omitempty"`
}

// NewIndexFile initializes an index.
func NewIndexFile() *IndexFile {
	return &IndexFile{
		APIVersion: APIVersionV1,
		Generated:  time.Now(),
		Entries:    map[string]ChartVersions{},
		PublicKeys: []string{},
	}
}

// LoadIndexFile takes a file at the given path and returns an IndexFile object
func LoadIndexFile(path string) (*IndexFile, error) {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return loadIndex(b)
}

// Add adds a file to the index
// This can leave the index in an unsorted state
func (i IndexFile) Add(md *chart.Metadata, filename, baseURL, digest string) {
	u := filename
	if baseURL != "" {
		var err error
		_, file := filepath.Split(filename)
		u, err = urlutil.URLJoin(baseURL, file)
		if err != nil {
			u = path.Join(baseURL, file)
		}
	}
	cr := &ChartVersion{
		URLs:     []string{u},
		Metadata: md,
		Digest:   digest,
		Created:  time.Now(),
	}
	if ee, ok := i.Entries[md.Name]; !ok {
		i.Entries[md.Name] = ChartVersions{cr}
	} else {
		i.Entries[md.Name] = append(ee, cr)
	}
}

// Has returns true if the index has an entry for a chart with the given name and exact version.
func (i IndexFile) Has(name, version string) bool {
	_, err := i.Get(name, version)
	return err == nil
}

// SortEntries sorts the entries by version in descending order.
//
// In canonical form, the individual version records should be sorted so that
// the most recent release for every version is in the 0th slot in the
// Entries.ChartVersions array. That way, tooling can predict the newest
// version without needing to parse SemVers.
func (i IndexFile) SortEntries() {
	for _, versions := range i.Entries {
		sort.Sort(sort.Reverse(versions))
	}
}

// Get returns the ChartVersion for the given name.
//
// If version is empty, this will return the chart with the highest version.
func (i IndexFile) Get(name, version string) (*ChartVersion, error) {
	vs, ok := i.Entries[name]
	if !ok {
		return nil, ErrNoChartName
	}
	if len(vs) == 0 {
		return nil, ErrNoChartVersion
	}

	var constraint *semver.Constraints
	if len(version) == 0 {
		constraint, _ = semver.NewConstraint("*")
	} else {
		var err error
		constraint, err = semver.NewConstraint(version)
		if err != nil {
			return nil, err
		}
	}

	// when customer input exact version, check whether have exact match one first
	if len(version) != 0 {
		for _, ver := range vs {
			if version == ver.Version {
				return ver, nil
			}
		}
	}

	for _, ver := range vs {
		test, err := semver.NewVersion(ver.Version)
		if err != nil {
			continue
		}

		if constraint.Check(test) {
			return ver, nil
		}
	}
	return nil, fmt.Errorf("No chart version found for %s-%s", name, version)
}

// WriteFile writes an index file to the given destination path.
//
// The mode on the file is set to 'mode'.
func (i IndexFile) WriteFile(dest string, mode os.FileMode) error {
	b, err := yaml.Marshal(i)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(dest, b, mode)
}

// Merge merges the given index file into this index.
//
// This merges by name and version.
//
// If one of the entries in the given index does _not_ already exist, it is added.
// In all other cases, the existing record is preserved.
//
// This can leave the index in an unsorted state
func (i *IndexFile) Merge(f *IndexFile) {
	for _, cvs := range f.Entries {
		for _, cv := range cvs {
			if !i.Has(cv.Name, cv.Version) {
				e := i.Entries[cv.Name]
				i.Entries[cv.Name] = append(e, cv)
			}
		}
	}
}

// Need both JSON and YAML annotations until we get rid of gopkg.in/yaml.v2

// ChartVersion represents a chart entry in the IndexFile
type ChartVersion struct {
	*chart.Metadata
	URLs    []string  `json:"urls"`
	Created time.Time `json:"created,omitempty"`
	Removed bool      `json:"removed,omitempty"`
	Digest  string    `json:"digest,omitempty"`
}

// IndexDirectory reads a (flat) directory and generates an index.
//
// It indexes only charts that have been packaged (*.tgz).
//
// The index returned will be in an unsorted state
func IndexDirectory(dir, baseURL string) (*IndexFile, error) {
	archives, err := filepath.Glob(filepath.Join(dir, "*.tgz"))
	if err != nil {
		return nil, err
	}
	moreArchives, err := filepath.Glob(filepath.Join(dir, "**/*.tgz"))
	if err != nil {
		return nil, err
	}
	archives = append(archives, moreArchives...)

	index := NewIndexFile()
	for _, arch := range archives {
		fname, err := filepath.Rel(dir, arch)
		if err != nil {
			return index, err
		}

		var parentDir string
		parentDir, fname = filepath.Split(fname)
		// filepath.Split appends an extra slash to the end of parentDir. We want to strip that out.
		parentDir = strings.TrimSuffix(parentDir, string(os.PathSeparator))
		parentURL, err := urlutil.URLJoin(baseURL, parentDir)
		if err != nil {
			parentURL = path.Join(baseURL, parentDir)
		}

		c, err := chartutil.Load(arch)
		if err != nil {
			// Assume this is not a chart.
			continue
		}
		hash, err := provenance.DigestFile(arch)
		if err != nil {
			return index, err
		}
		index.Add(c.Metadata, fname, parentURL, hash)
	}
	return index, nil
}

// loadIndex loads an index file and does minimal validity checking.
//
// This will fail if API Version is not set (ErrNoAPIVersion) or if the unmarshal fails.
func loadIndex(data []byte) (*IndexFile, error) {
	i := &IndexFile{}
	if err := yaml.Unmarshal(data, i); err != nil {
		return i, err
	}
	i.SortEntries()
	if i.APIVersion == "" {
		// When we leave Beta, we should remove legacy support and just
		// return this error:
		//return i, ErrNoAPIVersion
		return loadUnversionedIndex(data)
	}
	return i, nil
}

// unversionedEntry represents a deprecated pre-Alpha.5 format.
//
// This will be removed prior to v2.0.0
type unversionedEntry struct {
	Checksum  string          `json:"checksum"`
	URL       string          `json:"url"`
	Chartfile *chart.Metadata `json:"chartfile"`
}

// loadUnversionedIndex loads a pre-Alpha.5 index.yaml file.
//
// This format is deprecated. This function will be removed prior to v2.0.0.
func loadUnversionedIndex(data []byte) (*IndexFile, error) {
	fmt.Fprintln(os.Stderr, "WARNING: Deprecated index file format. Try 'helm repo update'")
	i := map[string]unversionedEntry{}

	// This gets around an error in the YAML parser. Instead of parsing as YAML,
	// we convert to JSON, and then decode again.
	var err error
	data, err = yaml.YAMLToJSON(data)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &i); err != nil {
		return nil, err
	}

	if len(i) == 0 {
		return nil, ErrNoAPIVersion
	}
	ni := NewIndexFile()
	for n, item := range i {
		if item.Chartfile == nil || item.Chartfile.Name == "" {
			parts := strings.Split(n, "-")
			ver := ""
			if len(parts) > 1 {
				ver = strings.TrimSuffix(parts[1], ".tgz")
			}
			item.Chartfile = &chart.Metadata{Name: parts[0], Version: ver}
		}
		ni.Add(item.Chartfile, item.URL, "", item.Checksum)
	}
	return ni, nil
}
