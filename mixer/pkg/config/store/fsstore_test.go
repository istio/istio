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

package store

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ghodss/yaml"
)

const testingCheckDuration = time.Millisecond * 5

func getTempFSStore2() (*fsStore, string) {
	fsroot, _ := ioutil.TempDir("/tmp/", "fsStore-")
	s := newFsStore(fsroot).(*fsStore)
	s.checkDuration = testingCheckDuration
	return s, fsroot
}

func cleanupRootIfOK(t *testing.T, fsroot string) {
	t.Helper()
	if t.Failed() {
		t.Errorf("Test failed. The data remains at %s", fsroot)
		return
	}
	if err := os.RemoveAll(fsroot); err != nil {
		t.Errorf("Failed on cleanup %s: %v", fsroot, err)
	}
}

func waitFor(wch <-chan BackendEvent, ct ChangeType, key Key) {
	for ev := range wch {
		if ev.Key == key && ev.Type == ct {
			return
		}
	}
}

func write(fsroot string, k Key, data map[string]interface{}) error {
	path := filepath.Join(fsroot, k.Kind, k.Namespace, k.Name+".yaml")
	bytes, err := yaml.Marshal(&BackEndResource{Kind: k.Kind, Metadata: ResourceMeta{Namespace: k.Namespace, Name: k.Name}, Spec: data})
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return ioutil.WriteFile(path, bytes, 0644)
}

func TestFSStore2(t *testing.T) {
	s, fsroot := getTempFSStore2()
	defer cleanupRootIfOK(t, fsroot)
	const ns = "istio-mixer-testing"
	if err := s.Init([]string{"Handler", "Action"}); err != nil {
		t.Fatal(err.Error())
	}
	defer s.Stop()

	wch, err := s.Watch()
	if err != nil {
		t.Fatal(err.Error())
	}
	k := Key{Kind: "Handler", Namespace: ns, Name: "default"}
	if _, err = s.Get(k); err != ErrNotFound {
		t.Errorf("Got %v, Want ErrNotFound", err)
	}
	h := map[string]interface{}{"name": "default", "adapter": "noop"}
	if err = write(fsroot, k, h); err != nil {
		t.Fatalf("Got %v, Want nil", err)
	}
	waitFor(wch, Update, k)
	h2, err := s.Get(k)
	if err != nil {
		t.Errorf("Got %v, Want nil", err)
	}
	if !reflect.DeepEqual(h, h2.Spec) {
		t.Errorf("Got %+v, Want %+v", h2.Spec, h)
	}
	want := map[Key]*BackEndResource{k: h2}
	if lst := s.List(); !reflect.DeepEqual(lst, want) {
		t.Errorf("Got %+v, Want %+v", lst, want)
	}
	h["adapter"] = "noop2"
	if err = write(fsroot, k, h); err != nil {
		t.Fatalf("Got %v, Want nil", err)
	}
	waitFor(wch, Update, k)
	if h2, err = s.Get(k); err != nil {
		t.Errorf("Got %v, Want nil", err)
	}
	if !reflect.DeepEqual(h, h2.Spec) {
		t.Errorf("Got %+v, Want %+v", h2.Spec, h)
	}
	if err = os.Remove(filepath.Join(fsroot, k.Kind, k.Namespace, k.Name+".yaml")); err != nil {
		t.Errorf("Got %v, Want nil", err)
	}
	waitFor(wch, Delete, k)
	if _, err := s.Get(k); err != ErrNotFound {
		t.Errorf("Got %v, Want ErrNotFound", err)
	}
}

func TestFSStore2WrongKind(t *testing.T) {
	s, fsroot := getTempFSStore2()
	defer cleanupRootIfOK(t, fsroot)
	const ns = "istio-mixer-testing"
	if err := s.Init([]string{"Action"}); err != nil {
		t.Fatal(err.Error())
	}
	defer s.Stop()

	k := Key{Kind: "Handler", Namespace: ns, Name: "default"}
	h := map[string]interface{}{"name": "default", "adapter": "noop"}
	if err := write(fsroot, k, h); err != nil {
		t.Error("Got nil, Want error")
	}
	time.Sleep(testingCheckDuration)

	if _, err := s.Get(k); err == nil {
		t.Errorf("Got nil, Want error")
	}
}

func TestFSStore2FileExtensions(t *testing.T) {
	for _, ext := range []string{"yaml", "yml"} {
		t.Run(ext, func(tt *testing.T) {
			s, fsroot := getTempFSStore2()
			defer cleanupRootIfOK(tt, fsroot)
			err := ioutil.WriteFile(filepath.Join(fsroot, "foo."+ext), []byte(`
kind: Kind
apiVersion: testing
metadata:
  namespace: ns
  name: foo
spec:
`), 0644)
			if err != nil {
				tt.Fatal(err)
			}
			if err := s.Init([]string{"Kind"}); err != nil {
				tt.Fatal(err.Error())
			}
			defer s.Stop()
			if lst := s.List(); len(lst) != 1 {
				tt.Errorf("Got %d elements, Want 1", len(lst))
			}

		})
	}
}

func TestFSStore2FileFormat(t *testing.T) {
	const good = `
kind: Foo
apiVersion: testing
metadata:
  namespace: ns
  name: foo
spec:
`
	const bad = "abc"
	for _, c := range []struct {
		title         string
		resourceCount int
		data          string
	}{
		{
			"base",
			1,
			good,
		},
		{
			"illformed",
			0,
			bad,
		},
		{
			"key missing",
			0,
			`
kind: Foo
apiVersion: testing
metadata:
  name: foo
spec:
			`,
		},
		{
			"hyphened",
			1,
			"---\n" + good + "\n---",
		},
		{
			"empty",
			0,
			"",
		},
		{
			"hyphen",
			0,
			"---",
		},
		{
			"multiple",
			2,
			good + "\n---\n" + good + "\n---\n",
		},
		{
			"fail later",
			1,
			good + "\n---\n" + bad,
		},
		{
			"fail former",
			1,
			bad + "\n---\n" + good,
		},
		{
			"fail mulitiple",
			0,
			bad + "\n---\n" + bad,
		},
		{
			"trailing white space",
			1,
			good + "\n---\n\n    \n",
		},
	} {
		t.Run(c.title, func(tt *testing.T) {
			resources := parseFile(c.title, []byte(c.data))
			if len(resources) != c.resourceCount {
				tt.Errorf("Got %d, Want %d", len(resources), c.resourceCount)
			}
		})
	}
}

func TestFsStore2_ParseChunk(t *testing.T) {
	for _, c := range []struct {
		title string
		isNil bool
		data  string
	}{
		{
			"whitespace only",
			true,
			"      \n",
		},
		{
			"whitespace with comments",
			true,
			"   \n#This is a comments\n",
		},
	} {
		t.Run(c.title, func(t *testing.T) {
			r, err := ParseChunk([]byte(c.data))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if (r == nil) != c.isNil {
				t.Fatalf("want Got %v, Want %t", r, c.isNil)
			}
		})
	}
}

func TestFSStore2MissingRoot(t *testing.T) {
	s, fsroot := getTempFSStore2()
	if err := os.RemoveAll(fsroot); err != nil {
		t.Fatal(err)
	}
	if err := s.Init([]string{"Kind"}); err != nil {
		t.Errorf("Got %v, Want nil", err)
	}
	defer s.Stop()
	if lst := s.List(); len(lst) != 0 {
		t.Errorf("Got %+v, Want empty", lst)
	}
}

func TestFSStore2Robust(t *testing.T) {
	const ns = "testing"
	for _, c := range []struct {
		title   string
		prepare func(fsroot string) error
	}{
		{
			"illformed yaml",
			func(fsroot string) error {
				path := filepath.Join(fsroot, "Handler", ns, "bb.yaml")
				return ioutil.WriteFile(path, []byte("abc"), 0644)
			},
		},
		{
			"directory",
			func(fsroot string) error {
				return os.MkdirAll(filepath.Join(fsroot, "Handler", ns, "cc.yaml"), 0755)
			},
		},
		{
			"unknown kind",
			func(fsroot string) error {
				k := Key{Kind: "Unknown", Namespace: ns, Name: "default"}
				return write(fsroot, k, map[string]interface{}{"foo": "bar"})
			},
		},
	} {
		t.Run(c.title, func(tt *testing.T) {
			s, fsroot := getTempFSStore2()
			defer cleanupRootIfOK(tt, fsroot)

			// Normal data
			k := Key{Kind: "Handler", Namespace: ns, Name: "default"}
			data := map[string]interface{}{"foo": "bar"}
			if err := write(fsroot, k, data); err != nil {
				tt.Fatalf("Failed to write: %v", err)
			}
			if err := c.prepare(fsroot); err != nil {
				tt.Fatalf("Failed to prepare precondition: %v", err)
			}
			if err := s.Init([]string{"Handler"}); err != nil {
				tt.Fatalf("Init failed: %v", err)
			}
			defer s.Stop()
			want := map[Key]*BackEndResource{k: {Spec: data}}
			got := s.List()
			if len(got) != len(want) {
				tt.Fatalf("data length does not match, got %d, want %d", len(got), len(want))
			}
			for k, v := range got {
				vwant := want[k]
				if vwant == nil {
					tt.Fatalf("Did not get key for %s", k)
				}
				if !reflect.DeepEqual(v.Spec, vwant.Spec) {
					tt.Fatalf("Got %+v, Want %+v", v, vwant)
				}
			}
		})
	}
}
