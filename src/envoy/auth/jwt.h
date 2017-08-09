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
namespace Jwt {

// This function verifies JWT signature and returns the decoded payload as a
// JSON if the signature is valid.
// If verification failed, it returns nullptr.
std::unique_ptr<rapidjson::Document> Decode(const std::string& jwt,
                                            const std::string& pkey_pem);

std::unique_ptr<rapidjson::Document> DecodeWithJwk(const std::string& jwt,
                                                   const std::string& jwks);

}  // Jwt
}  // Auth
}  // Http
}  // Envoy

#endif  // PROXY_JWT_H
