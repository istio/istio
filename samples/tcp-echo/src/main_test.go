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

package main

import (
	"bufio"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// TestTcpEchoServer tests the behavior of the TCP Echo Server.
func TestTcpEchoServer(t *testing.T) {
	// set up test parameters
	prefix := "hello"
	request := "world"
	want := prefix + " " + request

	// start the TCP Echo Server
	os.Args = []string{"main", "9000,9001", prefix}
	go main()

	// wait for the TCP Echo Server to start
	time.Sleep(2 * time.Second)

	for _, addr := range []string{":9000", ":9001"} {
		// connect to the TCP Echo Server
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatalf("couldn't connect to the server: %v", err)
		}
		defer conn.Close()

		// test the TCP Echo Server output
		if _, err := conn.Write([]byte(request + "\n")); err != nil {
			t.Fatalf("couldn't send request: %v", err)
		} else {
			reader := bufio.NewReader(conn)
			if response, err := reader.ReadBytes(byte('\n')); err != nil {
				t.Fatalf("couldn't read server response: %v", err)
			} else if !strings.HasPrefix(string(response), want) {
				t.Errorf("output doesn't match, wanted: %s, got: %s", want, response)
			}
		}
	}
}
