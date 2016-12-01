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
#ifndef API_MANAGER_REQUEST_H_
#define API_MANAGER_REQUEST_H_

#include <map>
#include <string>

#include "include/api_manager/protocol.h"
#include "include/api_manager/utils/status.h"

namespace google {
namespace api_manager {

// Request provides an interface for CallHandler::Check to use to
// query information about a request.
class Request {
 public:
  virtual ~Request() {}

  // Returns the HTTP method used for this call.
  virtual std::string GetRequestHTTPMethod() = 0;

  // Returns the REST path or RPC path for this call.
  virtual std::string GetRequestPath() = 0;
  // Returns the query parameters
  virtual std::string GetQueryParameters() = 0;
  // Returns the request path before parsed.
  virtual std::string GetUnparsedRequestPath() = 0;

  // Gets Client IP
  // This will be used by service control Check() call.
  virtual std::string GetClientIP() = 0;

  // Get GRPC stats.
  virtual int64_t GetGrpcRequestBytes() = 0;
  virtual int64_t GetGrpcResponseBytes() = 0;
  virtual int64_t GetGrpcRequestMessageCounts() = 0;
  virtual int64_t GetGrpcResponseMessageCounts() = 0;

  // Finds a HTTP query parameter with a name. Returns true if found.
  virtual bool FindQuery(const std::string &name, std::string *query) = 0;

  // Finds a HTTP header with a name. Returns true if found.
  // Don't support multiple headers with same name for now. In that case,
  // the first header will be returned.
  virtual bool FindHeader(const std::string &name, std::string *header) = 0;

  // Returns the protocol used for this call.
  virtual ::google::api_manager::protocol::Protocol GetRequestProtocol() = 0;

  // Sets auth token to the request object. Caller of RequestHandler::Check
  // need to use it compose error message if authentication fails.
  virtual void SetAuthToken(const std::string &auth_token) = 0;

  // Adds a header to backend. If the header exists, overwrite its value
  virtual utils::Status AddHeaderToBackend(const std::string &key,
                                           const std::string &value) = 0;
};

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_REQUEST_H_
