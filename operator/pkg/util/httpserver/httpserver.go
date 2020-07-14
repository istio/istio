// Copyright Istio Authors
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

package httpserver

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"path/filepath"
)

// NewServer creates a new Server and returns a pointer to it.
func NewServer(root string) *Server {
	srv := &Server{Root: root}
	srv.start()
	return srv
}

// Server is an in-memory HTTP server for testing.
type Server struct {
	Srv  *httptest.Server
	Root string
}

// URL returns the root URL served by s.
func (s *Server) URL() string {
	return s.Srv.URL
}

// Close closes the server.
func (s *Server) Close() {
	s.Srv.Close()
}

func (s *Server) start() {
	fd := http.FileServer(http.Dir(s.Root))
	http.Handle(s.Root+"/", fd)
	s.Srv = httptest.NewServer(fd)
}

func (s *Server) MoveFiles(origin string) ([]string, error) {
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
		newName := filepath.Join(s.Root, filepath.Base(file))
		if err := ioutil.WriteFile(newName, data, 0755); err != nil {
			return []string{}, err
		}
		tmpFiles[i] = newName
	}
	return tmpFiles, nil
}
