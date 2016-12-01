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
#ifndef API_MANAGER_MOCK_METHOD_INFO_H_
#define API_MANAGER_MOCK_METHOD_INFO_H_

#include "gmock/gmock.h"
#include "include/api_manager/method.h"

namespace google {
namespace api_manager {

class MockMethodInfo : public MethodInfo {
 public:
  virtual ~MockMethodInfo() {}
  MOCK_CONST_METHOD0(name, const std::string&());
  MOCK_CONST_METHOD0(api_name, const std::string&());
  MOCK_CONST_METHOD0(api_version, const std::string&());
  MOCK_CONST_METHOD0(selector, const std::string&());
  MOCK_CONST_METHOD0(auth, bool());
  MOCK_CONST_METHOD0(allow_unregistered_calls, bool());
  MOCK_CONST_METHOD1(isIssuerAllowed, bool(const std::string&));
  MOCK_CONST_METHOD2(isAudienceAllowed,
                     bool(const std::string&, const std::set<std::string>&));
  MOCK_CONST_METHOD1(http_header_parameters,
                     const std::vector<std::string>*(const std::string&));
  MOCK_CONST_METHOD1(url_query_parameters,
                     const std::vector<std::string>*(const std::string&));
  MOCK_CONST_METHOD0(api_key_http_headers, const std::vector<std::string>*());
  MOCK_CONST_METHOD0(api_key_url_query_parameters,
                     const std::vector<std::string>*());
  MOCK_CONST_METHOD0(backend_address, const std::string&());
  MOCK_CONST_METHOD0(rpc_method_full_name, const std::string&());
  MOCK_CONST_METHOD0(request_type_url, const std::string&());
  MOCK_CONST_METHOD0(request_streaming, bool());
  MOCK_CONST_METHOD0(response_type_url, const std::string&());
  MOCK_CONST_METHOD0(response_streaming, bool());
  MOCK_CONST_METHOD0(system_query_parameter_names,
                     const std::set<std::string>&());
};

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_MOCK_METHOD_INFO_H_
