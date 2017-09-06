/* Copyright 2017 Istio Authors. All Rights Reserved.
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

#ifndef PROXY_CONFIG_H
#define PROXY_CONFIG_H

#include "jwt.h"

#include "common/common/logger.h"
#include "common/http/message_impl.h"
#include "envoy/http/async_client.h"
#include "envoy/json/json_object.h"
#include "envoy/json/json_object.h"
#include "envoy/upstream/cluster_manager.h"
#include "server/config/network/http_connection_manager.h"

#include <vector>

namespace Envoy {
namespace Http {
namespace Auth {

// Callback class for AsyncClient.
class AsyncClientCallbacks : public AsyncClient::Callbacks,
                             public Logger::Loggable<Logger::Id::http> {
 public:
  AsyncClientCallbacks(Upstream::ClusterManager &cm, const std::string &cluster,
                       std::function<void(bool, const std::string &)> cb)
      : cm_(cm),
        cluster_(cm.get(cluster)->info()),
        timeout_(Optional<std::chrono::milliseconds>()),
        cb_(cb) {}
  // AsyncClient::Callbacks
  void onSuccess(MessagePtr &&response);
  void onFailure(AsyncClient::FailureReason);

  void Call(const std::string &uri);
  void Cancel();

 private:
  Upstream::ClusterManager &cm_;
  Upstream::ClusterInfoConstSharedPtr cluster_;
  Optional<std::chrono::milliseconds> timeout_;
  std::function<void(bool, const std::string &)> cb_;
  AsyncClient::Request *request_;
};

// Struct to hold an issuer's information.
struct IssuerInfo : public Logger::Loggable<Logger::Id::http> {
  // This constructor loads config from JSON. When public key is given via URI,
  // it just keeps URI and cluster name, and public key will be fetched later,
  // namely in decodeHeaders() of the filter class.
  IssuerInfo(Json::Object *json);

  // True if the config loading in the constructor or fetching public key failed
  bool failed_ = false;

  // True if the public key is loaded
  bool loaded_ = false;

  std::string uri_;      // URI for public key
  std::string cluster_;  // Envoy cluster name for public key

  std::string name_;               // e.g. "https://accounts.example.com"
  std::string pkey_type_;          // Format of public key. "jwks" or "pem"
  std::unique_ptr<Pubkeys> pkey_;  // Public key
};

// A config for Jwt auth filter
struct JwtAuthConfig : public Logger::Loggable<Logger::Id::http> {
  // Load the config from envoy config.
  // It will abort when "issuers" is missing or bad-formatted.
  JwtAuthConfig(const Json::Object &config,
                Server::Configuration::FactoryContext &context);

  // It specify which information will be included in the HTTP header of an
  // authenticated request.
  enum UserInfoType {
    kPayload,                // Payload JSON
    kPayloadBase64Url,       // base64url-encoded Payload JSON
    kHeaderPayloadBase64Url  // JWT with signature
  };
  UserInfoType user_info_type_;

  // Time to expire a cached public key (sec).
  int64_t pubkey_cache_expiration_sec_;

  // Audiences
  std::vector<std::string> audiences_;

  // Each element corresponds to an issuer
  std::vector<std::shared_ptr<IssuerInfo> > issuers_;

  Upstream::ClusterManager &cm_;
};

}  // namespace Auth
}  // namespace Http
}  // namespace Envoy

#endif  // PROXY_CONFIG_H
