// Copyright 2017 Istio Authors
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
package redis

import (
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis"

	"istio.io/mixer/pkg/config/store"
)

func TestRedisStore(t *testing.T) {
	store.RunStoreTest(t, func() *store.TestManager {
		s, err := miniredis.Run()
		if err != nil {
			t.Fatalf("unable to start mini redis: %v", err)
		}
		rs, err := newStore(&url.URL{Scheme: "redis", Host: s.Addr(), Path: ""})
		if err != nil {
			s.Close()
			t.Fatalf("unable to connect to the mini redis server: %v", err)
		}
		return store.NewTestManager(rs, nil)
	})
}

func TestNewStore(t *testing.T) {
	// make sure store.NewStore can create a redis instance.
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start mini redis: %v", err)
	}
	defer s.Close()
	r := store.NewRegistry(Register)
	_, err = r.NewStore("redis://" + s.Addr() + "/1")
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
}

func TestAuth(t *testing.T) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("unable to start mini redis: %v", err)
	}
	s.RequireAuth("passwd")
	defer s.Close()

	for _, tt := range []struct {
		url     string
		success bool
	}{
		{"redis://" + s.Addr() + "/", false},
		{"redis://:passwd@" + s.Addr() + "/", true},
		{"redis://passwd@" + s.Addr() + "/", false},
		{"redis://:pass@" + s.Addr() + "/", false},
		{"redis://:passwd@" + s.Addr() + "/1", true},
		{"redis://" + s.Addr() + "/1", false},
	} {
		u, err := url.Parse(tt.url)
		if err != nil {
			t.Errorf("failed to parse URL %s: %v", tt.url, err)
			continue
		}
		rs, err := newStore(u)
		if tt.success && err != nil {
			t.Errorf("expected to success to establish the connection, but failed with %v", err)
		} else if !tt.success && err == nil {
			t.Errorf("expected to fail but successfully connected with %s", tt.url)
		}
		if rs != nil {
			rs.Close()
		}
	}
}

func TestDBNum(t *testing.T) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("unable to start mini redis: %v", err)
	}
	defer s.Close()

	// Setup DBs; redis does not provide the way to obtain the current DB number,
	// so here sets the value per DB.
	for i := 0; i < 16; i++ {
		db := s.DB(i)
		_, err := db.Incr("dbnum", i)
		if err != nil {
			t.Fatalf("failed to set up the db %d: %v", i, err)
		}
	}

	for _, tt := range []struct {
		url   string
		dbNum int
	}{
		{"redis://" + s.Addr() + "/", 0},
		{"redis://" + s.Addr() + "/1", 1},
		{"redis://" + s.Addr() + "/10", 10},
		{"redis://" + s.Addr() + "/010", 10},
	} {
		u, err := url.Parse(tt.url)
		if err != nil {
			t.Errorf("failed to parse URL %s: %v", tt.url, err)
			continue
		}
		rs, err := newStore(u)
		if err != nil {
			t.Errorf("failed to connect to the DB: %v", err)
			continue
		}
		resp := rs.(*redisStore).client.Cmd("GET", "dbnum")
		if resp.Err != nil {
			t.Errorf("can't find the dbnum in the connection: %v", resp.Err)
		} else if num, err := resp.Int(); err != nil {
			t.Errorf("dbnum is not an integer: %v", err)
		} else if num != tt.dbNum {
			t.Errorf("expected to connect %d but connected to %d", tt.dbNum, num)
		}
		rs.Close()
	}
}

func TestInvalidDBNum(t *testing.T) {
	for _, ustr := range []string{
		"redis://localhost:6379/-1",
		"redis://localhost:6379/deadbeef",
		"redis://localhost:6379/123abc",
		"redis://localhost:6379/0x10",
	} {
		u, err := url.Parse(ustr)
		if err != nil {
			t.Errorf("failed to parse URL %s: %v", ustr, err)
			continue
		}
		rs, err := newStore(u)
		if err == nil {
			t.Errorf("expected to fail, but successfully connected to %s", ustr)
			rs.Close()
		} else if !strings.Contains(err.Error(), "dbNum") {
			t.Errorf("unexpected error \"%v\", message should contain \"dbNum\", target %s", err, ustr)
		}
	}
}

func TestRedisConfigValues(t *testing.T) {
	for _, tt := range []struct {
		value string
		avail bool
	}{
		{"AKE", true},
		{"AK", false},
		{"AE", true},
		{"Eg$", true},
		{"Kg$", false},
		{"Eg", false},
		{"", false},
	} {
		avail := doesConfigSupportsChangeNotifications(tt.value)
		if avail != tt.avail {
			t.Errorf("config value %s expected %v got %v", tt.value, tt.avail, avail)
		}
	}
}

type testStoreListener struct {
	lastCalledIndex int
}

func (l *testStoreListener) NotifyStoreChanged(idx int) {
	l.lastCalledIndex = idx
}

// This test case does not run by default, since this requires a real redis server;
// miniredis does not support pub/sub nor keyspace notifications. To run the test
// code, you need to manually specify the redis:// URL through REDIS_SERVER environment
// variable when running the test, like
//   % bazel test pkg/config/... --action_env=REDIS_SERVER=redis://localhost:6379/
//
// TODO: introduce some mocks to allow running this without a real server.
func TestNotifications(t *testing.T) {
	const wait = time.Millisecond * 10

	redisServer := os.Getenv("REDIS_SERVER")
	if redisServer == "" {
		return
	}
	u, err := url.Parse(redisServer)
	if err != nil {
		t.Fatalf("failed to parse %s: %v", redisServer, err)
	}

	// cstore keeps the 'controller' client.
	cstore, err := newStore(u)
	if err != nil {
		t.Fatalf("failed: %v", err)
	}
	// controller controls the target redis database outside of the testing redisstore
	// instance, for verifying that it receives updates independently.
	controller := cstore.(*redisStore).client
	defer func() {
		// clear the test data in the db.
		controller.Cmd("FLUSHDB")
		cstore.Close()
	}()

	// Update the config in the DB to support the change updates.
	if resp := controller.Cmd("CONFIG", "SET", keyspaceEventsConfigKey, "AKE"); resp.Err != nil {
		t.Fatalf("failed to configure: %v", resp.Err)
	}

	// rs is the actual redisstore instance we want to test.
	s, err := newStore(u)
	if err != nil {
		t.Fatalf("failed to create redisstore instance: %v", err)
	}
	defer s.Close()
	rs := s.(*redisStore)
	if rs.subscriber == nil {
		t.Fatalf("store does not start subscribing changes")
	}
	if i := rs.index(0); i != 0 {
		t.Fatalf("initial index: expected 0, got %v", i)
	}

	l := &testStoreListener{}
	s.(store.ChangeNotifier).RegisterListener(l)

	controller.Cmd("SET", "foo", "foo")
	controller.Cmd("SET", "bar", "bar")
	controller.Cmd("GET", "foo")

	time.Sleep(wait)
	if l.lastCalledIndex != 2 {
		t.Fatalf("index expected 2, got %d", l.lastCalledIndex)
	}

	cr := s.(store.ChangeLogReader)
	c, err := cr.Read(0)
	if err != nil {
		t.Fatalf("failed to read changes: %v", err)
	}
	if len(c) != 2 {
		t.Fatalf("unexpected changes: %v", c)
	}

	controller.Cmd("DEL", "foo")
	time.Sleep(wait)
	if l.lastCalledIndex != 3 {
		t.Fatalf("index expected 3, got %d", l.lastCalledIndex)
	}
	c, err = cr.Read(2)
	if err != nil {
		t.Fatalf("failed to read changes: %v", err)
	}
	if !reflect.DeepEqual(c, []store.Change{{Key: "foo", Type: store.Delete, Index: 3}}) {
		t.Fatalf("unexpected changes: %v", c)
	}

	rs.Close()

	time.Sleep(wait)
	controller.Cmd("SET", "buzz", "buzz")
	time.Sleep(wait)
	if l.lastCalledIndex != 3 {
		t.Fatalf("index expected 3, got %d", l.lastCalledIndex)
	}
}
