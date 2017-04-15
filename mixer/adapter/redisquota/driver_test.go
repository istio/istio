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

package redisquota

import (
	"sync"
	"testing"

	"github.com/alicebob/miniredis"
)

func TestPool(t *testing.T) {
	// Start miniredis as a mock redis for unit test.
	s, err := miniredis.Run()
	if err != nil {
		t.Errorf("Unable to start mini redis: %v", err)
	}
	defer s.Close()

	pool, err := newConnPool(s.Addr(), "tcp", 10)
	if err != nil {
		t.Errorf("Unable to create aspect: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 1; i++ {
		wg.Add(1)
		go func() {
			for i := 0; i < 1; i++ {
				conn, err := pool.get()
				if err != nil {
					t.Errorf("Unable to get connection from pool: %v", err)
				}
				pool.put(conn)
			}
			wg.Done()
		}()
	}
	wg.Wait()
	pool.empty()
}

func TestNoConnection(t *testing.T) {
	pool, err := newConnPool("localhost:6379", "tcp", 10)
	if err == nil {
		t.Errorf("Expecting error, got success")
	}
	if pool != nil {
		t.Errorf("Expecting failure, got success")
	}

}

func TestPipe(t *testing.T) {
	s, err := miniredis.Run()
	if err != nil {
		t.Errorf("Unable to start mini redis: %v", err)
	}
	defer s.Close()
	err = s.Set("key1", "0")
	if err != nil {
		t.Errorf("Unable to set value: %v", err)
	}

	pool, err := newConnPool(s.Addr(), "tcp", 10)
	if err != nil {
		t.Errorf("Unable to create aspect: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		conn, err := pool.get()
		if err != nil {
			t.Errorf("Unable to get connection: %v", err)
		}

		conn.pipeAppend("INCRBY", "key1", "10")
		if conn.pending != 1 {
			t.Errorf("Unable to pipe command: %v", err)
		}

		resp, err := conn.pipeResponse()
		if err != nil {
			t.Errorf("Unable to get response: %v", err)
		}

		result, err := resp.int()
		if err != nil {
			t.Errorf("Unable to get integer: %v", err)
		}
		if result != 10 {
			t.Errorf("Wrong response: %v", err)
		}

		if conn.pending != 0 {
			t.Errorf("Unable to delete command: %v", err)
		}

		resp, _ = conn.pipeResponse()
		result, err = resp.int()
		if err == nil {
			t.Errorf("Expecting error, got success")
		}
		if result != 0 {
			t.Errorf("Unable to get response command: %v", err)
		}

		result, err = conn.getIntResp()
		if err == nil {
			t.Errorf("Expecting error, got success")
		}
		if result != 0 {
			t.Errorf("Unable to get response command: %v", err)
		}

		pool.put(conn)
		wg.Done()
	}()
	wg.Wait()
	pool.empty()
}
