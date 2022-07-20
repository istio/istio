package ipset

import (
	"net"
	"runtime"
	"testing"

	"k8s.io/apimachinery/pkg/util/rand"
)

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

func TestXxx(t *testing.T) {
	// skip if not linux
	if runtime.GOOS != "linux" {
		t.Skipf("skipping test on %s. this test only works on linux", runtime.GOOS)
	}

	// skip if not net admin

	// create a set with a random name
	ipset := &IPSet{
		Name: "test" + rand.String(5),
	}
	ipset.CreateSet()
	defer ipset.DestroySet()

	err := ipset.AddIP(net.ParseIP("1.2.3.4").To4(), "foo")
	if err != nil {
		t.Fatalf("failed to add ipset %s: %v", ipset.Name, err)
	}
	res, err := ipset.List()
	if err != nil {
		t.Fatalf("failed to list ipset %s: %v", ipset.Name, err)
	}
	if len(res) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(res))
	}

	// test clearing using a comment
	err = ipset.AddIP(net.ParseIP("1.2.3.3").To4(), "bar")
	if err != nil {
		t.Fatalf("failed to add ipset %s: %v", ipset.Name, err)
	}

	err = ipset.AddIP(net.ParseIP("1.2.3.2").To4(), "foo")
	if err != nil {
		t.Fatalf("failed to add ipset %s: %v", ipset.Name, err)
	}

	res, err = ipset.List()
	if err != nil {
		t.Fatalf("failed to list ipset %s: %v", ipset.Name, err)
	}
	if len(res) != 3 {
		t.Fatalf("expected 3 entry, got %d", len(res))
	}
	// This is only supported in the kernel module from revision 2 or 4, so comments may be ignored.
	err = ipset.ClearEntriesWithComment("foo")
	if err != nil {
		t.Fatalf("failed to add ipset %s: %v", ipset.Name, err)
	}

	// one entry left
	res, err = ipset.List()
	if err != nil {
		t.Fatalf("failed to list ipset %s: %v", ipset.Name, err)
	}
	// Since not all kernels support ipset entry comments, check to see if the comment is blank
	// before marking the test as failed.
	if len(res) != 1 {
		if len(res) == 3 && res[0].Comment != "" {
			t.Fatalf("expected 1 entry, got %d (%+v)", len(res), res)
		}
	}
}
