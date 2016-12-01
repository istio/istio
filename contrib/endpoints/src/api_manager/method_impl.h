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
#ifndef API_MANAGER_METHOD_IMPL_H_
#define API_MANAGER_METHOD_IMPL_H_

#include <map>
#include <memory>
#include <set>

#include "include/api_manager/method.h"
#include "src/api_manager/utils/stl_util.h"

namespace google {
namespace api_manager {

// An implementation of MethodInfo interface.
class MethodInfoImpl : public MethodInfo {
 public:
  MethodInfoImpl(const std::string &name, const std::string &api_name,
                 const std::string &api_version);

  const std::string &name() const { return name_; }
  const std::string &api_name() const { return api_name_; }
  const std::string &api_version() const { return api_version_; }
  const std::string &selector() const { return selector_; }
  bool auth() const { return auth_; }
  bool allow_unregistered_calls() const { return allow_unregistered_calls_; }

  bool isIssuerAllowed(const std::string &issuer) const;

  bool isAudienceAllowed(const std::string &issuer,
                         const std::set<std::string> &jwt_audiences) const;

  const std::vector<std::string> *http_header_parameters(
      const std::string &name) const {
    return utils::FindOrNull(http_header_parameters_, name);
  }
  const std::vector<std::string> *url_query_parameters(
      const std::string &name) const {
    return utils::FindOrNull(url_query_parameters_, name);
  }

  const std::vector<std::string> *api_key_http_headers() const {
    return api_key_http_headers_;
  }

  const std::vector<std::string> *api_key_url_query_parameters() const {
    return api_key_url_query_parameters_;
  }

  const std::string &backend_address() const { return backend_address_; }

  const std::string &rpc_method_full_name() const {
    return rpc_method_full_name_;
  }

  const std::string &request_type_url() const { return request_type_url_; }

  bool request_streaming() const { return request_streaming_; }

  const std::string &response_type_url() const { return response_type_url_; }

  bool response_streaming() const { return response_streaming_; }

  // Adds allowed audiences (comma delimated, no space) for the issuer.
  // audiences_list can be empty.
  void addAudiencesForIssuer(const std::string &issuer,
                             const std::string &audiences_list);
  void set_auth(bool v) { auth_ = v; }
  void set_allow_unregistered_calls(bool v) { allow_unregistered_calls_ = v; }

  void add_http_header_parameter(const std::string &name,
                                 const std::string &http_header) {
    http_header_parameters_[name].push_back(http_header);
  }
  void add_url_query_parameter(const std::string &name,
                               const std::string &url_query_parameter) {
    url_query_parameters_[name].push_back(url_query_parameter);
  }

  // After add all system parameters, lookup some of them to cache
  // their lookup results.
  void process_system_parameters();

  void set_selector(const std::string &selector) { selector_ = selector; }

  void set_backend_address(const std::string &address) {
    backend_address_ = address;
  }

  void set_rpc_method_full_name(std::string rpc_method_full_name) {
    rpc_method_full_name_ = std::move(rpc_method_full_name);
  }

  void set_request_type_url(std::string request_type_url) {
    request_type_url_ = std::move(request_type_url);
  }

  void set_request_streaming(bool request_streaming) {
    request_streaming_ = request_streaming;
  }

  void set_response_type_url(std::string response_type_url) {
    response_type_url_ = std::move(response_type_url);
  }

  void set_response_streaming(bool response_streaming) {
    response_streaming_ = response_streaming;
  }

  const std::set<std::string> &system_query_parameter_names() const {
    return system_query_parameter_names_;
  }

  void ProcessSystemQueryParameterNames();

 private:
  // Method name
  std::string name_;
  // API name
  std::string api_name_;
  // API version
  std::string api_version_;
  // Whether auth is enabled.
  bool auth_;
  // Does the method allow unregistered callers (callers without client identity
  // such as API Key)?
  bool allow_unregistered_calls_;
  // Issuers to allowed audiences map.
  std::map<std::string, std::set<std::string> > issuer_audiences_map_;

  // system parameter map of parameter name to http_header name.
  std::map<std::string, std::vector<std::string> > http_header_parameters_;

  // system parameter map of parameter name to url query parameter name.
  std::map<std::string, std::vector<std::string> > url_query_parameters_;

  // all the names of system query parameters
  std::set<std::string> system_query_parameter_names_;

  // http header for api_key.
  const std::vector<std::string> *api_key_http_headers_;

  const std::vector<std::string> *api_key_url_query_parameters_;

  // The backend address for this method.
  std::string backend_address_;

  // Method selector
  std::string selector_;

  // The RPC method name
  std::string rpc_method_full_name_;

  // The request type url
  std::string request_type_url_;

  // Whether the request is streaming or not.
  bool request_streaming_;

  // The response type url
  std::string response_type_url_;

  // Whether the response is streaming or not.
  bool response_streaming_;
};

typedef std::unique_ptr<MethodInfoImpl> MethodInfoImplPtr;

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_METHOD_IMPL_H_
