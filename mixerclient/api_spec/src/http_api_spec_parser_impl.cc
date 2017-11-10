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

#include "http_api_spec_parser_impl.h"
#include "google/protobuf/stubs/logging.h"

using ::istio::mixer::v1::Attributes;
using ::istio::mixer::v1::config::client::HTTPAPISpec;
using ::istio::mixer::v1::config::client::HTTPAPISpecPattern;

namespace istio {
namespace api_spec {

HttpApiSpecParserImpl::HttpApiSpecParserImpl(const HTTPAPISpec& api_spec)
    : api_spec_(api_spec) {
  PathMatcherBuilder<const Attributes*> pmb;
  for (const auto& pattern : api_spec_.patterns()) {
    if (pattern.pattern_case() == HTTPAPISpecPattern::kUriTemplate) {
      if (!pmb.Register(pattern.http_method(), pattern.uri_template(),
                        std::string(), &pattern.attributes())) {
        GOOGLE_LOG(WARNING) << "Invalid uri_template: "
                            << pattern.uri_template();
      }
    } else {
      regex_list_.emplace_back(pattern.regex(), pattern.http_method(),
                               &pattern.attributes());
    }
  }
  path_matcher_ = pmb.Build();
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

std::unique_ptr<HttpApiSpecParser> HttpApiSpecParser::Create(
    const ::istio::mixer::v1::config::client::HTTPAPISpec& api_spec) {
  return std::unique_ptr<HttpApiSpecParser>(
      new HttpApiSpecParserImpl(api_spec));
}

}  // namespace api_spec
}  // namespace istio
