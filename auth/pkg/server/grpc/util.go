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

package grpc

import (
	"crypto/x509/pkix"

	"github.com/golang/glog"
	"istio.io/auth/pkg/pki"
)

// This method first finds the SAN extension from the given extension set, then
// extract identities from the SAN extension.
func extractIDs(exts []pkix.Extension) []string {
	sanExt := pki.ExtractSANExtension(exts)
	if sanExt == nil {
		glog.Info("a SAN extension does not exist and thus no identities are extracted")

		return nil
	}

	idsWithType, err := pki.ExtractIDsFromSAN(sanExt)
	if err != nil {
		glog.Warningf("failed to extract identities from SAN extension (error %v)", err)

		return nil
	}

	ids := []string{}
	for _, id := range idsWithType {
		ids = append(ids, string(id.Value))
	}
	return ids
}
