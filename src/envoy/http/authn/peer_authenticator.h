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

// PeerAuthenticator performs peer authentication for given policy.
class PeerAuthenticator : public AuthenticatorBase {
 public:
  PeerAuthenticator(FilterContext* filter_context,
                    const DoneCallback& done_callback,
                    const istio::authentication::v1alpha1::Policy& policy);

  void run() override;

 protected:
  // Runs given authentication method.
  void runMethod(
      const istio::authentication::v1alpha1::PeerAuthenticationMethod& method,
      const MethodDoneCallback& done_callback);

  // Callback for runMethod.
  void onMethodDone(const istio::authn::Payload* payload, bool success);

 private:
  // Reference to the authentication policy that the authenticator should
  // enforce. Typically, the actual object is owned by filter.
  const istio::authentication::v1alpha1::Policy& policy_;

  // Internal variable to keep track which peer authentication methods have been
  // tried.
  int peer_method_index_{0};
};

}  // namespace AuthN
}  // namespace Istio
}  // namespace Http
}  // namespace Envoy
