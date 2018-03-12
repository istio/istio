/* Copyright 2018 Istio Authors. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

#pragma once

#include "authentication/v1alpha1/policy.pb.h"
#include "src/envoy/http/authn/authenticator_base.h"

namespace Envoy {
namespace Http {
namespace Istio {
namespace AuthN {

// OriginAuthenticator performs origin authentication for given credential rule.
class OriginAuthenticator : public AuthenticatorBase {
 public:
  OriginAuthenticator(
      FilterContext* filter_context, const DoneCallback& done_callback,
      const istio::authentication::v1alpha1::CredentialRule& credential_rule);

  void run() override;

 protected:
  // Runs specific authentication method.
  void runMethod(
      const istio::authentication::v1alpha1::OriginAuthenticationMethod& method,
      const MethodDoneCallback& callback);

  // Callback for runMethod.
  void onMethodDone(const Payload* payload, bool success);

 private:
  // Reference to the credential rule that the authenticator should enforce. The
  // object is typically owned by filter.
  const istio::authentication::v1alpha1::CredentialRule& credential_rule_;

  // Internal variable to keep track which (origin) authentication methods have
  // been tried.
  int method_index_{0};
};

}  // namespace AuthN
}  // namespace Istio
}  // namespace Http
}  // namespace Envoy
