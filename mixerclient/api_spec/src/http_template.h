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

#ifndef API_SPEC_HTTP_TEMPLATE_H_
#define API_SPEC_HTTP_TEMPLATE_H_

#include <memory>
#include <string>
#include <vector>

namespace istio {
namespace api_spec {

class HttpTemplate {
 public:
  static std::unique_ptr<HttpTemplate> Parse(const std::string &ht);
  const std::vector<std::string> &segments() const { return segments_; }
  const std::string &verb() const { return verb_; }

  // The info about a variable binding {variable=subpath} in the template.
  struct Variable {
    // Specifies the range of segments [start_segment, end_segment) the
    // variable binds to. Both start_segment and end_segment are 0 based.
    // end_segment can also be negative, which means that the position is
    // specified relative to the end such that -1 corresponds to the end
    // of the path.
    int start_segment;
    int end_segment;

    // The path of the protobuf field the variable binds to.
    std::vector<std::string> field_path;

    // Do we have a ** in the variable template?
    bool has_wildcard_path;
  };

  std::vector<Variable> &Variables() { return variables_; }

  // '/.': match any single path segment.
  static const char kSingleParameterKey[];
  // '*': Wildcard match for one path segment.
  static const char kWildCardPathPartKey[];
  // '**': Wildcard match the remaining path.
  static const char kWildCardPathKey[];

 private:
  HttpTemplate(std::vector<std::string> &&segments, std::string &&verb,
               std::vector<Variable> &&variables)
      : segments_(std::move(segments)),
        verb_(std::move(verb)),
        variables_(std::move(variables)) {}
  const std::vector<std::string> segments_;
  std::string verb_;
  std::vector<Variable> variables_;
};

}  // namespace api_spec
}  // namespace istio

#endif  // API_SPEC_HTTP_TEMPLATE_H_
