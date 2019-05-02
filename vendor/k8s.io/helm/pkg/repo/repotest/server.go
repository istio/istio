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

package repotest

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	"github.com/ghodss/yaml"

	"k8s.io/helm/pkg/helm/helmpath"
	"k8s.io/helm/pkg/repo"
)

// NewTempServer creates a server inside of a temp dir.
//
// If the passed in string is not "", it will be treated as a shell glob, and files
// will be copied from that path to the server's docroot.
//
// The caller is responsible for destroying the temp directory as well as stopping
// the server.
func NewTempServer(glob string) (*Server, helmpath.Home, error) {
	tdir, err := ioutil.TempDir("", "helm-repotest-")
	tdirh := helmpath.Home(tdir)
	if err != nil {
		return nil, tdirh, err
	}
	srv := NewServer(tdir)

	if glob != "" {
		if _, err := srv.CopyCharts(glob); err != nil {
			srv.Stop()
			return srv, tdirh, err
		}
	}

	return srv, tdirh, nil
}

// NewServer creates a repository server for testing.
//
// docroot should be a temp dir managed by the caller.
//
// This will start the server, serving files off of the docroot.
//
// Use CopyCharts to move charts into the repository and then index them
// for service.
func NewServer(docroot string) *Server {
	root, err := filepath.Abs(docroot)
	if err != nil {
		panic(err)
	}
	srv := &Server{
		docroot: root,
	}
	srv.start()
	// Add the testing repository as the only repo.
	if err := setTestingRepository(helmpath.Home(docroot), "test", srv.URL()); err != nil {
		panic(err)
	}
	return srv
}

// Server is an implementation of a repository server for testing.
type Server struct {
	docroot string
	srv     *httptest.Server
}

// Root gets the docroot for the server.
func (s *Server) Root() string {
	return s.docroot
}

// CopyCharts takes a glob expression and copies those charts to the server root.
func (s *Server) CopyCharts(origin string) ([]string, error) {
	files, err := filepath.Glob(origin)
	if err != nil {
		return []string{}, err
	}
	copied := make([]string, len(files))
	for i, f := range files {
		base := filepath.Base(f)
		newname := filepath.Join(s.docroot, base)
		data, err := ioutil.ReadFile(f)
		if err != nil {
			return []string{}, err
		}
		if err := ioutil.WriteFile(newname, data, 0755); err != nil {
			return []string{}, err
		}
		copied[i] = newname
	}

	err = s.CreateIndex()
	return copied, err
}

// CreateIndex will read docroot and generate an index.yaml file.
func (s *Server) CreateIndex() error {
	// generate the index
	index, err := repo.IndexDirectory(s.docroot, s.URL())
	if err != nil {
		return err
	}

	d, err := yaml.Marshal(index)
	if err != nil {
		return err
	}

	ifile := filepath.Join(s.docroot, "index.yaml")
	return ioutil.WriteFile(ifile, d, 0755)
}

func (s *Server) start() {
	s.srv = httptest.NewServer(http.FileServer(http.Dir(s.docroot)))
}

// Stop stops the server and closes all connections.
//
// It should be called explicitly.
func (s *Server) Stop() {
	s.srv.Close()
}

// URL returns the URL of the server.
//
// Example:
//	http://localhost:1776
func (s *Server) URL() string {
	return s.srv.URL
}

// LinkIndices links the index created with CreateIndex and makes a symbolic link to the repositories/cache directory.
//
// This makes it possible to simulate a local cache of a repository.
func (s *Server) LinkIndices() error {
	destfile := "test-index.yaml"
	// Link the index.yaml file to the
	lstart := filepath.Join(s.docroot, "index.yaml")
	ldest := filepath.Join(s.docroot, "repository/cache", destfile)
	return os.Symlink(lstart, ldest)
}

// setTestingRepository sets up a testing repository.yaml with only the given name/URL.
func setTestingRepository(home helmpath.Home, name, url string) error {
	r := repo.NewRepoFile()
	r.Add(&repo.Entry{
		Name:  name,
		URL:   url,
		Cache: home.CacheIndex(name),
	})
	os.MkdirAll(filepath.Join(home.Repository(), name), 0755)
	return r.WriteFile(home.RepositoryFile(), 0644)
}
