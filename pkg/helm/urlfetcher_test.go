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
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
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
		verify                  bool
		verifyFail              bool
	}{
		{
			name:                    "Charts download only",
			installationPackageName: "istio-installer-1.3.0.tar.gz",
			verify:                  false,
		},
		{
			name:                    "Charts download and verify",
			installationPackageName: "istio-installer-1.3.0.tar.gz",
			verify:                  true,
		},
		{
			name:                    "Charts download but verification fail",
			installationPackageName: "istio-installer-1.3.0.tar.gz",
			verify:                  true,
			verifyFail:              true,
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
	for _, test := range tests {
		outdir := filepath.Join(server.root, "testout")
		os.RemoveAll(outdir)
		os.Mkdir(outdir, 0755)
		fq, err := NewURLFetcher(server.URL()+"/"+test.installationPackageName, tmp+"/testout")
		if err != nil {
			t.Error(err)
			return
		}
		if test.verify {
			fq.verify = test.verify
			savedShaF, err := fq.fetchSha()
			if err != nil {
				t.Error(err)
				return
			}
			if test.verifyFail {
				f, _ := os.OpenFile(savedShaF, os.O_APPEND|os.O_WRONLY, os.ModeAppend)
				_, err := f.Write([]byte{115, 111})
				if err != nil {
					fmt.Println("failed to modify sha file")
				}
			}
			err = fq.fetchChart(savedShaF)
			if test.verifyFail {
				assert.NotNil(t, err)
				continue
			}
			if err != nil {
				t.Error(err)
				return
			}
		} else {
			err = fq.fetchChart("")
			if err != nil {
				t.Error(err)
				return
			}
		}
		ef := filepath.Join(fq.destDir, test.installationPackageName)
		if _, err := os.Stat(ef); err != nil {
			t.Error(err)
			return
		}
	}
}
