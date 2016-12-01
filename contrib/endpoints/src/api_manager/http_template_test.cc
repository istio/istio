// Copyright (C) Extensible Service Proxy Authors
// All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions
// are met:
// 1. Redistributions of source code must retain the above copyright
//    notice, this list of conditions and the following disclaimer.
// 2. Redistributions in binary form must reproduce the above copyright
//    notice, this list of conditions and the following disclaimer in the
//    documentation and/or other materials provided with the distribution.
//
// THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS ``AS IS'' AND
// ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
// IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
// ARE DISCLAIMED.  IN NO EVENT SHALL THE AUTHOR OR CONTRIBUTORS BE LIABLE
// FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
// DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
// OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
// HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
// LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
// OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
// SUCH DAMAGE.
//
////////////////////////////////////////////////////////////////////////////////
//
#include "http_template.h"
#include "gtest/gtest.h"

#include <ostream>
#include <string>
#include <vector>

namespace google {
namespace api_manager {

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
  HttpTemplate *ht = HttpTemplate::Parse("/shelves/{shelf}/books/{book}");
  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"shelves", "*", "books", "*"}), ht->segments());
  ASSERT_EQ(Variables({
                Variable{1, 2, FieldPath{"shelf"}, false},
                Variable{3, 4, FieldPath{"book"}, false},
            }),
            ht->Variables());
}

TEST(HttpTemplate, ParseTest2) {
  HttpTemplate *ht = HttpTemplate::Parse("/shelves/**");
  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"shelves", "**"}), ht->segments());
  ASSERT_EQ("", ht->verb());
  ASSERT_EQ(Variables({}), ht->Variables());
}

TEST(HttpTemplate, ParseTest3) {
  HttpTemplate *ht = HttpTemplate::Parse("/**");
  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"**"}), ht->segments());
  ASSERT_EQ("", ht->verb());
  ASSERT_EQ(Variables(), ht->Variables());
}

TEST(HttpTemplate, ParseTest4a) {
  HttpTemplate *ht = HttpTemplate::Parse("/a:foo");
  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"a"}), ht->segments());
  ASSERT_EQ("foo", ht->verb());
  ASSERT_EQ(Variables(), ht->Variables());
}

TEST(HttpTemplate, ParseTest4b) {
  HttpTemplate *ht = HttpTemplate::Parse("/a/b/c:foo");
  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"a", "b", "c"}), ht->segments());
  ASSERT_EQ("foo", ht->verb());
  ASSERT_EQ(Variables(), ht->Variables());
}

TEST(HttpTemplate, ParseTest5) {
  HttpTemplate *ht = HttpTemplate::Parse("/*/**");
  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"*", "**"}), ht->segments());
  ASSERT_EQ("", ht->verb());
  ASSERT_EQ(Variables(), ht->Variables());
}

TEST(HttpTemplate, ParseTest6) {
  HttpTemplate *ht = HttpTemplate::Parse("/*/a/**");
  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"*", "a", "**"}), ht->segments());
  ASSERT_EQ("", ht->verb());
  ASSERT_EQ(Variables(), ht->Variables());
}

TEST(HttpTemplate, ParseTest7) {
  HttpTemplate *ht = HttpTemplate::Parse("/a/{a.b.c}");
  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"a", "*"}), ht->segments());
  ASSERT_EQ("", ht->verb());
  ASSERT_EQ(Variables({
                Variable{1, 2, FieldPath{"a", "b", "c"}, false},
            }),
            ht->Variables());
}

TEST(HttpTemplate, ParseTest8) {
  HttpTemplate *ht = HttpTemplate::Parse("/a/{a.b.c=*}");
  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"a", "*"}), ht->segments());
  ASSERT_EQ("", ht->verb());
  ASSERT_EQ(Variables({
                Variable{1, 2, FieldPath{"a", "b", "c"}, false},
            }),
            ht->Variables());
}

TEST(HttpTemplate, ParseTest9) {
  HttpTemplate *ht = HttpTemplate::Parse("/a/{b=*}");
  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"a", "*"}), ht->segments());
  ASSERT_EQ("", ht->verb());
  ASSERT_EQ(Variables({
                Variable{1, 2, FieldPath{"b"}, false},
            }),
            ht->Variables());
}

TEST(HttpTemplate, ParseTest10) {
  HttpTemplate *ht = HttpTemplate::Parse("/a/{b=**}");
  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"a", "**"}), ht->segments());
  ASSERT_EQ("", ht->verb());
  ASSERT_EQ(Variables({
                Variable{1, -1, FieldPath{"b"}, true},
            }),
            ht->Variables());
}

TEST(HttpTemplate, ParseTest11) {
  HttpTemplate *ht = HttpTemplate::Parse("/a/{b=c/*}");
  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"a", "c", "*"}), ht->segments());
  ASSERT_EQ("", ht->verb());
  ASSERT_EQ(Variables({
                Variable{1, 3, FieldPath{"b"}, false},
            }),
            ht->Variables());
}

TEST(HttpTemplate, ParseTest12) {
  HttpTemplate *ht = HttpTemplate::Parse("/a/{b=c/*/d}");
  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"a", "c", "*", "d"}), ht->segments());
  ASSERT_EQ("", ht->verb());
  ASSERT_EQ(Variables({
                Variable{1, 4, FieldPath{"b"}, false},
            }),
            ht->Variables());
}

TEST(HttpTemplate, ParseTest13) {
  HttpTemplate *ht = HttpTemplate::Parse("/a/{b=c/**}");
  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"a", "c", "**"}), ht->segments());
  ASSERT_EQ("", ht->verb());
  ASSERT_EQ(Variables({
                Variable{1, -1, FieldPath{"b"}, true},
            }),
            ht->Variables());
}

TEST(HttpTemplate, ParseTest14) {
  HttpTemplate *ht = HttpTemplate::Parse("/a/{b=c/**}/d/e");
  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"a", "c", "**", "d", "e"}), ht->segments());
  ASSERT_EQ("", ht->verb());
  ASSERT_EQ(Variables({
                Variable{1, -3, FieldPath{"b"}, true},
            }),
            ht->Variables());
}

TEST(HttpTemplate, ParseTest15) {
  HttpTemplate *ht = HttpTemplate::Parse("/a/{b=c/**/d}/e");
  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"a", "c", "**", "d", "e"}), ht->segments());
  ASSERT_EQ("", ht->verb());
  ASSERT_EQ(Variables({
                Variable{1, -2, FieldPath{"b"}, true},
            }),
            ht->Variables());
}

TEST(HttpTemplate, ParseTest16) {
  HttpTemplate *ht = HttpTemplate::Parse("/a/{b=c/**/d}/e:verb");
  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(Segments({"a", "c", "**", "d", "e"}), ht->segments());
  ASSERT_EQ("verb", ht->verb());
  ASSERT_EQ(Variables({
                Variable{1, -3, FieldPath{"b"}, true},
            }),
            ht->Variables());
}

TEST(HttpTemplate, CustomVerbTests) {
  HttpTemplate *ht;

  ht = HttpTemplate::Parse("/*:verb");
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
  HttpTemplate *ht = HttpTemplate::Parse("/{x}");

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
  HttpTemplate *ht = HttpTemplate::Parse("/{x}:verb");

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
  HttpTemplate *ht = HttpTemplate::Parse("/a/*:verb");
  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(ht->segments(), Segments({"a", "*"}));
  ASSERT_EQ("verb", ht->verb());
}

TEST(HttpTemplate, ParseVerbTest3) {
  HttpTemplate *ht = HttpTemplate::Parse("/a/**:verb");
  ASSERT_NE(nullptr, ht);
  ASSERT_EQ(ht->segments(), Segments({"a", "**"}));
  ASSERT_EQ("verb", ht->verb());
}

TEST(HttpTemplate, ParseVerbTest4) {
  HttpTemplate *ht = HttpTemplate::Parse("/a/{b=*}/**:verb");
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

}  // namespace api_manager
}  // namespace google
