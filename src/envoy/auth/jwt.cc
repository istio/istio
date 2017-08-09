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
#include <sstream>
#include <string>
#include <utility>
#include <vector>

namespace Envoy {
namespace Http {
namespace Auth {
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

bssl::UniquePtr<EVP_PKEY> EvpPkeyFromRsa(RSA *rsa) {
  if (!rsa) {
    return nullptr;
  }
  bssl::UniquePtr<EVP_PKEY> key(EVP_PKEY_new());
  EVP_PKEY_set1_RSA(key.get(), rsa);
  return key;
}

bssl::UniquePtr<EVP_PKEY> EvpPkeyFromStr(const std::string &pkey_pem) {
  std::string pkey_der = Base64::decode(pkey_pem);
  return EvpPkeyFromRsa(
      bssl::UniquePtr<RSA>(
          RSA_public_key_from_bytes(CastToUChar(pkey_der), pkey_der.length()))
          .get());
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
  if (!rsa) {
    // Couldn't create RSA key.
    return nullptr;
  }
  rsa->n = BigNumFromBase64UrlString(n).release();
  rsa->e = BigNumFromBase64UrlString(e).release();
  if (!rsa->n || !rsa->e) {
    // RSA public key field is missing.
    return nullptr;
  }
  return rsa;
}

bssl::UniquePtr<EVP_PKEY> EvpPkeyFromJwk(const std::string &n,
                                         const std::string &e) {
  return EvpPkeyFromRsa(RsaFromJwk(n, e).get());
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
    return false;
  }
  if (!md_ctx) {
    return false;
  }
  if (EVP_DigestVerifyInit(md_ctx.get(), nullptr, md, nullptr, key) != 1) {
    return false;
  }
  if (EVP_DigestVerifyUpdate(md_ctx.get(), signed_data, signed_data_len) != 1) {
    return false;
  }
  if (EVP_DigestVerifyFinal(md_ctx.get(), signature, signature_len) != 1) {
    return false;
  }
  return true;
}

bool VerifySignature(EVP_PKEY *key, const std::string &alg,
                     const std::string &signature,
                     const std::string &signed_data) {
  return VerifySignature(key, alg, CastToUChar(signature), signature.length(),
                         CastToUChar(signed_data), signed_data.length());
}

}  // namespace

namespace Jwt {
namespace {

// Class to decode and verify JWT. Setup() must be called before
// VerifySignature() and Payload(). If you do not need the signature
// verification, VerifySignature() can be skipped.
// Usage example:
//   Verifier v;
//   if(!v.Setup(jwt)) return nullptr;
//   if(!v.VerifySignature(publickey)) return nullptr;
//   return v.Payload();
class Verifier {
 public:
  rapidjson::Document header;
  std::string alg;

  // Parses header JSON. This function must be called before accessing header or
  // alg.
  // It returns false if parse fails.
  bool Setup(const std::string &jwt) {
    // jwt must have exactly 2 dots
    if (std::count(jwt.begin(), jwt.end(), '.') != 2) {
      return false;
    }
    jwt_split = StringUtil::split(jwt, '.');
    if (jwt_split.size() != 3) {
      return false;
    }

    // parse header json
    if (header.Parse(Base64UrlDecode(jwt_split[0]).c_str()).HasParseError()) {
      return false;
    }

    if (!header.HasMember("alg")) {
      return false;
    }
    rapidjson::Value &alg_v = header["alg"];
    if (!alg_v.IsString()) {
      return false;
    }
    alg = alg_v.GetString();

    return true;
  }

  // Setup() must be called before VerifySignature().
  bool VerifySignature(EVP_PKEY *key) {
    std::string signature = Base64UrlDecode(jwt_split[2]);
    if (signature == "") {
      // invalid signature
      return false;
    }
    std::string signed_data = jwt_split[0] + '.' + jwt_split[1];
    return Auth::VerifySignature(key, alg, signature, signed_data);
  }

  // Returns payload JSON.
  // VerifySignature() must be called before Payload().
  std::unique_ptr<rapidjson::Document> Payload() {
    // decode payload
    std::unique_ptr<rapidjson::Document> payload_json(
        new rapidjson::Document());
    if (payload_json->Parse(Base64UrlDecode(jwt_split[1]).c_str())
            .HasParseError()) {
      return nullptr;
    }
    return payload_json;
  }

 private:
  std::vector<std::string> jwt_split;
};

}  // namespace

std::unique_ptr<rapidjson::Document> Decode(const std::string &jwt,
                                            const std::string &pkey_pem) {
  /*
   * TODO: return failure reason (something like
   * https://github.com/grpc/grpc/blob/master/src/core/lib/security/credentials/jwt/jwt_verifier.h#L38)
   */
  Verifier v;
  return v.Setup(jwt) && v.VerifySignature(EvpPkeyFromStr(pkey_pem).get())
             ? v.Payload()
             : nullptr;
}

std::unique_ptr<rapidjson::Document> DecodeWithJwk(const std::string &jwt,
                                                   const std::string &jwks) {
  Verifier verifier;
  if (!verifier.Setup(jwt)) {
    return nullptr;
  }
  std::string kid_jwt = "";
  if (verifier.header.HasMember("kid")) {
    if (verifier.header["kid"].IsString()) {
      kid_jwt = verifier.header["kid"].GetString();
    } else {
      // if header has invalid format (non-string) "kid", verification is
      // considered to be failed
      return nullptr;
    }
  }

  // parse JWKs
  rapidjson::Document jwks_json;
  if (jwks_json.Parse(jwks.c_str()).HasParseError()) {
    return nullptr;
  }
  auto keys = jwks_json.FindMember("keys");
  if (keys == jwks_json.MemberEnd()) {
    // jwks doesn't have "keys"
    return nullptr;
  }
  if (!keys->value.IsArray()) {
    return nullptr;
  }

  for (auto &jwk : keys->value.GetArray()) {
    // If kid is specified in JWT, JWK with the same kid is used for
    // verification.
    // If kid is not specified in JWT, try all JWK.
    if (kid_jwt != "") {
      if (!jwk.HasMember("kid") || !jwk["kid"].IsString() ||
          jwk["kid"].GetString() != kid_jwt) {
        continue;
      }
    }

    // the same alg must be used.
    if (!jwk.HasMember("alg") || !jwk["alg"].IsString() ||
        jwk["alg"].GetString() != verifier.alg) {
      continue;
    }

    // verification
    if (!jwk.HasMember("n") || !jwk["n"].IsString()) {
      continue;
    }
    if (!jwk.HasMember("e") || !jwk["e"].IsString()) {
      continue;
    }
    if (verifier.VerifySignature(
            EvpPkeyFromJwk(jwk["n"].GetString(), jwk["e"].GetString()).get())) {
      return verifier.Payload();
    }
  }
  return nullptr;
}

}  // Jwt
}  // Auth
}  // Http
}  // Envoy