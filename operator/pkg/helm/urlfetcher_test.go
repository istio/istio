// Copyright 2019 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package helm

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

type Server struct {
	srv  *httptest.Server
	root string
}

func (s *Server) start() {
	fd := http.FileServer(http.Dir(s.root))
	http.Handle(s.root+"/", fd)
	s.srv = httptest.NewServer(fd)
}

func (s *Server) URL() string {
	return s.srv.URL
}

func NewServer(root string) *Server {
	srv := &Server{root: root}
	srv.start()
	return srv
}

func (s *Server) moveFiles(origin string) ([]string, error) {
	files, err := filepath.Glob(origin)
	if err != nil {
		return []string{}, err
	}
	tmpFiles := make([]string, len(files))
	for i, file := range files {
		data, err := ioutil.ReadFile(file)
		if err != nil {
			return []string{}, err
		}
		newName := filepath.Join(s.root, filepath.Base(file))
		if err := ioutil.WriteFile(newName, data, 0755); err != nil {
			return []string{}, err
		}
		tmpFiles[i] = newName
	}
	return tmpFiles, nil
}

func TestFetch(t *testing.T) {
	tests := []struct {
		name                    string
		installationPackageName string
	}{
		{
			name:                    "Charts download only",
			installationPackageName: "istio-1.3.0-linux.tar.gz",
		},
	}
	tmp, err := ioutil.TempDir("", InstallationDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)

	server := NewServer(tmp)
	defer server.srv.Close()
	if _, err := server.moveFiles("testdata/*.tar.gz*"); err != nil {
		t.Error(err)
		return
	}
	for _, tt := range tests {
		outdir := filepath.Join(server.root, "testout")
		os.RemoveAll(outdir)
		os.Mkdir(outdir, 0755)
		fq := NewURLFetcher(server.URL()+"/"+tt.installationPackageName, tmp+"/testout")

		err = fq.Fetch()
		if err != nil {
			t.Error(err)
			return
		}

		ef := filepath.Join(fq.destDirRoot, tt.installationPackageName)
		if _, err := os.Stat(ef); err != nil {
			t.Error(err)
			return
		}
	}
}
