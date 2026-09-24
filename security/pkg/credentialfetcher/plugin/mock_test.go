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

package plugin

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
)

const fakeTokenPrefix = "fake-token-"

// MetadataServer mocks GCE metadata server.
type MetadataServer struct {
	server *httptest.Server

	numGetTokenCall int
	credential      string
	mutex           sync.RWMutex
}

// StartMetadataServer starts a mock GCE metadata server.
func StartMetadataServer() (*MetadataServer, error) {
	ms := &MetadataServer{}
	httpServer := httptest.NewServer(http.HandlerFunc(ms.getToken))
	ms.server = httpServer
	url, err := url.Parse(httpServer.URL)
	if err != nil {
		return nil, fmt.Errorf("parse URL failed: %v", err)
	}
	if err := os.Setenv("GCE_METADATA_HOST", url.Host); err != nil {
		fmt.Printf("Error running os.Setenv: %v", err)
		ms.Stop()
		return nil, err
	}
	return ms, nil
}

func (ms *MetadataServer) setToken(t string) {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()
	ms.credential = t
}

// NumGetTokenCall returns the number of token fetching request.
func (ms *MetadataServer) NumGetTokenCall() int {
	ms.mutex.RLock()
	defer ms.mutex.RUnlock()

	return ms.numGetTokenCall
}

// Reset resets members to default values.
func (ms *MetadataServer) Reset() {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()

	ms.numGetTokenCall = 0
	ms.credential = ""
}

func (ms *MetadataServer) getToken(w http.ResponseWriter, req *http.Request) {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()

	ms.numGetTokenCall++
	token := fmt.Sprintf("%s%d", fakeTokenPrefix, ms.numGetTokenCall)
	if ms.credential != "" {
		token = ms.credential
	}
	fmt.Fprint(w, token)
}

func (ms *MetadataServer) Stop() {
	ms.server.Close()
}
