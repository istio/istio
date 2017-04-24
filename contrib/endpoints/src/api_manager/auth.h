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
#ifndef API_MANAGER_AUTH_H_
#define API_MANAGER_AUTH_H_

#include <set>
#include <sstream>
#include <string>

namespace google {
namespace api_manager {

// Holds authentication results, used as a bridge between host proxy
// and grpc auth lib.
struct UserInfo {
  // Unique ID of authenticated user.
  std::string id;
  // Email address of authenticated user.
  std::string email;
  // Consumer ID that identifies client app, used for servicecontrol.
  std::string consumer_id;
  // Issuer of the incoming JWT.
  // See https://tools.ietf.org/html/rfc7519.
  std::string issuer;
  // Audience of the incoming JWT.
  // See https://tools.ietf.org/html/rfc7519.
  std::set<std::string> audiences;
  // Authorized party of the incoming JWT.
  // See http://openid.net/specs/openid-connect-core-1_0.html#IDToken
  std::string authorized_party;
  // String of claims
  std::string claims;

  // Returns audiences as a comma separated strings.
  std::string AudiencesAsString() const {
    std::ostringstream os;
    for (auto it = audiences.begin(); it != audiences.end(); ++it) {
      if (it != audiences.begin()) {
        os << ",";
      }
      os << *it;
    }
    return os.str();
  }
};

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_AUTH_H_
