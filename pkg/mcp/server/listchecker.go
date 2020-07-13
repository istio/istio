//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package server

import (
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
	"google.golang.org/grpc/credentials"

	"istio.io/istio/security/pkg/pki/util"
	"istio.io/pkg/log"
)

var scope = log.RegisterScope("mcp", "mcp debugging", 0)

// AllowAllChecker is a simple auth checker that allows all requests.
type AllowAllChecker struct{}

// NewAllowAllChecker creates a new AllowAllChecker.
func NewAllowAllChecker() *AllowAllChecker { return &AllowAllChecker{} }

// Check is an implementation of AuthChecker.Check that allows all check requests.
func (*AllowAllChecker) Check(credentials.AuthInfo) error { return nil }

// AuthListMode indicates the list checking mode
type AuthListMode bool

const (
	// AuthDenylist indicates that the list should work as a deny list
	AuthDenylist AuthListMode = false

	// AuthAllowlist indicates that the list should work as an allowlist
	AuthAllowlist AuthListMode = true
)

// ListAuthChecker implements AuthChecker function and is backed by a set of ids.
type ListAuthChecker struct {
	mode     AuthListMode
	idsMutex sync.RWMutex
	ids      map[string]struct{}

	checkFailureRecordLimiter   *rate.Limiter
	failureCountSinceLastRecord int

	// overridable functions for testing
	extractIDsFn func(exts []pkix.Extension) ([]string, error)
}

type ListAuthCheckerOptions struct {
	// For the purposes of logging rate limiting authz failures, this controls how
	// many authz failures are logged in a burst every AuthzFailureLogFreq.
	AuthzFailureLogBurstSize int

	// For the purposes of logging rate limiting authz failures, this controls how
	// frequently bursts of authz failures are logged.
	AuthzFailureLogFreq time.Duration

	// AuthMode indicates the list checking mode
	AuthMode AuthListMode
}

func DefaultListAuthCheckerOptions() *ListAuthCheckerOptions {
	return &ListAuthCheckerOptions{
		AuthzFailureLogBurstSize: 1,
		AuthzFailureLogFreq:      time.Minute,
		AuthMode:                 AuthAllowlist,
	}
}

// NewListAuthChecker returns a new instance of ListAuthChecker
func NewListAuthChecker(options *ListAuthCheckerOptions) *ListAuthChecker {
	// Initialize record limiter for the auth checker.
	limit := rate.Every(options.AuthzFailureLogFreq)
	limiter := rate.NewLimiter(limit, options.AuthzFailureLogBurstSize)

	return &ListAuthChecker{
		mode:                      options.AuthMode,
		ids:                       make(map[string]struct{}),
		extractIDsFn:              util.ExtractIDs,
		checkFailureRecordLimiter: limiter,
	}
}

// Add the provided id to the list of ids.
func (l *ListAuthChecker) Add(id string) {
	l.idsMutex.Lock()
	defer l.idsMutex.Unlock()

	l.ids[id] = struct{}{}
}

// Remove the provided id from the list of ids.
func (l *ListAuthChecker) Remove(id string) {
	l.idsMutex.Lock()
	defer l.idsMutex.Unlock()

	delete(l.ids, id)
}

// Set new sets of ids. Previous ones are removed.
func (l *ListAuthChecker) Set(ids ...string) {

	newIds := make(map[string]struct{})
	for _, id := range ids {
		newIds[id] = struct{}{}
	}

	l.idsMutex.Lock()
	defer l.idsMutex.Unlock()
	l.ids = newIds
}

// SetMode sets the list-checking mode for this list.
func (l *ListAuthChecker) SetMode(mode AuthListMode) {
	l.idsMutex.Lock()
	defer l.idsMutex.Unlock()
	l.mode = mode
}

// Allowed checks whether the given id is allowed.
func (l *ListAuthChecker) Allowed(id string) bool {
	l.idsMutex.RLock()
	defer l.idsMutex.RUnlock()

	if l.mode == AuthAllowlist {
		return l.contains(id)
	}
	return !l.contains(id) // AuthDenylist
}

func (l *ListAuthChecker) contains(id string) bool {
	_, found := l.ids[id]
	return found
}

// String is an implementation of Stringer.String.
func (l *ListAuthChecker) String() string {
	l.idsMutex.RLock()
	defer l.idsMutex.RUnlock()
	var ids []string
	for id := range l.ids {
		ids = append(ids, id)
	}

	sort.Strings(ids)

	result := ""
	switch l.mode {
	case AuthAllowlist:
		result += "Mode: allowlist\n"
	case AuthDenylist:
		result += "Mode: denylist\n"
	}

	result += "Known ids:\n"
	result += strings.Join(ids, "\n")

	return result
}

func (l *ListAuthChecker) Check(authInfo credentials.AuthInfo) error {
	if err := l.check(authInfo); err != nil {
		l.failureCountSinceLastRecord++
		if l.checkFailureRecordLimiter.Allow() {
			scope.Warnf("NewConnection: auth check failed: %v (repeated %d times).",
				err, l.failureCountSinceLastRecord)
			l.failureCountSinceLastRecord = 0
		}
		return err
	}
	return nil
}

// Check is an implementation of AuthChecker.Check.
func (l *ListAuthChecker) check(authInfo credentials.AuthInfo) error {
	l.idsMutex.RLock()
	defer l.idsMutex.RUnlock()

	// Extract the identities

	if authInfo == nil {
		// Do not allow access
		return errors.New("denying by default: no auth info found")
	}

	tlsInfo, ok := authInfo.(credentials.TLSInfo)
	if !ok {
		return errors.New("unable to extract TLS info from the supplied auth info")
	}

	for i, arr := range tlsInfo.State.VerifiedChains {
		for j, cert := range arr {
			ids, err := l.extractIDsFn(cert.Extensions)
			// The error maybe due to SAN extensions not existing in a particular certificate.
			// Simply skip to the next one.
			if err != nil {
				scope.Debugf("Error during id extraction from certificate (%d,%d): %v", i, j, err)
				continue
			}

			for _, id := range ids {
				if l.contains(id) {
					switch l.mode {
					case AuthAllowlist:
						scope.Infof("Allowing access from peer with id: %s", id)
						return nil
					case AuthDenylist:
						scope.Infof("Blocking access from peer with id: %s", id)
						return fmt.Errorf("id is denylisted: %s", id)
					}
				}
			}
		}
	}

	if l.mode == AuthAllowlist {
		return errors.New("no allowed identity found in peer's authentication info")
	}
	return nil // AuthDenylist
}
