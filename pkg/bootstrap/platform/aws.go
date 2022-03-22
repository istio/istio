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
	"strings"
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

var (
	awsMetadataIPv4URL = "http://169.254.169.254/latest/meta-data"
	awsMetadataIPv6URL = "http://[fd00:ec2::254]/latest/meta-data"
)

// Approach derived from the following:
// https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/identify_ec2_instances.html
// https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/instancedata-data-retrieval.html

// IsAWS returns whether or not the platform for bootstrapping is Amazon Web Services.
func IsAWS(ipv6 bool) bool {
	info, err := getAWSInfo("iam/info", ipv6)
	return err == nil && strings.Contains(info, "arn:aws:iam")
}

type awsEnv struct {
	region           string
	availabilityzone string
	instanceID       string
}

// NewAWS returns a platform environment customized for AWS.
// Metadata returned by the AWS Environment is taken link-local address running on each node.
func NewAWS(ipv6 bool) Environment {
	return &awsEnv{
		region:           getRegion(ipv6),
		availabilityzone: getAvailabilityZone(ipv6),
		instanceID:       getInstanceID(ipv6),
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

func getAWSInfo(path string, ipv6 bool) (string, error) {
	url := awsMetadataIPv4URL + "/" + path
	if ipv6 {
		url = awsMetadataIPv6URL + "/" + path
	}

	resp, err := http.DoHTTPGetWithTimeout(url, time.Millisecond*100)
	if err != nil {
		log.Debugf("error in getting aws info for %s : %v", path, err)
		return "", err
	}
	return resp.String(), nil
}

// getRegion returns the Region that the instance is running in.
func getRegion(ipv6 bool) string {
	region, _ := getAWSInfo("placement/region", ipv6)
	return region
}

// getAvailabilityZone returns the AvailabilityZone that the instance is running in.
func getAvailabilityZone(ipv6 bool) string {
	az, _ := getAWSInfo("placement/availability-zone", ipv6)
	return az
}

func getInstanceID(ipv6 bool) string {
	instance, _ := getAWSInfo("instance-id", ipv6)
	return instance
}
