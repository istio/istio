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
#ifndef API_MANAGER_METHOD_H_
#define API_MANAGER_METHOD_H_

#include <set>
#include <string>
#include <vector>

namespace google {
namespace api_manager {

// A minimal set of data attached to a method.
class MethodInfo {
 public:
  virtual ~MethodInfo() {}

  // Return the method name
  virtual const std::string &name() const = 0;

  // Return the API name
  virtual const std::string &api_name() const = 0;

  // Return the API version
  virtual const std::string &api_version() const = 0;

  // Return the method selector
  virtual const std::string &selector() const = 0;

  // Return if auth is enabled for this method.
  virtual bool auth() const = 0;

  // Return if this method allows unregistered calls.
  virtual bool allow_unregistered_calls() const = 0;

  // Check an issuer is allowed.
  virtual bool isIssuerAllowed(const std::string &issuer) const = 0;

  // Check if an audience is allowed for an issuer.
  virtual bool isAudienceAllowed(
      const std::string &issuer,
      const std::set<std::string> &jwt_audiences) const = 0;

  // Get http header system parameters by name.
  virtual const std::vector<std::string> *http_header_parameters(
      const std::string &name) const = 0;

  // Get url query system parameters by name.
  virtual const std::vector<std::string> *url_query_parameters(
      const std::string &name) const = 0;

  // Get http header system parameters for api_key.
  virtual const std::vector<std::string> *api_key_http_headers() const = 0;

  // Get url query system parameters for api_key.
  virtual const std::vector<std::string> *api_key_url_query_parameters()
      const = 0;

  // Get the backend address for this method.
  virtual const std::string &backend_address() const = 0;

  // Get the RPC method full name. The full name has the following form:
  // "/<API name>/<method name>".
  virtual const std::string &rpc_method_full_name() const = 0;

  // Get the request_type_url
  virtual const std::string &request_type_url() const = 0;

  // Get whether request is streaming
  virtual bool request_streaming() const = 0;

  // Get the response_type_url
  virtual const std::string &response_type_url() const = 0;

  // Get whether response is streaming
  virtual bool response_streaming() const = 0;

  // Get the names of url system parameters
  virtual const std::set<std::string> &system_query_parameter_names() const = 0;
};

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_METHOD_H_
