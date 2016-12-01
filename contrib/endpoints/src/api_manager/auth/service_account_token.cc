// Copyright (C) Extensible Service Proxy Authors
// All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions
// are met:
// 1. Redistributions of source code must retain the above copyright
//    notice, this list of conditions and the following disclaimer.
// 2. Redistributions in binary form must reproduce the above copyright
//    notice, this list of conditions and the following disclaimer in the
//    documentation and/or other materials provided with the distribution.
//
// THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS ``AS IS'' AND
// ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
// IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
// ARE DISCLAIMED.  IN NO EVENT SHALL THE AUTHOR OR CONTRIBUTORS BE LIABLE
// FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
// DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
// OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
// HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
// LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
// OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
// SUCH DAMAGE.
//
////////////////////////////////////////////////////////////////////////////////
//
#include "src/api_manager/auth/service_account_token.h"

#include "google/protobuf/stubs/logging.h"
#include "src/api_manager/auth/lib/auth_token.h"

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
  jwt_tokens_[type].set_audience(audience);
}

const std::string& ServiceAccountToken::GetAuthToken(JWT_TOKEN_TYPE type) {
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
