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

#include <cassert>
#include <string>
#include <vector>

#include "http_template.h"

namespace istio {
namespace api_spec {

namespace {

// TODO: implement an error sink.

// HTTP Template Grammar:
// Questions:
//   - what are the constraints on LITERAL and IDENT?
//   - what is the character set for the grammar?
//
// Template = "/" Segments [ Verb ] ;
// Segments = Segment { "/" Segment } ;
// Segment  = "*" | "**" | LITERAL | Variable ;
// Variable = "{" FieldPath [ "=" Segments ] "}" ;
// FieldPath = IDENT { "." IDENT } ;
// Verb     = ":" LITERAL ;
class Parser {
 public:
  Parser(const std::string &input)
      : input_(input), tb_(0), te_(0), in_variable_(false) {}

  bool Parse() {
    if (!ParseTemplate() || !ConsumedAllInput()) {
      return false;
    }
    PostProcessVariables();
    return true;
  }

  std::vector<std::string> &segments() { return segments_; }
  std::string &verb() { return verb_; }
  std::vector<HttpTemplate::Variable> &variables() { return variables_; }

  // only constant path segments are allowed after '**'.
  bool ValidateParts() {
    bool found_wild_card = false;
    for (size_t i = 0; i < segments_.size(); i++) {
      if (!found_wild_card) {
        if (segments_[i] == HttpTemplate::kWildCardPathKey) {
          found_wild_card = true;
        }
      } else if (segments_[i] == HttpTemplate::kSingleParameterKey ||
                 segments_[i] == HttpTemplate::kWildCardPathPartKey ||
                 segments_[i] == HttpTemplate::kWildCardPathKey) {
        return false;
      }
    }
    return true;
  }

 private:
  // Template = "/" Segments [ Verb ] ;
  bool ParseTemplate() {
    if (!Consume('/')) {
      // Expected '/'
      return false;
    }
    if (!ParseSegments()) {
      return false;
    }

    if (EnsureCurrent() && current_char() == ':') {
      if (!ParseVerb()) {
        return false;
      }
    }
    return true;
  }

  // Segments = Segment { "/" Segment } ;
  bool ParseSegments() {
    if (!ParseSegment()) {
      return false;
    }

    for (;;) {
      if (!Consume('/')) break;
      if (!ParseSegment()) {
        return false;
      }
    }

    return true;
  }

  // Segment  = "*" | "**" | LITERAL | Variable ;
  bool ParseSegment() {
    if (!EnsureCurrent()) {
      return false;
    }
    switch (current_char()) {
      case '*': {
        Consume('*');
        if (Consume('*')) {
          // **
          segments_.push_back("**");
          if (in_variable_) {
            return MarkVariableHasWildCardPath();
          }
          return true;
        } else {
          segments_.push_back("*");
          return true;
        }
      }

      case '{':
        return ParseVariable();
      default:
        return ParseLiteralSegment();
    }
  }

  // Variable = "{" FieldPath [ "=" Segments ] "}" ;
  bool ParseVariable() {
    if (!Consume('{')) {
      return false;
    }
    if (!StartVariable()) {
      return false;
    }
    if (!ParseFieldPath()) {
      return false;
    }
    if (Consume('=')) {
      if (!ParseSegments()) {
        return false;
      }
    } else {
      // {field_path} is equivalent to {field_path=*}
      segments_.push_back("*");
    }
    if (!EndVariable()) {
      return false;
    }
    if (!Consume('}')) {
      return false;
    }
    return true;
  }

  bool ParseLiteralSegment() {
    std::string ls;
    if (!ParseLiteral(&ls)) {
      return false;
    }
    segments_.push_back(ls);
    return true;
  }

  // FieldPath = IDENT { "." IDENT } ;
  bool ParseFieldPath() {
    if (!ParseIdentifier()) {
      return false;
    }
    while (Consume('.')) {
      if (!ParseIdentifier()) {
        return false;
      }
    }
    return true;
  }

  // Verb     = ":" LITERAL ;
  bool ParseVerb() {
    if (!Consume(':')) return false;
    if (!ParseLiteral(&verb_)) return false;
    return true;
  }

  bool ParseIdentifier() {
    std::string idf;

    // Initialize to false to handle empty literal.
    bool result = false;

    while (NextChar()) {
      char c;
      switch (c = current_char()) {
        case '.':
        case '}':
        case '=':
          return result && AddFieldIdentifier(std::move(idf));
        default:
          Consume(c);
          idf.push_back(c);
          break;
      }
      result = true;
    }
    return result && AddFieldIdentifier(std::move(idf));
  }

  bool ParseLiteral(std::string *lit) {
    if (!EnsureCurrent()) {
      return false;
    }

    // Initialize to false in case we encounter an empty literal.
    bool result = false;

    for (;;) {
      char c;
      switch (c = current_char()) {
        case '/':
        case ':':
        case '}':
          return result;
        default:
          Consume(c);
          lit->push_back(c);
          break;
      }

      result = true;

      if (!NextChar()) {
        break;
      }
    }
    return result;
  }

  bool Consume(char c) {
    if (tb_ >= te_ && !NextChar()) {
      return false;
    }
    if (current_char() != c) {
      return false;
    }
    tb_++;
    return true;
  }

  bool ConsumedAllInput() { return tb_ >= input_.size(); }

  bool EnsureCurrent() { return tb_ < te_ || NextChar(); }

  bool NextChar() {
    if (te_ < input_.size()) {
      te_++;
      return true;
    } else {
      return false;
    }
  }

  // Returns the character looked at.
  char current_char() const {
    return tb_ < te_ && te_ <= input_.size() ? input_[te_ - 1] : -1;
  }

  HttpTemplate::Variable &CurrentVariable() { return variables_.back(); }

  bool StartVariable() {
    if (!in_variable_) {
      variables_.push_back(HttpTemplate::Variable{});
      CurrentVariable().start_segment = segments_.size();
      CurrentVariable().has_wildcard_path = false;
      in_variable_ = true;
      return true;
    } else {
      // nested variables are not allowed
      return false;
    }
  }

  bool EndVariable() {
    if (in_variable_ && !variables_.empty()) {
      CurrentVariable().end_segment = segments_.size();
      in_variable_ = false;
      return ValidateVariable(CurrentVariable());
    } else {
      // something's wrong we're not in a variable
      return false;
    }
  }

  bool AddFieldIdentifier(std::string id) {
    if (in_variable_ && !variables_.empty()) {
      CurrentVariable().field_path.emplace_back(std::move(id));
      return true;
    } else {
      // something's wrong we're not in a variable
      return false;
    }
  }

  bool MarkVariableHasWildCardPath() {
    if (in_variable_ && !variables_.empty()) {
      CurrentVariable().has_wildcard_path = true;
      return true;
    } else {
      // something's wrong we're not in a variable
      return false;
    }
  }

  bool ValidateVariable(const HttpTemplate::Variable &var) {
    return !var.field_path.empty() && (var.start_segment < var.end_segment) &&
           (var.end_segment <= static_cast<int>(segments_.size()));
  }

  void PostProcessVariables() {
    for (auto &var : variables_) {
      if (var.has_wildcard_path) {
        // if the variable contains a '**', store the end_positon
        // relative to the end, such that -1 corresponds to the end
        // of the path. As we only support fixed path after '**',
        // this will allow the matcher code to reconstruct the variable
        // value based on the url segments.
        var.end_segment = (var.end_segment - segments_.size() - 1);

        if (!verb_.empty()) {
          // a custom verb will add an additional segment, so
          // the end_postion needs a -1
          --var.end_segment;
        }
      }
    }
  }

  const std::string &input_;

  // Token delimiter indexes
  size_t tb_;
  size_t te_;

  // are we in nested Segments of a variable?
  bool in_variable_;

  std::vector<std::string> segments_;
  std::string verb_;
  std::vector<HttpTemplate::Variable> variables_;
};

}  // namespace

const char HttpTemplate::kSingleParameterKey[] = "/.";

const char HttpTemplate::kWildCardPathPartKey[] = "*";

const char HttpTemplate::kWildCardPathKey[] = "**";

std::unique_ptr<HttpTemplate> HttpTemplate::Parse(const std::string &ht) {
  Parser p(ht);
  if (!p.Parse() || !p.ValidateParts()) {
    return nullptr;
  }

  return std::unique_ptr<HttpTemplate>(new HttpTemplate(
      std::move(p.segments()), std::move(p.verb()), std::move(p.variables())));
}

}  // namespace api_spec
}  // namespace istio
