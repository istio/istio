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
#ifndef API_MANAGER_HTTP_REQUEST_H_
#define API_MANAGER_HTTP_REQUEST_H_

#include <functional>
#include <string>

#include "include/api_manager/utils/status.h"

namespace google {
namespace api_manager {

// Represents a non-streaming HTTP request issued by the API Manager and
// processed by the environment.
class HTTPRequest {
 public:
  // HTTPRequest constructor without headers in the callback function.
  // Callback receives NGX_ERROR status code if the request fails to initiate or
  // complete.
  // Callback receives HTTP response code in status, if the request succeeds.
  // The keys in headers passed to the callback are in lowercase.
  HTTPRequest(
      std::function<void(utils::Status, std::map<std::string, std::string>&&,
                         std::string&&)>
          callback)
      : callback_(callback),
        requires_response_headers_(false),
        timeout_ms_(0),
        max_retries_(0),
        timeout_backoff_factor_(2.0) {}

  // A callback for the environment to invoke when the request is
  // complete.  This will be invoked by the environment exactly once,
  // and then the environment will drop the std::unique_ptr
  // referencing the HTTPRequest.
  // Headers are supplied only if the request requires them; otherwise, it's an
  // empty map.
  void OnComplete(utils::Status status,
                  std::map<std::string, std::string>&& headers,
                  std::string&& body) {
    callback_(status, std::move(headers), std::move(body));
  }

  // Request properties.
  const std::string& method() const { return method_; }
  HTTPRequest& set_method(const std::string& value) {
    method_ = value;
    return *this;
  }
  HTTPRequest& set_method(std::string&& value) {
    method_ = std::move(value);
    return *this;
  }

  const std::string& url() const { return url_; }
  HTTPRequest& set_url(const std::string& value) {
    url_ = value;
    return *this;
  }
  HTTPRequest& set_url(std::string&& value) {
    url_ = std::move(value);
    return *this;
  }

  const std::string& body() const { return body_; }
  HTTPRequest& set_body(const std::string& value) {
    body_ = value;
    return *this;
  }
  HTTPRequest& set_body(std::string&& value) {
    body_ = std::move(value);
    return *this;
  }

  HTTPRequest& set_auth_token(const std::string& value) {
    if (!value.empty()) {
      set_header("Authorization", "Bearer " + value);
    }
    return *this;
  }

  int timeout_ms() const { return timeout_ms_; }
  HTTPRequest& set_timeout_ms(int value) {
    timeout_ms_ = value;
    return *this;
  }

  int max_retries() const { return max_retries_; }
  HTTPRequest& set_max_retries(int value) {
    max_retries_ = value;
    return *this;
  }

  double timeout_backoff_factor() const { return timeout_backoff_factor_; }
  HTTPRequest& set_timeout_backoff_factor(double value) {
    timeout_backoff_factor_ = value;
    return *this;
  }

  const std::map<std::string, std::string>& request_headers() const {
    return headers_;
  }
  HTTPRequest& set_header(const std::string& name, const std::string& value) {
    headers_[name] = value;
    return *this;
  }

  bool requires_response_headers() const { return requires_response_headers_; }
  HTTPRequest& set_requires_response_headers(bool value) {
    requires_response_headers_ = value;
    return *this;
  }

 private:
  std::function<void(utils::Status, std::map<std::string, std::string>&&,
                     std::string&&)>
      callback_;
  std::string method_;
  std::string url_;
  std::string body_;
  std::map<std::string, std::string> headers_;

  // Indicates whether to extract headers from the response
  bool requires_response_headers_;

  // Timeout, in milliseconds, for the request.
  int timeout_ms_;

  // Maximum number of retries, 0 to skip
  int max_retries_;

  // Exponential back-off for the retries
  double timeout_backoff_factor_;
};

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_HTTP_REQUEST_H_
