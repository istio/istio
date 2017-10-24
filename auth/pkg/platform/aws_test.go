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

package platform

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/awstesting/unit"
)

func initTestServer(path string, resp []byte) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.RequestURI != path {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		_, _ = w.Write(resp)
	}))
}

func TestIsProperPlatform(t *testing.T) {
	server := initTestServer(
		"/latest/meta-data/instance-id",
		[]byte("instance-id"),
	)

	c := ec2metadata.New(unit.Session, &aws.Config{Endpoint: aws.String(server.URL + "/latest")})
	na := &AwsClientImpl{client: c}
	if !na.IsProperPlatform() {
		t.Errorf("On Proper Platform: expected true")
	}

	server.Close()
	if na.IsProperPlatform() {
		t.Errorf("On Proper Platform: expected false")
	}
}

func TestGetInstanceIdentityDocument(t *testing.T) {
	sigFile := "testdata/sig.pem"
	sigBytes, err := ioutil.ReadFile(sigFile)
	if err != nil {
		t.Fatalf("unable to read file %s", sigFile)
	}

	server := initTestServer(
		"/latest/dynamic/instance-identity/pkcs7",
		sigBytes,
	)
	defer server.Close()

	c := ec2metadata.New(unit.Session, &aws.Config{Endpoint: aws.String(server.URL + "/latest")})
	na := &AwsClientImpl{client: c}

	testcase := "Get Identity Document"
	docBytes, err := na.getInstanceIdentityDocument()
	if err != nil {
		t.Fatalf("%s: Unexpected Error: %v", testcase, err)
	}

	doc := ec2metadata.EC2InstanceIdentityDocument{}
	if err := json.NewDecoder(bytes.NewReader(docBytes)).Decode(&doc); err != nil {
		t.Fatalf("%s: Unexpected Error: %v", testcase, err)
	}

	// check if some fields agree
	if doc.InstanceType != "c3.large" {
		t.Errorf("%s: Wrong Instance Type. Expected %s, Actual %s", testcase, "c3.large", doc.InstanceType)
	}

	if doc.Region != "us-west-2" {
		t.Errorf("%s: Wrong Region. Expected %s, Actual %s", testcase, "us-west-2", doc.Region)
	}
}
