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

package helmpath

import (
	"fmt"
	"os"
	"path/filepath"
)

// Home describes the location of a CLI configuration.
//
// This helper builds paths relative to a Helm Home directory.
type Home string

// String returns Home as a string.
//
// Implements fmt.Stringer.
func (h Home) String() string {
	return os.ExpandEnv(string(h))
}

// Path returns Home with elements appended.
func (h Home) Path(elem ...string) string {
	p := []string{h.String()}
	p = append(p, elem...)
	return filepath.Join(p...)
}

// Repository returns the path to the local repository.
func (h Home) Repository() string {
	return h.Path("repository")
}

// RepositoryFile returns the path to the repositories.yaml file.
func (h Home) RepositoryFile() string {
	return h.Path("repository", "repositories.yaml")
}

// Cache returns the path to the local cache.
func (h Home) Cache() string {
	return h.Path("repository", "cache")
}

// CacheIndex returns the path to an index for the given named repository.
func (h Home) CacheIndex(name string) string {
	target := fmt.Sprintf("%s-index.yaml", name)
	return h.Path("repository", "cache", target)
}

// Starters returns the path to the Helm starter packs.
func (h Home) Starters() string {
	return h.Path("starters")
}

// LocalRepository returns the location to the local repo.
//
// The local repo is the one used by 'helm serve'
//
// If additional path elements are passed, they are appended to the returned path.
func (h Home) LocalRepository(elem ...string) string {
	p := []string{"repository", "local"}
	p = append(p, elem...)
	return h.Path(p...)
}

// Plugins returns the path to the plugins directory.
func (h Home) Plugins() string {
	return h.Path("plugins")
}

// Archive returns the path to download chart archives.
func (h Home) Archive() string {
	return h.Path("cache", "archive")
}

// TLSCaCert returns the path to fetch the CA certificate.
func (h Home) TLSCaCert() string {
	return h.Path("ca.pem")
}

// TLSCert returns the path to fetch the client certificate.
func (h Home) TLSCert() string {
	return h.Path("cert.pem")
}

// TLSKey returns the path to fetch the client public key.
func (h Home) TLSKey() string {
	return h.Path("key.pem")
}
