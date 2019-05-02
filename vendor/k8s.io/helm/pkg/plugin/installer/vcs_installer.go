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

package installer // import "k8s.io/helm/pkg/plugin/installer"

import (
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/Masterminds/semver"
	"github.com/Masterminds/vcs"

	"k8s.io/helm/pkg/helm/helmpath"
	"k8s.io/helm/pkg/plugin/cache"
)

// VCSInstaller installs plugins from remote a repository.
type VCSInstaller struct {
	Repo    vcs.Repo
	Version string
	base
}

func existingVCSRepo(location string, home helmpath.Home) (Installer, error) {
	repo, err := vcs.NewRepo("", location)
	if err != nil {
		return nil, err
	}
	i := &VCSInstaller{
		Repo: repo,
		base: newBase(repo.Remote(), home),
	}
	return i, err
}

// NewVCSInstaller creates a new VCSInstaller.
func NewVCSInstaller(source, version string, home helmpath.Home) (*VCSInstaller, error) {
	key, err := cache.Key(source)
	if err != nil {
		return nil, err
	}
	cachedpath := home.Path("cache", "plugins", key)
	repo, err := vcs.NewRepo(source, cachedpath)
	if err != nil {
		return nil, err
	}
	i := &VCSInstaller{
		Repo:    repo,
		Version: version,
		base:    newBase(source, home),
	}
	return i, err
}

// Install clones a remote repository and creates a symlink to the plugin directory in HELM_HOME.
//
// Implements Installer.
func (i *VCSInstaller) Install() error {
	if err := i.sync(i.Repo); err != nil {
		return err
	}

	ref, err := i.solveVersion(i.Repo)
	if err != nil {
		return err
	}
	if ref != "" {
		if err := i.setVersion(i.Repo, ref); err != nil {
			return err
		}
	}

	if !isPlugin(i.Repo.LocalPath()) {
		return ErrMissingMetadata
	}

	return i.link(i.Repo.LocalPath())
}

// Update updates a remote repository
func (i *VCSInstaller) Update() error {
	debug("updating %s", i.Repo.Remote())
	if i.Repo.IsDirty() {
		return errors.New("plugin repo was modified")
	}
	if err := i.Repo.Update(); err != nil {
		return err
	}
	if !isPlugin(i.Repo.LocalPath()) {
		return ErrMissingMetadata
	}
	return nil
}

func (i *VCSInstaller) solveVersion(repo vcs.Repo) (string, error) {
	if i.Version == "" {
		return "", nil
	}

	if repo.IsReference(i.Version) {
		return i.Version, nil
	}

	// Create the constraint first to make sure it's valid before
	// working on the repo.
	constraint, err := semver.NewConstraint(i.Version)
	if err != nil {
		return "", err
	}

	// Get the tags
	refs, err := repo.Tags()
	if err != nil {
		return "", err
	}
	debug("found refs: %s", refs)

	// Convert and filter the list to semver.Version instances
	semvers := getSemVers(refs)

	// Sort semver list
	sort.Sort(sort.Reverse(semver.Collection(semvers)))
	for _, v := range semvers {
		if constraint.Check(v) {
			// If the constrint passes get the original reference
			ver := v.Original()
			debug("setting to %s", ver)
			return ver, nil
		}
	}

	return "", fmt.Errorf("requested version %q does not exist for plugin %q", i.Version, i.Repo.Remote())
}

// setVersion attempts to checkout the version
func (i *VCSInstaller) setVersion(repo vcs.Repo, ref string) error {
	debug("setting version to %q", i.Version)
	return repo.UpdateVersion(ref)
}

// sync will clone or update a remote repo.
func (i *VCSInstaller) sync(repo vcs.Repo) error {
	if _, err := os.Stat(repo.LocalPath()); os.IsNotExist(err) {
		debug("cloning %s to %s", repo.Remote(), repo.LocalPath())
		return repo.Get()
	}
	debug("updating %s", repo.Remote())
	return repo.Update()
}

// Filter a list of versions to only included semantic versions. The response
// is a mapping of the original version to the semantic version.
func getSemVers(refs []string) []*semver.Version {
	var sv []*semver.Version
	for _, r := range refs {
		if v, err := semver.NewVersion(r); err == nil {
			sv = append(sv, v)
		}
	}
	return sv
}
