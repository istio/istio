//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package configz

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/mcp/source"
	"istio.io/istio/pkg/mcp/testing/monitoring"

	"github.com/gogo/protobuf/types"
	"google.golang.org/grpc"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/ctrlz"
	"istio.io/istio/pkg/ctrlz/fw"
	"istio.io/istio/pkg/mcp/client"
	"istio.io/istio/pkg/mcp/sink"
	"istio.io/istio/pkg/mcp/snapshot"
	mcptest "istio.io/istio/pkg/mcp/testing"
)

type updater struct {
}

func (u *updater) Apply(c *sink.Change) error {
	return nil
}

const testEmptyCollection = "/test/collection/empty"

func TestConfigZ(t *testing.T) {
	s, err := mcptest.NewServer(0, []source.CollectionOptions{{Name: testEmptyCollection}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	cc, err := grpc.Dial(fmt.Sprintf("localhost:%d", s.Port), grpc.WithInsecure())
	if err != nil {
		t.Fatal(err)
	}

	u := &updater{}
	clnt := mcp.NewAggregatedMeshConfigServiceClient(cc)

	options := &sink.Options{
		CollectionOptions: []sink.CollectionOptions{{Name: testEmptyCollection}},
		Updater:           u,
		ID:                snapshot.DefaultGroup,
		Metadata:          map[string]string{"foo": "bar"},
		Reporter:          monitoring.NewInMemoryStatsContext(),
	}
	cl := client.New(clnt, options)

	ctx, cancel := context.WithCancel(context.Background())
	go cl.Run(ctx)
	defer cancel()

	o := ctrlz.DefaultOptions()
	o.Port = 0
	cz, err := ctrlz.Run(o, []fw.Topic{CreateTopic(cl)})
	if err != nil {
		t.Fatalf("ctrlz.Run: %v", err)
	}
	defer cz.Close()

	baseURL := "http://" + cz.Addr().String()

	// wait for client to make first watch request
	for {
		if status := s.Cache.Status(snapshot.DefaultGroup); status != nil {
			if status.Watches() > 0 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Run("configz with initial requests", func(tt *testing.T) { testConfigZWithNoRequest(tt, baseURL) })

	b := snapshot.NewInMemoryBuilder()
	b.SetVersion(testEmptyCollection, "23")
	err = b.SetEntry(testEmptyCollection, "foo", "v0", time.Time{}, nil, nil, &types.Empty{})
	if err != nil {
		t.Fatalf("Setting an entry should not have failed: %v", err)
	}
	prevSnapshotTime := time.Now()
	s.Cache.SetSnapshot(snapshot.DefaultGroup, b.Build())

	// wait for client to ACK the pushed snapshot
	for {
		if status := s.Cache.Status(snapshot.DefaultGroup); status != nil {
			if status.LastWatchRequestTime().After(prevSnapshotTime) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Run("configz with 2 request", func(tt *testing.T) { testConfigZWithOneRequest(tt, baseURL) })

	t.Run("configj with 2 request", func(tt *testing.T) { testConfigJWithOneRequest(tt, baseURL) })
}

func testConfigZWithNoRequest(t *testing.T, baseURL string) {
	// First, test configz, with no recent requests.
	data := request(t, baseURL+"/configz")
	if !strings.Contains(data, snapshot.DefaultGroup) {
		t.Fatalf("Node id should have been displayed: %q", data)
	}
	if !strings.Contains(data, "foo") || !strings.Contains(data, "bar") {
		t.Fatalf("Metadata should have been displayed: %q", data)
	}
	if !strings.Contains(data, testEmptyCollection) {
		t.Fatalf("Collections should have been displayed: %q", data)
	}
	want := 2
	if got := strings.Count(data, testEmptyCollection); got != want {
		t.Fatalf("Only the collection and initial ACK request should have been displayed: got %v want %v: %q",
			got, want, data)
	}
}

func testConfigZWithOneRequest(t *testing.T, baseURL string) {
	t.Helper()

	for i := 0; i < 10; i++ {
		data := request(t, baseURL+"/configz")
		if strings.Count(data, testEmptyCollection) != 3 {
			time.Sleep(time.Millisecond * 100)
			continue
		}
		return
	}
	t.Fatal("Both collections, the initial request, and a recent ACK request should have been displayed")
}

func testConfigJWithOneRequest(t *testing.T, baseURL string) {
	t.Helper()

	data := request(t, baseURL+"/configj/")

	m := make(map[string]interface{})
	err := json.Unmarshal([]byte(data), &m)
	if err != nil {
		t.Fatalf("Should have unmarshalled json: %v", err)
	}

	if m["ID"] != snapshot.DefaultGroup {
		t.Fatalf("Should have contained id: %v", data)
	}

	if m["Metadata"].(map[string]interface{})["foo"] != "bar" {
		t.Fatalf("Should have contained metadata: %v", data)
	}

	if len(m["Collections"].([]interface{})) != 1 ||
		m["Collections"].([]interface{})[0].(string) != testEmptyCollection {
		t.Fatalf("Should have contained supported collections: %v", data)
	}

	if len(m["LatestRequests"].([]interface{})) != 2 {
		t.Fatalf("There should have been an initial request and subsequent ACK LatestRequest entry: %v", data)
	}
}

func request(t *testing.T, url string) string {
	var e error
	for i := 1; i < 10; i++ {
		resp, err := http.Get(url)
		if err != nil {
			e = err
			time.Sleep(time.Millisecond * 100)
			continue
		}
		defer func() { _ = resp.Body.Close() }()
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			e = err
			time.Sleep(time.Millisecond * 100)
			continue
		}

		return string(body)
	}

	t.Fatalf("Unable to complete get request: url='%s', last err='%v'", url, e)
	return ""
}
