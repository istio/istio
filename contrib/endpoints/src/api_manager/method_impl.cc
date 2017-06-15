// Copyright 2016 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
////////////////////////////////////////////////////////////////////////////////
//
#include "contrib/endpoints/src/api_manager/method_impl.h"
#include "contrib/endpoints/src/api_manager/utils/url_util.h"

#include <sstream>

using std::map;
using std::set;
using std::string;
using std::stringstream;

namespace google {
namespace api_manager {

// The name for api key in system parameter from service config.
const char api_key_parameter_name[] = "api_key";

MethodInfoImpl::MethodInfoImpl(const string &name, const string &api_name,
                               const string &api_version)
    : name_(name),
      api_name_(api_name),
      api_version_(api_version),
      auth_(false),
      allow_unregistered_calls_(false),
      skip_service_control_(false),
      api_key_http_headers_(nullptr),
      api_key_url_query_parameters_(nullptr),
      request_streaming_(false),
      response_streaming_(false) {}

void MethodInfoImpl::addAudiencesForIssuer(const string &issuer,
                                           const string &audiences_list) {
  if (issuer.empty()) {
    return;
  }
  std::string iss = utils::GetUrlContent(issuer);
  if (iss.empty()) {
    return;
  }
  set<string> &audiences = issuer_audiences_map_[iss];
  stringstream ss(audiences_list);
  string audience;
  // Audience list is comma-delimited.
  while (getline(ss, audience, ',')) {
    if (!audience.empty()) {  // Only adds non-empty audience.
      std::string aud = utils::GetUrlContent(audience);
      if (!aud.empty()) {
        audiences.insert(aud);
      }
    }
  }
}

bool MethodInfoImpl::isIssuerAllowed(const std::string &issuer) const {
  return !issuer.empty() &&
         issuer_audiences_map_.find(issuer) != issuer_audiences_map_.end();
}

bool MethodInfoImpl::isAudienceAllowed(
    const string &issuer, const std::set<string> &jwt_audiences) const {
  if (issuer.empty() || jwt_audiences.empty() || !isIssuerAllowed(issuer)) {
    return false;
  }
  const set<string> &audiences = issuer_audiences_map_.at(issuer);
  for (const auto &it : jwt_audiences) {
    if (audiences.find(it) != audiences.end()) {
      return true;
    }
  }
  return false;
}

void MethodInfoImpl::process_system_parameters() {
  api_key_http_headers_ = http_header_parameters(api_key_parameter_name);
  api_key_url_query_parameters_ = url_query_parameters(api_key_parameter_name);
}

void MethodInfoImpl::ProcessSystemQueryParameterNames() {
  for (const auto &param : url_query_parameters_) {
    for (const auto &name : param.second) {
      system_query_parameter_names_.insert(name);
    }
  }

  if (!api_key_http_headers_ && !api_key_url_query_parameters_) {
    // Adding the default api_key url query parameters
    system_query_parameter_names_.insert("key");
    system_query_parameter_names_.insert("api_key");
  }
}

}  // namespace api_manager
}  // namespace google
