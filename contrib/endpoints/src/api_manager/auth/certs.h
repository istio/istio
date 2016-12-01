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
#ifndef API_MANAGER_AUTH_CERTS_H_
#define API_MANAGER_AUTH_CERTS_H_

#include <chrono>
#include <map>
#include <string>

namespace google {
namespace api_manager {
namespace auth {

// A class to manage certs for token validation.
class Certs {
 public:
  void Update(const std::string& issuer, const std::string& cert,
              std::chrono::system_clock::time_point expiration) {
    issuer_cert_map_[issuer] = std::make_pair(cert, expiration);
  }

  const std::pair<std::string, std::chrono::system_clock::time_point>* GetCert(
      const std::string& iss) {
    return issuer_cert_map_.find(iss) == issuer_cert_map_.end()
               ? nullptr
               : &(issuer_cert_map_[iss]);
  }

 private:
  // Map from issuer to a verification key and its absolute expiration time.
  std::map<std::string,
           std::pair<std::string, std::chrono::system_clock::time_point> >
      issuer_cert_map_;
};

}  // namespace auth
}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_AUTH_CERTS_H_
