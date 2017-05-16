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
#include "contrib/endpoints/src/api_manager/auth/lib/auth_jwt_validator.h"

// Implementation of JWT token verification.

// Support public keys in x509 format or JWK (Json Web Keys).
// -- Sample x509 keys
// {
// "8f3e950b309186540c314ecf348bb14f1784d79d": "-----BEGIN
// CERTIFICATE-----\nMIIDHDCCAgSgAwIBAgIIYJnxRhkHEz8wDQYJKoZIhvcNAQEFBQAwMTEvMC0GA1UE\nAxMmc2VjdXJldG9rZW4uc3lzdGVtLmdzZXJ2aWNlYWNjb3VudC5jb20wHhcNMTUw\nNjE2MDEwMzQxWhcNMTUwNjE3MTQwMzQxWjAxMS8wLQYDVQQDEyZzZWN1cmV0b2tl\nbi5zeXN0ZW0uZ3NlcnZpY2VhY2NvdW50LmNvbTCCASIwDQYJKoZIhvcNAQEBBQAD\nggEPADCCAQoCggEBAMNKd/jkdD+ifIw806pawXZo656ycjL1KB/kUJbPopTzKKxZ\nR/eYJpd5BZIZPnWbXGvoY2kGne8jYJptQLLHr18u7TDVMpnh41jvLWYHXJv8Zd/W\n1HZk4t5mm4+JzZ2WUAx881aiEieO7/cMSIT3VC2I98fMFuEJ8jAWUDWY3KzHsXp0\nlj5lJknFCiESQ8s+UxFYHF/EgS8S2eJBvs2unq1a4NVan/GupA1OB5LrlFXm09Vt\na+dB4gulBrPh0/AslRd36uiXLRFnvAr+EF25WyZsUcq0ANCFx1Rd5z3Fv/5zC9hw\n3EeHEpc+NgovzPJ+IDHfqiU4BLTPgT70DYeLHUcCAwEAAaM4MDYwDAYDVR0TAQH/\nBAIwADAOBgNVHQ8BAf8EBAMCB4AwFgYDVR0lAQH/BAwwCgYIKwYBBQUHAwIwDQYJ\nKoZIhvcNAQEFBQADggEBAK2b2Mw5W0BtXS+onaKyyvC9O2Ysh7gTjjOGTVaTpYaB\nDg2vDgqFHM5xeYwMUf8O163rZz4R/DusZ5GsNav9BSsco9VsaIp5oIuy++tepnVN\niAdzK/bH/mo6w0s/+q46v3yhSE2Yd9WzKS9eSfQ6Yw/6y1rCgygTIVsdtKwN2u9L\nXj3RQGcHk7tgrICETqeMULZWMJQG2webNAu8bqkONgo+JP54QNVCzWgCznbCbmOR\nExlMusHMG60j8CxmMG/WLUhX+46m5HxVVx29AH8RhqpwmeFs17QXpGjOMW+ZL/Vf\nwmtv14KGfeX0z2A2iQAP5w6R1r6c+HWizj80mXHWI5U=\n-----END
// CERTIFICATE-----\n",
// "0f980915096a38d8de8d7398998c7fb9152e14fc": "-----BEGIN
// CERTIFICATE-----\nMIIDHDCCAgSgAwIBAgIIVhHsCCeHFBowDQYJKoZIhvcNAQEFBQAwMTEvMC0GA1UE\nAxMmc2VjdXJldG9rZW4uc3lzdGVtLmdzZXJ2aWNlYWNjb3VudC5jb20wHhcNMTUw\nNjE3MDA0ODQxWhcNMTUwNjE4MTM0ODQxWjAxMS8wLQYDVQQDEyZzZWN1cmV0b2tl\nbi5zeXN0ZW0uZ3NlcnZpY2VhY2NvdW50LmNvbTCCASIwDQYJKoZIhvcNAQEBBQAD\nggEPADCCAQoCggEBAKbpeArSNTeYN977uD7GxgFhjghjsTFAq52IW04BdoXQmT9G\nP38s4q06UKGjaZbvEQmdxS+IX2BvswHxiOOgA210C4vRIBu6k1fAnt4JYBy1QHf8\n6C4K9cArp5Sx7/NJcTyu0cj/Ce1fi2iKcvuaQG7+e6VsERWjCFoUHbBohx9a92ch\nMVzQU3Bp8Ix6err6gsxxX8AcrgTN9Ux1Z2x6Ahd/x6Id2HkP8N4dGq72ksk1T9y6\n+Q8LmCzgILSyKvtVVW9G44neFDQvcvJyQljfM996b03yur4XRBs3dPS9AyJlGuN3\nagxBLwM2ieXyM73Za8khlR8PJMUcy4vA6zVHQeECAwEAAaM4MDYwDAYDVR0TAQH/\nBAIwADAOBgNVHQ8BAf8EBAMCB4AwFgYDVR0lAQH/BAwwCgYIKwYBBQUHAwIwDQYJ\nKoZIhvcNAQEFBQADggEBAJyWqYryl4K/DS0RCD66V4v8pZ5ja80Dr/dp8U7cH3Xi\nxHfCqtR5qmKZC48CHg4OPD9WSiVYFsDGLJn0KGU85AXghqcMSJN3ffABNMZBX5a6\nQGFjnvL4b2wZGgqXdaxstJ6pnaM6lQ5J6ZwFTMf0gaeo+jkwx9ENPRLudD5Mf91z\nLvLdp+gRWAlZ3Avo1YTG916kMnGRRwNJ7xgy1YSsEOUzzsNlQWca/XdGmj1I3BW/\nimaI/QRPePs3LlpPVtgu5jvOqyRpaNaYNQU7ki+jdEU4ZDOAvteqd5svXitfpB+a\nx5Bj4hUbZhw2U9AMMDmknhH4w3JKeKYGcdQrO/qVWFQ=\n-----END
// CERTIFICATE-----\n"
// }
//
// -- Sample JWK keys
// {
// "keys": [
//  {
//   "kty": "RSA",
//   "alg": "RS256",
//   "use": "sig",
//   "kid": "62a93512c9ee4c7f8067b5a216dade2763d32a47",
//   "n":
//   "0YWnm_eplO9BFtXszMRQNL5UtZ8HJdTH2jK7vjs4XdLkPW7YBkkm_2xNgcaVpkW0VT2l4mU3KftR-6s3Oa5Rnz5BrWEUkCTVVolR7VYksfqIB2I_x5yZHdOiomMTcm3DheUUCgbJRv5OKRnNqszA4xHn3tA3Ry8VO3X7BgKZYAUh9fyZTFLlkeAh0-bLK5zvqCmKW5QgDIXSxUTJxPjZCgfx1vmAfGqaJb-nvmrORXQ6L284c73DUL7mnt6wj3H6tVqPKA27j56N0TB1Hfx4ja6Slr8S4EB3F1luYhATa1PKUSH8mYDW11HolzZmTQpRoLV8ZoHbHEaTfqX_aYahIw",
//   "e": "AQAB"
//  },
//  {
//   "kty": "RSA",
//   "alg": "RS256",
//   "use": "sig",
//  "kid": "b3319a147514df7ee5e4bcdee51350cc890cc89e",
//   "n":
//   "qDi7Tx4DhNvPQsl1ofxxc2ePQFcs-L0mXYo6TGS64CY_2WmOtvYlcLNZjhuddZVV2X88m0MfwaSA16wE-RiKM9hqo5EY8BPXj57CMiYAyiHuQPp1yayjMgoE1P2jvp4eqF-BTillGJt5W5RuXti9uqfMtCQdagB8EC3MNRuU_KdeLgBy3lS3oo4LOYd-74kRBVZbk2wnmmb7IhP9OoLc1-7-9qU1uhpDxmE6JwBau0mDSwMnYDS4G_ML17dC-ZDtLd1i24STUw39KH0pcSdfFbL2NtEZdNeam1DDdk0iUtJSPZliUHJBI_pj8M-2Mn_oA8jBuI8YKwBqYkZCN1I95Q",
//   "e": "AQAB"
//  }
// ]
// }

extern "C" {
#include <grpc/support/alloc.h>
#include <grpc/support/log.h>
}

#include "grpc_internals.h"

#include <openssl/hmac.h>
#include <openssl/pem.h>
#include <cstring>
#include <set>
#include <string>

#include "contrib/endpoints/src/api_manager/auth/lib/json_util.h"

using std::string;
using std::chrono::system_clock;
using ::google::protobuf::util::error::Code;

namespace google {
namespace api_manager {
namespace auth {
namespace {

// JOSE header. see http://tools.ietf.org/html/rfc7515#section-4
struct JoseHeader {
  const char *alg;
  const char *kid;
};

// An implementation of JwtValidator, hold ALL allocated memory data.
class JwtValidatorImpl : public JwtValidator {
 public:
  JwtValidatorImpl(const char *jwt, size_t jwt_len);
  Status Parse(UserInfo *user_info);
  Status VerifySignature(const char *pkey, size_t pkey_len);
  system_clock::time_point &GetExpirationTime() { return exp_; }
  ~JwtValidatorImpl();

 private:
  // Validates given JWT with pkey.
  grpc_jwt_verifier_status Validate(const char *jwt, size_t jwt_len,
                                    const char *pkey, size_t pkey_len,
                                    const char *aud);
  grpc_jwt_verifier_status ParseImpl();
  grpc_jwt_verifier_status VerifySignatureImpl(const char *pkey,
                                               size_t pkey_len);
  // Parses the audiences and removes the audiences from the json object.
  void UpdateAudience(grpc_json *json);

  // Creates header_ from header_json_.
  void CreateJoseHeader();
  // Checks required fields and fills User Info from claims_.
  // And sets expiration time to exp_.
  grpc_jwt_verifier_status FillUserInfoAndSetExp(UserInfo *user_info);
  // Finds the public key and verifies JWT signature with it.
  grpc_jwt_verifier_status FindAndVerifySignature();
  // Extracts the public key from x509 string (key) and sets it to pkey_.
  // Returns true if successful.
  bool ExtractPubkeyFromX509(const char *key);
  // Extracts the public key from a jwk key (jkey) and sets it to pkey_.
  // Returns true if successful.
  bool ExtractPubkeyFromJwk(const grpc_json *jkey);
  // Extracts the public key from jwk key set and verifies JWT signature with
  // it.
  grpc_jwt_verifier_status ExtractAndVerifyJwkKeys(const grpc_json *jwt_keys);
  // Extracts the public key from pkey_json_ and verifies JWT signature with
  // it.
  grpc_jwt_verifier_status ExtractAndVerifyX509Keys();
  // Verifies signature with pkey_.
  grpc_jwt_verifier_status VerifyPubkey();
  // Verifies RS (asymmetric) signature.
  grpc_jwt_verifier_status VerifyRsSignature(const char *pkey, size_t pkey_len);
  // Verifies HS (symmetric) signature.
  grpc_jwt_verifier_status VerifyHsSignature(const char *pkey, size_t pkey_len);

  // Not owned.
  const char *jwt;
  int jwt_len;

  JoseHeader *header_;
  grpc_json *header_json_;
  grpc_slice header_buffer_;
  grpc_jwt_claims *claims_;
  grpc_slice sig_buffer_;
  grpc_slice signed_buffer_;

  std::set<std::string> audiences_;
  system_clock::time_point exp_;

  grpc_json *pkey_json_;
  grpc_slice pkey_buffer_;
  BIO *bio_;
  X509 *x509_;
  RSA *rsa_;
  EVP_PKEY *pkey_;
  EVP_MD_CTX *md_ctx_;

  grpc_exec_ctx exec_ctx_;
};

// Gets EVP_MD mapped from an alg (algorithm string).
const EVP_MD *EvpMdFromAlg(const char *alg);

// Gets hash size from HS algorithm string.
size_t HashSizeFromAlg(const char *alg);

// Parses str into grpc_json object. Does not own buffer.
grpc_json *DecodeBase64AndParseJson(grpc_exec_ctx *exec_ctx, const char *str,
                                    size_t len, grpc_slice *buffer);

// Gets BIGNUM from b64 string, used for extracting pkey from jwk.
// Result owned by rsa_.
BIGNUM *BigNumFromBase64String(grpc_exec_ctx *exec_ctx, const char *b64);

}  // namespace

std::unique_ptr<JwtValidator> JwtValidator::Create(const char *jwt,
                                                   size_t jwt_len) {
  return std::unique_ptr<JwtValidator>(new JwtValidatorImpl(jwt, jwt_len));
}

namespace {
JwtValidatorImpl::JwtValidatorImpl(const char *jwt, size_t jwt_len)
    : jwt(jwt),
      jwt_len(jwt_len),
      header_(nullptr),
      header_json_(nullptr),
      claims_(nullptr),
      pkey_json_(nullptr),
      bio_(nullptr),
      x509_(nullptr),
      rsa_(nullptr),
      pkey_(nullptr),
      md_ctx_(nullptr),
      exec_ctx_(GRPC_EXEC_CTX_INIT) {
  header_buffer_ = grpc_empty_slice();
  signed_buffer_ = grpc_empty_slice();
  sig_buffer_ = grpc_empty_slice();
  pkey_buffer_ = grpc_empty_slice();
}

// Makes sure all data are cleaned up, both success and failure case.
JwtValidatorImpl::~JwtValidatorImpl() {
  if (header_ != nullptr) {
    gpr_free(header_);
  }
  if (header_json_ != nullptr) {
    grpc_json_destroy(header_json_);
  }
  if (pkey_json_ != nullptr) {
    grpc_json_destroy(pkey_json_);
  }
  if (claims_ != nullptr) {
    grpc_jwt_claims_destroy(&exec_ctx_, claims_);
  }
  if (!GRPC_SLICE_IS_EMPTY(header_buffer_)) {
    grpc_slice_unref(header_buffer_);
  }
  if (!GRPC_SLICE_IS_EMPTY(signed_buffer_)) {
    grpc_slice_unref(signed_buffer_);
  }
  if (!GRPC_SLICE_IS_EMPTY(sig_buffer_)) {
    grpc_slice_unref(sig_buffer_);
  }
  if (!GRPC_SLICE_IS_EMPTY(pkey_buffer_)) {
    grpc_slice_unref(pkey_buffer_);
  }
  if (bio_ != nullptr) {
    BIO_free(bio_);
  }
  if (x509_ != nullptr) {
    X509_free(x509_);
  }
  if (rsa_ != nullptr) {
    RSA_free(rsa_);
  }
  if (pkey_ != nullptr) {
    EVP_PKEY_free(pkey_);
  }
  if (md_ctx_ != nullptr) {
    EVP_MD_CTX_destroy(md_ctx_);
  }
}

Status JwtValidatorImpl::Parse(UserInfo *user_info) {
  grpc_jwt_verifier_status status = ParseImpl();
  if (status == GRPC_JWT_VERIFIER_OK) {
    status = FillUserInfoAndSetExp(user_info);
    if (status == GRPC_JWT_VERIFIER_OK) {
      return Status::OK;
    }
  }

  return Status(Code::UNAUTHENTICATED,
                grpc_jwt_verifier_status_to_string(status));
}

// Extracts and removes the audiences from the token.
// This is a workaround to deal with GRPC library not accepting
// multiple audiences.
void JwtValidatorImpl::UpdateAudience(grpc_json *json) {
  grpc_json *cur;
  for (cur = json->child; cur != nullptr; cur = cur->next) {
    if (strcmp(cur->key, "aud") == 0) {
      if (cur->type == GRPC_JSON_ARRAY) {
        grpc_json *aud;
        for (aud = cur->child; aud != nullptr; aud = aud->next) {
          if (aud->type == GRPC_JSON_STRING && aud->value != nullptr) {
            audiences_.insert(aud->value);
          }
        }
        // Replaces the array of audiences with an empty string.
        grpc_json *prev = cur->prev;
        grpc_json *next = cur->next;
        grpc_json_destroy(cur);
        grpc_json *fake_audience = grpc_json_create(GRPC_JSON_STRING);
        fake_audience->key = "aud";
        fake_audience->value = "";
        fake_audience->parent = json;
        fake_audience->prev = prev;
        fake_audience->next = next;
        if (prev) {
          prev->next = fake_audience;
        } else {
          json->child = fake_audience;
        }
        if (next) {
          next->prev = fake_audience;
        }
      } else if (cur->type == GRPC_JSON_STRING && cur->value != nullptr) {
        audiences_.insert(cur->value);
      }
      return;
    }
  }
}

grpc_jwt_verifier_status JwtValidatorImpl::ParseImpl() {
  // ====================
  // Basic check.
  // ====================
  if (jwt == nullptr || jwt_len <= 0) {
    return GRPC_JWT_VERIFIER_BAD_FORMAT;
  }

  // ====================
  // Creates Jose Header.
  // ====================
  const char *cur = jwt;
  const char *dot = strchr(cur, '.');
  if (dot == nullptr) {
    return GRPC_JWT_VERIFIER_BAD_FORMAT;
  }
  header_json_ =
      DecodeBase64AndParseJson(&exec_ctx_, cur, dot - cur, &header_buffer_);
  CreateJoseHeader();
  if (header_ == nullptr) {
    return GRPC_JWT_VERIFIER_BAD_FORMAT;
  }

  // =============================
  // Creates Claims/Payload.
  // =============================
  cur = dot + 1;
  dot = strchr(cur, '.');
  if (dot == nullptr) {
    return GRPC_JWT_VERIFIER_BAD_FORMAT;
  }

  // claim_buffer is the only exception that requires deallocation for failure
  // case, and it is owned by claims_ for successful case.
  grpc_slice claims_buffer = grpc_empty_slice();
  grpc_json *claims_json =
      DecodeBase64AndParseJson(&exec_ctx_, cur, dot - cur, &claims_buffer);
  if (claims_json == nullptr) {
    if (!GRPC_SLICE_IS_EMPTY(claims_buffer)) {
      grpc_slice_unref(claims_buffer);
    }
    return GRPC_JWT_VERIFIER_BAD_FORMAT;
  }
  UpdateAudience(claims_json);
  // Takes ownershp of claims_json and claims_buffer.
  claims_ = grpc_jwt_claims_from_json(&exec_ctx_, claims_json, claims_buffer);

  if (claims_ == nullptr) {
    gpr_log(GPR_ERROR,
            "JWT claims could not be created."
            " Incompatible value types for some claim(s)");
    return GRPC_JWT_VERIFIER_BAD_FORMAT;
  }

  // issuer is mandatory. grpc_jwt_claims_issuer checks if claims_ is nullptr.
  if (grpc_jwt_claims_issuer(claims_) == nullptr) {
    return GRPC_JWT_VERIFIER_BAD_FORMAT;
  }

  // Check timestamp.
  // Passing in its own audience to skip audience check.
  // Audience check should be done by the caller.
  grpc_jwt_verifier_status status =
      grpc_jwt_claims_check(claims_, grpc_jwt_claims_audience(claims_));
  if (status != GRPC_JWT_VERIFIER_OK) {
    return status;
  }

  // =============================
  // Creates Buffer for signature check
  // =============================
  size_t signed_jwt_len = (size_t)(dot - jwt);
  signed_buffer_ = grpc_slice_from_copied_buffer(jwt, signed_jwt_len);
  if (GRPC_SLICE_IS_EMPTY(signed_buffer_)) {
    return GRPC_JWT_VERIFIER_BAD_FORMAT;
  }
  cur = dot + 1;
  sig_buffer_ = grpc_base64_decode_with_len(&exec_ctx_, cur,
                                            jwt_len - signed_jwt_len - 1, 1);
  if (GRPC_SLICE_IS_EMPTY(sig_buffer_)) {
    return GRPC_JWT_VERIFIER_BAD_FORMAT;
  }

  return GRPC_JWT_VERIFIER_OK;
}

Status JwtValidatorImpl::VerifySignature(const char *pkey, size_t pkey_len) {
  grpc_jwt_verifier_status status = VerifySignatureImpl(pkey, pkey_len);
  if (status == GRPC_JWT_VERIFIER_OK) {
    return Status::OK;
  } else {
    return Status(Code::UNAUTHENTICATED,
                  grpc_jwt_verifier_status_to_string(status));
  }
}

grpc_jwt_verifier_status JwtValidatorImpl::VerifySignatureImpl(
    const char *pkey, size_t pkey_len) {
  if (pkey == nullptr || pkey_len <= 0) {
    return GRPC_JWT_VERIFIER_KEY_RETRIEVAL_ERROR;
  }
  if (jwt == nullptr || jwt_len <= 0) {
    return GRPC_JWT_VERIFIER_BAD_FORMAT;
  }
  if (GRPC_SLICE_IS_EMPTY(signed_buffer_) || GRPC_SLICE_IS_EMPTY(sig_buffer_)) {
    return GRPC_JWT_VERIFIER_BAD_FORMAT;
  }
  if (strncmp(header_->alg, "RS", 2) == 0) {  // Asymmetric keys.
    return VerifyRsSignature(pkey, pkey_len);
  } else {  // Symmetric key.
    return VerifyHsSignature(pkey, pkey_len);
  }
}

void JwtValidatorImpl::CreateJoseHeader() {
  if (header_json_ == nullptr) {
    return;
  }
  const char *alg = GetStringValue(header_json_, "alg");
  if (alg == nullptr) {
    gpr_log(GPR_ERROR, "Missing alg field.");
    return;
  }
  if (EvpMdFromAlg(alg) == nullptr) {
    gpr_log(GPR_ERROR, "Invalid alg field [%s].", alg);
    return;
  }

  header_ = reinterpret_cast<JoseHeader *>(gpr_malloc(sizeof(JoseHeader)));
  if (header_ == nullptr) {
    gpr_log(GPR_ERROR, "Jose header creation failed");
  }
  header_->alg = alg;
  header_->kid = GetStringValue(header_json_, "kid");
}

grpc_jwt_verifier_status JwtValidatorImpl::FindAndVerifySignature() {
  if (pkey_json_ == nullptr) {
    gpr_log(GPR_ERROR, "The public keys are empty.");
    return GRPC_JWT_VERIFIER_KEY_RETRIEVAL_ERROR;
  }
  if (header_ == nullptr) {
    gpr_log(GPR_ERROR, "JWT header is empty.");
    return GRPC_JWT_VERIFIER_BAD_FORMAT;
  }
  // JWK set https://tools.ietf.org/html/rfc7517#section-5.
  const grpc_json *jwk_keys = GetProperty(pkey_json_, "keys");
  if (jwk_keys == nullptr) {
    // Try x509 format.
    return ExtractAndVerifyX509Keys();
  } else {
    // JWK format.
    return ExtractAndVerifyJwkKeys(jwk_keys);
  }
}

grpc_jwt_verifier_status JwtValidatorImpl::ExtractAndVerifyX509Keys() {
  // Precondition (checked by caller): pkey_json_ and header_ are not nullptr.
  if (header_->kid != nullptr) {
    const char *value = GetStringValue(pkey_json_, header_->kid);
    if (value == nullptr) {
      gpr_log(GPR_ERROR,
              "Cannot find matching key in key set for kid=%s and alg=%s",
              header_->kid, header_->alg);
      return GRPC_JWT_VERIFIER_KEY_RETRIEVAL_ERROR;
    }
    if (!ExtractPubkeyFromX509(value)) {
      gpr_log(GPR_ERROR, "Failed to extract public key from X509 key (%s)",
              header_->kid);
      return GRPC_JWT_VERIFIER_KEY_RETRIEVAL_ERROR;
    }
    return VerifyPubkey();
  }
  // If kid is not specified in the header, try all keys. If the JWT can be
  // validated with any of the keys, the request is successful.
  const grpc_json *cur;
  if (pkey_json_->child == nullptr) {
    gpr_log(GPR_ERROR, "Failed to extract public key from X509 key (%s)",
            header_->kid);
    return GRPC_JWT_VERIFIER_KEY_RETRIEVAL_ERROR;
  }
  for (cur = pkey_json_->child; cur != nullptr; cur = cur->next) {
    if (cur->value == nullptr || !ExtractPubkeyFromX509(cur->value)) {
      // Failed to extract public key from current X509 key, try next one.
      continue;
    }
    if (VerifyPubkey() == GRPC_JWT_VERIFIER_OK) {
      return GRPC_JWT_VERIFIER_OK;
    }
  }
  // header_->kid is nullptr. The JWT cannot be validated with any of the keys.
  // Return error.
  gpr_log(GPR_ERROR,
          "The JWT cannot be validated with any of the public keys.");
  return GRPC_JWT_VERIFIER_BAD_SIGNATURE;
}

bool JwtValidatorImpl::ExtractPubkeyFromX509(const char *key) {
  if (bio_ != nullptr) {
    BIO_free(bio_);
  }
  bio_ = BIO_new(BIO_s_mem());
  if (bio_ == nullptr) {
    gpr_log(GPR_ERROR, "Unable to allocate a BIO object.");
    return false;
  }
  if (BIO_write(bio_, key, strlen(key)) <= 0) {
    gpr_log(GPR_ERROR, "BIO write error for key (%s).", key);
    return false;
  }
  if (x509_ != nullptr) {
    X509_free(x509_);
  }
  x509_ = PEM_read_bio_X509(bio_, nullptr, nullptr, nullptr);
  if (x509_ == nullptr) {
    gpr_log(GPR_ERROR, "Unable to parse x509 cert for key (%s).", key);
    return false;
  }
  if (pkey_ != nullptr) {
    EVP_PKEY_free(pkey_);
  }
  pkey_ = X509_get_pubkey(x509_);
  if (pkey_ == nullptr) {
    gpr_log(GPR_ERROR, "X509_get_pubkey failed");
    return false;
  }
  return true;
}

grpc_jwt_verifier_status JwtValidatorImpl::ExtractAndVerifyJwkKeys(
    const grpc_json *jwk_keys) {
  // Precondition (checked by caller): jwk_keys and header_ are not nullptr.
  if (jwk_keys->type != GRPC_JSON_ARRAY) {
    gpr_log(GPR_ERROR,
            "Unexpected value type of keys property in jwks key set.");
    return GRPC_JWT_VERIFIER_KEY_RETRIEVAL_ERROR;
  }

  const grpc_json *jkey = nullptr;

  if (jwk_keys->child == nullptr) {
    gpr_log(GPR_ERROR, "The jwks key set is empty");
    return GRPC_JWT_VERIFIER_KEY_RETRIEVAL_ERROR;
  }
  // JWK format from https://tools.ietf.org/html/rfc7518#section-6.
  for (jkey = jwk_keys->child; jkey != nullptr; jkey = jkey->next) {
    if (jkey->type != GRPC_JSON_OBJECT) continue;
    const char *alg = GetStringValue(jkey, "alg");
    if (alg == nullptr || strcmp(alg, header_->alg) != 0) {
      continue;
    }
    const char *kid = GetStringValue(jkey, "kid");
    if (kid == nullptr ||
        (header_->kid != nullptr && strcmp(kid, header_->kid) != 0)) {
      continue;
    }
    const char *kty = GetStringValue(jkey, "kty");
    if (kty == nullptr || strcmp(kty, "RSA") != 0) {
      gpr_log(GPR_ERROR, "Missing or unsupported key type %s.", kty);
      continue;
    }
    if (!ExtractPubkeyFromJwk(jkey)) {
      // Failed to extract public key from this Jwk key.
      continue;
    }
    if (header_->kid != nullptr) {
      return VerifyPubkey();
    }
    // If kid is not specified in the header, try all keys. If the JWT can be
    // validated with any of the keys, the request is successful.
    if (VerifyPubkey() == GRPC_JWT_VERIFIER_OK) {
      return GRPC_JWT_VERIFIER_OK;
    }
  }

  if (header_->kid != nullptr) {
    gpr_log(GPR_ERROR,
            "Cannot find matching key in key set for kid=%s and alg=%s",
            header_->kid, header_->alg);
    return GRPC_JWT_VERIFIER_KEY_RETRIEVAL_ERROR;
  }
  // header_->kid is nullptr. The JWT cannot be validated with any of the keys.
  // Return error.
  gpr_log(GPR_ERROR,
          "The JWT cannot be validated with any of the public keys.");
  return GRPC_JWT_VERIFIER_BAD_SIGNATURE;
}

bool JwtValidatorImpl::ExtractPubkeyFromJwk(const grpc_json *jkey) {
  if (rsa_ != nullptr) {
    RSA_free(rsa_);
  }
  rsa_ = RSA_new();
  if (rsa_ == nullptr) {
    gpr_log(GPR_ERROR, "Could not create rsa key.");
    return false;
  }

  const char *rsa_n = GetStringValue(jkey, "n");
  rsa_->n =
      rsa_n == nullptr ? nullptr : BigNumFromBase64String(&exec_ctx_, rsa_n);
  const char *rsa_e = GetStringValue(jkey, "e");
  rsa_->e =
      rsa_e == nullptr ? nullptr : BigNumFromBase64String(&exec_ctx_, rsa_e);

  if (rsa_->e == nullptr || rsa_->n == nullptr) {
    gpr_log(GPR_ERROR, "Missing RSA public key field.");
    return false;
  }

  if (pkey_ != nullptr) {
    EVP_PKEY_free(pkey_);
  }
  pkey_ = EVP_PKEY_new();
  if (EVP_PKEY_set1_RSA(pkey_, rsa_) == 0) {
    gpr_log(GPR_ERROR, "EVP_PKEY_ste1_RSA failed");
    return false;
  }
  return true;
}

grpc_jwt_verifier_status JwtValidatorImpl::VerifyRsSignature(const char *pkey,
                                                             size_t pkey_len) {
  pkey_buffer_ = grpc_slice_from_copied_buffer(pkey, pkey_len);
  if (GRPC_SLICE_IS_EMPTY(pkey_buffer_)) {
    return GRPC_JWT_VERIFIER_KEY_RETRIEVAL_ERROR;
  }
  pkey_json_ = grpc_json_parse_string_with_len(
      reinterpret_cast<char *>(GRPC_SLICE_START_PTR(pkey_buffer_)),
      GRPC_SLICE_LENGTH(pkey_buffer_));
  if (pkey_json_ == nullptr) {
    return GRPC_JWT_VERIFIER_KEY_RETRIEVAL_ERROR;
  }

  return FindAndVerifySignature();
}

grpc_jwt_verifier_status JwtValidatorImpl::VerifyPubkey() {
  if (pkey_ == nullptr) {
    gpr_log(GPR_ERROR, "Cannot find public key.");
    return GRPC_JWT_VERIFIER_KEY_RETRIEVAL_ERROR;
  }
  if (md_ctx_ != nullptr) {
    EVP_MD_CTX_destroy(md_ctx_);
  }
  md_ctx_ = EVP_MD_CTX_create();
  if (md_ctx_ == nullptr) {
    gpr_log(GPR_ERROR, "Could not create EVP_MD_CTX.");
    return GRPC_JWT_VERIFIER_BAD_SIGNATURE;
  }
  const EVP_MD *md = EvpMdFromAlg(header_->alg);

  GPR_ASSERT(md != nullptr);  // Checked before.

  if (EVP_DigestVerifyInit(md_ctx_, nullptr, md, nullptr, pkey_) != 1) {
    gpr_log(GPR_ERROR, "EVP_DigestVerifyInit failed.");
    return GRPC_JWT_VERIFIER_BAD_SIGNATURE;
  }
  if (EVP_DigestVerifyUpdate(md_ctx_, GRPC_SLICE_START_PTR(signed_buffer_),
                             GRPC_SLICE_LENGTH(signed_buffer_)) != 1) {
    gpr_log(GPR_ERROR, "EVP_DigestVerifyUpdate failed.");
    return GRPC_JWT_VERIFIER_BAD_SIGNATURE;
  }
  if (EVP_DigestVerifyFinal(md_ctx_, GRPC_SLICE_START_PTR(sig_buffer_),
                            GRPC_SLICE_LENGTH(sig_buffer_)) != 1) {
    gpr_log(GPR_ERROR, "JWT signature verification failed.");
    return GRPC_JWT_VERIFIER_BAD_SIGNATURE;
  }
  return GRPC_JWT_VERIFIER_OK;
}

grpc_jwt_verifier_status JwtValidatorImpl::VerifyHsSignature(const char *pkey,
                                                             size_t pkey_len) {
  const EVP_MD *md = EvpMdFromAlg(header_->alg);
  GPR_ASSERT(md != nullptr);  // Checked before.

  pkey_buffer_ = grpc_base64_decode_with_len(&exec_ctx_, pkey, pkey_len, 1);
  if (GRPC_SLICE_IS_EMPTY(pkey_buffer_)) {
    gpr_log(GPR_ERROR, "Unable to decode base64 of secret");
    return GRPC_JWT_VERIFIER_KEY_RETRIEVAL_ERROR;
  }

  unsigned char res[HashSizeFromAlg(header_->alg)];
  unsigned int res_len = 0;
  HMAC(md, GRPC_SLICE_START_PTR(pkey_buffer_), GRPC_SLICE_LENGTH(pkey_buffer_),
       GRPC_SLICE_START_PTR(signed_buffer_), GRPC_SLICE_LENGTH(signed_buffer_),
       res, &res_len);
  if (res_len == 0) {
    gpr_log(GPR_ERROR, "Cannot compute HMAC from secret.");
    return GRPC_JWT_VERIFIER_BAD_SIGNATURE;
  }

  if (res_len != GRPC_SLICE_LENGTH(sig_buffer_) ||
      CRYPTO_memcmp(reinterpret_cast<void *>(GRPC_SLICE_START_PTR(sig_buffer_)),
                    reinterpret_cast<void *>(res), res_len) != 0) {
    gpr_log(GPR_ERROR, "JWT signature verification failed.");
    return GRPC_JWT_VERIFIER_BAD_SIGNATURE;
  }
  return GRPC_JWT_VERIFIER_OK;
}

grpc_jwt_verifier_status JwtValidatorImpl::FillUserInfoAndSetExp(
    UserInfo *user_info) {
  // Required fields.
  const char *issuer = grpc_jwt_claims_issuer(claims_);
  if (issuer == nullptr) {
    gpr_log(GPR_ERROR, "Missing issuer field.");
    return GRPC_JWT_VERIFIER_BAD_FORMAT;
  }
  if (audiences_.empty()) {
    gpr_log(GPR_ERROR, "Missing audience field.");
    return GRPC_JWT_VERIFIER_BAD_FORMAT;
  }
  const char *subject = grpc_jwt_claims_subject(claims_);
  if (subject == nullptr) {
    gpr_log(GPR_ERROR, "Missing subject field.");
    return GRPC_JWT_VERIFIER_BAD_FORMAT;
  }
  user_info->issuer = issuer;
  user_info->audiences = audiences_;
  user_info->id = subject;

  // Optional field.
  const grpc_json *grpc_json = grpc_jwt_claims_json(claims_);

  char *json_str =
      grpc_json_dump_to_string(const_cast<::grpc_json *>(grpc_json), 0);
  if (json_str != nullptr) {
    user_info->claims = json_str;
    gpr_free(json_str);
  }

  const char *email = GetStringValue(grpc_json, "email");
  user_info->email = email == nullptr ? "" : email;
  const char *authorized_party = GetStringValue(grpc_json, "azp");
  user_info->authorized_party =
      authorized_party == nullptr ? "" : authorized_party;
  exp_ = system_clock::from_time_t(grpc_jwt_claims_expires_at(claims_).tv_sec);

  return GRPC_JWT_VERIFIER_OK;
}

const EVP_MD *EvpMdFromAlg(const char *alg) {
  if (strcmp(alg, "RS256") == 0 || strcmp(alg, "HS256") == 0) {
    return EVP_sha256();
  } else if (strcmp(alg, "RS384") == 0 || strcmp(alg, "HS384") == 0) {
    return EVP_sha384();
  } else if (strcmp(alg, "RS512") == 0 || strcmp(alg, "HS512") == 0) {
    return EVP_sha512();
  } else {
    return nullptr;
  }
}

// Gets hash byte size from HS algorithm string.
size_t HashSizeFromAlg(const char *alg) {
  if (strcmp(alg, "HS256") == 0) {
    return 32;
  } else if (strcmp(alg, "HS384") == 0) {
    return 48;
  } else if (strcmp(alg, "HS512") == 0) {
    return 64;
  } else {
    return 0;
  }
}

grpc_json *DecodeBase64AndParseJson(grpc_exec_ctx *exec_ctx, const char *str,
                                    size_t len, grpc_slice *buffer) {
  grpc_json *json;

  *buffer = grpc_base64_decode_with_len(exec_ctx, str, len, 1);
  if (GRPC_SLICE_IS_EMPTY(*buffer)) {
    gpr_log(GPR_ERROR, "Invalid base64.");
    return nullptr;
  }
  json = grpc_json_parse_string_with_len(
      reinterpret_cast<char *>(GRPC_SLICE_START_PTR(*buffer)),
      GRPC_SLICE_LENGTH(*buffer));
  if (json == nullptr) {
    gpr_log(GPR_ERROR, "JSON parsing error.");
  }
  return json;
}

BIGNUM *BigNumFromBase64String(grpc_exec_ctx *exec_ctx, const char *b64) {
  BIGNUM *result = nullptr;
  grpc_slice bin;

  if (b64 == nullptr) return nullptr;
  bin = grpc_base64_decode(exec_ctx, b64, 1);
  if (GRPC_SLICE_IS_EMPTY(bin)) {
    gpr_log(GPR_ERROR, "Invalid base64 for big num.");
    return nullptr;
  }
  result =
      BN_bin2bn(GRPC_SLICE_START_PTR(bin), GRPC_SLICE_LENGTH(bin), nullptr);
  grpc_slice_unref(bin);
  return result;
}

}  // namespace
}  // namespace auth
}  // namespace api_manager
}  // namespace google
