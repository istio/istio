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

package na

import (
	"fmt"
	"time"

	"istio.io/istio/security/pkg/pki"
)

// CertUtil is an interface for utility functions on certificate.
type CertUtil interface {
	// GetWaitTime returns the waiting time before renewing the certificate.
	GetWaitTime([]byte, time.Time, int) (time.Duration, error)
}

// CertUtilImpl is the implementation of CertUtil, for production use.
type CertUtilImpl struct {
}

// GetWaitTime returns the waititng time before renewing the cert, based on current time, the timestamps in cert and
// graceperiod.
func (cu CertUtilImpl) GetWaitTime(certBytes []byte, now time.Time, gracePeriodPercentage int) (time.Duration, error) {
	cert, certErr := pki.ParsePemEncodedCertificate(certBytes)
	if certErr != nil {
		return time.Duration(0), certErr
	}
	timeToExpire := cert.NotAfter.Sub(now)
	if timeToExpire < 0 {
		return time.Duration(0), fmt.Errorf("certificate already expired at %s, but now is %s",
			cert.NotAfter, now)
	}
	gracePeriod := cert.NotAfter.Sub(cert.NotBefore) * time.Duration(gracePeriodPercentage) / time.Duration(100)
	// waitTime is the duration between now and the grace period starts.
	// It is the time until cert expiration minus the length of grace period.
	waitTime := timeToExpire - gracePeriod
	if waitTime < 0 {
		// We are within the grace period.
		return time.Duration(0), fmt.Errorf("got a certificate that should be renewed now")
	}
	return waitTime, nil
}
