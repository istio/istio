/* Copyright 2016 Google Inc. All Rights Reserved.
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
#ifndef API_MANAGER_AUTH_LIB_AUTH_JWT_VALIDATOR_H_
#define API_MANAGER_AUTH_LIB_AUTH_JWT_VALIDATOR_H_

#include <chrono>
#include <cstdlib>
#include <memory>

#include "include/api_manager/utils/status.h"
#include "src/api_manager/auth.h"

using ::google::api_manager::utils::Status;

namespace google {
namespace api_manager {
namespace auth {

class JwtValidator {
 public:
  // Create JwtValidator with JWT.
  static std::unique_ptr<JwtValidator> Create(const char *jwt, size_t jwt_len);

  // Parse JWT.
  // Returns Status::OK when parsing is successful, and fills user_info.
  // Otherwise, produces a status error message.
  virtual Status Parse(UserInfo *user_info) = 0;

  // Verify signature.
  // Returns Status::OK when signature verification is successful.
  // Otherwise, produces a status error message.
  virtual Status VerifySignature(const char *pkey, size_t pkey_len) = 0;

  // Returns the expiration time of the JWT.
  virtual std::chrono::system_clock::time_point &GetExpirationTime() = 0;

  virtual ~JwtValidator() {}
};

}  // namespace auth
}  // namespace api_manager
}  // namespace google

#endif /* API_MANAGER_AUTH_LIB_AUTH_JWT_VALIDATOR_H_ */
