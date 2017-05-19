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
package config

import (
	"net/url"
	"strings"
	"testing"

	"github.com/alicebob/miniredis"
)

func TestRedisStore(t *testing.T) {
	testStore(t, func() *kvMgr {
		s, err := miniredis.Run()
		if err != nil {
			t.Fatalf("unable to start mini redis: %v", err)
		}
		rs, err := newRedisStore(&url.URL{Scheme: "redis", Host: s.Addr(), Path: ""})
		if err != nil {
			s.Close()
			t.Fatalf("unable to connect to the mini redis server: %v", err)
		}
		return &kvMgr{rs, func() { s.Close() }}
	})
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
		rs, err := newRedisStore(u)
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
		rs, err := newRedisStore(u)
		if err != nil {
			t.Errorf("failed to connect to the DB: %v", err)
			continue
		}
		resp := rs.client.Cmd("GET", "dbnum")
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
		rs, err := newRedisStore(u)
		if err == nil {
			t.Errorf("expected to fail, but successfully connected to %s", ustr)
			rs.Close()
		} else if !strings.Contains(err.Error(), "dbNum") {
			t.Errorf("unexpected error \"%v\", message should contain \"dbNum\", target %s", err, ustr)
		}
	}
}
