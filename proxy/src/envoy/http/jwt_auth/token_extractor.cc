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

#include "src/envoy/http/jwt_auth/token_extractor.h"
#include "common/common/utility.h"
#include "common/http/utility.h"

using ::istio::envoy::config::filter::http::jwt_auth::v2alpha1::
    JwtAuthentication;

namespace Envoy {
namespace Http {
namespace JwtAuth {
namespace {

// The autorization bearer prefix.
const std::string kBearerPrefix = "Bearer ";

// The query parameter name to get JWT token.
const std::string kParamAccessToken = "access_token";

}  // namespace

JwtTokenExtractor::JwtTokenExtractor(const JwtAuthentication& config) {
  for (const auto& jwt : config.rules()) {
    bool use_default = true;
    if (jwt.from_headers_size() > 0) {
      use_default = false;
      for (const auto& header : jwt.from_headers()) {
        auto& issuers = header_maps_[LowerCaseString(header.name())];
        issuers.insert(jwt.issuer());
      }
    }
    if (jwt.from_params_size() > 0) {
      use_default = false;
      for (const std::string& param : jwt.from_params()) {
        auto& issuers = param_maps_[param];
        issuers.insert(jwt.issuer());
      }
    }

    // If not specified, use default
    if (use_default) {
      authorization_issuers_.insert(jwt.issuer());

      auto& param_issuers = param_maps_[kParamAccessToken];
      param_issuers.insert(jwt.issuer());
    }
  }
}

void JwtTokenExtractor::Extract(
    const HeaderMap& headers,
    std::vector<std::unique_ptr<JwtTokenExtractor::Token>>* tokens) const {
  if (!authorization_issuers_.empty()) {
    const HeaderEntry* entry = headers.Authorization();
    if (entry) {
      // Extract token from header.
      const HeaderString& value = entry->value();
      if (StringUtil::startsWith(value.c_str(), kBearerPrefix, true)) {
        tokens->emplace_back(new Token(value.c_str() + kBearerPrefix.length(),
                                       authorization_issuers_, true, nullptr));
        // Only take the first one.
        return;
      }
    }
  }

  // Check header first
  for (const auto& header_it : header_maps_) {
    const HeaderEntry* entry = headers.get(header_it.first);
    if (entry) {
      tokens->emplace_back(
          new Token(std::string(entry->value().c_str(), entry->value().size()),
                    header_it.second, false, &header_it.first));
      // Only take the first one.
      return;
    }
  }

  if (param_maps_.empty() || headers.Path() == nullptr) {
    return;
  }

  const auto& params = Utility::parseQueryString(std::string(
      headers.Path()->value().c_str(), headers.Path()->value().size()));
  for (const auto& param_it : param_maps_) {
    const auto& it = params.find(param_it.first);
    if (it != params.end()) {
      tokens->emplace_back(
          new Token(it->second, param_it.second, false, nullptr));
      // Only take the first one.
      return;
    }
  }
}

}  // namespace JwtAuth
}  // namespace Http
}  // namespace Envoy
