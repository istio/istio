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
