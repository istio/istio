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
#include "openssl/bn.h"
#include "openssl/evp.h"
#include "openssl/rsa.h"
#include "rapidjson/document.h"

#include <algorithm>
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
class EvpPkeyGetter {
 public:
  EvpPkeyGetter() : status_(Status::OK) {}

  // It returns "OK" or the failure reason.
  Status GetStatus() { return status_; }

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
  // It holds failure reason.
  Status status_;

  void UpdateStatus(Status status) {
    if (status_ == Status::OK) {
      status_ = status;
    }
  }

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

// Class to decode and verify JWT. Setup() must be called before
// VerifySignature() and Payload(). If you do not need the signature
// verification, VerifySignature() can be skipped.
// When verification fails, status_ holds the reason of failure.
//
// Usage example:
//   Verifier v;
//   if(!v.Setup(jwt)) return nullptr;
//   if(!v.VerifySignature(publickey)) return nullptr;
//   return v.Payload();
class Verifier {
 public:
  Verifier() : status_(Status::OK) {}

  // Returns the parsed header. Setup() must be called before this.
  const rapidjson::Document &Header() { return header_; }

  // Returns "alg" in the header. Setup() must be called before this.
  const std::string &Alg() { return alg_; };

  // Returns "OK" or the failure reason.
  Status GetStatus() { return status_; }

  // Parses header JSON. This function must be called before calling Header() or
  // Alg().
  // It returns false if parse fails.
  bool Setup(const std::string &jwt) {
    // jwt must have exactly 2 dots
    if (std::count(jwt.begin(), jwt.end(), '.') != 2) {
      UpdateStatus(Status::JWT_BAD_FORMAT);
      return false;
    }
    jwt_split = StringUtil::split(jwt, '.');
    if (jwt_split.size() != 3) {
      UpdateStatus(Status::JWT_BAD_FORMAT);
      return false;
    }

    // parse header json
    if (header_.Parse(Base64UrlDecode(jwt_split[0]).c_str()).HasParseError()) {
      UpdateStatus(Status::JWT_HEADER_PARSE_ERROR);
      return false;
    }

    if (!header_.HasMember("alg")) {
      UpdateStatus(Status::JWT_HEADER_NO_ALG);
      return false;
    }
    rapidjson::Value &alg_v = header_["alg"];
    if (!alg_v.IsString()) {
      UpdateStatus(Status::JWT_HEADER_BAD_ALG);
      return false;
    }
    alg_ = alg_v.GetString();

    // Set up signature
    signature_ = Base64UrlDecode(jwt_split[2]);
    if (signature_ == "") {
      // Signature is a bad Base64url input.
      UpdateStatus(Status::JWT_SIGNATURE_PARSE_ERROR);
      return false;
    }

    return true;
  }

  // Setup() must be called before VerifySignature().
  // When verification fails, UpdateStatus(Status::JWT_INVALID_SIGNATURE) is NOT
  // called.
  bool VerifySignature(EVP_PKEY *key) {
    std::string signed_data = jwt_split[0] + '.' + jwt_split[1];
    return VerifySignature(key, alg_, signature_, signed_data);
  }

  // Returns payload JSON.
  // VerifySignature() must be called before Payload().
  std::unique_ptr<rapidjson::Document> Payload() {
    // decode payload
    std::unique_ptr<rapidjson::Document> payload_json(
        new rapidjson::Document());
    if (payload_json->Parse(Base64UrlDecode(jwt_split[1]).c_str())
            .HasParseError()) {
      UpdateStatus(Status::JWT_PAYLOAD_PARSE_ERROR);
      return nullptr;
    }
    return payload_json;
  }

 private:
  std::vector<std::string> jwt_split;
  rapidjson::Document header_;
  std::string signature_;
  std::string alg_;
  Status status_;

  // Not overwrite failure status to keep the reason of the first failure
  void UpdateStatus(Status status) {
    if (status_ == Status::OK) {
      status_ = status;
    }
  }

  const EVP_MD *EvpMdFromAlg(const std::string &alg) {
    // may use
    // EVP_sha384() if alg == "RS384" and
    // EVP_sha512() if alg == "RS512"
    if (alg == "RS256") {
      return EVP_sha256();
    } else {
      return nullptr;
    }
  }

  bool VerifySignature(EVP_PKEY *key, const std::string &alg,
                       const uint8_t *signature, size_t signature_len,
                       const uint8_t *signed_data, size_t signed_data_len) {
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

  bool VerifySignature(EVP_PKEY *key, const std::string &alg,
                       const std::string &signature,
                       const std::string &signed_data) {
    return VerifySignature(key, alg, CastToUChar(signature), signature.length(),
                           CastToUChar(signed_data), signed_data.length());
  }
};

}  // namespace

void Pubkeys::ParseFromPemCore(const std::string &pkey_pem) {
  keys_.clear();
  std::unique_ptr<Pubkey> key_ptr(new Pubkey());
  EvpPkeyGetter e;
  key_ptr->key_ = e.EvpPkeyFromStr(pkey_pem);
  UpdateStatus(e.GetStatus());
  if (e.GetStatus() == Status::OK) {
    keys_.push_back(std::move(key_ptr));
  }
}

std::unique_ptr<Pubkeys> Pubkeys::ParseFromPem(const std::string &pkey_pem) {
  std::unique_ptr<Pubkeys> keys(new Pubkeys());
  keys->ParseFromPemCore(pkey_pem);
  return keys;
}

void Pubkeys::ParseFromJwksCore(const std::string &pkey_jwks) {
  keys_.clear();

  rapidjson::Document jwks_json;
  if (jwks_json.Parse(pkey_jwks.c_str()).HasParseError()) {
    UpdateStatus(Status::JWK_PARSE_ERROR);
    return;
  }
  auto keys = jwks_json.FindMember("keys");
  if (keys == jwks_json.MemberEnd()) {
    UpdateStatus(Status::JWK_NO_KEYS);
    return;
  }
  if (!keys->value.IsArray()) {
    UpdateStatus(Status::JWK_BAD_KEYS);
    return;
  }

  for (auto &jwk_json : keys->value.GetArray()) {
    std::unique_ptr<Pubkey> pubkey(new Pubkey());

    if (!jwk_json.HasMember("kid") || !jwk_json["kid"].IsString()) {
      continue;
    }
    pubkey->kid_ = jwk_json["kid"].GetString();

    if (!jwk_json.HasMember("alg") || !jwk_json["alg"].IsString()) {
      continue;
    }
    pubkey->alg_specified_ = true;
    pubkey->alg_ = jwk_json["alg"].GetString();

    // public key
    if (!jwk_json.HasMember("n") || !jwk_json["n"].IsString()) {
      continue;
    }
    if (!jwk_json.HasMember("e") || !jwk_json["e"].IsString()) {
      continue;
    }
    EvpPkeyGetter e;
    pubkey->key_ =
        e.EvpPkeyFromJwk(jwk_json["n"].GetString(), jwk_json["e"].GetString());

    keys_.push_back(std::move(pubkey));
  }
  if (keys_.size() == 0) {
    UpdateStatus(Status::JWK_NO_VALID_PUBKEY);
  }
}

std::unique_ptr<Pubkeys> Pubkeys::ParseFromJwks(const std::string &pkey_jwks) {
  std::unique_ptr<Pubkeys> keys(new Pubkeys());
  keys->ParseFromJwksCore(pkey_jwks);
  return keys;
}

std::unique_ptr<rapidjson::Document> JwtVerifier::Decode(
    const Pubkeys &pubkeys, const std::string &jwt) {
  // If pubkeys status is not OK, inherits its status and return nullptr.
  if (pubkeys.GetStatus() != Status::OK) {
    UpdateStatus(pubkeys.GetStatus());
    return nullptr;
  }

  Verifier v;
  if (!v.Setup(jwt)) {
    UpdateStatus(v.GetStatus());
    return nullptr;
  }
  std::string kid_jwt = "";
  if (v.Header().HasMember("kid")) {
    if (v.Header()["kid"].IsString()) {
      kid_jwt = v.Header()["kid"].GetString();
    } else {
      // if header has invalid format (non-string) "kid", verification is
      // considered to be failed
      UpdateStatus(Status::JWT_HEADER_BAD_KID);
      return nullptr;
    }
  }

  bool kid_alg_matched = false;
  for (auto &pubkey : pubkeys.keys_) {
    // If kid is specified in JWT, JWK with the same kid is used for
    // verification.
    // If kid is not specified in JWT, try all JWK.
    if (kid_jwt != "" && pubkey->kid_ != kid_jwt) {
      continue;
    }

    // The same alg must be used.
    if (pubkey->alg_specified_ && pubkey->alg_ != v.Alg()) {
      continue;
    }
    kid_alg_matched = true;

    if (v.VerifySignature(pubkey->key_.get())) {
      auto payload = v.Payload();
      UpdateStatus(v.GetStatus());
      return payload;
    }
  }

  // Verification failed.
  UpdateStatus(v.GetStatus());
  if (kid_alg_matched) {
    UpdateStatus(Status::JWT_INVALID_SIGNATURE);
  } else {
    UpdateStatus(Status::KID_ALG_UNMATCH);
  }
  return nullptr;
}

}  // Auth
}  // Http
}  // Envoy