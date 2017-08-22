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

#ifndef PROXY_JWT_H
#define PROXY_JWT_H

#include "openssl/evp.h"
#include "rapidjson/document.h"

#include <string>
#include <utility>
#include <vector>

namespace Envoy {
namespace Http {
namespace Auth {

enum class Status {
  OK,

  // Given JWT is not in the form of Header.Payload.Signature
  JWT_BAD_FORMAT,

  // Header is an invalid Base64url input or an invalid JSON.
  JWT_HEADER_PARSE_ERROR,

  // Header does not have "alg".
  JWT_HEADER_NO_ALG,

  // "alg" in the header is not a string.
  JWT_HEADER_BAD_ALG,

  // Signature is an invalid Base64url input.
  JWT_SIGNATURE_PARSE_ERROR,

  // Signature Verification failed (= Failed in DigestVerifyFinal())
  JWT_INVALID_SIGNATURE,

  // Signature is valid but payload is an invalid Base64url input or an invalid
  // JSON.
  JWT_PAYLOAD_PARSE_ERROR,

  // "kid" in the JWT header is not a string.
  JWT_HEADER_BAD_KID,

  // JWK is an invalid JSON.
  JWK_PARSE_ERROR,

  // JWK does not have "keys".
  JWK_NO_KEYS,

  // "keys" in JWK is not an array.
  JWK_BAD_KEYS,

  // There are no valid public key in given JWKs.
  JWK_NO_VALID_PUBKEY,

  // There is no key the kid and the alg of which match those of the given JWT.
  KID_ALG_UNMATCH,

  // Value of "alg" in the header is invalid.
  ALG_NOT_IMPLEMENTED,

  // Given PEM formatted public key is an invalid Base64 input.
  PEM_PUBKEY_BAD_BASE64,

  // A parse error on PEM formatted public key happened.
  PEM_PUBKEY_PARSE_ERROR,

  // "n" or" "e" field of a JWK has a parse error or is missing.
  JWK_PUBKEY_PARSE_ERROR,
};

std::string StatusToString(Status status);

// Base class to keep the status that represents "OK" or the first failure
// reason
class WithStatus {
 public:
  WithStatus() : status_(Status::OK) {}
  Status GetStatus() const { return status_; }

 protected:
  void UpdateStatus(Status status) {
    // Not overwrite failure status to keep the reason of the first failure
    if (status_ == Status::OK) {
      status_ = status;
    }
  }

 private:
  Status status_;
};

class Pubkeys;

// JWT Verifier class.
//
// Usage example:
//   JwtVerifier v;
//   std::unique_ptr<Pubkeys> pubkey = ...
//   auto payload = v.Decode(pubkey, jwt);
//   Status s = v.GetStatus();
class JwtVerifier : public WithStatus {
 public:
  // This function verifies JWT signature and returns the decoded payload as a
  // JSON if the signature is valid.
  // If verification failed, it returns nullptr, and GetStatus() returns a
  // Status object of the failture reason.
  // When pubkeys.GetStatus() is not equal to Status::OK, this function returns
  // nullptr and the public key's status is handed over to JwtVerifier.
  std::unique_ptr<rapidjson::Document> Decode(const Pubkeys &pubkeys,
                                              const std::string &jwt);
};

// Class to parse and a hold public key(s).
// It also holds the failure reason if parse failed.
//
// Usage example:
//   std::unique_ptr<Pubkeys> keys = Pubkeys::ParseFromJwks(jwks_string);
//   if(keys->GetStatus() == Status::OK) { ... }
class Pubkeys : public WithStatus {
 public:
  Pubkeys(){};
  static std::unique_ptr<Pubkeys> ParseFromPem(const std::string &pkey_pem);
  static std::unique_ptr<Pubkeys> ParseFromJwks(const std::string &pkey_jwks);

 private:
  void ParseFromPemCore(const std::string &pkey_pem);
  void ParseFromJwksCore(const std::string &pkey_jwks);

  class Pubkey {
   public:
    Pubkey(){};
    bssl::UniquePtr<EVP_PKEY> key_;
    std::string kid_;
    bool alg_specified_ = false;
    std::string alg_;
  };
  std::vector<std::unique_ptr<Pubkey> > keys_;

  friend std::unique_ptr<rapidjson::Document> JwtVerifier::Decode(
      const Pubkeys &pubkeys, const std::string &jwt);
};

}  // Auth
}  // Http
}  // Envoy

#endif  // PROXY_JWT_H
