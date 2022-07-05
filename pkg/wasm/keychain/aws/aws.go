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

package aws

import (
	"io/ioutil"
	"time"

	"istio.io/istio/pkg/bootstrap/platform"
	"istio.io/istio/pkg/wasm/keychain"

	ecr "github.com/awslabs/amazon-ecr-credential-helper/ecr-login"
	"github.com/google/go-containerregistry/pkg/authn"
)

const (
	// According to https://docs.aws.amazon.com/cli/latest/reference/ecr/get-authorization-token.html#output,
	// the expiration interval of the auth token in Amazon is 12 hour. For safety, let's uses 6 hours.
	ecrCredExpiration = time.Hour * 6
)

func init() {
	// ECR helpers does not provide simple way to cache the credential before expiration at this moment.
	// So, `cachedHelper` keeps the credential for the specified duration.
	keychain.RegisterKeychain(platform.PlatformTypeAWS, authn.NewKeychainFromHelper(
		keychain.WrapHelperWithCache(ecr.NewECRHelper(ecr.WithLogger(ioutil.Discard)), ecrCredExpiration)))
}
