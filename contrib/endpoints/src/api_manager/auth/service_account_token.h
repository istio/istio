/* Copyright 2016 Google Inc. All Rights Reserved.
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
#ifndef API_MANAGER_AUTH_SERVICE_ACCOUNT_TOKEN_H_
#define API_MANAGER_AUTH_SERVICE_ACCOUNT_TOKEN_H_

#include <time.h>

#include "contrib/endpoints/include/api_manager/env_interface.h"

namespace google {
namespace api_manager {
namespace auth {

// Stores service account tokens to access Google services, such as service
// control and cloud tracing. There are two kinds of auth token:
// 1) client auth secret is a client secret can be used to generate auth
// JWT token. But JWT token is audience specific. Need to generate auth
// JWT token for each service with its audience.
// 2) GCE service account token is fetched from GCP metadata server.
// This auth token can be used for any Google services.
class ServiceAccountToken {
 public:
  ServiceAccountToken(ApiManagerEnvInterface* env) : env_(env), state_(NONE) {}

  // Sets the client auth secret and it can be used to generate JWT token.
  utils::Status SetClientAuthSecret(const std::string& secret);

  // Fetching state of the token from the metadata server
  enum FetchState { NONE = 0, FETCHING, FETCHED, FAILED };

  // Set fetching state
  void set_state(FetchState state) { state_ = state; }

  // Get fetching state
  FetchState state() const { return state_; }

  // Returns whether the client auth secret exists
  bool has_client_secret() const { return !client_auth_secret_.empty(); }

  // Set access token value and expiration duration
  void set_access_token(const std::string& token, time_t expiration) {
    access_token_.set_token(token, expiration);
  }

  // Returns true if access token is valid `duration` seconds from now.
  // Use 0 for `duration` to check if the token is valid now.
  bool is_access_token_valid(time_t duration) const {
    return access_token_.is_valid(duration);
  }

  // JWT token calcualted from client auth secret are audience dependent.
  enum JWT_TOKEN_TYPE {
    JWT_TOKEN_FOR_SERVICE_CONTROL = 0,
    JWT_TOKEN_FOR_CLOUD_TRACING,
    JWT_TOKEN_FOR_FIREBASE,

    // JWT token for accessing the http endpoints defined in Firebase Rules.
    JWT_TOKEN_FOR_AUTHORIZATION_SERVICE,
    JWT_TOKEN_FOR_QUOTA_CONTROL,
    JWT_TOKEN_FOR_SERVICEMANAGEMENT_SERVICES,
    JWT_TOKEN_TYPE_MAX,
  };
  // Set audience.  Only calcualtes JWT token with specified audience.
  void SetAudience(JWT_TOKEN_TYPE type, const std::string& audience);

  // Gets the auth token to access Google services.
  // If client auth secret is specified, use it to calcualte JWT token.
  // Otherwise, use the access token fetched from metadata server.
  const std::string& GetAuthToken(JWT_TOKEN_TYPE type);

  // Gets the auth token to access Google services. This method accepts an
  // audience parameter to set when generating JWT token.
  // If client auth secret is specified, use it to calcualte JWT token.
  // Otherwise, use the access token fetched from metadata server.
  const std::string& GetAuthToken(JWT_TOKEN_TYPE type,
                                  const std::string& audience);

 private:
  // Stores base token info. Used for both OAuth and JWT tokens.
  class TokenInfo {
   public:
    // Token available and not expired in `duration` seconds
    bool is_valid(time_t duration) const {
      return !token_.empty() && expiration_time_ >= time(nullptr) + duration;
    }

    // Set token and its expiration duration
    void set_token(const std::string& token, time_t expiration) {
      token_ = token;
      expiration_time_ = time(nullptr) + expiration;
    }

    // Get the token
    const std::string& token() const { return token_; }

    // Get expiration time in seconds
    time_t expiration_time() const { return expiration_time_; }

   private:
    // The auth token.
    std::string token_;
    // The token expiration time.
    time_t expiration_time_;
  };

  // Stores JWT token info
  class JwtTokenInfo : public TokenInfo {
   public:
    void set_audience(const std::string audience) { audience_ = audience; }
    const std::string& audience() const { return audience_; }

    // Generates auth JWT token from client auth secret.
    utils::Status GenerateJwtToken(const std::string& client_auth_secret);

   private:
    // The audiences.
    std::string audience_;
  };

  // environment interface.
  ApiManagerEnvInterface* env_;

  // The client auth secret which can be used to generate JWT auth token.
  std::string client_auth_secret_;

  // JWT tokens calcualted from client auth secrect.
  JwtTokenInfo jwt_tokens_[JWT_TOKEN_TYPE_MAX];

  // GCE service account access token fetched from GCE metadata server.
  TokenInfo access_token_;

  // Fetching state
  FetchState state_;
};

}  // namespace auth
}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_AUTH_SERVICE_ACCOUNT_TOKEN_H_
