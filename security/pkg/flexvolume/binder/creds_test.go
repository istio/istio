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

package binder

import (
	"context"
	"io/ioutil"
	"net"
	"os"
	"reflect"
	"testing"
)

// testUnixAddr uses ioutil.TempFile to get a name that is unique.
// It also uses /tmp directory in case it is prohibited to create UNIX
// sockets in TMPDIR.
func testUnixAddr(prefix string) string {
	f, err := ioutil.TempFile("", prefix)
	if err != nil {
		panic(err)
	}
	addr := f.Name()
	f.Close()       //nolint: errcheck
	os.Remove(addr) //nolint: errcheck
	return addr
}

func newLocalServer(prefix string) (*localServer, error) {
	ln, err := net.Listen("unix", testUnixAddr(prefix))
	if err != nil {
		return nil, err
	}
	return &localServer{Listener: ln}, nil
}

type localServer struct {
	net.Listener
}

func (ls *localServer) teardown() error {
	if ls.Listener != nil {
		address := ls.Listener.Addr().String()
		ls.Listener.Close() //nolint: errcheck
		ls.Listener = nil
		os.Remove(address) //nolint: errcheck
	}
	return nil
}

func getBaseWls() (*workloadStore, *localServer, error) {
	wls := &workloadStore{creds: make(map[string]workload)}
	testls, e := newLocalServer("tmp")
	if e != nil {
		return nil, nil, e
	}
	wls.creds["q"] = workload{uid: "q", listener: testls}
	wls.creds["r"] = workload{uid: "r", listener: testls}
	wls.creds["s"] = workload{uid: "s", listener: testls}
	return wls, testls, nil
}

func TestClientHandshakeError(t *testing.T) {
	wls := newWorkloadStore()
	_, w := net.Pipe()
	defer w.Close()
	_, _, err := wls.ClientHandshake(context.Background(), "foo", w)
	if err == nil {
		t.Errorf("Expected an error. Received none.")
	}
}

func TestServerHandshake(t *testing.T) {
	ls, err := newLocalServer("test-listener")
	if err != nil {
		t.Fatal(err)
	}
	defer ls.teardown()

	wls, lsTmp, eWls := getBaseWls()
	if eWls != nil {
		t.Fatal(eWls)
	}
	defer lsTmp.teardown()

	wls.creds["t"] = workload{uid: "t", listener: ls}

	c, err := net.Dial(ls.Listener.Addr().Network(), ls.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	serverConn, err := ls.Listener.Accept()
	if err != nil {
		t.Fatal(err.Error())
	}
	defer serverConn.Close()

	_, _, e := wls.ServerHandshake(serverConn)
	if e != nil {
		t.Errorf("Expected conn %v to match listener %v (%s)", c.RemoteAddr(), ls.Addr(), e.Error())
	}

}

func TestServerHandshakeNoMatch(t *testing.T) {
	ls, err := newLocalServer("test-listener")
	if err != nil {
		t.Fatal(err)
	}
	defer ls.teardown()

	wls, lsTmp, eWls := getBaseWls()
	if eWls != nil {
		t.Fatal(eWls)
	}
	defer lsTmp.teardown()

	c, err := net.Dial(ls.Listener.Addr().Network(), ls.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	serverConn, err := ls.Listener.Accept()
	if err != nil {
		t.Fatal(err.Error())
	}
	defer serverConn.Close()

	_, _, e := wls.ServerHandshake(serverConn)
	if e == nil {
		t.Errorf("Expected an Error but got none")
	}
}

func TestInfo(t *testing.T) {
	wls := newWorkloadStore()

	info := wls.Info()
	if info.SecurityProtocol != authType {
		t.Errorf("Expected %v got %v", authType, info.SecurityProtocol)
	}
}

func TestClone(t *testing.T) {
	wls, lsTmp, eWls := getBaseWls()
	if eWls != nil {
		t.Fatal(eWls)
	}
	defer lsTmp.teardown()

	gotWls := wls.Clone()
	switch w := gotWls.(type) {
	case *workloadStore:
		if !reflect.DeepEqual(wls.creds, w.creds) {
			t.Errorf("Expected %v got %v", wls.creds, w.creds)
		} //
	default:
		t.Errorf("Expected type to be workloadStore")
	}
}

func TestOverrideServerName(t *testing.T) {
	wls, lsTmp, eWls := getBaseWls()
	if eWls != nil {
		t.Fatal(eWls)
	}
	defer lsTmp.teardown()

	if wls.OverrideServerName("foo") != nil {
		t.Errorf("Expected nil")
	}
}

func TestGetAll(t *testing.T) {
	wls, lsTmp, eWls := getBaseWls()
	if eWls != nil {
		t.Fatal(eWls)
	}
	defer lsTmp.teardown()

	w := wls.getAll()
	if len(w) != len(wls.creds) {
		t.Errorf("Expected length %d got %d (%v  %v)", len(wls.creds), len(w), wls.creds, w)
	}
	// cannot depend on the response's to be ordered.
	for _, cred := range w {
		if wls.get(cred.uid).uid != cred.uid {
			t.Errorf("Expected to find %v in %v", cred.uid, w)
		}
	}
}

func TestGet(t *testing.T) {
	wls, lsTmp, eWls := getBaseWls()
	if eWls != nil {
		t.Fatal(eWls)
	}
	defer lsTmp.teardown()

	wl1 := wls.get("p")
	if wl1.uid == "p" {
		t.Errorf("Expected to not find")
	}

	wl2 := wls.get("s")
	if wl2.uid != "s" {
		t.Errorf("Expected to find")
	}
}

func TestDelete(t *testing.T) {
	wls, lsTmp, eWls := getBaseWls()
	if eWls != nil {
		t.Fatal(eWls)
	}
	defer lsTmp.teardown()

	wls.delete("t")
	if _, ok := wls.creds["t"]; ok == true {
		t.Errorf("Expected to delete")
	}
}

func TestStore(t *testing.T) {
	wls, lsTmp, eWls := getBaseWls()
	if eWls != nil {
		t.Fatal(eWls)
	}
	defer lsTmp.teardown()

	wl := workload{uid: "j"}
	wls.store("j", wl)
	if _, ok := wls.creds["j"]; !ok {
		t.Errorf("Expected a store")
	}
}

type localAddr struct {
	network string
	address string
}

func (a localAddr) Network() string {
	return a.network
}

func (a localAddr) String() string {
	return a.address
}

func TestAddrEqual(t *testing.T) {
	var tests = []struct {
		this    localAddr
		that    localAddr
		areSame bool
	}{
		{localAddr{"unix", "/foo/sock"},
			localAddr{"unix", "/foo/sock"},
			true},
		{localAddr{"tcp", "1.1.1.1:1000"},
			localAddr{"tcp", "1.1.1.1:1000"},
			true},
		{localAddr{"udp", "1.1.1.100:100"},
			localAddr{"udp", "1.1.1.100:100"},
			true},
		{localAddr{"foo", "bar"},
			localAddr{"foo", "notbar"},
			false},
		{localAddr{"foo", "same"}, localAddr{"bar", "same"},
			false},
	}

	for _, tt := range tests {
		result := addrEqual(tt.this, tt.that)
		if result != tt.areSame {
			var q string
			if tt.areSame {
				q = "same"
			} else {
				q = "not same"
			}
			t.Errorf("Expected %v and %v to be %s", tt.this, tt.that, q)
		}
	}
}
