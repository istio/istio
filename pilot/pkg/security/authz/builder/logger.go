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

package builder

import (
	"fmt"
	"strings"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/pkg/log"
)

var (
	authzLog = log.RegisterScope("authorization", "Istio Authorization Policy", 0)
)

type AuthzLogger struct {
	debugMsg []string
	errMsg   error
}

func (al *AuthzLogger) Debugf(format string, args ...interface{}) {
	al.debugMsg = append(al.debugMsg, fmt.Sprintf(format, args...))
}

func (al *AuthzLogger) AppendError(err error) {
	al.errMsg = multierror.Append(al.errMsg, err)
}

func (al *AuthzLogger) Report(in *plugin.InputParams) {
	if al.errMsg != nil {
		authzLog.Errorf("Processed authorization policy for node %s with errors:\n%v", in.Node.ID, multierror.Flatten(al.errMsg))
	}
	if authzLog.DebugEnabled() && len(al.debugMsg) != 0 {
		out := strings.Join(al.debugMsg, "\n\t* ")
		authzLog.Debugf("Processed authorization policy for %s with details:\n\t* %v", in.Node.ID, out)
	} else {
		authzLog.Debugf("Processed authorization policy for %s", in.Node.ID)
	}
}
