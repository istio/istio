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

#include "jwt.h"

#include "common/common/base64.h"
#include "common/common/utility.h"
#include "common/json/json_loader.h"
#include "openssl/bn.h"
#include "openssl/evp.h"
#include "openssl/rsa.h"

#include <algorithm>
#include <cassert>
#include <map>
#include <sstream>
#include <string>
#include <utility>
#include <vector>

namespace Envoy {
namespace Http {
namespace Auth {

std::string StatusToString(Status status) {
  static std::map<Status, std::string> table = {
      {Status::OK, "OK"},
      {Status::JWT_BAD_FORMAT, "JWT_BAD_FORMAT"},
      {Status::JWT_HEADER_PARSE_ERROR, "JWT_HEADER_PARSE_ERROR"},
      {Status::JWT_HEADER_NO_ALG, "JWT_HEADER_NO_ALG"},
      {Status::JWT_HEADER_BAD_ALG, "JWT_HEADER_BAD_ALG"},
      {Status::JWT_SIGNATURE_PARSE_ERROR, "JWT_SIGNATURE_PARSE_ERROR"},
      {Status::JWT_INVALID_SIGNATURE, "JWT_INVALID_SIGNATURE"},
      {Status::JWT_PAYLOAD_PARSE_ERROR, "JWT_PAYLOAD_PARSE_ERROR"},
      {Status::JWT_HEADER_BAD_KID, "JWT_HEADER_BAD_KID"},
      {Status::JWK_PARSE_ERROR, "JWK_PARSE_ERROR"},
      {Status::JWK_NO_KEYS, "JWK_NO_KEYS"},
      {Status::JWK_BAD_KEYS, "JWK_BAD_KEYS"},
      {Status::JWK_NO_VALID_PUBKEY, "JWK_NO_VALID_PUBKEY"},
      {Status::KID_ALG_UNMATCH, "KID_ALG_UNMATCH"},
      {Status::ALG_NOT_IMPLEMENTED, "ALG_NOT_IMPLEMENTED"},
      {Status::PEM_PUBKEY_BAD_BASE64, "PEM_PUBKEY_BAD_BASE64"},
      {Status::PEM_PUBKEY_PARSE_ERROR, "PEM_PUBKEY_PARSE_ERROR"},
      {Status::JWK_PUBKEY_PARSE_ERROR, "JWK_PUBKEY_PARSE_ERROR"}};
  return table[status];
}

namespace {

// Conversion table is taken from
// https://opensource.apple.com/source/QuickTimeStreamingServer/QuickTimeStreamingServer-452/CommonUtilitiesLib/base64.c
//
// and modified the position of 62 ('+' to '-') and 63 ('/' to '_')
const uint8_t kReverseLookupTableBase64Url[256] = {
    64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64,
    64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64,
    64, 64, 64, 64, 64, 64, 64, 62, 64, 64, 52, 53, 54, 55, 56, 57, 58, 59, 60,
    61, 64, 64, 64, 64, 64, 64, 64, 0,  1,  2,  3,  4,  5,  6,  7,  8,  9,  10,
    11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 64, 64, 64, 64,
    63, 64, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40, 41, 42,
    43, 44, 45, 46, 47, 48, 49, 50, 51, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64,
    64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64,
    64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64,
    64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64,
    64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64,
    64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64,
    64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64,
    64, 64, 64, 64, 64, 64, 64, 64, 64};

bool IsNotBase64UrlChar(int8_t c) {
  return kReverseLookupTableBase64Url[static_cast<int32_t>(c)] & 64;
}

std::string Base64UrlDecode(std::string input) {
  // allow at most 2 padding letters at the end of the input, only if input
  // length is divisible by 4
  int len = input.length();
  if (len % 4 == 0) {
    if (input[len - 1] == '=') {
      input.pop_back();
      if (input[len - 2] == '=') {
        input.pop_back();
      }
    }
  }
  // if input contains non-base64url character, return empty string
  // Note: padding letter must not be contained
  if (std::find_if(input.begin(), input.end(), IsNotBase64UrlChar) !=
      input.end()) {
    return "";
  }

  // base64url is using '-', '_' instead of '+', '/' in base64 string.
  std::replace(input.begin(), input.end(), '-', '+');
  std::replace(input.begin(), input.end(), '_', '/');

  // base64 string should be padded with '=' so as to the length of the string
  // is divisible by 4.
  switch (input.length() % 4) {
    case 0:
      break;
    case 2:
      input += "==";
      break;
    case 3:
      input += "=";
      break;
    default:
      // * an invalid base64url input. return empty string.
      return "";
  }
  return Base64::decode(input);
}

const uint8_t *CastToUChar(const std::string &str) {
  return reinterpret_cast<const uint8_t *>(str.c_str());
}

// Class to create EVP_PKEY object from string of public key, formatted in PEM
// or JWKs.
// If it failed, status_ holds the failure reason.
//
// Usage example:
//   EvpPkeyGetter e;
//   bssl::UniquePtr<EVP_PKEY> pkey =
//   e.EvpPkeyFromStr(pem_formatted_public_key);
// (You can use EvpPkeyFromJwk() for JWKs)
class EvpPkeyGetter : public WithStatus {
 public:
  EvpPkeyGetter() {}

  bssl::UniquePtr<EVP_PKEY> EvpPkeyFromStr(const std::string &pkey_pem) {
    std::string pkey_der = Base64::decode(pkey_pem);
    if (pkey_der == "") {
      UpdateStatus(Status::PEM_PUBKEY_BAD_BASE64);
      return nullptr;
    }
    auto rsa = bssl::UniquePtr<RSA>(
        RSA_public_key_from_bytes(CastToUChar(pkey_der), pkey_der.length()));
    if (!rsa) {
      UpdateStatus(Status::PEM_PUBKEY_PARSE_ERROR);
    }
    return EvpPkeyFromRsa(rsa.get());
  }

  bssl::UniquePtr<EVP_PKEY> EvpPkeyFromJwk(const std::string &n,
                                           const std::string &e) {
    return EvpPkeyFromRsa(RsaFromJwk(n, e).get());
  }

 private:
  // In the case where rsa is nullptr, UpdateStatus() should be called
  // appropriately elsewhere.
  bssl::UniquePtr<EVP_PKEY> EvpPkeyFromRsa(RSA *rsa) {
    if (!rsa) {
      return nullptr;
    }
    bssl::UniquePtr<EVP_PKEY> key(EVP_PKEY_new());
    EVP_PKEY_set1_RSA(key.get(), rsa);
    return key;
  }

  bssl::UniquePtr<BIGNUM> BigNumFromBase64UrlString(const std::string &s) {
    std::string s_decoded = Base64UrlDecode(s);
    if (s_decoded == "") {
      return nullptr;
    }
    return bssl::UniquePtr<BIGNUM>(
        BN_bin2bn(CastToUChar(s_decoded), s_decoded.length(), NULL));
  };

  bssl::UniquePtr<RSA> RsaFromJwk(const std::string &n, const std::string &e) {
    bssl::UniquePtr<RSA> rsa(RSA_new());
    // It crash if RSA object couldn't be created.
    assert(rsa);

    rsa->n = BigNumFromBase64UrlString(n).release();
    rsa->e = BigNumFromBase64UrlString(e).release();
    if (!rsa->n || !rsa->e) {
      // RSA public key field is missing or has parse error.
      UpdateStatus(Status::JWK_PUBKEY_PARSE_ERROR);
      return nullptr;
    }
    return rsa;
  }
};

}  // namespace

JwtVerifier::JwtVerifier(const std::string &jwt) {
  // jwt must have exactly 2 dots
  if (std::count(jwt.begin(), jwt.end(), '.') != 2) {
    UpdateStatus(Status::JWT_BAD_FORMAT);
    return;
  }
  jwt_split = StringUtil::split(jwt, '.');
  if (jwt_split.size() != 3) {
    UpdateStatus(Status::JWT_BAD_FORMAT);
    return;
  }

  // Parse header json
  header_str_base64url_ = jwt_split[0];
  header_str_ = Base64UrlDecode(jwt_split[0]);
  try {
    header_ = Json::Factory::loadFromString(header_str_);
  } catch (...) {
    UpdateStatus(Status::JWT_HEADER_PARSE_ERROR);
    return;
  }

  // Header should contain "alg".
  if (!header_->hasObject("alg")) {
    UpdateStatus(Status::JWT_HEADER_NO_ALG);
    return;
  }
  try {
    alg_ = header_->getString("alg");
  } catch (...) {
    UpdateStatus(Status::JWT_HEADER_BAD_ALG);
    return;
  }

  // Header may contain "kid", which should be a string if exists.
  try {
    kid_ = header_->getString("kid", "");
  } catch (...) {
    UpdateStatus(Status::JWT_HEADER_BAD_KID);
    return;
  }

  // Parse payload json
  payload_str_base64url_ = jwt_split[1];
  payload_str_ = Base64UrlDecode(jwt_split[1]);
  try {
    payload_ = Json::Factory::loadFromString(payload_str_);
  } catch (...) {
    UpdateStatus(Status::JWT_PAYLOAD_PARSE_ERROR);
    return;
  }

  iss_ = payload_->getString("iss", "");
  exp_ = payload_->getInteger("exp", 0);

  // Set up signature
  signature_ = Base64UrlDecode(jwt_split[2]);
  if (signature_ == "") {
    // Signature is a bad Base64url input.
    UpdateStatus(Status::JWT_SIGNATURE_PARSE_ERROR);
    return;
  }
}

const EVP_MD *JwtVerifier::EvpMdFromAlg(const std::string &alg) {
  // may use
  // EVP_sha384() if alg == "RS384" and
  // EVP_sha512() if alg == "RS512"
  if (alg == "RS256") {
    return EVP_sha256();
  } else {
    return nullptr;
  }
}

bool JwtVerifier::VerifySignature(EVP_PKEY *key, const std::string &alg,
                                  const uint8_t *signature,
                                  size_t signature_len,
                                  const uint8_t *signed_data,
                                  size_t signed_data_len) {
  bssl::UniquePtr<EVP_MD_CTX> md_ctx(EVP_MD_CTX_create());
  const EVP_MD *md = EvpMdFromAlg(alg);

  if (!md) {
    UpdateStatus(Status::ALG_NOT_IMPLEMENTED);
    return false;
  }
  EVP_DigestVerifyInit(md_ctx.get(), nullptr, md, nullptr, key);
  EVP_DigestVerifyUpdate(md_ctx.get(), signed_data, signed_data_len);
  return (EVP_DigestVerifyFinal(md_ctx.get(), signature, signature_len) == 1);
}

bool JwtVerifier::VerifySignature(EVP_PKEY *key, const std::string &alg,
                                  const std::string &signature,
                                  const std::string &signed_data) {
  return VerifySignature(key, alg, CastToUChar(signature), signature.length(),
                         CastToUChar(signed_data), signed_data.length());
}

bool JwtVerifier::VerifySignature(EVP_PKEY *key) {
  std::string signed_data = jwt_split[0] + '.' + jwt_split[1];
  return VerifySignature(key, alg_, signature_, signed_data);
}

bool JwtVerifier::Verify(const Pubkeys &pubkeys) {
  // If setup is not successfully done, return false.
  if (GetStatus() != Status::OK) {
    return false;
  }

  // If pubkeys status is not OK, inherits its status and return false.
  if (pubkeys.GetStatus() != Status::OK) {
    UpdateStatus(pubkeys.GetStatus());
    return false;
  }

  std::string kid_jwt = Kid();
  bool kid_alg_matched = false;
  for (auto &pubkey : pubkeys.keys_) {
    // If kid is specified in JWT, JWK with the same kid is used for
    // verification.
    // If kid is not specified in JWT, try all JWK.
    if (kid_jwt != "" && pubkey->kid_ != kid_jwt) {
      continue;
    }

    // The same alg must be used.
    if (pubkey->alg_specified_ && pubkey->alg_ != Alg()) {
      continue;
    }
    kid_alg_matched = true;

    if (VerifySignature(pubkey->key_.get())) {
      // Verification succeeded.
      return true;
    }
  }

  // Verification failed.
  if (kid_alg_matched) {
    UpdateStatus(Status::JWT_INVALID_SIGNATURE);
  } else {
    UpdateStatus(Status::KID_ALG_UNMATCH);
  }
  return false;
}

// Returns the parsed header.
Json::ObjectSharedPtr JwtVerifier::Header() { return header_; }

const std::string &JwtVerifier::HeaderStr() { return header_str_; }
const std::string &JwtVerifier::HeaderStrBase64Url() {
  return header_str_base64url_;
}
const std::string &JwtVerifier::Alg() { return alg_; }
const std::string &JwtVerifier::Kid() { return kid_; }

// Returns payload JSON.
Json::ObjectSharedPtr JwtVerifier::Payload() { return payload_; }

const std::string &JwtVerifier::PayloadStr() { return payload_str_; }
const std::string &JwtVerifier::PayloadStrBase64Url() {
  return payload_str_base64url_;
}
const std::string &JwtVerifier::Iss() { return iss_; }
int64_t JwtVerifier::Exp() { return exp_; }

void Pubkeys::CreateFromPemCore(const std::string &pkey_pem) {
  keys_.clear();
  std::unique_ptr<Pubkey> key_ptr(new Pubkey());
  EvpPkeyGetter e;
  key_ptr->key_ = e.EvpPkeyFromStr(pkey_pem);
  UpdateStatus(e.GetStatus());
  if (e.GetStatus() == Status::OK) {
    keys_.push_back(std::move(key_ptr));
  }
}

std::unique_ptr<Pubkeys> Pubkeys::CreateFromPem(const std::string &pkey_pem) {
  std::unique_ptr<Pubkeys> keys(new Pubkeys());
  keys->CreateFromPemCore(pkey_pem);
  return keys;
}

void Pubkeys::CreateFromJwksCore(const std::string &pkey_jwks) {
  keys_.clear();

  Json::ObjectSharedPtr jwks_json;
  try {
    jwks_json = Json::Factory::loadFromString(pkey_jwks);
  } catch (...) {
    UpdateStatus(Status::JWK_PARSE_ERROR);
    return;
  }
  std::vector<Json::ObjectSharedPtr> keys;
  if (!jwks_json->hasObject("keys")) {
    UpdateStatus(Status::JWK_NO_KEYS);
    return;
  }
  try {
    keys = jwks_json->getObjectArray("keys", true);
  } catch (...) {
    UpdateStatus(Status::JWK_BAD_KEYS);
    return;
  }

  for (auto jwk_json : keys) {
    std::unique_ptr<Pubkey> pubkey(new Pubkey());

    std::string n_str, e_str;
    try {
      pubkey->kid_ = jwk_json->getString("kid");
      pubkey->alg_ = jwk_json->getString("alg");
      pubkey->alg_specified_ = true;
      n_str = jwk_json->getString("n");
      e_str = jwk_json->getString("e");
    } catch (...) {
      continue;
    }
    EvpPkeyGetter e;
    pubkey->key_ = e.EvpPkeyFromJwk(n_str, e_str);
    keys_.push_back(std::move(pubkey));
  }
  if (keys_.size() == 0) {
    UpdateStatus(Status::JWK_NO_VALID_PUBKEY);
  }
}

std::unique_ptr<Pubkeys> Pubkeys::CreateFromJwks(const std::string &pkey_jwks) {
  std::unique_ptr<Pubkeys> keys(new Pubkeys());
  keys->CreateFromJwksCore(pkey_jwks);
  return keys;
}

}  // Auth
}  // Http
}  // Envoy