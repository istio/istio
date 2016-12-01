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
