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

#include "envoy/json/json_object.h"
#include "openssl/evp.h"

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
//   JwtVerifier v(jwt);
//   std::unique_ptr<Pubkeys> pubkey = ...
//   if (v.Verify(*pubkey)) {
//     auto payload = v.Payload();
//     ...
//   } else {
//     Status s = v.GetStatus();
//     ...
//   }
class JwtVerifier : public WithStatus {
 public:
  // This constructor parses the given JWT and prepares for verification.
  // You can check if the setup was successfully done by seeing if GetStatus()
  // == Status::OK. When the given JWT has a format error, GetStatus() returns
  // the error detail.
  JwtVerifier(const std::string& jwt);

  // This function verifies JWT signature.
  // If verification failed, GetStatus() returns the failture reason.
  // When the given JWT has a format error, this verification always fails.
  // When pubkeys.GetStatus() is not equal to Status::OK, this verification
  // always fails and the public key's status is handed over to JwtVerifier.
  bool Verify(const Pubkeys& pubkeys);

  // It returns a pointer to a JSON object of the header of the given JWT.
  // When the given JWT has a format error, it returns nullptr.
  // It returns the header JSON even if the signature is invalid.
  Json::ObjectSharedPtr Header();

  // They return a string (or base64url-encoded string) of the header JSON of
  // the given JWT.
  const std::string& HeaderStr();
  const std::string& HeaderStrBase64Url();

  // They return the "alg" (or "kid") value of the header of the given JWT.
  const std::string& Alg();

  // It returns the "kid" value of the header of the given JWT, or an empty
  // string if "kid" does not exist in the header.
  const std::string& Kid();

  // It returns a pointer to a JSON object of the payload of the given JWT.
  // When the given jWT has a format error, it returns nullptr.
  // It returns the payload JSON even if the signature is invalid.
  Json::ObjectSharedPtr Payload();

  // They return a string (or base64url-encoded string) of the payload JSON of
  // the given JWT.
  const std::string& PayloadStr();
  const std::string& PayloadStrBase64Url();

  // It returns the "iss" claim value of the given JWT, or an empty string if
  // "iss" claim does not exist.
  const std::string& Iss();

  // It returns the "aud" claim value of the given JWT, or an empty string if
  // "aud" claim does not exist.
  const std::string& Aud();

  // It returns the "exp" claim value of the given JWT, or 0 if "exp" claim does
  // not exist.
  int64_t Exp();

 private:
  const EVP_MD* EvpMdFromAlg(const std::string& alg);

  // Functions to verify with single public key.
  // (Note: Pubkeys object passed to Verify() may contains multiple public keys)
  // When verification fails, UpdateStatus(Status::JWT_INVALID_SIGNATURE) is NOT
  // called.
  bool VerifySignature(EVP_PKEY* key);
  bool VerifySignature(EVP_PKEY* key, const std::string& alg,
                       const uint8_t* signature, size_t signature_len,
                       const uint8_t* signed_data, size_t signed_data_len);
  bool VerifySignature(EVP_PKEY* key, const std::string& alg,
                       const std::string& signature,
                       const std::string& signed_data);

  std::vector<std::string> jwt_split;
  Json::ObjectSharedPtr header_;
  std::string header_str_;
  std::string header_str_base64url_;
  Json::ObjectSharedPtr payload_;
  std::string payload_str_;
  std::string payload_str_base64url_;
  std::string signature_;
  std::string alg_;
  std::string kid_;
  std::string iss_;
  std::string aud_;
  int64_t exp_;
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
  static std::unique_ptr<Pubkeys> CreateFromPem(const std::string& pkey_pem);
  static std::unique_ptr<Pubkeys> CreateFromJwks(const std::string& pkey_jwks);

 private:
  void CreateFromPemCore(const std::string& pkey_pem);
  void CreateFromJwksCore(const std::string& pkey_jwks);

  class Pubkey {
   public:
    Pubkey(){};
    bssl::UniquePtr<EVP_PKEY> key_;
    std::string kid_;
    bool alg_specified_ = false;
    std::string alg_;
  };
  std::vector<std::unique_ptr<Pubkey> > keys_;

  friend bool JwtVerifier::Verify(const Pubkeys& pubkeys);
};

}  // Auth
}  // Http
}  // Envoy

#endif  // PROXY_JWT_H
