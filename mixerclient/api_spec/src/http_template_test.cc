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

#include "http_template.h"
#include "gtest/gtest.h"

#include <ostream>
#include <string>
#include <vector>

namespace istio {
namespace api_spec {

typedef std::vector<std::string> Segments;
typedef HttpTemplate::Variable Variable;
typedef std::vector<Variable> Variables;
typedef std::vector<std::string> FieldPath;

bool operator==(const Variable &v1, const Variable &v2) {
  return v1.field_path == v2.field_path &&
         v1.start_segment == v2.start_segment &&
         v1.end_segment == v2.end_segment &&
         v1.has_wildcard_path == v2.has_wildcard_path;
}

std::string FieldPathToString(const FieldPath &fp) {
  std::string s;
  for (const auto &f : fp) {
    if (!s.empty()) {
      s += ".";
    }
    s += f;
  }
  return s;
}

std::ostream &operator<<(std::ostream &os, const Variable &var) {
  return os << "{ " << FieldPathToString(var.field_path) << ", ["
            << var.start_segment << ", " << var.end_segment << "), "
            << var.has_wildcard_path << "}";
}

std::ostream &operator<<(std::ostream &os, const Variables &vars) {
  for (const auto &var : vars) {
    os << var << std::endl;
  }
  return os;
}

TEST(HttpTemplate, ParseTest1) {
  auto ht = HttpTemplate::Parse("/shelves/{shelf}/books/{book}");
  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"shelves", "*", "books", "*"}), ht->segments());
  ASSERT_EQ(Variables({
                Variable{1, 2, FieldPath{"shelf"}, false},
                Variable{3, 4, FieldPath{"book"}, false},
            }),
            ht->Variables());
}

TEST(HttpTemplate, ParseTest2) {
  auto ht = HttpTemplate::Parse("/shelves/**");
  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"shelves", "**"}), ht->segments());
  ASSERT_EQ("", ht->verb());
  ASSERT_EQ(Variables({}), ht->Variables());
}

TEST(HttpTemplate, ParseTest3) {
  auto ht = HttpTemplate::Parse("/**");
  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"**"}), ht->segments());
  ASSERT_EQ("", ht->verb());
  ASSERT_EQ(Variables(), ht->Variables());
}

TEST(HttpTemplate, ParseTest4a) {
  auto ht = HttpTemplate::Parse("/a:foo");
  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"a"}), ht->segments());
  ASSERT_EQ("foo", ht->verb());
  ASSERT_EQ(Variables(), ht->Variables());
}

TEST(HttpTemplate, ParseTest4b) {
  auto ht = HttpTemplate::Parse("/a/b/c:foo");
  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"a", "b", "c"}), ht->segments());
  ASSERT_EQ("foo", ht->verb());
  ASSERT_EQ(Variables(), ht->Variables());
}

TEST(HttpTemplate, ParseTest5) {
  auto ht = HttpTemplate::Parse("/*/**");
  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"*", "**"}), ht->segments());
  ASSERT_EQ("", ht->verb());
  ASSERT_EQ(Variables(), ht->Variables());
}

TEST(HttpTemplate, ParseTest6) {
  auto ht = HttpTemplate::Parse("/*/a/**");
  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"*", "a", "**"}), ht->segments());
  ASSERT_EQ("", ht->verb());
  ASSERT_EQ(Variables(), ht->Variables());
}

TEST(HttpTemplate, ParseTest7) {
  auto ht = HttpTemplate::Parse("/a/{a.b.c}");
  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"a", "*"}), ht->segments());
  ASSERT_EQ("", ht->verb());
  ASSERT_EQ(Variables({
                Variable{1, 2, FieldPath{"a", "b", "c"}, false},
            }),
            ht->Variables());
}

TEST(HttpTemplate, ParseTest8) {
  auto ht = HttpTemplate::Parse("/a/{a.b.c=*}");
  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"a", "*"}), ht->segments());
  ASSERT_EQ("", ht->verb());
  ASSERT_EQ(Variables({
                Variable{1, 2, FieldPath{"a", "b", "c"}, false},
            }),
            ht->Variables());
}

TEST(HttpTemplate, ParseTest9) {
  auto ht = HttpTemplate::Parse("/a/{b=*}");
  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"a", "*"}), ht->segments());
  ASSERT_EQ("", ht->verb());
  ASSERT_EQ(Variables({
                Variable{1, 2, FieldPath{"b"}, false},
            }),
            ht->Variables());
}

TEST(HttpTemplate, ParseTest10) {
  auto ht = HttpTemplate::Parse("/a/{b=**}");
  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"a", "**"}), ht->segments());
  ASSERT_EQ("", ht->verb());
  ASSERT_EQ(Variables({
                Variable{1, -1, FieldPath{"b"}, true},
            }),
            ht->Variables());
}

TEST(HttpTemplate, ParseTest11) {
  auto ht = HttpTemplate::Parse("/a/{b=c/*}");
  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"a", "c", "*"}), ht->segments());
  ASSERT_EQ("", ht->verb());
  ASSERT_EQ(Variables({
                Variable{1, 3, FieldPath{"b"}, false},
            }),
            ht->Variables());
}

TEST(HttpTemplate, ParseTest12) {
  auto ht = HttpTemplate::Parse("/a/{b=c/*/d}");
  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"a", "c", "*", "d"}), ht->segments());
  ASSERT_EQ("", ht->verb());
  ASSERT_EQ(Variables({
                Variable{1, 4, FieldPath{"b"}, false},
            }),
            ht->Variables());
}

TEST(HttpTemplate, ParseTest13) {
  auto ht = HttpTemplate::Parse("/a/{b=c/**}");
  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"a", "c", "**"}), ht->segments());
  ASSERT_EQ("", ht->verb());
  ASSERT_EQ(Variables({
                Variable{1, -1, FieldPath{"b"}, true},
            }),
            ht->Variables());
}

TEST(HttpTemplate, ParseTest14) {
  auto ht = HttpTemplate::Parse("/a/{b=c/**}/d/e");
  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"a", "c", "**", "d", "e"}), ht->segments());
  ASSERT_EQ("", ht->verb());
  ASSERT_EQ(Variables({
                Variable{1, -3, FieldPath{"b"}, true},
            }),
            ht->Variables());
}

TEST(HttpTemplate, ParseTest15) {
  auto ht = HttpTemplate::Parse("/a/{b=c/**/d}/e");
  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"a", "c", "**", "d", "e"}), ht->segments());
  ASSERT_EQ("", ht->verb());
  ASSERT_EQ(Variables({
                Variable{1, -2, FieldPath{"b"}, true},
            }),
            ht->Variables());
}

TEST(HttpTemplate, ParseTest16) {
  auto ht = HttpTemplate::Parse("/a/{b=c/**/d}/e:verb");
  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"a", "c", "**", "d", "e"}), ht->segments());
  ASSERT_EQ("verb", ht->verb());
  ASSERT_EQ(Variables({
                Variable{1, -3, FieldPath{"b"}, true},
            }),
            ht->Variables());
}

TEST(HttpTemplate, CustomVerbTests) {
  auto ht = HttpTemplate::Parse("/*:verb");
  ASSERT_EQ(Segments({"*"}), ht->segments());
  ASSERT_EQ(Variables(), ht->Variables());

  ht = HttpTemplate::Parse("/**:verb");
  ASSERT_EQ(Segments({"**"}), ht->segments());
  ASSERT_EQ(Variables(), ht->Variables());

  ht = HttpTemplate::Parse("/{a}:verb");
  ASSERT_EQ(Segments({"*"}), ht->segments());
  ASSERT_EQ(Variables({
                Variable{0, 1, FieldPath{"a"}, false},
            }),
            ht->Variables());

  ht = HttpTemplate::Parse("/a/b/*:verb");
  ASSERT_EQ(Segments({"a", "b", "*"}), ht->segments());
  ASSERT_EQ(Variables(), ht->Variables());

  ht = HttpTemplate::Parse("/a/b/**:verb");
  ASSERT_EQ(Segments({"a", "b", "**"}), ht->segments());
  ASSERT_EQ(Variables(), ht->Variables());

  ht = HttpTemplate::Parse("/a/b/{a}:verb");
  ASSERT_EQ(Segments({"a", "b", "*"}), ht->segments());
  ASSERT_EQ(Variables({
                Variable{2, 3, FieldPath{"a"}, false},
            }),
            ht->Variables());
}

TEST(HttpTemplate, MoreVariableTests) {
  auto ht = HttpTemplate::Parse("/{x}");

  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"*"}), ht->segments());
  ASSERT_EQ("", ht->verb());
  ASSERT_EQ(Variables({
                Variable{0, 1, FieldPath{"x"}, false},
            }),
            ht->Variables());

  ht = HttpTemplate::Parse("/{x.y.z}");

  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"*"}), ht->segments());
  ASSERT_EQ("", ht->verb());
  ASSERT_EQ(Variables({
                Variable{0, 1, FieldPath{"x", "y", "z"}, false},
            }),
            ht->Variables());

  ht = HttpTemplate::Parse("/{x=*}");

  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"*"}), ht->segments());
  ASSERT_EQ("", ht->verb());
  ASSERT_EQ(Variables({
                Variable{0, 1, FieldPath{"x"}, false},
            }),
            ht->Variables());

  ht = HttpTemplate::Parse("/{x=a/*}");

  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"a", "*"}), ht->segments());
  ASSERT_EQ("", ht->verb());
  ASSERT_EQ(Variables({
                Variable{0, 2, FieldPath{"x"}, false},
            }),
            ht->Variables());

  ht = HttpTemplate::Parse("/{x.y.z=*/a/b}/c");

  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"*", "a", "b", "c"}), ht->segments());
  ASSERT_EQ("", ht->verb());
  ASSERT_EQ(Variables({
                Variable{0, 3, FieldPath{"x", "y", "z"}, false},
            }),
            ht->Variables());

  ht = HttpTemplate::Parse("/{x=**}");

  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"**"}), ht->segments());
  ASSERT_EQ("", ht->verb());
  ASSERT_EQ(Variables({
                Variable{0, -1, FieldPath{"x"}, true},
            }),
            ht->Variables());

  ht = HttpTemplate::Parse("/{x.y.z=**}");

  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"**"}), ht->segments());
  ASSERT_EQ("", ht->verb());
  ASSERT_EQ(Variables({
                Variable{0, -1, FieldPath{"x", "y", "z"}, true},
            }),
            ht->Variables());

  ht = HttpTemplate::Parse("/{x.y.z=a/**/b}");

  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"a", "**", "b"}), ht->segments());
  ASSERT_EQ("", ht->verb());
  ASSERT_EQ(Variables({
                Variable{0, -1, FieldPath{"x", "y", "z"}, true},
            }),
            ht->Variables());

  ht = HttpTemplate::Parse("/{x.y.z=a/**/b}/c/d");

  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"a", "**", "b", "c", "d"}), ht->segments());
  ASSERT_EQ("", ht->verb());
  ASSERT_EQ(Variables({
                Variable{0, -3, FieldPath{"x", "y", "z"}, true},
            }),
            ht->Variables());
}

TEST(HttpTemplate, VariableAndCustomVerbTests) {
  auto ht = HttpTemplate::Parse("/{x}:verb");

  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"*"}), ht->segments());
  ASSERT_EQ("verb", ht->verb());
  ASSERT_EQ(Variables({
                Variable{0, 1, FieldPath{"x"}, false},
            }),
            ht->Variables());

  ht = HttpTemplate::Parse("/{x.y.z}:verb");

  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"*"}), ht->segments());
  ASSERT_EQ("verb", ht->verb());
  ASSERT_EQ(Variables({
                Variable{0, 1, FieldPath{"x", "y", "z"}, false},
            }),
            ht->Variables());

  ht = HttpTemplate::Parse("/{x.y.z=*/*}:verb");

  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"*", "*"}), ht->segments());
  ASSERT_EQ("verb", ht->verb());
  ASSERT_EQ(Variables({
                Variable{0, 2, FieldPath{"x", "y", "z"}, false},
            }),
            ht->Variables());

  ht = HttpTemplate::Parse("/{x=**}:myverb");

  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"**"}), ht->segments());
  ASSERT_EQ("myverb", ht->verb());
  ASSERT_EQ(Variables({
                Variable{0, -2, FieldPath{"x"}, true},
            }),
            ht->Variables());

  ht = HttpTemplate::Parse("/{x.y.z=**}:myverb");

  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"**"}), ht->segments());
  ASSERT_EQ("myverb", ht->verb());
  ASSERT_EQ(Variables({
                Variable{0, -2, FieldPath{"x", "y", "z"}, true},
            }),
            ht->Variables());

  ht = HttpTemplate::Parse("/{x.y.z=a/**/b}:custom");

  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"a", "**", "b"}), ht->segments());
  ASSERT_EQ("custom", ht->verb());
  ASSERT_EQ(Variables({
                Variable{0, -2, FieldPath{"x", "y", "z"}, true},
            }),
            ht->Variables());

  ht = HttpTemplate::Parse("/{x.y.z=a/**/b}/c/d:custom");

  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"a", "**", "b", "c", "d"}), ht->segments());
  ASSERT_EQ("custom", ht->verb());
  ASSERT_EQ(Variables({
                Variable{0, -4, FieldPath{"x", "y", "z"}, true},
            }),
            ht->Variables());
}

TEST(HttpTemplate, ErrorTests) {
  ASSERT_EQ(nullptr, HttpTemplate::Parse(""));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("/"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("//"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("/{}"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("/a/"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("/a//b"));

  ASSERT_EQ(nullptr, HttpTemplate::Parse(":verb"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("/:verb"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("/a/:verb"));

  ASSERT_EQ(nullptr, HttpTemplate::Parse(":"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("/:"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("/*:"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("/**:"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("/{var}:"));

  ASSERT_EQ(nullptr, HttpTemplate::Parse("/a/b/:"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("/a/b/*:"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("/a/b/**:"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("/a/b/{var}:"));

  ASSERT_EQ(nullptr, HttpTemplate::Parse("/a/{"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("/a/{var"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("/a/{var."));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("/a/{x=var:verb}"));

  ASSERT_EQ(nullptr, HttpTemplate::Parse("a"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("{x}"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("{x=/a}"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("{x=/a/b}"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("a/b"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("a/b/{x}"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("a/{x}/b"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("a/{x}/b:verb"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("/a/{var=/b}"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("/{var=a/{nested=b}}"));

  ASSERT_EQ(nullptr, HttpTemplate::Parse("/a{x}"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("/{x}a"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("/a{x}b"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("/{x}a{y}"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("/a/b{x}"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("/a/{x}b"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("/a/b{x}c"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("/a/{x}b{y}"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("/a/b{x}/s"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("/a/{x}b/s"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("/a/b{x}c/s"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("/a/{x}b{y}/s"));
}

TEST(HttpTemplate, ParseVerbTest2) {
  auto ht = HttpTemplate::Parse("/a/*:verb");
  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(ht->segments(), Segments({"a", "*"}));
  ASSERT_EQ("verb", ht->verb());
}

TEST(HttpTemplate, ParseVerbTest3) {
  auto ht = HttpTemplate::Parse("/a/**:verb");
  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(ht->segments(), Segments({"a", "**"}));
  ASSERT_EQ("verb", ht->verb());
}

TEST(HttpTemplate, ParseVerbTest4) {
  auto ht = HttpTemplate::Parse("/a/{b=*}/**:verb");
  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(ht->segments(), Segments({"a", "*", "**"}));
  ASSERT_EQ("verb", ht->verb());
}

TEST(HttpTemplate, ParseNonVerbTest) {
  ASSERT_EQ(nullptr, HttpTemplate::Parse(":"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("/:"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("/a/:"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("/a/*:"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("/a/**:"));
  ASSERT_EQ(nullptr, HttpTemplate::Parse("/a/{b=*}/**:"));
}

}  // namespace api_spec
}  // namespace istio
