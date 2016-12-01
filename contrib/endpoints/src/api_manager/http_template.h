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
#ifndef API_MANAGER_HTTP_TEMPLATE_H_
#define API_MANAGER_HTTP_TEMPLATE_H_

#include <string>
#include <vector>

namespace google {
namespace api_manager {

class HttpTemplate {
 public:
  static HttpTemplate *Parse(const std::string &ht);
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

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_HTTP_TEMPLATE_H_
