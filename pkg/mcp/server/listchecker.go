//  Copyright 2018 Istio Authors
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
	"sort"
	"strings"
	"sync"

	"google.golang.org/grpc/credentials"

	"istio.io/istio/security/pkg/pki/util"
)

// ListAuthChecker implements AuthChecker function and is backed by a set of ids.
type ListAuthChecker struct {
	idsMutex sync.Mutex
	ids      map[string]struct{}

	// overridable functions for testing
	extractIDsFn func(exts []pkix.Extension) ([]string, error)
}

var _ AuthChecker = &ListAuthChecker{}

// NewListAuthChecker returns a new instance of ListAuthChecker
func NewListAuthChecker() *ListAuthChecker {
	return &ListAuthChecker{
		ids:          make(map[string]struct{}),
		extractIDsFn: util.ExtractIDs,
	}
}

// Add the provided id to the list of allowed ids.
func (l *ListAuthChecker) Add(id string) {
	l.idsMutex.Lock()
	defer l.idsMutex.Unlock()

	l.ids[id] = struct{}{}
}

// Remove the provided id from the list of allowed ids.
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

// Allowed checks whether the given id is allowed.
func (l *ListAuthChecker) Allowed(id string) bool {
	l.idsMutex.Lock()
	defer l.idsMutex.Unlock()

	_, found := l.ids[id]
	return found
}

// String is an implementation of Stringer.String.
func (l *ListAuthChecker) String() string {
	var ids []string
	for id := range l.ids {
		ids = append(ids, id)
	}

	sort.Strings(ids)

	result := `Allowed ids:
`
	result += strings.Join(ids, "\n")

	return result
}

// Check is an implementation of AuthChecker.Check.
func (l *ListAuthChecker) Check(authInfo credentials.AuthInfo) error {
	l.idsMutex.Lock()
	defer l.idsMutex.Unlock()

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
				if _, ok := l.ids[id]; ok {
					scope.Infof("Allowing access from peer with id: %s", id)
					return nil
				}
			}
		}
	}

	return errors.New("no allowed identity found in peer's authentication info")
}
