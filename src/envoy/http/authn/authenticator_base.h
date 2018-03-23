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
#include "common/common/logger.h"
#include "src/envoy/http/authn/filter_context.h"
#include "src/istio/authn/context.pb.h"

namespace Envoy {
namespace Http {
namespace Istio {
namespace AuthN {

// AuthenticatorBase is the base class for authenticator. It provides functions
// to perform individual authentication methods, which can be used to construct
// compound authentication flow.
class AuthenticatorBase : public Logger::Loggable<Logger::Id::filter> {
 public:
  // Callback type for individual authentication method.
  typedef std::function<void(istio::authn::Payload*, bool)> MethodDoneCallback;

  // Callback type for the whole authenticator.
  typedef std::function<void(bool)> DoneCallback;

  AuthenticatorBase(FilterContext* filter_context,
                    const DoneCallback& callback);
  virtual ~AuthenticatorBase();

  // Perform authentication.
  virtual void run() PURE;

  // Calls done_callback_ with success value.
  void done(bool success) const;

  // Validates x509 given the params (more or less, just check if x509 exists,
  // actual validation is not neccessary as it already done when the connection
  // establish), and extract authenticate attributes (just user/identity for
  // now). Calling callback with the extracted payload and corresponding status.
  virtual void validateX509(
      const istio::authentication::v1alpha1::MutualTls& params,
      const MethodDoneCallback& done_callback) const;

  // Validates JWT given the jwt params. If JWT is validated, it will call
  // the callback function with the extracted attributes and claims (JwtPayload)
  // and status SUCCESS. Otherwise, calling callback with status FAILED.
  virtual void validateJwt(const istio::authentication::v1alpha1::Jwt& params,
                           const MethodDoneCallback& done_callback);

  // Mutable accessor to filter context.
  FilterContext* filter_context() { return &filter_context_; }

 private:
  // Pointer to filter state. Do not own.
  FilterContext& filter_context_;

  const DoneCallback done_callback_;
};

}  // namespace AuthN
}  // namespace Istio
}  // namespace Http
}  // namespace Envoy
