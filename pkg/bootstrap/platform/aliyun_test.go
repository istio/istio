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
	"os"
	"reflect"
	"strconv"
	"testing"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"

	"istio.io/pkg/log"
)

type (
	mockHandlerFunc func(http.ResponseWriter, *http.Request)
	mockHandlers    map[string]mockHandlerFunc
)

func TestAliyunMetadataFromFileWithContentError(t *testing.T) {
	mockMetadata := aliyunInstance{
		CPUInfo: "Intel(R) Xeon(R) Platinum 8369B CPU @ 2.70GHz",
	}

	t.Setenv(MetadataConfigSourceEnvKey, "file")
	t.Setenv(MetadataFileConfigSourceEnvKey, "../testdata/labels_error.cfg")
	t.Setenv(CPUInfoPathEnvKey, "../testdata/cpuinfo.txt")

	aliyunMetadata := NewAliyun().Metadata()
	retry, err := strconv.Atoi(aliyunMetadata["retry-count"])
	if err != nil {
		t.Errorf("unexpected retry count info, error: %v", err)
	}
	if retry != MaxRetryCount {
		t.Errorf("unexpected retry count info. want :%d, got :%d", MaxRetryCount, retry)
	}

	if aliyunMetadata["zone"] != mockMetadata.ZoneId {
		t.Errorf("unexpected zone info. want :%v, got :%v", mockMetadata.ZoneId, aliyunMetadata["zone"])
	}

	if aliyunMetadata["region"] != mockMetadata.RegionId {
		t.Errorf("unexpected region info. want :%v, got :%v", mockMetadata.RegionId, aliyunMetadata["region"])
	}

	if aliyunMetadata["instance-id"] != mockMetadata.InstanceId {
		t.Errorf("unexpected instance-id info. want :%v, got :%v", mockMetadata.InstanceId, aliyunMetadata["instance-id"])
	}

	if aliyunMetadata["instance-type"] != mockMetadata.InstanceType {
		t.Errorf("unexpected instance-type info. want :%v, got :%v", mockMetadata.InstanceType, aliyunMetadata["instance-type"])
	}

	if aliyunMetadata["private-ipv4"] != mockMetadata.PrivateIpv4 {
		t.Errorf("unexpected private-ipv4 info. want :%v, got :%v", mockMetadata.PrivateIpv4, aliyunMetadata["private-ipv4"])
	}

	if aliyunMetadata["cpu-info"] != mockMetadata.CPUInfo {
		t.Errorf("unexpected cpu-info. want :%v, got :%v", mockMetadata.CPUInfo, aliyunMetadata["cpu-info"])
	}
}

func TestAliyunMetadataFromFileWithNoExist(t *testing.T) {
	mockMetadata := aliyunInstance{
		CPUInfo: "Intel(R) Xeon(R) Platinum 8369B CPU @ 2.70GHz",
	}

	t.Setenv(MetadataConfigSourceEnvKey, "file")
	t.Setenv(MetadataFileConfigSourceEnvKey, "../testdata/labels_no_exist.cfg")
	t.Setenv(CPUInfoPathEnvKey, "../testdata/cpuinfo.txt")

	aliyunMetadata := NewAliyun().Metadata()
	retry, err := strconv.Atoi(aliyunMetadata["retry-count"])
	if err != nil {
		t.Errorf("unexpected retry count info, error: %v", err)
	}
	if retry != MaxRetryCount {
		t.Errorf("unexpected retry count info. want :%d, got :%d", MaxRetryCount, retry)
	}

	if aliyunMetadata["zone"] != mockMetadata.ZoneId {
		t.Errorf("unexpected zone info. want :%v, got :%v", mockMetadata.ZoneId, aliyunMetadata["zone"])
	}

	if aliyunMetadata["region"] != mockMetadata.RegionId {
		t.Errorf("unexpected region info. want :%v, got :%v", mockMetadata.RegionId, aliyunMetadata["region"])
	}

	if aliyunMetadata["instance-id"] != mockMetadata.InstanceId {
		t.Errorf("unexpected instance-id info. want :%v, got :%v", mockMetadata.InstanceId, aliyunMetadata["instance-id"])
	}

	if aliyunMetadata["instance-type"] != mockMetadata.InstanceType {
		t.Errorf("unexpected instance-type info. want :%v, got :%v", mockMetadata.InstanceType, aliyunMetadata["instance-type"])
	}

	if aliyunMetadata["private-ipv4"] != mockMetadata.PrivateIpv4 {
		t.Errorf("unexpected private-ipv4 info. want :%v, got :%v", mockMetadata.PrivateIpv4, aliyunMetadata["private-ipv4"])
	}

	if aliyunMetadata["cpu-info"] != mockMetadata.CPUInfo {
		t.Errorf("unexpected cpu-info. want :%v, got :%v", mockMetadata.CPUInfo, aliyunMetadata["cpu-info"])
	}
}

func TestAliyunMetadataFromFile(t *testing.T) {
	mockMetadata := aliyunInstance{
		ZoneId:       "cn-hangzhou-i",
		RegionId:     "cn-hangzhou",
		InstanceId:   "i-bp1ce4jpb3j8qcombybz",
		PrivateIpv4:  "22.1.82.167",
		InstanceType: "ecs.c7.8xlarge",
	}

	t.Setenv(MetadataConfigSourceEnvKey, "file")
	t.Setenv(MetadataFileConfigSourceEnvKey, "../testdata/labels.cfg")

	aliyunMetadata := NewAliyun().Metadata()
	if aliyunMetadata["zone"] != mockMetadata.ZoneId {
		t.Errorf("unexpected zone info. want :%v, got :%v", mockMetadata.ZoneId, aliyunMetadata["zone"])
	}

	if aliyunMetadata["region"] != mockMetadata.RegionId {
		t.Errorf("unexpected region info. want :%v, got :%v", mockMetadata.RegionId, aliyunMetadata["region"])
	}

	if aliyunMetadata["instance-id"] != mockMetadata.InstanceId {
		t.Errorf("unexpected instance-id info. want :%v, got :%v", mockMetadata.InstanceId, aliyunMetadata["instance-id"])
	}

	if aliyunMetadata["instance-type"] != mockMetadata.InstanceType {
		t.Errorf("unexpected instance-type info. want :%v, got :%v", mockMetadata.InstanceType, aliyunMetadata["instance-type"])
	}

	if aliyunMetadata["private-ipv4"] != mockMetadata.PrivateIpv4 {
		t.Errorf("unexpected private-ipv4 info. want :%v, got :%v", mockMetadata.PrivateIpv4, aliyunMetadata["private-ipv4"])
	}

	if aliyunMetadata["cpu-info"] != mockMetadata.CPUInfo {
		t.Errorf("unexpected cpu-info. want :%v, got :%v", mockMetadata.CPUInfo, aliyunMetadata["cpu-info"])
	}
}

func TestAliyunMetadataFromEnv(t *testing.T) {
	mockMetadata := aliyunInstance{
		ZoneId:       "cn-hangzhou-i",
		RegionId:     "cn-hangzhou",
		InstanceId:   "i-bp101imauyb1ygbon3myxxx",
		PrivateIpv4:  "1.1.1.1",
		InstanceType: "ecs.c7.8xlarge",
	}

	t.Setenv(MetadataConfigSourceEnvKey, "env")
	t.Setenv(NodeInstanceTypeEnvKey, mockMetadata.InstanceType)
	t.Setenv(NodeInstanceIdEnvKey, mockMetadata.InstanceId)
	t.Setenv(NodePrivateIpv4EnvKey, mockMetadata.PrivateIpv4)
	t.Setenv(NodeZoneIdEnvKey, mockMetadata.ZoneId)
	t.Setenv(NodeRegionIdEnvKey, mockMetadata.RegionId)

	if os.Getenv(NodeZoneIdEnvKey) != mockMetadata.ZoneId {
		t.Errorf("unexpected zone info. want :%v, got :%v", os.Getenv(NodeZoneIdEnvKey), mockMetadata.ZoneId)
	}

	aliyunMetadata := NewAliyun().Metadata()
	if aliyunMetadata["zone"] != mockMetadata.ZoneId {
		t.Errorf("unexpected zone info. want :%v, got :%v", mockMetadata.ZoneId, aliyunMetadata["zone"])
	}

	if aliyunMetadata["region"] != mockMetadata.RegionId {
		t.Errorf("unexpected region info. want :%v, got :%v", mockMetadata.RegionId, aliyunMetadata["region"])
	}

	if aliyunMetadata["instance-id"] != mockMetadata.InstanceId {
		t.Errorf("unexpected instance-id info. want :%v, got :%v", mockMetadata.InstanceId, aliyunMetadata["instance-id"])
	}

	if aliyunMetadata["instance-type"] != mockMetadata.InstanceType {
		t.Errorf("unexpected instance-type info. want :%v, got :%v", mockMetadata.InstanceType, aliyunMetadata["instance-type"])
	}

	if aliyunMetadata["private-ipv4"] != mockMetadata.PrivateIpv4 {
		t.Errorf("unexpected private-ipv4 info. want :%v, got :%v", mockMetadata.PrivateIpv4, aliyunMetadata["private-ipv4"])
	}

	if aliyunMetadata["cpu-info"] != mockMetadata.CPUInfo {
		t.Errorf("unexpected cpu-info. want :%v, got :%v", mockMetadata.CPUInfo, aliyunMetadata["cpu-info"])
	}
}

func TestAliyunMetadataFromMetaserver(t *testing.T) {
	errorHandler := func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
	}

	successHandler := func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
		writer.Write([]byte(`{"zone-id":"cn-hangzhou-i","serial-number":"4b6e4e7f-f9c6-4c3d-9ad5-320d57c9afea","instance-id":"i-bp146pjhxkpaq3z9bje8","region-id":"cn-hangzhou","private-ipv4":"24.1.93.216","owner-account-id":"1365462831915338","mac":"00:16:3e:18:4d:eb","image-id":"m-bp16ztcklqhfz04r00go","instance-type":"ecs.c7.8xlarge"}`))
	}

	testCases := []struct {
		name     string
		handlers map[string]mockHandlerFunc
		want     *core.Locality
	}{
		{
			"error",
			map[string]mockHandlerFunc{"/latest/dynamic/instance-identity/document": errorHandler},
			&core.Locality{},
		},
		{
			"success",
			map[string]mockHandlerFunc{"/latest/dynamic/instance-identity/document": successHandler},
			&core.Locality{
				Region: "cn-hangzhou",
				Zone:   "cn-hangzhou-i",
			},
		},
	}

	for _, v := range testCases {
		t.Run(v.name, func(tt *testing.T) {
			server, url := setupAliyunMetaServer(v.handlers)
			defer server.Close()
			AliyunEndpoint = url.String()
			log.Warnf("HTTP request url: %v", AliyunEndpoint)
			aliyunMetadata := NewAliyun().Locality()
			if !reflect.DeepEqual(aliyunMetadata, v.want) {
				t.Errorf("unexpected aliyun metadata. want :%v, got :%v", v.want, aliyunMetadata)
			}
		})
	}
}

func setupAliyunMetaServer(handlers mockHandlers) (*httptest.Server, *url.URL) {
	handler := http.NewServeMux()
	for path, handle := range handlers {
		handler.HandleFunc(path, handle)
	}
	server := httptest.NewServer(handler)
	url, _ := url.Parse(server.URL)
	log.Warnf("HTTP request url: %v", url)
	return server, url
}

func TestReadCPUInfo(t *testing.T) {
	result := readeCPUInfo("../testdata/cpuinfo.txt")

	// 验证结果
	expected := "Intel(R) Xeon(R) Platinum 8369B CPU @ 2.70GHz"
	if result != expected {
		t.Errorf("expected %s, but got %s", expected, result)
	}
}
