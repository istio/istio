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

#include "common/common/logger.h"
#include "envoy/http/async_client.h"

#include "src/envoy/http/jwt_auth/auth_store.h"

namespace Envoy {
namespace Http {
namespace JwtAuth {

// A per-request JWT authenticator to handle all JWT authentication:
// * fetch remote public keys and cache them.
class JwtAuthenticator : public Logger::Loggable<Logger::Id::filter>,
                         public AsyncClient::Callbacks {
 public:
  JwtAuthenticator(Upstream::ClusterManager& cm, JwtAuthStore& store);

  // The callback interface to notify the completion event.
  class Callbacks {
   public:
    virtual ~Callbacks() {}
    virtual void onDone(const Status& status) PURE;
    virtual void savePayload(const std::string& key,
                             const std::string& payload) PURE;
  };
  void Verify(HeaderMap& headers, Callbacks* callback);

  // Called when the object is about to be destroyed.
  void onDestroy();

  // The HTTP header key to carry the verified JWT payload.
  static const LowerCaseString& JwtPayloadKey();

 private:
  // Fetch a remote public key.
  void FetchPubkey(PubkeyCacheItem* issuer);
  // Following two functions are for AyncClient::Callbacks
  void onSuccess(MessagePtr&& response);
  void onFailure(AsyncClient::FailureReason);

  // Verify with a specific public key.
  void VerifyKey(const PubkeyCacheItem& issuer);

  // Handle the public key fetch done event.
  void OnFetchPubkeyDone(const std::string& pubkey);

  // Calls the callback with status.
  void DoneWithStatus(const Status& status);

  // Return true if it is OK to forward this request without JWT.
  bool OkToBypass();

  // The cluster manager object to make HTTP call.
  Upstream::ClusterManager& cm_;
  // The cache object.
  JwtAuthStore& store_;
  // The JWT object.
  std::unique_ptr<JwtAuth::Jwt> jwt_;
  // The token data
  std::unique_ptr<JwtTokenExtractor::Token> token_;

  // The HTTP request headers
  HeaderMap* headers_{};
  // The on_done function.
  Callbacks* callback_{};

  // The pending uri_, only used for logging.
  std::string uri_;
  // The pending remote request so it can be canceled.
  AsyncClient::Request* request_{};
};

}  // namespace JwtAuth
}  // namespace Http
}  // namespace Envoy
