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

package util

import (
	"fmt"
	"io"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	binversion "istio.io/istio/operator/version"
)

var NeverMatch = &metav1.LabelSelector{
	MatchLabels: map[string]string{
		"istio.io/deactivated": "never-match",
	},
}

var ManifestsFlagHelpStr = `Specify a path to a directory of charts and profiles
(e.g. ~/Downloads/istio-` + binversion.OperatorVersionString + `/manifests).`

// CommandParseError distinguishes an error parsing istioctl CLI arguments from an error processing
type CommandParseError struct {
	Err error
}

func (c CommandParseError) Error() string {
	return c.Err.Error()
}

// Confirm waits for a user to confirm with the supplied message.
func Confirm(msg string, writer io.Writer) bool {
	for {
		_, _ = fmt.Fprintf(writer, "%s ", msg)
		var response string
		_, err := fmt.Scanln(&response)
		if err != nil {
			return false
		}
		switch strings.ToUpper(response) {
		case "Y", "YES":
			return true
		case "N", "NO":
			return false
		}
	}
}
