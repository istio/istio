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

#ifndef API_SPEC_HTTP_API_SPEC_PARSER_H_
#define API_SPEC_HTTP_API_SPEC_PARSER_H_

#include <string>

#include "mixer/v1/attributes.pb.h"
#include "mixer/v1/config/client/api_spec.pb.h"

namespace istio {
namespace api_spec {

// The interface to parse HTTPAPISpec and generate api attributes.
class HttpApiSpecParser {
 public:
  virtual ~HttpApiSpecParser() {}

  // Extract API attributes based on the APISpec.
  // For a given http_method and path.
  virtual void AddAttributes(const std::string& http_method,
                             const std::string& path,
                             ::istio::mixer::v1::Attributes* attributes) = 0;

  // The factory function to create an instance.
  static std::unique_ptr<HttpApiSpecParser> Create(
      const ::istio::mixer::v1::config::client::HTTPAPISpec& api_spec);
};

}  // namespace api_spec
}  // namespace istio

#endif  // API_SPEC_HTTP_API_SPEC_PARSER_H_
