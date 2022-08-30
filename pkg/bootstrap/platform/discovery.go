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
	"sync"
	"time"

	"istio.io/pkg/env"
	"istio.io/pkg/log"
)

const (
	defaultTimeout = 5 * time.Second
	numPlatforms   = 3
)

var CloudPlatform = env.Register("CLOUD_PLATFORM", "", "Cloud Platform on which proxy is running, if not specified, "+
	"Istio will try to discover the platform. Valid platform values are aws, azure, gcp, none").Get()

// Discover attempts to discover the host platform, defaulting to
// `Unknown` if a platform cannot be discovered.
func Discover(ipv6 bool) Environment {
	// First check if user has specified platform - use it if provided.
	if len(CloudPlatform) > 0 {
		switch strings.ToLower(CloudPlatform) {
		case "aws":
			return NewAWS(ipv6)
		case "azure":
			return NewAzure()
		case "gcp":
			return NewGCP()
		case "none":
			return &Unknown{}
		}
	}
	// Discover the platform if user has not specified.
	return DiscoverWithTimeout(defaultTimeout, ipv6)
}

// DiscoverWithTimeout attempts to discover the host platform, defaulting to
// `Unknown` after the provided timeout.
func DiscoverWithTimeout(timeout time.Duration, ipv6 bool) Environment {
	plat := make(chan Environment, numPlatforms) // sized to match number of platform goroutines
	done := make(chan bool)

	var wg sync.WaitGroup
	wg.Add(numPlatforms) // check GCP, AWS, and Azure

	go func() {
		if IsGCP() {
			log.Info("platform detected is GCP")
			plat <- NewGCP()
		}
		wg.Done()
	}()

	go func() {
		if IsAWS(ipv6) {
			log.Info("platform detected is AWS")
			plat <- NewAWS(ipv6)
		}
		wg.Done()
	}()

	go func() {
		if IsAzure() {
			log.Info("platform detected is Azure")
			plat <- NewAzure()
		}
		wg.Done()
	}()

	go func() {
		wg.Wait()
		close(done)
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case p := <-plat:
		return p
	case <-done:
		select {
		case p := <-plat:
			return p
		default:
			return &Unknown{}
		}
	case <-timer.C:
		log.Info("timed out waiting for platform detection, treating it as Unknown")
		return &Unknown{}
	}
}
