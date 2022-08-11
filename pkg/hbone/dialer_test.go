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

package hbone

import (
	"net"
	"testing"
)

func newTCPServer(t testing.TB, data string) string {
	n, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("opened listener on %v", n.Addr().String())
	go func() {
		for {
			c, err := n.Accept()
			if err != nil {
				log.Info(err)
				return
			}
			log.Info("accepted connection")
			c.Write([]byte(data))
			c.Close()
		}
	}()
	t.Cleanup(func() {
		n.Close()
	})
	return n.Addr().String()
}

func TestDialerError(t *testing.T) {
	d := NewDialer(Config{
		ProxyAddress: "127.0.0.10:1", // Random address that should fail to dial
		Headers: map[string][]string{
			"some-addition-metadata": {"test-value"},
		},
		TLS: nil, // No TLS for simplification
	})
	_, err := d.Dial("tcp", "fake")
	if err == nil {
		t.Fatal("expected error, got none.")
	}
}

func TestDialer(t *testing.T) {
	testAddr := newTCPServer(t, "hello")
	proxy := newHBONEServer(t)
	d := NewDialer(Config{
		ProxyAddress: proxy,
		Headers: map[string][]string{
			"some-addition-metadata": {"test-value"},
		},
		TLS: nil, // No TLS for simplification
	})
	send := func() {
		client, err := d.Dial("tcp", testAddr)
		if err != nil {
			t.Fatal(err)
		}
		defer client.Close()

		go func() {
			n, err := client.Write([]byte("hello world"))
			log.Infof("wrote %v/%v", n, err)
		}()

		buf := make([]byte, 8)
		n, err := client.Read(buf)
		if err != nil {
			t.Fatalf("err with %v: %v", n, err)
		}
		if string(buf[:n]) != "hello" {
			t.Fatalf("got unexpected buffer: %v", string(buf[:n]))
		}
		t.Logf("Read %v", string(buf[:n]))
	}
	// Make sure we can create multiple connections
	send()
	send()
}

func newHBONEServer(t *testing.T) string {
	s := NewServer()
	l, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		_ = s.Serve(l)
	}()
	t.Cleanup(func() {
		_ = l.Close()
	})
	return l.Addr().String()
}
