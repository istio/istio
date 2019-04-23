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

package version

import (
	"context"
	"errors"
	"testing"

	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

func TestRecordComponentBuildTag(t *testing.T) {
	cases := []struct {
		name    string
		in      BuildInfo
		wantTag string
	}{
		{"record", BuildInfo{
			Version:       "VER",
			GitRevision:   "GITREV",
			Host:          "HOST",
			GolangVersion: "GOLANGVER",
			DockerHub:     "DH",
			User:          "USER",
			BuildStatus:   "STATUS",
			GitTag:        "1.0.5-test"},
			"1.0.5-test",
		},
	}

	for _, v := range cases {
		t.Run(v.name, func(tt *testing.T) {
			v.in.RecordComponentBuildTag("test")

			d1, _ := view.RetrieveData("istio/build")
			gauge := d1[0].Data.(*view.LastValueData)
			if got, want := gauge.Value, 1.0; got != want {
				tt.Errorf("bad value for build tag gauge: got %f, want %f", got, want)
			}

			for _, tag := range d1[0].Tags {
				if tag.Key == gitTagKey && tag.Value == v.wantTag {
					return
				}
			}

			tt.Errorf("build tag not found for metric: %#v", d1)
		})
	}
}

func TestRecordComponentBuildTagError(t *testing.T) {
	bi := BuildInfo{
		Version:       "VER",
		GitRevision:   "GITREV",
		Host:          "HOST",
		GolangVersion: "GOLANGVER",
		DockerHub:     "DH",
		User:          "USER",
		BuildStatus:   "STATUS",
		GitTag:        "TAG",
	}

	bi.recordBuildTag("failure", func(context.Context, ...tag.Mutator) (context.Context, error) {
		return context.Background(), errors.New("error")
	})

	d1, _ := view.RetrieveData("istio/build")
	for _, data := range d1 {
		for _, tag := range data.Tags {
			if tag.Key.Name() == "component" && tag.Value == "failure" {
				t.Errorf("a value was recorded for the failure component unexpectedly")
			}
		}
	}
}

func TestRegisterStatsPanics(t *testing.T) {
	cases := []struct {
		name        string
		newTagKeyFn func(string) (tag.Key, error)
	}{
		{"tag", func(n string) (tag.Key, error) {
			if n == "tag" {
				return tag.Key{}, errors.New("failure")
			}
			return tag.NewKey(n)
		},
		},
		{"component", func(n string) (tag.Key, error) {
			if n == "component" {
				return tag.Key{}, errors.New("failure")
			}
			return tag.NewKey(n)
		},
		},
		{"duplicate registration", tag.NewKey},
	}

	for _, v := range cases {
		t.Run(v.name, func(tt *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					tt.Fatalf("expected panic!")
				}
			}()

			registerStats(v.newTagKeyFn)
		})
	}
}
