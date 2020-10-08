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

package tokensource

import (
	"fmt"
	"io/ioutil"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"istio.io/pkg/log"
)

var (
	fileTokenLog = log.RegisterScope("filetoken", "OAuth File Token Source", 0)
)

// FromFile implement TokenSource support for retrieval from file
type FromFile struct {
	Path   string
	Period time.Duration
}

var _ = oauth2.TokenSource(&FromFile{})

// Token return token from token file
func (ts *FromFile) Token() (*oauth2.Token, error) {
	tokb, err := ioutil.ReadFile(ts.Path)
	if err != nil {
		fileTokenLog.Errorf("failed to read token file %q: %v", ts.Path, err)
		return nil, fmt.Errorf("failed to read token file %q: %v", ts.Path, err)
	}
	tok := strings.TrimSpace(string(tokb))
	if len(tok) == 0 {
		fileTokenLog.Errorf("read empty token from file %q", ts.Path)
		return nil, fmt.Errorf("read empty token from file %q", ts.Path)
	}

	return &oauth2.Token{
		AccessToken: tok,
		Expiry:      time.Now().Add(ts.Period),
	}, nil
}
