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

package admin

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"istio.io/istio/pilot/test/util"
)

func TestAPI_PrintClusterDump(t *testing.T) {
	tests := []struct {
		name           string
		envoySuccess   bool
		envoyNoURL     bool
		wantOutputFile string
		wantStdErr     bool
	}{
		{
			name:           "returns expected cluster dump from Envoy onto Stdout",
			wantOutputFile: "testdata/clusterdump.json",
			envoySuccess:   true,
		},
		{
			name:         "prints to standard error if envoy 500s",
			wantStdErr:   true,
			envoySuccess: false,
		},
		{
			name:       "prints to standard error if envoy GET fails",
			wantStdErr: true,
			envoyNoURL: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubEnvoy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !tt.envoySuccess {
					w.WriteHeader(http.StatusInternalServerError)
					fmt.Fprint(w, "bang")
					return
				}
				if r.URL.Path == "/config_dump" {
					bytes, _ := ioutil.ReadFile("testdata/configdump.json")
					w.WriteHeader(http.StatusOK)
					w.Write(bytes)
					return
				}
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprintf(w, "incorrect path: %v", r.URL.Path)
			}))
			gotOut, gotErr := &bytes.Buffer{}, &bytes.Buffer{}
			url := stubEnvoy.URL
			if tt.envoyNoURL {
				url = "NotAURL"
			}
			a := &API{
				URL:    url,
				Stdout: gotOut,
				Stderr: gotErr,
			}
			a.PrintClusterDump()
			if tt.wantOutputFile != "" {
				util.CompareContent(gotOut.Bytes(), tt.wantOutputFile, t)
			}
			if tt.wantStdErr {
				if gotErr.String() == "" {
					t.Error("expected output on Stderr but received nothing")
				}
			}
		})
	}
}

func TestAPI_PrintListenerDump(t *testing.T) {
	tests := []struct {
		name           string
		envoySuccess   bool
		envoyNoURL     bool
		wantOutputFile string
		wantStdErr     bool
	}{
		// TODO: Turn on when protobuf bug is resolved - https://github.com/golang/protobuf/issues/632
		// {
		// 	name:           "returns expected listener dump from Envoy onto Stdout",
		// 	wantOutputFile: "testdata/listenerdump.json",
		// 	envoySuccess:   true,
		// },
		{
			name:         "prints to standard error if envoy 500s",
			wantStdErr:   true,
			envoySuccess: false,
		},
		{
			name:       "prints to standard error if envoy GET fails",
			wantStdErr: true,
			envoyNoURL: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubEnvoy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !tt.envoySuccess {
					w.WriteHeader(http.StatusInternalServerError)
					fmt.Fprint(w, "bang")
					return
				}
				if r.URL.Path == "/config_dump" {
					bytes, _ := ioutil.ReadFile("testdata/configdump.json")
					w.WriteHeader(http.StatusOK)
					w.Write(bytes)
					return
				}
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprintf(w, "incorrect path: %v", r.URL.Path)
			}))
			gotOut, gotErr := &bytes.Buffer{}, &bytes.Buffer{}
			url := stubEnvoy.URL
			if tt.envoyNoURL {
				url = "NotAURL"
			}
			a := &API{
				URL:    url,
				Stdout: gotOut,
				Stderr: gotErr,
			}
			a.PrintListenerDump()
			if tt.wantOutputFile != "" {
				util.CompareContent(gotOut.Bytes(), tt.wantOutputFile, t)
			}
			if tt.wantStdErr {
				if gotErr.String() == "" {
					t.Error("expected output on Stderr but received nothing")
				}
			}
		})
	}
}

func TestAPI_PrintRoutesDump(t *testing.T) {
	tests := []struct {
		name           string
		envoySuccess   bool
		envoyNoURL     bool
		wantOutputFile string
		wantStdErr     bool
	}{
		// TODO: Turn on when protobuf bug is resolved - https://github.com/golang/protobuf/issues/632
		// {
		// 	name:           "returns expected routes dump from Envoy onto Stdout",
		// 	wantOutputFile: "testdata/routesdump.json",
		// 	envoySuccess:   true,
		// },
		{
			name:         "prints to standard error if envoy 500s",
			wantStdErr:   true,
			envoySuccess: false,
		},
		{
			name:       "prints to standard error if envoy GET fails",
			wantStdErr: true,
			envoyNoURL: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubEnvoy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !tt.envoySuccess {
					w.WriteHeader(http.StatusInternalServerError)
					fmt.Fprint(w, "bang")
					return
				}
				if r.URL.Path == "/config_dump" {
					bytes, _ := ioutil.ReadFile("testdata/configdump.json")
					w.WriteHeader(http.StatusOK)
					w.Write(bytes)
					return
				}
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprintf(w, "incorrect path: %v", r.URL.Path)
			}))
			gotOut, gotErr := &bytes.Buffer{}, &bytes.Buffer{}
			url := stubEnvoy.URL
			if tt.envoyNoURL {
				url = "NotAURL"
			}
			a := &API{
				URL:    url,
				Stdout: gotOut,
				Stderr: gotErr,
			}
			a.PrintRoutesDump()
			if tt.wantOutputFile != "" {
				util.CompareContent(gotOut.Bytes(), tt.wantOutputFile, t)
			}
			if tt.wantStdErr {
				if gotErr.String() == "" {
					t.Error("expected output on Stderr but received nothing")
				}
			}
		})
	}
}

func TestAPI_PrintBootstrapDump(t *testing.T) {
	tests := []struct {
		name           string
		envoySuccess   bool
		envoyNoURL     bool
		wantOutputFile string
		wantStdErr     bool
	}{
		// TODO: Turn on when protobuf bug is resolved - https://github.com/golang/protobuf/issues/632
		// {
		// 	name:           "returns expected bootstrap dump from Envoy onto Stdout",
		// 	wantOutputFile: "testdata/bootstrapdump.json",
		// 	envoySuccess:   true,
		// },
		{
			name:         "prints to standard error if envoy 500s",
			wantStdErr:   true,
			envoySuccess: false,
		},
		{
			name:       "prints to standard error if envoy GET fails",
			wantStdErr: true,
			envoyNoURL: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubEnvoy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !tt.envoySuccess {
					w.WriteHeader(http.StatusInternalServerError)
					fmt.Fprint(w, "bang")
					return
				}
				if r.URL.Path == "/config_dump" {
					bytes, _ := ioutil.ReadFile("testdata/configdump.json")
					w.WriteHeader(http.StatusOK)
					w.Write(bytes)
					return
				}
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprintf(w, "incorrect path: %v", r.URL.Path)
			}))
			gotOut, gotErr := &bytes.Buffer{}, &bytes.Buffer{}
			url := stubEnvoy.URL
			if tt.envoyNoURL {
				url = "NotAURL"
			}
			a := &API{
				URL:    url,
				Stdout: gotOut,
				Stderr: gotErr,
			}
			a.PrintBootstrapDump()
			if tt.wantOutputFile != "" {
				util.CompareContent(gotOut.Bytes(), tt.wantOutputFile, t)
			}
			if tt.wantStdErr {
				if gotErr.String() == "" {
					t.Error("expected output on Stderr but received nothing")
				}
			}
		})
	}
}
