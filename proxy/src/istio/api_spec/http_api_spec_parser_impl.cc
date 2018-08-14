/* Copyright 2017 Istio Authors. All Rights Reserved.
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

#include "proxy/src/istio/api_spec/http_api_spec_parser_impl.h"
#include "google/protobuf/stubs/logging.h"

#include <algorithm>
#include <cctype>

using ::istio::control::http::CheckData;
using ::istio::mixer::v1::Attributes;
using ::istio::mixer::v1::config::client::APIKey;
using ::istio::mixer::v1::config::client::HTTPAPISpec;
using ::istio::mixer::v1::config::client::HTTPAPISpecPattern;

namespace istio {
namespace api_spec {
namespace {
// If api-key is not defined in APISpec, use following defaults.
const std::string kApiKeyDefaultQueryName1("key");
const std::string kApiKeyDefaultQueryName2("api_key");
const std::string kApiKeyDefaultHeader("x-api-key");
}  // namespace

HttpApiSpecParserImpl::HttpApiSpecParserImpl(const HTTPAPISpec& api_spec)
    : api_spec_(api_spec) {
  BuildPathMatcher();
  BuildApiKeyData();
}

void HttpApiSpecParserImpl::BuildPathMatcher() {
  PathMatcherBuilder<const Attributes*> pmb;
  for (const auto& pattern : api_spec_.patterns()) {
    if (pattern.pattern_case() == HTTPAPISpecPattern::kUriTemplate) {
      if (!pmb.Register(pattern.http_method(), pattern.uri_template(),
                        std::string(), &pattern.attributes())) {
        GOOGLE_LOG(WARNING)
            << "Invalid uri_template: " << pattern.uri_template();
      }
    } else {
      regex_list_.emplace_back(pattern.regex(), pattern.http_method(),
                               &pattern.attributes());
    }
  }
  path_matcher_ = pmb.Build();
}

void HttpApiSpecParserImpl::BuildApiKeyData() {
  if (api_spec_.api_keys_size() == 0) {
    api_spec_.add_api_keys()->set_query(kApiKeyDefaultQueryName1);
    api_spec_.add_api_keys()->set_query(kApiKeyDefaultQueryName2);
    api_spec_.add_api_keys()->set_header(kApiKeyDefaultHeader);
  }
}

void HttpApiSpecParserImpl::AddAttributes(
    const std::string& http_method, const std::string& path,
    ::istio::mixer::v1::Attributes* attributes) {
  // Add the global attributes.
  attributes->MergeFrom(api_spec_.attributes());

  const Attributes* matched_attributes =
      path_matcher_->Lookup(http_method, path);
  if (matched_attributes) {
    attributes->MergeFrom(*matched_attributes);
  }

  // Check regex list
  for (const auto& re : regex_list_) {
    if (re.http_method == http_method && std::regex_match(path, re.regex)) {
      attributes->MergeFrom(*re.attributes);
    }
  }
}

bool HttpApiSpecParserImpl::ExtractApiKey(CheckData* check_data,
                                          std::string* value) {
  for (const auto& api_key : api_spec_.api_keys()) {
    switch (api_key.key_case()) {
      case APIKey::kQuery:
        if (check_data->FindQueryParameter(api_key.query(), value)) {
          return true;
        }
        break;
      case APIKey::kHeader:
        if (check_data->FindHeaderByName(api_key.header(), value)) {
          return true;
        }
        break;
      case APIKey::kCookie:
        if (check_data->FindCookie(api_key.cookie(), value)) {
          return true;
        }
        break;
      case APIKey::KEY_NOT_SET:
        break;
    }
  }
  return false;
}

std::unique_ptr<HttpApiSpecParser> HttpApiSpecParser::Create(
    const ::istio::mixer::v1::config::client::HTTPAPISpec& api_spec) {
  return std::unique_ptr<HttpApiSpecParser>(
      new HttpApiSpecParserImpl(api_spec));
}

}  // namespace api_spec
}  // namespace istio
