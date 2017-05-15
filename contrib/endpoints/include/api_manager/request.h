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
#ifndef API_MANAGER_REQUEST_H_
#define API_MANAGER_REQUEST_H_

#include <map>
#include <string>

#include "contrib/endpoints/include/api_manager/protocol.h"
#include "contrib/endpoints/include/api_manager/utils/status.h"

namespace google {
namespace api_manager {

// Request provides an interface for CallHandler::Check to use to
// query information about a request.
class Request {
 public:
  virtual ~Request() {}

  // Returns the HTTP method used for this call.
  virtual std::string GetRequestHTTPMethod() = 0;

  // Returns the query parameters
  virtual std::string GetQueryParameters() = 0;

  // Returns the REST path or RPC path for this call.
  // It is the "Unparsed" path without the query parameters.
  virtual std::string GetRequestPath() = 0;

  // Returns the REST path or RPC path for this call.
  // It should be "Unparsed" original URL path.
  virtual std::string GetUnparsedRequestPath() = 0;

  // Gets Client IP
  // This will be used by service control Check() call.
  virtual std::string GetClientIP() = 0;
  // Gets Client Host.
  virtual std::string GetClientHost() { return ""; }

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
  virtual ::google::api_manager::protocol::Protocol GetFrontendProtocol() = 0;
  virtual ::google::api_manager::protocol::Protocol GetBackendProtocol() = 0;

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
