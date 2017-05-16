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
#include "contrib/endpoints/src/api_manager/auth/lib/auth_token.h"
#include "contrib/endpoints/src/api_manager/auth/lib/json_util.h"

#include <openssl/hmac.h>
#include <openssl/pem.h>
#include <cstring>
#include <string>

extern "C" {
#include "grpc/grpc.h"
#include "grpc/support/alloc.h"
#include "grpc/support/log.h"
#include "grpc/support/string_util.h"
#include "grpc/support/sync.h"
}

#include "grpc_internals.h"

using std::string;

namespace google {
namespace api_manager {
namespace auth {

namespace {

#define GRPC_AUTH_JSON_TYPE_INVALID "invalid"

const gpr_timespec TOKEN_LIFETIME = {3600, 0, GPR_TIMESPAN};

char *GenerateTokenSymmetricKey(const grpc_json *json, const char *audience);

char *GenerateJoseHeader(const char *algorithm);
char *GenerateJwtClaim(const char *issuer, const char *subject,
                       const char *audience, gpr_timespec token_lifetime);
char *GenerateSignatueHs256(const char *data, const char *key);
string DotConcat(const string &str1, const string &str2);

}  // namespace

// TODO: this function can return a string instead of char* that need to be
// freed.
char *esp_get_auth_token(const char *json_secret, const char *audience) {
  char *scratchpad = gpr_strdup(json_secret);
  grpc_json *json = grpc_json_parse_string(scratchpad);
  char *token = nullptr;

  if (GetStringValue(json, "client_secret") != nullptr) {  // Symmetric key.
    token = GenerateTokenSymmetricKey(json, audience);
  } else {
    grpc_auth_json_key json_key =
        grpc_auth_json_key_create_from_string(json_secret);
    if (strcmp(json_key.type, GRPC_AUTH_JSON_TYPE_INVALID) != 0) {
      token = grpc_jwt_encode_and_sign(&json_key, audience, TOKEN_LIFETIME,
                                       nullptr);
      grpc_auth_json_key_destruct(&json_key);
    }
  }

  if (json != nullptr) grpc_json_destroy(json);
  gpr_free(scratchpad);
  return token;
}

void esp_grpc_free(char *token) { gpr_free(token); }

// Parses a JSON service account auth token in the following format:
// {
//   "access_token":" ... ",
//   "expires_in":100,
//   "token_type":"Bearer"
// }
// Returns true on success, false otherwise. On success, *token is set to the
// malloc'd auth token (value of 'access_token' JSON property) and *expires is
// set to the value of 'expires_in' property (token expiration in seconds.
bool esp_get_service_account_auth_token(char *input, size_t size, char **token,
                                        int *expires) {
  bool result = false;  // fail by default
  grpc_json *json = grpc_json_parse_string_with_len(input, size);
  if (json) {
    const char *access_token = GetStringValue(json, "access_token");
    const char *expires_in = GetNumberValue(json, "expires_in");

    if (access_token && expires_in) {
      *token = strdup(access_token);
      if (*token) {
        *expires = atoi(expires_in);
        result = true;
      }
    }
    grpc_json_destroy(json);
  }
  return result;
}

namespace {

char *GenerateTokenSymmetricKey(const grpc_json *json, const char *audience) {
  const char *issuer = GetStringValue(json, "issuer");
  if (issuer == nullptr) {
    // TODO: move to common logging.
    gpr_log(GPR_ERROR, "Missing issuer");
    return nullptr;
  }
  const char *subject = GetStringValue(json, "subject");
  if (subject == nullptr) {
    gpr_log(GPR_ERROR, "Missing subject");
    return nullptr;
  }

  char *header = GenerateJoseHeader("HS256");
  if (header == nullptr) {
    gpr_log(GPR_ERROR, "Unable to create JOSE header");
    return nullptr;
  }
  char *claims = GenerateJwtClaim(issuer, subject, audience, TOKEN_LIFETIME);
  if (claims == nullptr) {
    gpr_log(GPR_ERROR, "Unable to create JWT claims payload");
    return nullptr;
  }
  string signed_data = DotConcat(header, claims);
  gpr_free(header);
  gpr_free(claims);

  const char *secret = GetStringValue(json, "client_secret");
  char *signature = GenerateSignatueHs256(signed_data.c_str(), secret);
  if (signature == nullptr) {
    gpr_log(GPR_ERROR, "Unable to compute HS256 signature");
    return nullptr;
  }

  string result = DotConcat(signed_data, signature);
  gpr_free(signature);
  // Match return type of esp_get_auth_token.
  return gpr_strdup(result.c_str());
}

char *GenerateJoseHeader(const char *algorithm) {
  grpc_json json_top;
  memset(&json_top, 0, sizeof(json_top));
  json_top.type = GRPC_JSON_OBJECT;

  grpc_json json_algorithm, json_type;

  FillChild(&json_algorithm, nullptr, &json_top, "alg", algorithm,
            GRPC_JSON_STRING);
  FillChild(&json_type, &json_algorithm, &json_top, "typ", "JWT",
            GRPC_JSON_STRING);

  char *json_str = grpc_json_dump_to_string(&json_top, 0);
  char *result = grpc_base64_encode(json_str, strlen(json_str), 1, 0);
  gpr_free(json_str);
  return result;
}

char *GenerateJwtClaim(const char *issuer, const char *subject,
                       const char *audience, gpr_timespec token_lifetime) {
  grpc_json json_top;
  memset(&json_top, 0, sizeof(json_top));
  json_top.type = GRPC_JSON_OBJECT;

  gpr_timespec now = gpr_now(GPR_CLOCK_REALTIME);
  gpr_timespec expiration = gpr_time_add(now, token_lifetime);
  char now_str[GPR_LTOA_MIN_BUFSIZE];
  char expiration_str[GPR_LTOA_MIN_BUFSIZE];
  gpr_ltoa(now.tv_sec, now_str);
  gpr_ltoa(expiration.tv_sec, expiration_str);

  grpc_json json_issuer, json_subject, json_audience, json_now, json_expiration;
  FillChild(&json_issuer, nullptr, &json_top, "iss", issuer, GRPC_JSON_STRING);
  FillChild(&json_subject, &json_issuer, &json_top, "sub", subject,
            GRPC_JSON_STRING);
  FillChild(&json_audience, &json_subject, &json_top, "aud", audience,
            GRPC_JSON_STRING);
  FillChild(&json_now, &json_audience, &json_top, "iat", now_str,
            GRPC_JSON_NUMBER);
  FillChild(&json_expiration, &json_now, &json_top, "exp", expiration_str,
            GRPC_JSON_NUMBER);

  char *json_str = grpc_json_dump_to_string(&json_top, 0);
  char *result = grpc_base64_encode(json_str, strlen(json_str), 1, 0);
  gpr_free(json_str);
  return result;
}

char *GenerateSignatueHs256(const char *data, const char *key) {
  grpc_exec_ctx exec_ctx = GRPC_EXEC_CTX_INIT;
  grpc_slice key_buffer = grpc_base64_decode(&exec_ctx, key, 1);
  if (GRPC_SLICE_IS_EMPTY(key_buffer)) {
    gpr_log(GPR_ERROR, "Unable to decode base64 of secret");
    return nullptr;
  }

  unsigned char res[256 / 8];
  unsigned int res_len = 0;
  HMAC(EVP_sha256(), GRPC_SLICE_START_PTR(key_buffer),
       GRPC_SLICE_LENGTH(key_buffer),
       reinterpret_cast<const unsigned char *>(data), strlen(data), res,
       &res_len);
  grpc_slice_unref(key_buffer);
  if (res_len == 0) {
    gpr_log(GPR_ERROR, "Cannot compute HMAC from secret.");
    return nullptr;
  }
  return grpc_base64_encode(res, res_len, 1, 0);
}

string DotConcat(const string &str1, const string &str2) {
  return str1 + "." + str2;
}

}  // namespace

}  // namespace auth
}  // namespace api_manager
}  // namespace google
