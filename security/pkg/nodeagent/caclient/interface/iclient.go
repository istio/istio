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

package iclient

import (
	"context"
)

// Client interface defines the clients need to implement to talk to CA for CSR.
type Client interface {
	// if withToken set to true the CSR request will attach the token , otherwise it will not
	CSRSign(ctx context.Context, reqID string, csrPEM []byte, subjectID string,
		certValidTTLInSec int64, withToken bool) ([]string /*PEM-encoded certificate chain*/, error)
	/* if the isRotate is set to True, which means it is reconnected special for rotate cert case
		then it will reconnect using cert located in environment variable PROV_CERT file path
	 otherwise it is used for start and restart case then client will use the cert located in environment variable
	 OUTPUT_CERTS file path
	*/
	Reconnect(isRotate bool) error
}
