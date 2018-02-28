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

package workload

import (
	"fmt"
	"io/ioutil"
	"net"
	"path/filepath"
	"testing"
	"time"

	api "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	sds "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"github.com/gogo/protobuf/proto"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

func unixDialer(target string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("unix", target, timeout)
}

func FetchSecrets(t *testing.T, udsPath string) *api.DiscoveryResponse {
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithInsecure())
	opts = append(opts, grpc.WithDialer(unixDialer))
	conn, err := grpc.Dial(udsPath, opts...)
	if err != nil {
		t.Fatalf("Failed to connect with server %v", err)
	}
	defer conn.Close()

	client := sds.NewSecretDiscoveryServiceClient(conn)
	response, err := client.FetchSecrets(context.Background(), &api.DiscoveryRequest{})
	if err != nil {
		t.Fatalf("Failed fetch secrets %v", err)
	}
	return response
}

func VerifySecrets(t *testing.T, response *api.DiscoveryResponse) {
	var secret auth.Secret
	resource := response.GetResources()[0]
	bytes := resource.Value

	err := proto.Unmarshal(bytes, &secret)
	if err != nil {
		t.Fatalf("failed parse the response %v", err)
	}
	if SecretTypeURL != response.GetTypeUrl() || SecretName != secret.GetName() {
		t.Fatalf("Unexpected response. Expected: type %s, name %s; Actual: type %s, name %s",
			SecretTypeURL, SecretName, response.GetTypeUrl(), secret.GetName())
	}
}

func TestSingleUdsPath(t *testing.T) {
	tmpdir, _ := ioutil.TempDir("", "uds")
	udsPath := filepath.Join(tmpdir, "test_path")
	server, err := NewSDSServer(udsPath)
	if err != nil {
		t.Fatalf("Unexpected Error: %v", err)
	}
	if server == nil {
		t.Fatalf("Fail to create SDSServer")
	}

	VerifySecrets(t, FetchSecrets(t, udsPath))
}

func TestMultipleUdsPaths(t *testing.T) {
	tmpdir, _ := ioutil.TempDir("", "uds")
	udsPath1 := filepath.Join(tmpdir, "test_path1")
	udsPath2 := filepath.Join(tmpdir, "test_path2")
	udsPath3 := filepath.Join(tmpdir, "test_path3")

	server1, err1 := NewSDSServer(udsPath1)
	server2, err2 := NewSDSServer(udsPath2)
	server3, err3 := NewSDSServer(udsPath3)

	if err1 != nil || err2 != nil || err3 != nil {
		t.Fatalf("Unexpected Error: %v %v %v", err1, err2, err3)
	}
	if server1 == nil || server2 == nil || server3 == nil {
		t.Fatalf("Fail to create all SDSServer")
	}

	VerifySecrets(t, FetchSecrets(t, udsPath1))
	VerifySecrets(t, FetchSecrets(t, udsPath2))
	VerifySecrets(t, FetchSecrets(t, udsPath3))
}

func TestDuplicateUdsPaths(t *testing.T) {
	tmpdir, _ := ioutil.TempDir("", "uds")
	udsPath := filepath.Join(tmpdir, "test_path")
	_, _ = NewSDSServer(udsPath)

	server, err := NewSDSServer(udsPath)
	expectedErr := fmt.Sprintf("UDS path %v already exists", udsPath)
	if err == nil || err.Error() != expectedErr {
		t.Fatalf("Expect error: %v, Actual error: %v", expectedErr, err)
	}
	if server != nil {
		t.Fatalf("Unexpectedly created SDSServer.")
	}
}
