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
#ifndef API_MANAGER_MOCK_REQUEST_H_
#define API_MANAGER_MOCK_REQUEST_H_

#include "gmock/gmock.h"
#include "include/api_manager/request.h"

namespace google {
namespace api_manager {

class MockRequest : public Request {
 public:
  MOCK_METHOD2(FindQuery, bool(const std::string &, std::string *));
  MOCK_METHOD2(FindHeader, bool(const std::string &, std::string *));
  MOCK_METHOD2(AddHeaderToBackend,
               utils::Status(const std::string &, const std::string &));
  MOCK_METHOD1(SetAuthToken, void(const std::string &));
  MOCK_METHOD0(GetRequestHTTPMethod, std::string());
  MOCK_METHOD0(GetRequestPath, std::string());
  MOCK_METHOD0(GetQueryParameters, std::string());
  MOCK_METHOD0(GetRequestProtocol, ::google::api_manager::protocol::Protocol());
  MOCK_METHOD0(GetUnparsedRequestPath, std::string());
  MOCK_METHOD0(GetInsecureCallerID, std::string());
  MOCK_METHOD0(GetClientIP, std::string());
  MOCK_METHOD0(GetRequestHeaders, std::multimap<std::string, std::string> *());
  MOCK_METHOD0(GetGrpcRequestBytes, int64_t());
  MOCK_METHOD0(GetGrpcResponseBytes, int64_t());
  MOCK_METHOD0(GetGrpcRequestMessageCounts, int64_t());
  MOCK_METHOD0(GetGrpcResponseMessageCounts, int64_t());
};

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_MOCK_REQUEST_H_
