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
#include "envoy/config/filter/http/jwt_auth/v2alpha1/config.pb.h"
#include "envoy/http/header_map.h"

namespace Envoy {
namespace Http {
namespace JwtAuth {

// Extracts JWT token from locations specified in the config.
//
// The rules of token extraction:
// * Each issuer can specify its token locations either at headers or
//   query parameters.
// * If an issuer doesn't specify any location, following default locations
//   are used:
//     header:  Authorization: Bear <token>
//     query parameter: ?access_token=<token>
// * A token must be extracted from the location specified by its issuer.
//
class JwtTokenExtractor : public Logger::Loggable<Logger::Id::filter> {
 public:
  JwtTokenExtractor(const ::istio::envoy::config::filter::http::jwt_auth::
                        v2alpha1::JwtAuthentication& config);

  // The object to store extracted token.
  // Based on the location the token is extracted from, it also
  // has the allowed issuers that have specified the location.
  class Token {
   public:
    Token(const std::string& token, const std::set<std::string>& issuers,
          bool from_authorization, const LowerCaseString* header_name)
        : token_(token),
          allowed_issuers_(issuers),
          from_authorization_(from_authorization),
          header_name_(header_name) {}

    const std::string& token() const { return token_; }

    bool IsIssuerAllowed(const std::string& issuer) const {
      return allowed_issuers_.find(issuer) != allowed_issuers_.end();
    }

    // TODO: to remove token from query parameter.
    void Remove(HeaderMap* headers) {
      if (from_authorization_) {
        headers->removeAuthorization();
      } else if (header_name_ != nullptr) {
        headers->remove(*header_name_);
      }
    }

   private:
    // Extracted token.
    std::string token_;
    // Allowed issuers specified the location the token is extacted from.
    const std::set<std::string>& allowed_issuers_;
    // True if token is extracted from default Authorization header
    bool from_authorization_;
    // Not nullptr if token is extracted from custom header.
    const LowerCaseString* header_name_;
  };

  // Return the extracted JWT tokens.
  // Only extract one token for now.
  void Extract(const HeaderMap& headers,
               std::vector<std::unique_ptr<Token>>* tokens) const;

 private:
  struct LowerCaseStringCmp {
    bool operator()(const LowerCaseString& lhs,
                    const LowerCaseString& rhs) const {
      return lhs.get() < rhs.get();
    }
  };
  // The map of header to set of issuers
  std::map<LowerCaseString, std::set<std::string>, LowerCaseStringCmp>
      header_maps_;
  // The map of parameters to set of issuers.
  std::map<std::string, std::set<std::string>> param_maps_;
  // Special handling of Authorization header.
  std::set<std::string> authorization_issuers_;
};

}  // namespace JwtAuth
}  // namespace Http
}  // namespace Envoy
