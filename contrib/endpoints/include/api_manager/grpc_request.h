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
#ifndef API_MANAGER_GRPC_REQUEST_H_
#define API_MANAGER_GRPC_REQUEST_H_

#include <functional>
#include <string>

#include "contrib/endpoints/include/api_manager/utils/status.h"

namespace google {
namespace api_manager {

// Represents a Grpc request issued by the API Manager and
// processed by the environment.
class GRPCRequest {
 public:
  // GRPCRequest constructor without headers in the callback function.
  GRPCRequest(std::function<void(utils::Status, std::string&&)> callback)
      : callback_(callback) {}

  // A callback for the environment to invoke when the request is
  // complete. This will be invoked by the environment exactly once,
  // and then the environment will drop the std::unique_ptr
  // referencing the GRPCRequest.
  void OnComplete(utils::Status status, std::string&& body) {
    callback_(status, std::move(body));
  }

  // the gRPC method to call
  const std::string& method() const { return method_; }
  GRPCRequest& set_method(const std::string& value) {
    method_ = value;
    return *this;
  }
  GRPCRequest& set_method(std::string&& value) {
    method_ = std::move(value);
    return *this;
  }

  // DNS or IP address of the gRPC server.
  const std::string& server() const { return server_; }
  GRPCRequest& set_server(const std::string& value) {
    server_ = value;
    return *this;
  }
  GRPCRequest& set_server(std::string&& value) {
    server_ = std::move(value);
    return *this;
  }

  // The gRPC service name.
  const std::string& service() const { return service_; }
  GRPCRequest& set_service(const std::string& value) {
    service_ = value;
    return *this;
  }
  GRPCRequest& set_service(std::string&& value) {
    service_ = std::move(value);
    return *this;
  }

  // The request body serialized as string.
  const std::string& body() const { return body_; }
  GRPCRequest& set_body(const std::string& value) {
    body_ = value;
    return *this;
  }
  GRPCRequest& set_body(std::string&& value) {
    body_ = std::move(value);
    return *this;
  }

 private:
  std::function<void(utils::Status, std::string&&)> callback_;
  std::string method_;
  std::string server_;
  std::string service_;
  std::string body_;
};

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_GRPC_REQUEST_H_
