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

package platform

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
)

type handlerFunc func(http.ResponseWriter, *http.Request)

func TestAWSLocality(t *testing.T) {
	cases := []struct {
		name     string
		handlers map[string]handlerFunc
		want     *core.Locality
	}{
		{
			"error",
			map[string]handlerFunc{"/placement/region": errorHandler, "/placement/availability-zone": errorHandler},
			&core.Locality{},
		},
		{
			"locality",
			map[string]handlerFunc{"/placement/region": regionHandler, "/placement/availability-zone": zoneHandler},
			&core.Locality{Region: "us-west-2", Zone: "us-west-2b"},
		},
	}

	for _, v := range cases {
		t.Run(v.name, func(tt *testing.T) {
			server, url := setupHTTPServer(v.handlers)
			defer server.Close()
			awsMetadataIPv4URL = url.String()
			locality := NewAWS(false).Locality()
			if !reflect.DeepEqual(locality, v.want) {
				t.Errorf("unexpected locality. want :%v, got :%v", v.want, locality)
			}
		})
	}
}

func TestIsAWS(t *testing.T) {
	cases := []struct {
		name     string
		handlers map[string]handlerFunc
		want     bool
	}{
		{"not aws", map[string]handlerFunc{"/iam/info": errorHandler}, false},
		{"aws", map[string]handlerFunc{"/iam/info": iamInfoHandler}, true},
	}

	for _, v := range cases {
		t.Run(v.name, func(tt *testing.T) {
			server, url := setupHTTPServer(v.handlers)
			defer server.Close()
			awsMetadataIPv4URL = url.String()
			aws := IsAWS(false)
			if !reflect.DeepEqual(aws, v.want) {
				t.Errorf("unexpected iam info. want :%v, got :%v", v.want, aws)
			}
		})
	}
}

func errorHandler(writer http.ResponseWriter, _ *http.Request) {
	writer.WriteHeader(http.StatusInternalServerError)
}

func regionHandler(writer http.ResponseWriter, _ *http.Request) {
	writer.WriteHeader(http.StatusOK)
	writer.Write([]byte("us-west-2"))
}

func zoneHandler(writer http.ResponseWriter, _ *http.Request) {
	writer.WriteHeader(http.StatusOK)
	writer.Write([]byte("us-west-2b"))
}

func iamInfoHandler(writer http.ResponseWriter, _ *http.Request) {
	writer.WriteHeader(http.StatusOK)
	// nolint: lll
	writer.Write([]byte("{\n\"Code\" : \"Success\",\n\"LastUpdated\" : \"2022-03-18T05:04:31Z\",\n\"InstanceProfileArn\" : \"arn:aws:iam::614624372165:instance-profile/sam-processing0000120190916053337315200000004\",\n\"InstanceProfileId\" : \"AIPAY6GTXUXC3LLJY7OG7\"\n\t  }"))
}

func setupHTTPServer(handlers map[string]handlerFunc) (*httptest.Server, *url.URL) {
	handler := http.NewServeMux()
	for path, handle := range handlers {
		handler.HandleFunc(path, handle)
	}
	server := httptest.NewServer(handler)
	url, _ := url.Parse(server.URL)
	return server, url
}
