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
	"istio.io/istio/pkg/log"
)

const (
	AWSRegion           = "aws_region"
	AWSAvailabilityZone = "aws_availability_zone"
	AWSInstanceID       = "aws_instance_id"
)

var (
	awsMetadataIPv4URL = "http://169.254.169.254/latest/meta-data"
	awsMetadataIPv6URL = "http://[fd00:ec2::254]/latest/meta-data"

	awsMetadataTokenIPv4URL = "http://169.254.169.254/latest/api/token"
	awsMetadataTokenIPv6URL = "http://[fd00:ec2::254]/latest/api/token"
)

// Approach derived from the following:
// https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/identify_ec2_instances.html
// https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/instancedata-data-retrieval.html

// IsAWS returns whether the platform for bootstrapping is Amazon Web Services.
func IsAWS(ipv6 bool) bool {
	headers := requestHeaders(ipv6)
	info, err := getAWSInfo("iam/info", ipv6, headers)
	return err == nil && strings.Contains(info, "arn:aws:iam")
}

type awsEnv struct {
	region           string
	availabilityZone string
	instanceID       string
}

// NewAWS returns a platform environment customized for AWS.
// Metadata returned by the AWS Environment is taken link-local address running on each node.
func NewAWS(ipv6 bool) Environment {
	headers := requestHeaders(ipv6)

	return &awsEnv{
		region:           getRegion(ipv6, headers),
		availabilityZone: getAvailabilityZone(ipv6, headers),
		instanceID:       getInstanceID(ipv6, headers),
	}
}

func requestHeaders(ipv6 bool) map[string]string {
	// try to get token first, if it fails, fallback to IMDSv1
	token := getToken(ipv6)
	if token == "" {
		log.Debugf("token is empty, will fallback to IMDSv1")
	}

	headers := make(map[string]string, 1)
	if token != "" {
		headers["X-aws-ec2-metadata-token"] = token
	}
	return headers
}

func (a *awsEnv) Metadata() map[string]string {
	md := map[string]string{}
	if len(a.availabilityZone) > 0 {
		md[AWSAvailabilityZone] = a.availabilityZone
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
		Zone:   a.availabilityZone,
		Region: a.region,
	}
}

func (a *awsEnv) Labels() map[string]string {
	return map[string]string{}
}

func (a *awsEnv) IsKubernetes() bool {
	return true
}

func getAWSInfo(path string, ipv6 bool, headers map[string]string) (string, error) {
	url := awsMetadataIPv4URL + "/" + path
	if ipv6 {
		url = awsMetadataIPv6URL + "/" + path
	}

	resp, err := http.GET(url, time.Millisecond*100, headers)
	if err != nil {
		log.Debugf("error in getting aws info for %s : %v", path, err)
		return "", err
	}
	return resp.String(), nil
}

// getRegion returns the Region that the instance is running in.
func getRegion(ipv6 bool, headers map[string]string) string {
	region, _ := getAWSInfo("placement/region", ipv6, headers)
	return region
}

// getAvailabilityZone returns the AvailabilityZone that the instance is running in.
func getAvailabilityZone(ipv6 bool, headers map[string]string) string {
	az, _ := getAWSInfo("placement/availability-zone", ipv6, headers)
	return az
}

func getInstanceID(ipv6 bool, headers map[string]string) string {
	instance, _ := getAWSInfo("instance-id", ipv6, headers)
	return instance
}

func getToken(ipv6 bool) string {
	url := awsMetadataTokenIPv4URL
	if ipv6 {
		url = awsMetadataTokenIPv6URL
	}

	resp, err := http.PUT(url, time.Millisecond*100, map[string]string{
		// more details can be found at https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/instancedata-data-retrieval.html
		"X-aws-ec2-metadata-token-ttl-seconds": "60",
	})
	if err != nil {
		log.Debugf("error in getting aws token : %v", err)
		return ""
	}
	return resp.String()
}
