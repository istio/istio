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

#include "config.h"

#include "common/filesystem/filesystem_impl.h"
#include "common/json/json_loader.h"
#include "envoy/json/json_object.h"
#include "envoy/upstream/cluster_manager.h"

#include "rapidjson/document.h"

#include <algorithm>
#include <chrono>
#include <fstream>
#include <iostream>
#include <memory>
#include <string>
#include <vector>

namespace Envoy {
namespace Http {
namespace Auth {

void AsyncClientCallbacks::onSuccess(MessagePtr &&response) {
  std::string status = response->headers().Status()->value().c_str();
  if (status == "200") {
    ENVOY_LOG(debug, "AsyncClientCallbacks [cluster = {}]: success",
              cluster_->name());
    std::string body;
    if (response->body()) {
      auto len = response->body()->length();
      body = std::string(static_cast<char *>(response->body()->linearize(len)),
                         len);
    } else {
      ENVOY_LOG(debug, "AsyncClientCallbacks [cluster = {}]: body is null",
                cluster_->name());
    }
    cb_(true, body);
  } else {
    ENVOY_LOG(debug,
              "AsyncClientCallbacks [cluster = {}]: response status code {}",
              cluster_->name(), status);
    cb_(false, "");
  }
}
void AsyncClientCallbacks::onFailure(AsyncClient::FailureReason) {
  ENVOY_LOG(debug, "AsyncClientCallbacks [cluster = {}]: failed",
            cluster_->name());
  cb_(false, "");
}

void AsyncClientCallbacks::Call(const std::string &uri) {
  ENVOY_LOG(debug, "AsyncClientCallbacks [cluster = {}]: {} {}",
            cluster_->name(), __func__, uri);
  // Example:
  // uri  = "https://example.com/certs"
  // pos  :          ^
  // pos1 :                     ^
  // host = "example.com"
  // path = "/certs"
  auto pos = uri.find("://");
  pos = pos == std::string::npos ? 0 : pos + 3;  // Start position of host
  auto pos1 = uri.find("/", pos);
  if (pos1 == std::string::npos) pos1 = uri.length();
  std::string host = uri.substr(pos, pos1 - pos);
  std::string path = "/" + uri.substr(pos1 + 1);

  MessagePtr message(new RequestMessageImpl());
  message->headers().insertMethod().value().setReference(
      Http::Headers::get().MethodValues.Get);
  message->headers().insertPath().value(path);
  message->headers().insertHost().value(host);

  request_ = cm_.httpAsyncClientForCluster(cluster_->name())
                 .send(std::move(message), *this, timeout_);
}

void AsyncClientCallbacks::Cancel() { request_->cancel(); }

IssuerInfo::IssuerInfo(Json::Object *json) {
  ENVOY_LOG(debug, "IssuerInfo: {}", __func__);
  // Check "name"
  name_ = json->getString("name", "");
  if (name_ == "") {
    ENVOY_LOG(debug, "IssuerInfo: Issuer name missing");
    failed_ = true;
    return;
  }
  // Check "audience". It will be an empty array if the key "audience" does not
  // exist
  try {
    audiences_ = json->getStringArray("audiences", true);
  } catch (...) {
    ENVOY_LOG(debug, "IssuerInfo [name = {}]: Bad audiences", name_);
    failed_ = true;
    return;
  }
  // Check "pubkey"
  Json::ObjectSharedPtr json_pubkey;
  try {
    json_pubkey = json->getObject("pubkey");
  } catch (...) {
    ENVOY_LOG(debug, "IssuerInfo [name = {}]: Public key missing", name_);
    failed_ = true;
    return;
  }
  // Check "type"
  std::string pkey_type_str = json_pubkey->getString("type", "");
  if (pkey_type_str == "pem") {
    pkey_type_ = Pubkeys::PEM;
  } else if (pkey_type_str == "jwks") {
    pkey_type_ = Pubkeys::JWKS;
  } else {
    ENVOY_LOG(debug,
              "IssuerInfo [name = {}]: Public key type missing or invalid",
              name_);
    failed_ = true;
    return;
  }
  // Check "value"
  std::string value = json_pubkey->getString("value", "");
  if (value != "") {
    // Public key is written in this JSON.
    pkey_ = Pubkeys::CreateFrom(value, pkey_type_);
    loaded_ = true;
    return;
  }
  // Check "file"
  std::string path = json_pubkey->getString("file", "");
  if (path != "") {
    // Public key is loaded from the specified file.
    pkey_ = Pubkeys::CreateFrom(Filesystem::fileReadToEnd(path), pkey_type_);
    loaded_ = true;
    return;
  }
  // Check "uri" and "cluster"
  std::string uri = json_pubkey->getString("uri", "");
  std::string cluster = json_pubkey->getString("cluster", "");
  if (uri != "" && cluster != "") {
    // Public key will be loaded from the specified URI.
    uri_ = uri;
    cluster_ = cluster;
    return;
  }

  // Public key not found
  ENVOY_LOG(debug, "IssuerInfo [name = {}]: Public key source missing", name_);
  failed_ = true;
}

bool IssuerInfo::IsAudienceAllowed(const std::string &aud) {
  return audiences_.empty() || (std::find(audiences_.begin(), audiences_.end(),
                                          aud) != audiences_.end());
}

/*
 * TODO: add test for config loading
 */
JwtAuthConfig::JwtAuthConfig(const Json::Object &config,
                             Server::Configuration::FactoryContext &context)
    : cm_(context.clusterManager()) {
  ENVOY_LOG(debug, "JwtAuthConfig: {}", __func__);
  std::string user_info_type_str =
      config.getString("userinfo_type", "payload_base64url");
  if (user_info_type_str == "payload") {
    user_info_type_ = UserInfoType::kPayload;
  } else if (user_info_type_str == "header_payload_base64url") {
    user_info_type_ = UserInfoType::kHeaderPayloadBase64Url;
  } else {
    user_info_type_ = UserInfoType::kPayloadBase64Url;
  }

  pubkey_cache_expiration_sec_ =
      config.getInteger("pubkey_cache_expiration_sec", 600);

  // Load the issuers
  issuers_.clear();
  std::vector<Json::ObjectSharedPtr> issuer_jsons;
  try {
    issuer_jsons = config.getObjectArray("issuers");
  } catch (...) {
    ENVOY_LOG(debug, "JwtAuthConfig: {}, Bad issuers", __func__);
    abort();
  }
  for (auto issuer_json : issuer_jsons) {
    auto issuer = std::make_shared<IssuerInfo>(issuer_json.get());
    issuers_.push_back(issuer);
  }
}

}  // namespace Auth
}  // namespace Http
}  // namespace Envoy
