// Copyright 2018 Istio Authors
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

package snapshot

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/mcp/server"
)

// fake protobuf types that implements required resource interface

type fakeTypeBase struct{ Info string }

func (f fakeTypeBase) Reset()                   {}
func (f fakeTypeBase) String() string           { return f.Info }
func (f fakeTypeBase) ProtoMessage()            {}
func (f fakeTypeBase) Marshal() ([]byte, error) { return []byte(f.Info), nil }

type fakeType0 struct{ fakeTypeBase }
type fakeType1 struct{ fakeTypeBase }
type fakeType2 struct{ fakeTypeBase }

const (
	typePrefix      = "type.googleapis.com/"
	fakeType0Prefix = "istio.io.galley.pkg.mcp.server.fakeType0"
	fakeType1Prefix = "istio.io.galley.pkg.mcp.server.fakeType1"
	fakeType2Prefix = "istio.io.galley.pkg.mcp.server.fakeType2"

	fakeType0TypeURL = typePrefix + fakeType0Prefix
	fakeType1TypeURL = typePrefix + fakeType1Prefix
	fakeType2TypeURL = typePrefix + fakeType2Prefix
)

func mustMarshalAny(pb proto.Message) *types.Any {
	a, err := types.MarshalAny(pb)
	if err != nil {
		panic(err.Error())
	}
	return a
}

func init() {
	proto.RegisterType((*fakeType0)(nil), fakeType0Prefix)
	proto.RegisterType((*fakeType1)(nil), fakeType1Prefix)
	proto.RegisterType((*fakeType2)(nil), fakeType2Prefix)

	fakeEnvelope0 = &mcp.Envelope{
		Metadata: &mcp.Metadata{Name: "f0"},
		Resource: mustMarshalAny(&fakeType0{fakeTypeBase{"f0"}}),
	}
	fakeEnvelope1 = &mcp.Envelope{
		Metadata: &mcp.Metadata{Name: "f1"},
		Resource: mustMarshalAny(&fakeType1{fakeTypeBase{"f1"}}),
	}
	fakeEnvelope2 = &mcp.Envelope{
		Metadata: &mcp.Metadata{Name: "f2"},
		Resource: mustMarshalAny(&fakeType2{fakeTypeBase{"f2"}}),
	}
}

type fakeSnapshot struct {
	// read-only fields - no locking required
	envelopes map[string][]*mcp.Envelope
	versions  map[string]string
}

func (fs *fakeSnapshot) Resources(typ string) []*mcp.Envelope { return fs.envelopes[typ] }
func (fs *fakeSnapshot) Version(typ string) string            { return fs.versions[typ] }

func (fs *fakeSnapshot) copy() *fakeSnapshot {
	fsCopy := &fakeSnapshot{
		envelopes: make(map[string][]*mcp.Envelope),
		versions:  make(map[string]string),
	}
	for typeURL, envelopes := range fs.envelopes {
		fsCopy.envelopes[typeURL] = append(fsCopy.envelopes[typeURL], envelopes...)
		fsCopy.versions[typeURL] = fs.versions[typeURL]
	}
	return fsCopy
}

func makeSnapshot(version string) *fakeSnapshot {
	return &fakeSnapshot{
		envelopes: map[string][]*mcp.Envelope{
			fakeType0TypeURL: {fakeEnvelope0},
			fakeType1TypeURL: {fakeEnvelope1},
			fakeType2TypeURL: {fakeEnvelope2},
		},
		versions: map[string]string{
			fakeType0TypeURL: version,
			fakeType1TypeURL: version,
			fakeType2TypeURL: version,
		},
	}
}

var _ Snapshot = &fakeSnapshot{}

var (
	node = &core.Node{
		Id:      "test-id",
		Cluster: "test-cluster",
	}

	key = node.Id

	fakeEnvelope0 *mcp.Envelope
	fakeEnvelope1 *mcp.Envelope
	fakeEnvelope2 *mcp.Envelope

	WatchResponseTypes = []string{
		fakeType0TypeURL,
		fakeType1TypeURL,
		fakeType2TypeURL,
	}
)

// TODO - refactor tests to not rely on sleeps
var (
	asyncResponseTimeout = 200 * time.Millisecond
)

func nextStrVersion(version *int64) string {
	v := atomic.AddInt64(version, 1)
	return strconv.FormatInt(v, 10)

}

func createTestWatch(c *Cache, typeURL, version string, responseC chan *server.WatchResponse, wantResponse, wantCancel bool) (*server.WatchResponse, server.CancelWatchFunc, error) { // nolint: lll
	req := &mcp.MeshConfigRequest{
		TypeUrl:     typeURL,
		VersionInfo: version,
		Client: &mcp.Client{
			Id: key,
		},
	}
	got, cancel := c.Watch(req, responseC)
	if wantResponse {
		if got == nil {
			return nil, nil, errors.New("wanted response, got none")
		}
	} else {
		if got != nil {
			return nil, nil, fmt.Errorf("wanted no response, got %v", got)
		}
	}
	if wantCancel {
		if cancel == nil {
			return nil, nil, errors.New("wanted cancel() function, got none")
		}
	} else {
		if cancel != nil {
			return nil, nil, fmt.Errorf("wanted no cancel() function, got %v", cancel)
		}
	}
	return got, cancel, nil
}

func getAsyncResponse(responseC chan *server.WatchResponse) (*server.WatchResponse, bool) {
	select {
	case got, more := <-responseC:
		if !more {
			return nil, false
		}
		return got, false
	case <-time.After(asyncResponseTimeout):
		return nil, true
	}
}

func TestCreateWatch(t *testing.T) {
	var versionInt int64 // atomic
	initVersion := nextStrVersion(&versionInt)
	snapshot := makeSnapshot(initVersion)

	c := New()
	c.SetSnapshot(key, snapshot)

	// verify immediate and async responses are handled independently across types.
	for _, typeURL := range WatchResponseTypes {
		t.Run(typeURL, func(t *testing.T) {
			typeVersion := initVersion
			responseC := make(chan *server.WatchResponse, 1)

			// verify immediate response
			if _, _, err := createTestWatch(c, typeURL, "", responseC, true, false); err != nil {
				t.Fatalf("CreateWatch() failed: %v", err)
			}

			// verify open watch, i.e. no immediate or async response
			if _, _, err := createTestWatch(c, typeURL, typeVersion, responseC, false, true); err != nil {
				t.Fatalf("CreateWatch() failed: %v", err)
			}
			if gotResponse, _ := getAsyncResponse(responseC); gotResponse != nil {
				t.Fatalf("open watch failed: received premature response: %v", gotResponse)
			}

			// verify async response
			snapshot = snapshot.copy()
			typeVersion = nextStrVersion(&versionInt)
			snapshot.versions[typeURL] = typeVersion
			c.SetSnapshot(key, snapshot)

			if gotResponse, _ := getAsyncResponse(responseC); gotResponse != nil {
				wantResponse := &server.WatchResponse{
					TypeURL:   typeURL,
					Version:   typeVersion,
					Envelopes: snapshot.Resources(typeURL),
				}
				if !reflect.DeepEqual(gotResponse, wantResponse) {
					t.Fatalf("received bad WatchResponse: got %v wantResponse %v", gotResponse, wantResponse)
				}
			} else {
				t.Fatalf("watch response channel did not produce response after %v", asyncResponseTimeout)
			}

			// verify lack of immediate response after async response.
			if _, _, err := createTestWatch(c, typeURL, typeVersion, responseC, false, true); err != nil {
				t.Fatalf("CreateWatch() failed after receiving prior response: %v", err)
			}

			if gotResponse, _ := getAsyncResponse(responseC); gotResponse != nil {
				t.Fatalf("open watch failed after receiving prior response: premature response: %v", gotResponse)
			}
		})
	}
}

func TestWatchCancel(t *testing.T) {
	var versionInt int64 // atomic
	initVersion := nextStrVersion(&versionInt)
	snapshot := makeSnapshot(initVersion)

	c := New()
	c.SetSnapshot(key, snapshot)

	for _, typeURL := range WatchResponseTypes {
		t.Run(typeURL, func(t *testing.T) {
			typeVersion := initVersion
			responseC := make(chan *server.WatchResponse, 1)

			// verify immediate response
			if _, _, err := createTestWatch(c, typeURL, "", responseC, true, false); err != nil {
				t.Fatalf("CreateWatch failed: immediate response not received: %v", err)
			}

			// verify watch can be canceled
			_, cancel, err := createTestWatch(c, typeURL, typeVersion, responseC, false, true)
			if err != nil {
				t.Fatalf("CreateWatche failed: %v", err)
			}
			cancel()

			// verify no response after watch is canceled
			snapshot = snapshot.copy()
			typeVersion = nextStrVersion(&versionInt)
			snapshot.versions[typeURL] = typeVersion
			c.SetSnapshot(key, snapshot)

			if gotResponse, _ := getAsyncResponse(responseC); gotResponse != nil {
				t.Fatalf("open watch failed: received premature response: %v", gotResponse)
			}
		})
	}
}

func TestClearSnapshot(t *testing.T) {
	var versionInt int64 // atomic
	initVersion := nextStrVersion(&versionInt)
	snapshot := makeSnapshot(initVersion)

	c := New()
	c.SetSnapshot(key, snapshot)

	for _, typeURL := range WatchResponseTypes {
		t.Run(typeURL, func(t *testing.T) {
			responseC := make(chan *server.WatchResponse, 1)

			// verify no immediate response if snapshot is cleared.
			c.ClearSnapshot(key)
			if _, _, err := createTestWatch(c, typeURL, "", responseC, false, true); err != nil {
				t.Fatalf("CreateWatch() failed: %v", err)
			}

			// verify async response after new snapshot is added
			snapshot = snapshot.copy()
			typeVersion := nextStrVersion(&versionInt)
			snapshot.versions[typeURL] = typeVersion
			c.SetSnapshot(key, snapshot)

			if gotResponse, _ := getAsyncResponse(responseC); gotResponse != nil {
				wantResponse := &server.WatchResponse{
					TypeURL:   typeURL,
					Version:   typeVersion,
					Envelopes: snapshot.Resources(typeURL),
				}
				if !reflect.DeepEqual(gotResponse, wantResponse) {
					t.Fatalf("received bad WatchResponse: got %v wantResponse %v", gotResponse, wantResponse)
				}
			} else {
				t.Fatalf("watch response channel did not produce response after %v", asyncResponseTimeout)
			}
		})
	}
}

func TestClearStatus(t *testing.T) {
	var versionInt int64 // atomic
	initVersion := nextStrVersion(&versionInt)
	snapshot := makeSnapshot(initVersion)

	c := New()

	for _, typeURL := range WatchResponseTypes {
		t.Run(typeURL, func(t *testing.T) {
			responseC := make(chan *server.WatchResponse, 1)

			if _, _, err := createTestWatch(c, typeURL, "", responseC, false, true); err != nil {
				t.Fatalf("CreateWatch() failed: %v", err)
			}

			c.ClearStatus(key)

			// verify that ClearStatus() cancels the open watch and
			// that any subsequent snapshot is not delivered.
			snapshot = snapshot.copy()
			typeVersion := nextStrVersion(&versionInt)
			snapshot.versions[typeURL] = typeVersion
			c.SetSnapshot(key, snapshot)

			if gotResponse, timeout := getAsyncResponse(responseC); gotResponse != nil {
				t.Fatalf("open watch failed: received unexpected response: %v", gotResponse)
			} else if timeout {
				t.Fatal("open watch was not canceled on ClearStatus()")
			}

			c.ClearSnapshot(key)
		})
	}
}
