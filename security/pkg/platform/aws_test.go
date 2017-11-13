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
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/awstesting/unit"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
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

func TestNewAwsClientImpl(t *testing.T) {
	client := NewAwsClientImpl(AwsConfig{})
	if client == nil {
		t.Errorf("NewAwsClientImpl should not return nil")
	}
}

func TestAwsGetInstanceIdentityDocument(t *testing.T) {
	testCases := map[string]struct {
		sigFile              string
		requestPath          string
		expectedErr          string
		expectedInstanceType string
		expectedRegion       string
		expectedCredential   string
	}{
		"Good Identity": {
			sigFile:              "testdata/sig.pem",
			requestPath:          "/latest/dynamic/instance-identity/pkcs7",
			expectedErr:          "",
			expectedInstanceType: "c3.large",
			expectedRegion:       "us-west-2",
			expectedCredential: "\"ewogICJkZXZwYXlQcm9kdWN0Q29kZXMiIDogbnVsbCwKICAicHJpdmF0ZUlwIiA6ICIx" +
				"MC4xNi4xNy4yNDgiLAogICJhdmFpbGFiaWxpdHlab25lIiA6ICJ1cy13ZXN0LTJiIiwKICAidmVyc2lvbiIgOi" +
				"AiMjAxMC0wOC0zMSIsCiAgImluc3RhbmNlSWQiIDogImktMDY0NmM5ZWZlMmU2MmRjNjMiLAogICJiaWxsaW5n" +
				"UHJvZHVjdHMiIDogbnVsbCwKICAiaW5zdGFuY2VUeXBlIiA6ICJjMy5sYXJnZSIsCiAgImFjY291bnRJZCIgOi" +
				"AiOTc3Nzc3NjU3NjExIiwKICAiYXJjaGl0ZWN0dXJlIiA6ICJ4ODZfNjQiLAogICJrZXJuZWxJZCIgOiBudWxs" +
				"LAogICJyYW1kaXNrSWQiIDogbnVsbCwKICAiaW1hZ2VJZCIgOiAiYW1pLWZhYmY1YzgyIiwKICAicGVuZGluZ1" +
				"RpbWUiIDogIjIwMTctMDgtMjdUMTc6MTg6MjBaIiwKICAicmVnaW9uIiA6ICJ1cy13ZXN0LTIiCn0=\"",
		},
	}

	for id, c := range testCases {
		sigBytes, err := ioutil.ReadFile(c.sigFile)
		if err != nil {
			t.Fatalf("unable to read file %s", c.sigFile)
		}

		server := initTestServer(
			c.requestPath,
			sigBytes,
		)
		defer server.Close()

		awsc := &AwsClientImpl{
			client: ec2metadata.New(unit.Session, &aws.Config{Endpoint: aws.String(server.URL + "/latest")}),
		}

		docBytes, err := awsc.getInstanceIdentityDocument()
		if len(c.expectedErr) > 0 {
			if err == nil {
				t.Errorf("%s: Succeeded. Error expected: %v", id, err)
			} else if err.Error() != c.expectedErr {
				t.Errorf("%s: incorrect error message: %s VS %s",
					id, err.Error(), c.expectedErr)
			}
			continue
		} else if err != nil {
			t.Fatalf("%s: Unexpected Error: %v", id, err)
		}

		doc := ec2metadata.EC2InstanceIdentityDocument{}
		decode := json.NewDecoder(bytes.NewReader(docBytes)).Decode(&doc)
		if decode != nil {
			t.Fatalf("%s: Unexpected Error: %v", id, err)
		}

		if doc.InstanceType != c.expectedInstanceType {
			t.Errorf("%s: Wrong Instance Type. Expected %s, Actual %s", id, c.expectedInstanceType, doc.InstanceType)
		}

		if doc.Region != c.expectedRegion {
			t.Errorf("%s: Wrong Region. Expected %s, Actual %s", id, c.expectedRegion, doc.Region)
		}
	}
}

func TestAwsGetServiceIdentity(t *testing.T) {
	testCases := map[string]struct {
		sigFile                 string
		requestPath             string
		expectedErr             string
		expectedServiceIdentity string
	}{
		"Good CredentialTypes": {
			sigFile:                 "testdata/sig.pem",
			requestPath:             "/latest/dynamic/instance-identity/pkcs7",
			expectedErr:             "",
			expectedServiceIdentity: "",
		},
	}

	for id, c := range testCases {
		sigBytes, err := ioutil.ReadFile(c.sigFile)
		if err != nil {
			t.Fatalf("unable to read file %s", c.sigFile)
		}

		server := initTestServer(
			c.requestPath,
			sigBytes,
		)
		defer server.Close()

		awsc := &AwsClientImpl{
			client: ec2metadata.New(unit.Session, &aws.Config{Endpoint: aws.String(server.URL + "/latest")}),
		}

		serviceIdentity, err := awsc.GetServiceIdentity()
		if serviceIdentity != c.expectedServiceIdentity {
			t.Errorf("%s: Wrong Service Identity. Expected %v, Actual %v", id,
				string(c.expectedServiceIdentity), string(serviceIdentity))
		}
	}
}

func TestGetGetAgentCredential(t *testing.T) {
	testCases := map[string]struct {
		sigFile            string
		requestPath        string
		expectedErr        string
		expectedCredential string
	}{
		"Good Identity": {
			sigFile:     "testdata/sig.pem",
			requestPath: "/latest/dynamic/instance-identity/pkcs7",
			expectedErr: "",
			expectedCredential: "\"ewogICJkZXZwYXlQcm9kdWN0Q29kZXMiIDogbnVsbCwKICAicHJpdmF0ZUlwIiA6ICIx" +
				"MC4xNi4xNy4yNDgiLAogICJhdmFpbGFiaWxpdHlab25lIiA6ICJ1cy13ZXN0LTJiIiwKICAidmVyc2lvbiIgOi" +
				"AiMjAxMC0wOC0zMSIsCiAgImluc3RhbmNlSWQiIDogImktMDY0NmM5ZWZlMmU2MmRjNjMiLAogICJiaWxsaW5n" +
				"UHJvZHVjdHMiIDogbnVsbCwKICAiaW5zdGFuY2VUeXBlIiA6ICJjMy5sYXJnZSIsCiAgImFjY291bnRJZCIgOi" +
				"AiOTc3Nzc3NjU3NjExIiwKICAiYXJjaGl0ZWN0dXJlIiA6ICJ4ODZfNjQiLAogICJrZXJuZWxJZCIgOiBudWxs" +
				"LAogICJyYW1kaXNrSWQiIDogbnVsbCwKICAiaW1hZ2VJZCIgOiAiYW1pLWZhYmY1YzgyIiwKICAicGVuZGluZ1" +
				"RpbWUiIDogIjIwMTctMDgtMjdUMTc6MTg6MjBaIiwKICAicmVnaW9uIiA6ICJ1cy13ZXN0LTIiCn0=\"",
		},
	}

	for id, c := range testCases {
		sigBytes, err := ioutil.ReadFile(c.sigFile)
		if err != nil {
			t.Fatalf("unable to read file %s", c.sigFile)
		}

		server := initTestServer(
			c.requestPath,
			sigBytes,
		)
		defer server.Close()

		awsc := &AwsClientImpl{
			client: ec2metadata.New(unit.Session, &aws.Config{Endpoint: aws.String(server.URL + "/latest")}),
		}

		credential, err := awsc.GetAgentCredential()
		if len(c.expectedErr) > 0 {
			if err == nil {
				t.Errorf("%s: Succeeded. Error expected: %v", id, err)
			} else if err.Error() != c.expectedErr {
				t.Errorf("%s: incorrect error message: %s VS %s",
					id, err.Error(), c.expectedErr)
			}
			// no need to move forward
			continue
		} else if err != nil {
			t.Fatalf("%s: Unexpected Error: %v", id, err)
		}

		if string(credential) != c.expectedCredential {
			t.Errorf("%s: Wrong Credential. Expected %s, Actual %s", id, c.expectedCredential, string(credential))
		}
	}
}

func TestAwsGetDialOptions(t *testing.T) {
	creds, err := credentials.NewClientTLSFromFile("testdata/cert-chain-good.pem", "")
	if err != nil {
		t.Errorf("Ubable to get credential for testdata/cert-chain-good.pem")
	}

	testCases := map[string]struct {
		sigFile         string
		requestPath     string
		expectedErr     string
		cfg             *ClientConfig
		expectedOptions []grpc.DialOption
	}{
		"Good DialOptions": {
			sigFile:     "testdata/sig.pem",
			requestPath: "/latest/dynamic/instance-identity/pkcs7",
			expectedErr: "",
			cfg: &ClientConfig{
				AwsConfig: AwsConfig{
					RootCACertFile: "testdata/cert-chain-good.pem",
				},
			},
			expectedOptions: []grpc.DialOption{
				grpc.WithTransportCredentials(creds),
			},
		},
		"Bad DialOptions": {
			sigFile:     "testdata/sig.pem",
			requestPath: "/latest/dynamic/instance-identity/pkcs7",
			expectedErr: "open testdata/cert-chain-good_not_exist.pem: no such file or directory",
			cfg: &ClientConfig{
				AwsConfig: AwsConfig{
					RootCACertFile: "testdata/cert-chain-good_not_exist.pem",
				},
			},
			expectedOptions: []grpc.DialOption{
				grpc.WithTransportCredentials(creds),
			},
		},
	}

	for id, c := range testCases {
		sigBytes, err := ioutil.ReadFile(c.sigFile)
		if err != nil {
			t.Fatalf("$v: Unable to read file: %s", id, c.sigFile)
		}

		server := initTestServer(
			c.requestPath,
			sigBytes,
		)
		defer server.Close()

		awsc := &AwsClientImpl{
			config: c.cfg.AwsConfig,
			client: ec2metadata.New(unit.Session, &aws.Config{Endpoint: aws.String(server.URL + "/latest")}),
		}

		options, err := awsc.GetDialOptions()
		if len(c.expectedErr) > 0 {
			if err == nil {
				t.Errorf("%s: Succeeded. Error expected: %v", id, err)
			} else if err.Error() != c.expectedErr {
				t.Errorf("%s: Incorrect error message: %s VS %s", id, err.Error(), c.expectedErr)
			}
			continue
		} else if err != nil {
			t.Fatalf("%s: Unexpected Error: %v", id, err)
		}

		// Make sure there're two dial options, one for TLS and one for JWT.
		if len(options) != len(c.expectedOptions) {
			t.Errorf("%s: Wrong dial options size. Expected %v, Actual %v",
				id, len(c.expectedOptions), len(options))
		}

		for index, option := range c.expectedOptions {
			if reflect.ValueOf(options[index]).Pointer() != reflect.ValueOf(option).Pointer() {
				t.Errorf("%s: Wrong option found", id)
			}
		}
	}
}

func TestAwsGetCredentialTypes(t *testing.T) {
	testCases := map[string]struct {
		sigFile      string
		requestPath  string
		expectedErr  string
		expectedType string
	}{
		"Good CredentialTypes": {
			sigFile:      "testdata/sig.pem",
			requestPath:  "/latest/dynamic/instance-identity/pkcs7",
			expectedErr:  "",
			expectedType: "aws",
		},
	}

	for id, c := range testCases {
		sigBytes, err := ioutil.ReadFile(c.sigFile)
		if err != nil {
			t.Fatalf("%id: Unable to read file %s", id, c.sigFile)
		}

		server := initTestServer(
			c.requestPath,
			sigBytes,
		)
		defer server.Close()

		awsc := &AwsClientImpl{
			client: ec2metadata.New(unit.Session, &aws.Config{Endpoint: aws.String(server.URL + "/latest")}),
		}

		credentialType := awsc.GetCredentialType()
		if credentialType != c.expectedType {
			t.Errorf("%s: Wrong Credential Type. Expected %v, Actual %v", id,
				string(c.expectedType), string(credentialType))
		}
	}
}
