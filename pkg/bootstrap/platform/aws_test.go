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
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
)

var (
	localityIdentity = ec2metadata.EC2InstanceIdentityDocument{Region: "region", AvailabilityZone: "zone"}
	fullIdentity     = ec2metadata.EC2InstanceIdentityDocument{Region: "region", AvailabilityZone: "zone", AccountID: "account", InstanceID: "instance"}
)

func TestAWSMetadata(t *testing.T) {
	cases := []struct {
		name string
		env  *awsEnv
		want map[string]string
	}{
		{"empty", &awsEnv{}, map[string]string{}},
		{"locality", &awsEnv{identity: localityIdentity}, map[string]string{AWSAvailabilityZone: "zone", AWSRegion: "region"}},
		{"full", &awsEnv{identity: fullIdentity},
			map[string]string{AWSAvailabilityZone: "zone", AWSRegion: "region", AWSAccountID: "account", AWSInstanceID: "instance"}},
	}

	for _, v := range cases {
		t.Run(v.name, func(tt *testing.T) {
			got := v.env.Metadata()
			if !reflect.DeepEqual(got, v.want) {
				t.Errorf("awsEnv.Metadata() => '%#v', wanted '%#v'", got, v.want)
			}
		})
	}
}

func TestAWSLocality(t *testing.T) {
	cases := []struct {
		name string
		env  *awsEnv
		want *core.Locality
	}{
		{"empty", &awsEnv{}, &core.Locality{}},
		{"locality", &awsEnv{identity: localityIdentity}, &core.Locality{Region: "region", Zone: "zone"}},
	}

	for _, v := range cases {
		t.Run(v.name, func(tt *testing.T) {
			got := v.env.Locality()
			if !reflect.DeepEqual(got, v.want) {
				t.Errorf("awsEnv.Locality() => '%#v', wanted '%#v'", got, v.want)
			}
		})
	}
}
