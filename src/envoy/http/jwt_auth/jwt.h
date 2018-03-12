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

#pragma once

#include "envoy/json/json_object.h"
#include "openssl/ec.h"
#include "openssl/evp.h"

#include <string>
#include <utility>
#include <vector>

namespace Envoy {
namespace Http {
namespace JwtAuth {

enum class Status {
  OK = 0,

  // JWT token is required.
  JWT_MISSED = 1,

  // Token expired.
  JWT_EXPIRED = 2,

  // Given JWT is not in the form of Header.Payload.Signature
  JWT_BAD_FORMAT = 3,

  // Header is an invalid Base64url input or an invalid JSON.
  JWT_HEADER_PARSE_ERROR = 4,

  // Header does not have "alg".
  JWT_HEADER_NO_ALG = 5,

  // "alg" in the header is not a string.
  JWT_HEADER_BAD_ALG = 6,

  // Signature is an invalid Base64url input.
  JWT_SIGNATURE_PARSE_ERROR = 7,

  // Signature Verification failed (= Failed in DigestVerifyFinal())
  JWT_INVALID_SIGNATURE = 8,

  // Signature is valid but payload is an invalid Base64url input or an invalid
  // JSON.
  JWT_PAYLOAD_PARSE_ERROR = 9,

  // "kid" in the JWT header is not a string.
  JWT_HEADER_BAD_KID = 10,

  // Issuer is not configured.
  JWT_UNKNOWN_ISSUER = 11,

  // JWK is an invalid JSON.
  JWK_PARSE_ERROR = 12,

  // JWK does not have "keys".
  JWK_NO_KEYS = 13,

  // "keys" in JWK is not an array.
  JWK_BAD_KEYS = 14,

  // There are no valid public key in given JWKs.
  JWK_NO_VALID_PUBKEY = 15,

  // There is no key the kid and the alg of which match those of the given JWT.
  KID_ALG_UNMATCH = 16,

  // Value of "alg" in the header is invalid.
  ALG_NOT_IMPLEMENTED = 17,

  // Given PEM formatted public key is an invalid Base64 input.
  PEM_PUBKEY_BAD_BASE64 = 18,

  // A parse error on PEM formatted public key happened.
  PEM_PUBKEY_PARSE_ERROR = 19,

  // "n" or "e" field of a JWK has a parse error or is missing.
  JWK_RSA_PUBKEY_PARSE_ERROR = 20,

  // Failed to create a EC_KEY object.
  FAILED_CREATE_EC_KEY = 21,

  // "x" or "y" field of a JWK has a parse error or is missing.
  JWK_EC_PUBKEY_PARSE_ERROR = 22,

  // Failed to create ECDSA_SIG object.
  FAILED_CREATE_ECDSA_SIGNATURE = 23,

  // Audience is not allowed.
  AUDIENCE_NOT_ALLOWED = 24,

  // Failed to fetch public key
  FAILED_FETCH_PUBKEY = 25,
};

std::string StatusToString(Status status);

std::string Base64UrlDecode(std::string input);

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
class Jwt;

// JWT Verifier class.
//
// Usage example:
//   Verifier v;
//   Jwt jwt(jwt_string);
//   std::unique_ptr<Pubkeys> pubkey = ...
//   if (v.Verify(jwt, *pubkey)) {
//     auto payload = jwt.Payload();
//     ...
//   } else {
//     Status s = v.GetStatus();
//     ...
//   }
class Verifier : public WithStatus {
 public:
  // This function verifies JWT signature.
  // If verification failed, GetStatus() returns the failture reason.
  // When the given JWT has a format error, this verification always fails and
  // the JWT's status is handed over to Verifier.
  // When pubkeys.GetStatus() is not equal to Status::OK, this verification
  // always fails and the public key's status is handed over to Verifier.
  bool Verify(const Jwt& jwt, const Pubkeys& pubkeys);

 private:
  // Functions to verify with single public key.
  // (Note: Pubkeys object passed to Verify() may contains multiple public keys)
  // When verification fails, UpdateStatus() is NOT called.
  bool VerifySignatureRSA(EVP_PKEY* key, const EVP_MD* md,
                          const uint8_t* signature, size_t signature_len,
                          const uint8_t* signed_data, size_t signed_data_len);
  bool VerifySignatureRSA(EVP_PKEY* key, const EVP_MD* md,
                          const std::string& signature,
                          const std::string& signed_data);
  bool VerifySignatureEC(EC_KEY* key, const std::string& signature,
                         const std::string& signed_data);
  bool VerifySignatureEC(EC_KEY* key, const uint8_t* signature,
                         size_t signature_len, const uint8_t* signed_data,
                         size_t signed_data_len);
};

// Class to parse and a hold a JWT.
// It also holds the failure reason if parse failed.
//
// Usage example:
//   Jwt jwt(jwt_string);
//   if(jwt.GetStatus() == Status::OK) { ... }
class Jwt : public WithStatus {
 public:
  // This constructor parses the given JWT and prepares for verification.
  // You can check if the setup was successfully done by seeing if GetStatus()
  // == Status::OK. When the given JWT has a format error, GetStatus() returns
  // the error detail.
  Jwt(const std::string& jwt);

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
  const std::vector<std::string>& Aud();

  // It returns the "sub" claim value of the given JWT, or an empty string if
  // "sub" claim does not exist.
  const std::string& Sub();

  // It returns the "exp" claim value of the given JWT, or 0 if "exp" claim does
  // not exist.
  int64_t Exp();

 private:
  const EVP_MD* md_;

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
  std::vector<std::string> aud_;
  std::string sub_;
  int64_t exp_;

  /*
   * TODO: try not to use friend function
   */
  friend bool Verifier::Verify(const Jwt& jwt, const Pubkeys& pubkeys);
};

// Class to parse and a hold public key(s).
// It also holds the failure reason if parse failed.
//
// Usage example:
//   std::unique_ptr<Pubkeys> keys = Pubkeys::ParseFromJwks(jwks_string);
//   if(keys->GetStatus() == Status::OK) { ... }
class Pubkeys : public WithStatus {
 public:
  // Format of public key.
  enum Type { PEM, JWKS };

  Pubkeys(){};
  static std::unique_ptr<Pubkeys> CreateFrom(const std::string& pkey,
                                             Type type);

 private:
  void CreateFromPemCore(const std::string& pkey_pem);
  void CreateFromJwksCore(const std::string& pkey_jwks);
  // Extracts the public key from a jwk key (jkey) and sets it to keys_;
  void ExtractPubkeyFromJwk(Json::ObjectSharedPtr jwk_json);
  void ExtractPubkeyFromJwkRSA(Json::ObjectSharedPtr jwk_json);
  void ExtractPubkeyFromJwkEC(Json::ObjectSharedPtr jwk_json);

  class Pubkey {
   public:
    Pubkey(){};
    bssl::UniquePtr<EVP_PKEY> evp_pkey_;
    bssl::UniquePtr<EC_KEY> ec_key_;
    std::string kid_;
    std::string kty_;
    bool alg_specified_ = false;
    bool kid_specified_ = false;
    bool pem_format_ = false;
    std::string alg_;
  };
  std::vector<std::unique_ptr<Pubkey> > keys_;

  /*
   * TODO: try not to use friend function
   */
  friend bool Verifier::Verify(const Jwt& jwt, const Pubkeys& pubkeys);
};

}  // namespace JwtAuth
}  // namespace Http
}  // namespace Envoy
