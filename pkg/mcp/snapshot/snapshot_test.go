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

package snapshot

import (
	"errors"
	"fmt"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/mcp/internal/test"
	"istio.io/istio/pkg/mcp/source"
	"istio.io/istio/pkg/mcp/testing/groups"
)

type fakeSnapshot struct {
	// read-only fields - no locking required
	resources map[string][]*mcp.Resource
	versions  map[string]string
}

func (fs *fakeSnapshot) Resources(collection string) []*mcp.Resource { return fs.resources[collection] }
func (fs *fakeSnapshot) Version(collection string) string            { return fs.versions[collection] }
func (fs *fakeSnapshot) Collections() []string {
	result := make([]string, 0, len(fs.resources))
	for collection := range fs.resources {
		result = append(result, collection)
	}

	return result
}

func (fs *fakeSnapshot) copy() *fakeSnapshot {
	fsCopy := &fakeSnapshot{
		resources: make(map[string][]*mcp.Resource),
		versions:  make(map[string]string),
	}
	for collection, resources := range fs.resources {
		fsCopy.resources[collection] = append(fsCopy.resources[collection], resources...)
		fsCopy.versions[collection] = fs.versions[collection]
	}
	return fsCopy
}

func makeSnapshot(version string) *fakeSnapshot {
	return &fakeSnapshot{
		resources: map[string][]*mcp.Resource{
			test.FakeType0Collection: {test.Type0A[0].Resource},
			test.FakeType1Collection: {test.Type1A[0].Resource},
			test.FakeType2Collection: {test.Type2A[0].Resource},
		},
		versions: map[string]string{
			test.FakeType0Collection: version,
			test.FakeType1Collection: version,
			test.FakeType2Collection: version,
		},
	}
}

var _ Snapshot = &fakeSnapshot{}

// TODO - refactor tests to not rely on sleeps
var (
	asyncResponseTimeout = 200 * time.Millisecond
)

func nextStrVersion(version *int64) string {
	v := atomic.AddInt64(version, 1)
	return strconv.FormatInt(v, 10)

}

// nolint: unparam
func createTestWatch(c source.Watcher, collection, version string, responseC chan *source.WatchResponse, wantResponse, wantCancel bool) (*source.WatchResponse, source.CancelWatchFunc, error) { // nolint: lll
	req := &source.Request{
		Collection:  collection,
		VersionInfo: version,
		SinkNode:    test.Node,
	}

	cancel := c.Watch(req, func(response *source.WatchResponse) {
		responseC <- response
	}, "192.168.1.1:1234")

	if wantResponse {
		select {
		case got := <-responseC:
			return got, nil, nil
		default:
			return nil, nil, errors.New("wanted response, got none")
		}
	} else {
		select {
		case got := <-responseC:
			if got != nil {
				return nil, nil, fmt.Errorf("wanted no response, got %v", got)
			}
		default:
		}
	}

	if wantCancel {
		if cancel == nil {
			return nil, nil, errors.New("wanted cancel() function, got none")
		}
	} else {
		if cancel != nil {
			return nil, nil, fmt.Errorf("wanted no cancel() function, got %p", cancel)
		}
	}

	return nil, cancel, nil
}

func getAsyncResponse(responseC chan *source.WatchResponse) (*source.WatchResponse, bool) {
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

	c := New(groups.DefaultIndexFn)
	c.SetSnapshot(groups.Default, snapshot)

	// verify immediate and async responses are handled independently across types.

	for _, collection := range test.SupportedCollections {
		t.Run(collection, func(t *testing.T) {
			collectionVersion := initVersion
			responseC := make(chan *source.WatchResponse, 1)

			// verify immediate response
			if _, _, err := createTestWatch(c, collection, "", responseC, true, false); err != nil {
				t.Fatalf("CreateWatch() failed: %v", err)
			}

			// verify open watch, i.e. no immediate or async response
			if _, _, err := createTestWatch(c, collection, collectionVersion, responseC, false, true); err != nil {
				t.Fatalf("CreateWatch() failed: %v", err)
			}

			if gotResponse, _ := getAsyncResponse(responseC); gotResponse != nil {
				t.Fatalf("open watch failed: received premature response: %v", gotResponse)
			}

			// verify async response
			snapshot = snapshot.copy()
			watchVersion := collectionVersion
			collectionVersion = nextStrVersion(&versionInt)
			snapshot.versions[collection] = collectionVersion
			c.SetSnapshot(groups.Default, snapshot)

			if gotResponse, _ := getAsyncResponse(responseC); gotResponse != nil {
				wantResponse := &source.WatchResponse{
					Collection: collection,
					Version:    collectionVersion,
					Resources:  snapshot.Resources(collection),
					Request: &source.Request{
						Collection:  collection,
						SinkNode:    test.Node,
						VersionInfo: watchVersion,
					},
				}
				if diff := cmp.Diff(gotResponse, wantResponse, cmpopts.IgnoreUnexported(source.Request{})); diff != "" {
					t.Fatalf("received bad WatchResponse: \n got %v \nwant %v \ndiff %v", gotResponse, wantResponse, diff)
				}
			} else {
				t.Fatalf("watch response channel did not produce response after %v", asyncResponseTimeout)
			}

			// verify lack of immediate response after async response.
			if _, _, err := createTestWatch(c, collection, collectionVersion, responseC, false, true); err != nil {
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

	c := New(groups.DefaultIndexFn)
	c.SetSnapshot(groups.Default, snapshot)

	for _, collection := range test.SupportedCollections {
		t.Run(collection, func(t *testing.T) {
			collectionVersion := initVersion
			responseC := make(chan *source.WatchResponse, 1)

			// verify immediate response
			if _, _, err := createTestWatch(c, collection, "", responseC, true, false); err != nil {
				t.Fatalf("CreateWatch failed: immediate response not received: %v", err)
			}

			// verify watch can be canceled
			_, cancel, err := createTestWatch(c, collection, collectionVersion, responseC, false, true)
			if err != nil {
				t.Fatalf("CreateWatche failed: %v", err)
			}
			cancel()

			// verify no response after watch is canceled
			snapshot = snapshot.copy()
			collectionVersion = nextStrVersion(&versionInt)
			snapshot.versions[collection] = collectionVersion
			c.SetSnapshot(groups.Default, snapshot)

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

	c := New(groups.DefaultIndexFn)
	c.SetSnapshot(groups.Default, snapshot)

	for _, collection := range test.SupportedCollections {
		t.Run(collection, func(t *testing.T) {
			responseC := make(chan *source.WatchResponse, 1)

			// verify no immediate response if snapshot is cleared.
			c.ClearSnapshot(groups.Default)
			if _, _, err := createTestWatch(c, collection, "", responseC, false, true); err != nil {
				t.Fatalf("CreateWatch() failed: %v", err)
			}

			// verify async response after new snapshot is added
			snapshot = snapshot.copy()
			typeVersion := nextStrVersion(&versionInt)
			snapshot.versions[collection] = typeVersion
			c.SetSnapshot(groups.Default, snapshot)

			if gotResponse, _ := getAsyncResponse(responseC); gotResponse != nil {
				wantResponse := &source.WatchResponse{
					Collection: collection,
					Version:    typeVersion,
					Resources:  snapshot.Resources(collection),
					Request: &source.Request{
						Collection: collection,
						SinkNode:   test.Node,
					},
				}
				if diff := cmp.Diff(gotResponse, wantResponse, cmpopts.IgnoreUnexported(source.Request{})); diff != "" {
					t.Fatalf("received bad WatchResponse: \n got %v \nwant %+v \ndiff %v", gotResponse, wantResponse, diff)
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

	c := New(groups.DefaultIndexFn)

	for _, collection := range test.SupportedCollections {
		t.Run(collection, func(t *testing.T) {
			responseC := make(chan *source.WatchResponse, 1)

			if _, _, err := createTestWatch(c, collection, "", responseC, false, true); err != nil {
				t.Fatalf("CreateWatch() failed: %v", err)
			}

			if status := c.Status(groups.Default); status == nil {
				t.Fatal("no status found")
			}

			c.ClearStatus(groups.Default)

			// verify that ClearStatus() cancels the open watch and
			// that any subsequent snapshot is not delivered.
			snapshot = snapshot.copy()
			typeVersion := nextStrVersion(&versionInt)
			snapshot.versions[collection] = typeVersion
			c.SetSnapshot(groups.Default, snapshot)

			if gotResponse, timeout := getAsyncResponse(responseC); gotResponse != nil {
				t.Fatalf("open watch failed: received unexpected response: %v", gotResponse)
			} else if timeout {
				t.Fatal("open watch was not canceled on ClearStatus()")
			}

			c.ClearSnapshot(groups.Default)
		})
	}
}
