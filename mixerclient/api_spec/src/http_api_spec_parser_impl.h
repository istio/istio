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

#ifndef API_SPEC_HTTP_API_SPEC_PARSER_IMPL_H_
#define API_SPEC_HTTP_API_SPEC_PARSER_IMPL_H_

#include "api_spec/include/http_api_spec_parser.h"
#include "path_matcher.h"

#include <regex>
#include <vector>

namespace istio {
namespace api_spec {

// The implementation class for HttpApiSpecParser interface.
class HttpApiSpecParserImpl : public HttpApiSpecParser {
 public:
  HttpApiSpecParserImpl(
      const ::istio::mixer::v1::config::client::HTTPAPISpec& api_spec);

  void AddAttributes(const std::string& http_method, const std::string& path,
                     ::istio::mixer::v1::Attributes* attributes) override;

 private:
  // The http api spec.
  ::istio::mixer::v1::config::client::HTTPAPISpec api_spec_;

  // The path matcher for all url templates
  PathMatcherPtr<const ::istio::mixer::v1::Attributes*> path_matcher_;

  struct RegexData {
    RegexData(const std::string& regex, const std::string& http_method,
              const ::istio::mixer::v1::Attributes* attributes)
        : regex(regex), http_method(http_method), attributes(attributes) {}

    std::regex regex;
    std::string http_method;
    // The attributes to add if matched.
    const ::istio::mixer::v1::Attributes* attributes;
  };
  std::vector<RegexData> regex_list_;
};

}  // namespace api_spec
}  // namespace istio

#endif  // API_SPEC_HTTP_API_SPEC_PARSER_IMPL_H_
