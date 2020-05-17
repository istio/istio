// Copyright 2020 Istio Authors
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

package util

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"strings"
	"sync"
	"time"

	"istio.io/pkg/filewatcher"
	"istio.io/pkg/log"
)

var (
	jwtFileReloadInitialTimeout = time.Second      // Fail quickly during start.
	jwtFileReloadTimeout        = time.Second * 10 // 10 second tolerance for file read retries.
)

// JwtLoader loads a JWT from a file and refreshes when the file changes.
type JwtLoader struct {
	JwtPath  string
	jwt      string
	jwtMutex sync.RWMutex
	watcher  filewatcher.FileWatcher
}

var jwtutilLog = log.RegisterScope("jwtutil", "JWT util", 0)

// NewJwtLoader creates a new JwtLoader instance.
func NewJwtLoader(jwtPath string) (*JwtLoader, error) {
	watcher := filewatcher.NewWatcher()
	if err := watcher.Add(jwtPath); err != nil {
		return nil, fmt.Errorf("error adding watcher for file [%s]: %v", jwtPath, err)
	}
	loader := &JwtLoader{
		JwtPath: jwtPath,
		jwt:     "",
		watcher: watcher,
	}
	if err := loader.loadJwtWithTimeout(jwtFileReloadInitialTimeout); err != nil {
		return nil, err
	}
	jwtutilLog.Infof("JWT loader created for file [%s]", jwtPath)
	return loader, nil
}

// Run receives notification from the watcher.
func (l *JwtLoader) Run(stopCh chan struct{}) {
	var timerC <-chan time.Time
	for {
		select {
		case <-timerC:
			timerC = nil
			if err := l.loadJwtWithTimeout(jwtFileReloadTimeout); err != nil {
				jwtutilLog.Errorf("Failed to load JWT from file: %v", err)
			}
		case e := <-l.watcher.Events(l.JwtPath):
			if len(e.Op.String()) > 0 { // To avoid spurious events, use a timer to debounce watch updates.
				if timerC == nil {
					timerC = time.After(500 * time.Millisecond)
				}
			}
		case <-stopCh:
			jwtutilLog.Info("Received stop message. Exit...")
			return
		}
	}
}

// GetJwt returns the JWT currently loaded.
func (l *JwtLoader) GetJwt() string {
	l.jwtMutex.RLock()
	defer l.jwtMutex.RUnlock()
	return l.jwt
}

// loadJwtWithTimeout can tolerate file read errors and conduct retries until timeout.
func (l *JwtLoader) loadJwtWithTimeout(timeout time.Duration) error {
	timeoutCh := time.After(timeout)
	interval := time.Millisecond * 100
	ticker := time.NewTicker(interval)
	for {
		select {
		case <-timeoutCh:
			ticker.Stop()
			return fmt.Errorf("exhausted retries after %v", jwtFileReloadTimeout)
		case <-ticker.C:
			err := l.loadJwt()
			if err == nil {
				ticker.Stop()
				return nil
			}
			ticker.Stop()
			interval *= 2
			jwtutilLog.Errorf("failed to read JWT [%s]: %v. Will retry in %v", l.JwtPath, err, interval)
			ticker = time.NewTicker(interval)
		}
	}
}

func (l *JwtLoader) loadJwt() error {
	tokenBytes, rErr := ioutil.ReadFile(l.JwtPath)
	if rErr != nil {
		return fmt.Errorf("failed to read JWT [%s]: %v", l.JwtPath, rErr)
	}
	token := string(tokenBytes)
	expired, expErr := IsJwtExpired(token, time.Now())
	if expErr != nil {
		return fmt.Errorf("failed to validate JWT expiration [%s]: %v. JWT: %s", l.JwtPath, expErr, token)
	}
	if expired {
		return fmt.Errorf("loaded JWT is expired [%s]", l.JwtPath)
	}
	jwtutilLog.Infof("Loaded new JWT content from file [%s]", l.JwtPath)
	l.jwtMutex.Lock()
	l.jwt = token
	l.jwtMutex.Unlock()
	return nil
}

// GetJwtExpiration gets the expiration time of a JWT, without validating it.
func GetJwtExpiration(token string) (time.Time, error) {
	claims, err := parseJwtClaims(token)
	if err != nil {
		return time.Now(), err
	}

	if claims["iat"] == nil {
		// The JWT doesn't have "iat", so it's always valid. E.g., the K8s first party JWT.
		// In this case, return expiration date after 5 years.
		return time.Now().Add(time.Hour * 24 * 365 * 5), nil
	}
	var expiration time.Time
	switch iat := claims["iat"].(type) {
	case float64:
		expiration = time.Unix(int64(iat), 0)
	case json.Number:
		v, _ := iat.Int64()
		expiration = time.Unix(v, 0)
	}
	return expiration, nil
}

// IsJwtExpired checks if the JWT token is expired compared with the given time, without validating it.
func IsJwtExpired(token string, now time.Time) (bool, error) {
	expiration, err := GetJwtExpiration(token)
	if err != nil {
		return true, err
	}
	if now.After(expiration) {
		return true, nil
	}
	return false, nil

}

func parseJwtClaims(token string) (map[string]interface{}, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("token contains an invalid number of segments: %d, expected: 3", len(parts))
	}

	// Decode the second part.
	claimBytes, err := decodeSegment(parts[1])
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewBuffer(claimBytes))

	claims := make(map[string]interface{})
	if err := dec.Decode(&claims); err != nil {
		return nil, fmt.Errorf("failed to decode the JWT claims")
	}
	return claims, nil
}

func decodeSegment(seg string) ([]byte, error) {
	if l := len(seg) % 4; l > 0 {
		seg += strings.Repeat("=", 4-l)
	}

	return base64.URLEncoding.DecodeString(seg)
}
