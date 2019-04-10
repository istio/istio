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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"testing"
	"time"

	"istio.io/istio/pkg/mcp/source"
	"istio.io/istio/pkg/mcp/testing/groups"

	"github.com/gogo/protobuf/types"

	"istio.io/istio/pkg/ctrlz"
	"istio.io/istio/pkg/ctrlz/fw"
	"istio.io/istio/pkg/mcp/snapshot"
	mcptest "istio.io/istio/pkg/mcp/testing"
)

const testK8sCollection = "k8s/core/v1/nodes"

func TestConfigZ(t *testing.T) {
	s, err := mcptest.NewServer(0, []source.CollectionOptions{{Name: testK8sCollection}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	b := snapshot.NewInMemoryBuilder()
	b.SetVersion(testK8sCollection, "23")
	err = b.SetEntry(testK8sCollection, "foo", "v0", time.Time{}, nil, nil, &types.Empty{})
	if err != nil {
		t.Fatalf("Setting an entry should not have failed: %v", err)
	}

	s.Cache.SetSnapshot(groups.Default, b.Build())

	o := ctrlz.DefaultOptions()
	cz, _ := ctrlz.Run(o, []fw.Topic{CreateTopic(s.Cache)})
	defer cz.Close()

	baseURL := fmt.Sprintf("http://%s:%d", o.Address, o.Port)

	t.Run("configj with 1 request", func(tt *testing.T) { testConfigJWithOneRequest(tt, baseURL) })
}

func testConfigJWithOneRequest(t *testing.T, baseURL string) {
	t.Helper()

	data := request(t, baseURL+"/configj/")

	m := make(map[string]interface{})
	err := json.Unmarshal([]byte(data), &m)
	if err != nil {
		t.Fatalf("Should have unmarshalled json: %v", err)
	}

	exists := false
	for _, group := range m["Groups"].([]interface{}) {
		if group.(string) == groups.Default {
			exists = true
			break
		}
	}
	if !exists {
		t.Fatalf("Should have contained metadata: %v", data)
	}

	exists = false
	for _, collection := range m["Snapshots"].([]interface{}) {
		if collection.(map[string]interface{})["Collection"].(string) == testK8sCollection {
			exists = true
			break
		}
	}
	if !exists {
		t.Fatalf("Should have contained supported collections: %v", data)
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
