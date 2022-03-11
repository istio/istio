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
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"

	"istio.io/istio/pkg/http"
	"istio.io/pkg/log"
)

const (
	AWSRegion           = "aws_region"
	AWSAvailabilityZone = "aws_availability_zone"
	AWSInstanceID       = "aws_instance_id"
)

var awsMetadataURL = "http://169.254.169.254/latest/meta-data"

// Approach derived from the following:
// https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/identify_ec2_instances.html
// https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/instancedata-data-retrieval.html

// IsAWS returns whether or not the platform for bootstrapping is Amazon Web Services.
func IsAWS() bool {
	_, err := getAWSInfo("instance-id")
	return err == nil
}

type awsEnv struct {
	region           string
	availabilityzone string
	instanceID       string
}

// NewAWS returns a platform environment customized for AWS.
// Metadata returned by the AWS Environment is taken link-local address running on each node.
func NewAWS() Environment {
	return &awsEnv{
		region:           getRegion(),
		availabilityzone: getAvailabilityZone(),
		instanceID:       getInstanceID(),
	}
}

func (a *awsEnv) Metadata() map[string]string {
	md := map[string]string{}
	if len(a.availabilityzone) > 0 {
		md[AWSAvailabilityZone] = a.availabilityzone
	}
	if len(a.region) > 0 {
		md[AWSRegion] = a.region
	}
	if len(a.instanceID) > 0 {
		md[AWSInstanceID] = a.instanceID
	}
	return md
}

func (a *awsEnv) Locality() *core.Locality {
	return &core.Locality{
		Zone:   a.availabilityzone,
		Region: a.region,
	}
}

func (a *awsEnv) Labels() map[string]string {
	return map[string]string{}
}

func (a *awsEnv) IsKubernetes() bool {
	return true
}

func getAWSInfo(path string) (string, error) {
	url := awsMetadataURL + "/" + path

	resp, err := http.DoHTTPGetWithTimeout(url, time.Millisecond*100)
	if err != nil {
		log.Errorf("error in getting aws info for %s : %v", path, err)
		return "", err
	}
	return resp.String(), nil
}

// getRegion returns the Region that the instance is running in.
func getRegion() string {
	region, _ := getAWSInfo("placement/region")
	return region
}

// getAvailabilityZone returns the AvailabilityZone that the instance is running in.
func getAvailabilityZone() string {
	az, _ := getAWSInfo("placement/availability-zone")
	return az
}

func getInstanceID() string {
	instance, _ := getAWSInfo("instance-id")
	return instance
}
