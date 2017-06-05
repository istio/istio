// Copyright 2016 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
////////////////////////////////////////////////////////////////////////////////
//
#include "contrib/endpoints/src/api_manager/auth/service_account_token.h"

#include "contrib/endpoints/src/api_manager/auth/lib/auth_token.h"
#include "google/protobuf/stubs/logging.h"

using ::google::api_manager::utils::Status;
using ::google::protobuf::util::error::Code;

namespace google {
namespace api_manager {
namespace auth {

namespace {

// The expiration time in seconds for auth token calculated from client secret.
// This should not be changed since it has to match the hard-coded value in
// esp_get_auth_token() function.
// Token expired in 1 hour, reduce 100 seconds for grace buffer.
const int kClientSecretAuthTokenExpiration(3600 - 100);

}  // namespace

Status ServiceAccountToken::SetClientAuthSecret(const std::string& secret) {
  if (secret.empty()) {
    env_->LogDebug("SetClientAuthSecret called with empty secret");
    return Status::OK;
  }

  client_auth_secret_ = secret;
  for (unsigned int i = 0; i < JWT_TOKEN_TYPE_MAX; i++) {
    if (!jwt_tokens_[i].audience().empty()) {
      Status status = jwt_tokens_[i].GenerateJwtToken(client_auth_secret_);
      if (!status.ok()) {
        if (env_) {
          env_->LogError("Failed to generate auth token.");
        }
        return status;
      }
    }
  }
  return Status::OK;
}

void ServiceAccountToken::SetAudience(JWT_TOKEN_TYPE type,
                                      const std::string& audience) {
  GOOGLE_CHECK(type >= 0 && type < JWT_TOKEN_TYPE_MAX);
  if (jwt_tokens_[type].audience() != audience) {
    jwt_tokens_[type].set_token("", 0);
    jwt_tokens_[type].set_audience(audience);
  }
}

const std::string& ServiceAccountToken::GetAuthToken(JWT_TOKEN_TYPE type) {
  return GetAuthToken(type, jwt_tokens_[type].audience());
}

const std::string& ServiceAccountToken::GetAuthToken(
    JWT_TOKEN_TYPE type, const std::string& audience) {
  SetAudience(type, audience);

  // Uses authentication secret if available.
  if (!client_auth_secret_.empty()) {
    GOOGLE_CHECK(type >= 0 && type < JWT_TOKEN_TYPE_MAX);
    if (!jwt_tokens_[type].is_valid(0)) {
      Status status = jwt_tokens_[type].GenerateJwtToken(client_auth_secret_);
      if (!status.ok()) {
        if (env_) {
          env_->LogError("Failed to generate auth token.");
        }
        static std::string empty;
        return empty;
      }
    }
    return jwt_tokens_[type].token();
  }
  return access_token_.token();
}

Status ServiceAccountToken::JwtTokenInfo::GenerateJwtToken(
    const std::string& client_auth_secret) {
  // Make sure audience is set.
  GOOGLE_CHECK(!audience_.empty());
  char* token =
      auth::esp_get_auth_token(client_auth_secret.c_str(), audience_.c_str());
  if (token == nullptr) {
    return Status(Code::INVALID_ARGUMENT,
                  "Invalid client auth secret, the file may be corrupted.");
  }
  set_token(token, kClientSecretAuthTokenExpiration);
  auth::esp_grpc_free(token);
  return Status::OK;
}

}  // namespace auth
}  // namespace api_manager
}  // namespace google
