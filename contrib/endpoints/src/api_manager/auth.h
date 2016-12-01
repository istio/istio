/*
 * Copyright (C) Extensible Service Proxy Authors
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions
 * are met:
 * 1. Redistributions of source code must retain the above copyright
 *    notice, this list of conditions and the following disclaimer.
 * 2. Redistributions in binary form must reproduce the above copyright
 *    notice, this list of conditions and the following disclaimer in the
 *    documentation and/or other materials provided with the distribution.
 *
 * THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS ``AS IS'' AND
 * ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
 * IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
 * ARE DISCLAIMED.  IN NO EVENT SHALL THE AUTHOR OR CONTRIBUTORS BE LIABLE
 * FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
 * DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
 * OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
 * HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
 * LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
 * OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
 * SUCH DAMAGE.
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
