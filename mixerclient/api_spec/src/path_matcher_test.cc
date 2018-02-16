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

#include "path_matcher.h"

#include <memory>
#include <ostream>
#include <string>
#include <vector>

#include "gmock/gmock.h"
#include "gtest/gtest.h"

using ::testing::ReturnRef;

namespace istio {
namespace api_spec {

namespace {

// VariableBinding specifies a value for a single field in the request message.
// When transcoding HTTP/REST/JSON to gRPC/proto the request message is
// constructed using the HTTP body and the variable bindings (specified through
// request url).
struct Binding {
  // The location of the field in the protobuf message, where the value
  // needs to be inserted, e.g. "shelf.theme" would mean the "theme" field
  // of the nested "shelf" message of the request protobuf message.
  std::vector<std::string> field_path;
  // The value to be inserted.
  std::string value;
};

typedef std::vector<Binding> Bindings;
typedef std::vector<std::string> FieldPath;
class MethodInfo {
 public:
  MOCK_CONST_METHOD0(system_query_parameter_names,
                     const std::set<std::string>&());
};

bool operator==(const Binding& b1, const Binding& b2) {
  return b1.field_path == b2.field_path && b1.value == b2.value;
}

std::string FieldPathToString(const FieldPath& fp) {
  std::string s;
  for (const auto& f : fp) {
    if (!s.empty()) {
      s += ".";
    }
    s += f;
  }
  return s;
}

}  // namespace

std::ostream& operator<<(std::ostream& os, const Binding& b) {
  return os << "{ " << FieldPathToString(b.field_path) << "=" << b.value << "}";
}

std::ostream& operator<<(std::ostream& os, const Bindings& bindings) {
  for (const auto& b : bindings) {
    os << b << std::endl;
  }
  return os;
}

namespace {

class PathMatcherTest : public ::testing::Test {
 protected:
  PathMatcherTest() {}
  ~PathMatcherTest() {}

  MethodInfo* AddPathWithBodyFieldPath(std::string http_method,
                                       std::string http_template,
                                       std::string body_field_path) {
    auto method = new MethodInfo();
    ON_CALL(*method, system_query_parameter_names())
        .WillByDefault(ReturnRef(empty_set_));
    if (!builder_.Register(http_method, http_template, body_field_path,
                           method)) {
      delete method;
      return nullptr;
    }
    stored_methods_.emplace_back(method);
    return method;
  }

  MethodInfo* AddPathWithSystemParams(
      std::string http_method, std::string http_template,
      const std::set<std::string>* system_params) {
    auto method = new MethodInfo();
    ON_CALL(*method, system_query_parameter_names())
        .WillByDefault(ReturnRef(*system_params));
    if (!builder_.Register(http_method, http_template, std::string(), method)) {
      delete method;
      return nullptr;
    }
    stored_methods_.emplace_back(method);
    return method;
  }

  MethodInfo* AddPath(std::string http_method, std::string http_template) {
    return AddPathWithBodyFieldPath(http_method, http_template, std::string());
  }

  MethodInfo* AddGetPath(std::string path) { return AddPath("GET", path); }

  void Build() { matcher_ = builder_.Build(); }

  MethodInfo* LookupWithBodyFieldPath(std::string method, std::string path,
                                      Bindings* bindings,
                                      std::string* body_field_path) {
    return matcher_->Lookup(method, path, "", bindings, body_field_path);
  }

  MethodInfo* Lookup(std::string method, std::string path, Bindings* bindings) {
    std::string body_field_path;
    return matcher_->Lookup(method, path, std::string(), bindings,
                            &body_field_path);
  }

  MethodInfo* LookupWithParams(std::string method, std::string path,
                               std::string query_params, Bindings* bindings) {
    std::string body_field_path;
    return matcher_->Lookup(method, path, query_params, bindings,
                            &body_field_path);
  }

  MethodInfo* LookupNoBindings(std::string method, std::string path) {
    Bindings bindings;
    std::string body_field_path;
    auto result = matcher_->Lookup(method, path, std::string(), &bindings,
                                   &body_field_path);
    EXPECT_EQ(0, bindings.size());
    return result;
  }

 private:
  PathMatcherBuilder<MethodInfo*> builder_;
  PathMatcherPtr<MethodInfo*> matcher_;
  std::vector<std::unique_ptr<MethodInfo>> stored_methods_;
  std::set<std::string> empty_set_;
};

TEST_F(PathMatcherTest, WildCardMatchesRoot) {
  MethodInfo* data = AddGetPath("/**");
  Build();

  EXPECT_NE(nullptr, data);

  EXPECT_EQ(LookupNoBindings("GET", "/"), data);
  EXPECT_EQ(LookupNoBindings("GET", "/a"), data);
  EXPECT_EQ(LookupNoBindings("GET", "/a/"), data);
}

TEST_F(PathMatcherTest, WildCardMatches) {
  // '*' only matches one path segment, but '**' matches the remaining path.
  MethodInfo* a__ = AddGetPath("/a/**");
  MethodInfo* b_ = AddGetPath("/b/*");
  MethodInfo* c_d__ = AddGetPath("/c/*/d/**");
  MethodInfo* c_de = AddGetPath("/c/*/d/e");
  MethodInfo* cfde = AddGetPath("/c/f/d/e");
  Build();

  EXPECT_NE(nullptr, a__);
  EXPECT_NE(nullptr, b_);
  EXPECT_NE(nullptr, c_d__);
  EXPECT_NE(nullptr, c_de);
  EXPECT_NE(nullptr, cfde);

  EXPECT_EQ(LookupNoBindings("GET", "/a/b"), a__);
  EXPECT_EQ(LookupNoBindings("GET", "/a/b/c"), a__);
  EXPECT_EQ(LookupNoBindings("GET", "/b/c"), b_);

  EXPECT_EQ(LookupNoBindings("GET", "b/c/d"), nullptr);
  EXPECT_EQ(LookupNoBindings("GET", "/c/u/d/v"), c_d__);
  EXPECT_EQ(LookupNoBindings("GET", "/c/v/d/w/x"), c_d__);
  EXPECT_EQ(LookupNoBindings("GET", "/c/x/y/d/z"), nullptr);
  EXPECT_EQ(LookupNoBindings("GET", "/c//v/d/w/x"), nullptr);

  // Test that more specific match overrides wildcard "**"" match.
  EXPECT_EQ(LookupNoBindings("GET", "/c/x/d/e"), c_de);
  // Test that more specific match overrides wildcard "*"" match.
  EXPECT_EQ(LookupNoBindings("GET", "/c/f/d/e"), cfde);
}

TEST_F(PathMatcherTest, WildCardMethodMatches) {
  MethodInfo* a__ = AddPath("*", "/a/**");
  MethodInfo* b_ = AddPath("*", "/b/*");
  Build();

  EXPECT_NE(nullptr, a__);
  EXPECT_NE(nullptr, b_);

  std::vector<std::string> all_methods{"GET", "POST", "DELETE", "PATCH", "PUT"};
  for (const auto& method : all_methods) {
    EXPECT_EQ(LookupNoBindings(method, "/a/b"), a__);
    EXPECT_EQ(LookupNoBindings(method, "/a/b/c"), a__);
    EXPECT_EQ(LookupNoBindings(method, "/b/c"), b_);
  }
}

TEST_F(PathMatcherTest, VariableBindings) {
  MethodInfo* a_cde = AddGetPath("/a/{x}/c/d/e");
  MethodInfo* a_b_c = AddGetPath("/{x=a/*}/b/{y=*}/c");
  MethodInfo* ab_d__ = AddGetPath("/a/{x=b/*}/{y=d/**}");
  MethodInfo* alpha_beta__gamma = AddGetPath("/alpha/{x=*}/beta/{y=**}/gamma");
  MethodInfo* _a = AddGetPath("/{x=*}/a");
  MethodInfo* __ab = AddGetPath("/{x=**}/a/b");
  MethodInfo* ab_ = AddGetPath("/a/b/{x=*}");
  MethodInfo* abc__ = AddGetPath("/a/b/c/{x=**}");
  MethodInfo* _def__ = AddGetPath("/{x=*}/d/e/f/{y=**}");
  Build();

  EXPECT_NE(nullptr, a_cde);
  EXPECT_NE(nullptr, a_b_c);
  EXPECT_NE(nullptr, ab_d__);
  EXPECT_NE(nullptr, alpha_beta__gamma);
  EXPECT_NE(nullptr, _a);
  EXPECT_NE(nullptr, __ab);
  EXPECT_NE(nullptr, ab_);
  EXPECT_NE(nullptr, abc__);
  EXPECT_NE(nullptr, _def__);

  Bindings bindings;
  EXPECT_EQ(Lookup("GET", "/a/book/c/d/e", &bindings), a_cde);
  EXPECT_EQ(Bindings({
                Binding{FieldPath{"x"}, "book"},
            }),
            bindings);

  EXPECT_EQ(Lookup("GET", "/a/hello/b/world/c", &bindings), a_b_c);
  EXPECT_EQ(
      Bindings({
          Binding{FieldPath{"x"}, "a/hello"}, Binding{FieldPath{"y"}, "world"},
      }),
      bindings);

  EXPECT_EQ(Lookup("GET", "/a/b/zoo/d/animal/tiger", &bindings), ab_d__);
  EXPECT_EQ(Bindings({
                Binding{FieldPath{"x"}, "b/zoo"},
                Binding{FieldPath{"y"}, "d/animal/tiger"},
            }),
            bindings);

  EXPECT_EQ(Lookup("GET", "/alpha/dog/beta/eat/bones/gamma", &bindings),
            alpha_beta__gamma);
  EXPECT_EQ(
      Bindings({
          Binding{FieldPath{"x"}, "dog"}, Binding{FieldPath{"y"}, "eat/bones"},
      }),
      bindings);

  EXPECT_EQ(Lookup("GET", "/foo/a", &bindings), _a);
  EXPECT_EQ(Bindings({
                Binding{FieldPath{"x"}, "foo"},
            }),
            bindings);

  EXPECT_EQ(Lookup("GET", "/foo/bar/a/b", &bindings), __ab);
  EXPECT_EQ(Bindings({
                Binding{FieldPath{"x"}, "foo/bar"},
            }),
            bindings);

  EXPECT_EQ(Lookup("GET", "/a/b/foo", &bindings), ab_);
  EXPECT_EQ(Bindings({
                Binding{FieldPath{"x"}, "foo"},
            }),
            bindings);

  EXPECT_EQ(Lookup("GET", "/a/b/c/foo/bar/baz", &bindings), abc__);
  EXPECT_EQ(Bindings({
                Binding{FieldPath{"x"}, "foo/bar/baz"},
            }),
            bindings);

  EXPECT_EQ(Lookup("GET", "/foo/d/e/f/bar/baz", &bindings), _def__);
  EXPECT_EQ(
      Bindings({
          Binding{FieldPath{"x"}, "foo"}, Binding{FieldPath{"y"}, "bar/baz"},
      }),
      bindings);
}

TEST_F(PathMatcherTest, PercentEscapesUnescapedForSingleSegment) {
  MethodInfo* a_c = AddGetPath("/a/{x}/c");
  Build();

  EXPECT_NE(nullptr, a_c);

  Bindings bindings;
  EXPECT_EQ(Lookup("GET", "/a/p%20q%2Fr/c", &bindings), a_c);
  EXPECT_EQ(Bindings({
                Binding{FieldPath{"x"}, "p q/r"},
            }),
            bindings);
}

namespace {

char HexDigit(unsigned char digit, bool uppercase) {
  if (digit < 10) {
    return '0' + digit;
  } else if (uppercase) {
    return 'A' + digit - 10;
  } else {
    return 'a' + digit - 10;
  }
}

}  // namespace {

TEST_F(PathMatcherTest, PercentEscapesUnescapedForSingleSegmentAllAsciiChars) {
  MethodInfo* a_c = AddGetPath("/{x}");
  Build();

  EXPECT_NE(nullptr, a_c);

  for (int u = 0; u < 2; ++u) {
    for (char c = 0; c < 0x7f; ++c) {
      std::string path("/%");
      path += HexDigit((c & 0xf0) >> 4, 0 != u);
      path += HexDigit(c & 0x0f, 0 != u);

      Bindings bindings;
      EXPECT_EQ(Lookup("GET", path, &bindings), a_c);
      EXPECT_EQ(Bindings({
                    Binding{FieldPath{"x"}, std::string(1, (char)c)},
                }),
                bindings);
    }
  }
}

TEST_F(PathMatcherTest, PercentEscapesNotUnescapedForMultiSegment1) {
  MethodInfo* ap_q_c = AddGetPath("/a/{x=p/*/q/*}/c");
  Build();

  EXPECT_NE(nullptr, ap_q_c);

  Bindings bindings;
  EXPECT_EQ(Lookup("GET", "/a/p/foo%20foo/q/bar%2Fbar/c", &bindings), ap_q_c);
  // space (%20) is escaped, but slash (%2F) isn't.
  EXPECT_EQ(Bindings({Binding{FieldPath{"x"}, "p/foo foo/q/bar%2Fbar"}}),
            bindings);
}

TEST_F(PathMatcherTest, PercentEscapesNotUnescapedForMultiSegment2) {
  MethodInfo* a__c = AddGetPath("/a/{x=**}/c");
  Build();

  EXPECT_NE(nullptr, a__c);

  Bindings bindings;
  EXPECT_EQ(Lookup("GET", "/a/p/foo%20foo/q/bar%2Fbar/c", &bindings), a__c);
  // space (%20) is escaped, but slash (%2F) isn't.
  EXPECT_EQ(Bindings({Binding{FieldPath{"x"}, "p/foo foo/q/bar%2Fbar"}}),
            bindings);
}

TEST_F(PathMatcherTest, OnlyUnreservedCharsAreUnescapedForMultiSegmentMatch) {
  MethodInfo* a__c = AddGetPath("/a/{x=**}/c");
  Build();

  EXPECT_NE(nullptr, a__c);

  Bindings bindings;
  EXPECT_EQ(
      Lookup("GET",
             "/a/%21%23%24%26%27%28%29%2A%2B%2C%2F%3A%3B%3D%3F%40%5B%5D/c",
             &bindings),
      a__c);

  // All %XX are reserved characters, they should be intact.
  EXPECT_EQ(Bindings({Binding{
                FieldPath{"x"},
                "%21%23%24%26%27%28%29%2A%2B%2C%2F%3A%3B%3D%3F%40%5B%5D"}}),
            bindings);
}

TEST_F(PathMatcherTest, VariableBindingsWithCustomVerb) {
  MethodInfo* a_verb = AddGetPath("/a/{y=*}:verb");
  MethodInfo* ad__verb = AddGetPath("/a/{y=d/**}:verb");
  MethodInfo* _averb = AddGetPath("/{x=*}/a:verb");
  MethodInfo* __bverb = AddGetPath("/{x=**}/b:verb");
  MethodInfo* e_fverb = AddGetPath("/e/{x=*}/f:verb");
  MethodInfo* g__hverb = AddGetPath("/g/{x=**}/h:verb");
  Build();

  EXPECT_NE(nullptr, a_verb);
  EXPECT_NE(nullptr, ad__verb);
  EXPECT_NE(nullptr, _averb);
  EXPECT_NE(nullptr, __bverb);
  EXPECT_NE(nullptr, e_fverb);
  EXPECT_NE(nullptr, g__hverb);

  Bindings bindings;
  EXPECT_EQ(Lookup("GET", "/a/world:verb", &bindings), a_verb);
  EXPECT_EQ(Bindings({Binding{FieldPath{"y"}, "world"}}), bindings);

  EXPECT_EQ(Lookup("GET", "/a/d/animal/tiger:verb", &bindings), ad__verb);
  EXPECT_EQ(Bindings({Binding{FieldPath{"y"}, "d/animal/tiger"}}), bindings);

  EXPECT_EQ(Lookup("GET", "/foo/a:verb", &bindings), _averb);
  EXPECT_EQ(Bindings({Binding{FieldPath{"x"}, "foo"}}), bindings);

  EXPECT_EQ(Lookup("GET", "/foo/bar/baz/b:verb", &bindings), __bverb);
  EXPECT_EQ(Bindings({Binding{FieldPath{"x"}, "foo/bar/baz"}}), bindings);

  EXPECT_EQ(Lookup("GET", "/e/foo/f:verb", &bindings), e_fverb);
  EXPECT_EQ(Bindings({Binding{FieldPath{"x"}, "foo"}}), bindings);

  EXPECT_EQ(Lookup("GET", "/g/foo/bar/h:verb", &bindings), g__hverb);
  EXPECT_EQ(Bindings({Binding{FieldPath{"x"}, "foo/bar"}}), bindings);
}

TEST_F(PathMatcherTest, ConstantSuffixesWithVariable) {
  MethodInfo* ab__ = AddGetPath("/a/{x=b/**}");
  MethodInfo* ab__z = AddGetPath("/a/{x=b/**}/z");
  MethodInfo* ab__yz = AddGetPath("/a/{x=b/**}/y/z");
  MethodInfo* ab__verb = AddGetPath("/a/{x=b/**}:verb");
  MethodInfo* a__ = AddGetPath("/a/{x=**}");
  MethodInfo* c_d__e = AddGetPath("/c/{x=*}/{y=d/**}/e");
  MethodInfo* c_d__everb = AddGetPath("/c/{x=*}/{y=d/**}/e:verb");
  MethodInfo* f___g = AddGetPath("/f/{x=*}/{y=**}/g");
  MethodInfo* f___gverb = AddGetPath("/f/{x=*}/{y=**}/g:verb");
  MethodInfo* ab_yz__foo = AddGetPath("/a/{x=b/*/y/z/**}/foo");
  MethodInfo* ab___yzfoo = AddGetPath("/a/{x=b/*/**/y/z}/foo");
  Build();

  EXPECT_NE(nullptr, ab__);
  EXPECT_NE(nullptr, ab__z);
  EXPECT_NE(nullptr, ab__yz);
  EXPECT_NE(nullptr, ab__verb);
  EXPECT_NE(nullptr, c_d__e);
  EXPECT_NE(nullptr, c_d__everb);
  EXPECT_NE(nullptr, f___g);
  EXPECT_NE(nullptr, f___gverb);
  EXPECT_NE(nullptr, ab_yz__foo);
  EXPECT_NE(nullptr, ab___yzfoo);

  Bindings bindings;

  EXPECT_EQ(Lookup("GET", "/a/b/hello/world/c", &bindings), ab__);
  EXPECT_EQ(Bindings({Binding{FieldPath{"x"}, "b/hello/world/c"}}), bindings);

  EXPECT_EQ(Lookup("GET", "/a/b/world/c/z", &bindings), ab__z);
  EXPECT_EQ(Bindings({Binding{FieldPath{"x"}, "b/world/c"}}), bindings);

  EXPECT_EQ(Lookup("GET", "/a/b/world/c/y/z", &bindings), ab__yz);
  EXPECT_EQ(Bindings({Binding{FieldPath{"x"}, "b/world/c"}}), bindings);

  EXPECT_EQ(Lookup("GET", "/a/b/world/c:verb", &bindings), ab__verb);
  EXPECT_EQ(Bindings({Binding{FieldPath{"x"}, "b/world/c"}}), bindings);

  EXPECT_EQ(Lookup("GET", "/a/hello/b/world/c", &bindings), a__);
  EXPECT_EQ(Bindings({Binding{FieldPath{"x"}, "hello/b/world/c"}}), bindings);

  EXPECT_EQ(Lookup("GET", "/c/hello/d/esp/world/e", &bindings), c_d__e);
  EXPECT_EQ(Bindings({Binding{FieldPath{"x"}, "hello"},
                      Binding{FieldPath{"y"}, "d/esp/world"}}),
            bindings);

  EXPECT_EQ(Lookup("GET", "/c/hola/d/esp/mundo/e:verb", &bindings), c_d__everb);
  EXPECT_EQ(Bindings({Binding{FieldPath{"x"}, "hola"},
                      Binding{FieldPath{"y"}, "d/esp/mundo"}}),
            bindings);

  EXPECT_EQ(Lookup("GET", "/f/foo/bar/baz/g", &bindings), f___g);
  EXPECT_EQ(Bindings({Binding{FieldPath{"x"}, "foo"},
                      Binding{FieldPath{"y"}, "bar/baz"}}),
            bindings);

  EXPECT_EQ(Lookup("GET", "/f/foo/bar/baz/g:verb", &bindings), f___gverb);
  EXPECT_EQ(Bindings({Binding{FieldPath{"x"}, "foo"},
                      Binding{FieldPath{"y"}, "bar/baz"}}),
            bindings);

  EXPECT_EQ(Lookup("GET", "/a/b/foo/y/z/bar/baz/foo", &bindings), ab_yz__foo);
  EXPECT_EQ(Bindings({
                Binding{FieldPath{"x"}, "b/foo/y/z/bar/baz"},
            }),
            bindings);

  EXPECT_EQ(Lookup("GET", "/a/b/foo/bar/baz/y/z/foo", &bindings), ab___yzfoo);
  EXPECT_EQ(Bindings({
                Binding{FieldPath{"x"}, "b/foo/bar/baz/y/z"},
            }),
            bindings);
}

TEST_F(PathMatcherTest, InvalidTemplates) {
  EXPECT_EQ(nullptr, AddGetPath("/a{x=b/**}/{y=*}"));
  EXPECT_EQ(nullptr, AddGetPath("/a{x=b/**}/bb/{y=*}"));
  EXPECT_EQ(nullptr, AddGetPath("/a{x=b/**}/{y=**}"));
  EXPECT_EQ(nullptr, AddGetPath("/a{x=b/**}/bb/{y=**}"));

  EXPECT_EQ(nullptr, AddGetPath("/a/**/*"));
  EXPECT_EQ(nullptr, AddGetPath("/a/**/foo/*"));
  EXPECT_EQ(nullptr, AddGetPath("/a/**/**"));
  EXPECT_EQ(nullptr, AddGetPath("/a/**/foo/**"));
}

TEST_F(PathMatcherTest, CustomVerbMatches) {
  MethodInfo* some_const_verb = AddGetPath("/some/const:verb");
  MethodInfo* some__verb = AddGetPath("/some/*:verb");
  MethodInfo* some__foo_verb = AddGetPath("/some/*/foo:verb");
  MethodInfo* other__verb = AddGetPath("/other/**:verb");
  MethodInfo* other__const_verb = AddGetPath("/other/**/const:verb");
  Build();

  EXPECT_NE(nullptr, some_const_verb);
  EXPECT_NE(nullptr, some__verb);
  EXPECT_NE(nullptr, some__foo_verb);
  EXPECT_NE(nullptr, other__verb);
  EXPECT_NE(nullptr, other__const_verb);

  EXPECT_EQ(LookupNoBindings("GET", "/some/const:verb"), some_const_verb);
  EXPECT_EQ(LookupNoBindings("GET", "/some/other:verb"), some__verb);
  EXPECT_EQ(LookupNoBindings("GET", "/some/other:verb/"), nullptr);
  EXPECT_EQ(LookupNoBindings("GET", "/some/bar/foo:verb"), some__foo_verb);
  EXPECT_EQ(LookupNoBindings("GET", "/some/foo1/foo2/foo:verb"), nullptr);
  EXPECT_EQ(LookupNoBindings("GET", "/some/foo/bar:verb"), nullptr);
  EXPECT_EQ(LookupNoBindings("GET", "/other/bar/foo:verb"), other__verb);
  EXPECT_EQ(LookupNoBindings("GET", "/other/bar/foo/const:verb"),
            other__const_verb);
}

TEST_F(PathMatcherTest, CustomVerbMatch2) {
  MethodInfo* verb = AddGetPath("/*/*:verb");
  Build();
  EXPECT_EQ(LookupNoBindings("GET", "/some:verb/const:verb"), verb);
}

TEST_F(PathMatcherTest, CustomVerbMatch3) {
  MethodInfo* verb = AddGetPath("/foo/*");
  Build();

  // This is not custom verb since it was not configured.
  EXPECT_EQ(LookupNoBindings("GET", "/foo/other:verb"), verb);
}

TEST_F(PathMatcherTest, CustomVerbMatch4) {
  MethodInfo* a = AddGetPath("/foo/*/hello");
  Build();

  EXPECT_NE(nullptr, a);

  // last slash is before last colon.
  EXPECT_EQ(LookupNoBindings("GET", "/foo/other:verb/hello"), a);
}

TEST_F(PathMatcherTest, RejectPartialMatches) {
  MethodInfo* prefix_middle_suffix = AddGetPath("/prefix/middle/suffix");
  MethodInfo* prefix_middle = AddGetPath("/prefix/middle");
  MethodInfo* prefix = AddGetPath("/prefix");
  Build();

  EXPECT_NE(nullptr, prefix_middle_suffix);
  EXPECT_NE(nullptr, prefix_middle);
  EXPECT_NE(nullptr, prefix);

  EXPECT_EQ(LookupNoBindings("GET", "/prefix/middle/suffix"),
            prefix_middle_suffix);
  EXPECT_EQ(LookupNoBindings("GET", "/prefix/middle"), prefix_middle);
  EXPECT_EQ(LookupNoBindings("GET", "/prefix"), prefix);

  EXPECT_EQ(LookupNoBindings("GET", "/prefix/middle/suffix/other"), nullptr);
  EXPECT_EQ(LookupNoBindings("GET", "/prefix/middle/other"), nullptr);
  EXPECT_EQ(LookupNoBindings("GET", "/prefix/other"), nullptr);
  EXPECT_EQ(LookupNoBindings("GET", "/other"), nullptr);
}

TEST_F(PathMatcherTest, LookupReturnsNullIfMatcherEmpty) {
  Build();
  EXPECT_EQ(LookupNoBindings("GET", "a/b/blue/foo"), nullptr);
}

TEST_F(PathMatcherTest, LookupSimplePaths) {
  MethodInfo* pms = AddGetPath("/prefix/middle/suffix");
  MethodInfo* pmo = AddGetPath("/prefix/middle/othersuffix");
  MethodInfo* pos = AddGetPath("/prefix/othermiddle/suffix");
  MethodInfo* oms = AddGetPath("/otherprefix/middle/suffix");
  MethodInfo* os = AddGetPath("/otherprefix/suffix");
  Build();

  EXPECT_NE(nullptr, pms);
  EXPECT_NE(nullptr, pmo);
  EXPECT_NE(nullptr, pos);
  EXPECT_NE(nullptr, oms);
  EXPECT_NE(nullptr, os);

  EXPECT_EQ(LookupNoBindings("GET", "/prefix/not/a/path"), nullptr);
  EXPECT_EQ(LookupNoBindings("GET", "/prefix/middle"), nullptr);
  EXPECT_EQ(LookupNoBindings("GET", "/prefix/not/othermiddle"), nullptr);
  EXPECT_EQ(LookupNoBindings("GET", "/otherprefix/suffix/othermiddle"),
            nullptr);

  EXPECT_EQ(LookupNoBindings("GET", "/prefix/middle/suffix"), pms);
  EXPECT_EQ(LookupNoBindings("GET", "/prefix/middle/othersuffix"), pmo);
  EXPECT_EQ(LookupNoBindings("GET", "/prefix/othermiddle/suffix"), pos);
  EXPECT_EQ(LookupNoBindings("GET", "/otherprefix/middle/suffix"), oms);
  EXPECT_EQ(LookupNoBindings("GET", "/otherprefix/suffix"), os);
  EXPECT_EQ(LookupNoBindings("GET", "/otherprefix/suffix?foo=bar"), os);
}

TEST_F(PathMatcherTest, ReplacevoidForPath) {
  const std::string path = "/foo/bar";
  auto first_mock_proc = AddGetPath(path);
  EXPECT_NE(nullptr, first_mock_proc);
  // Second call should fail
  EXPECT_EQ(nullptr, AddGetPath(path));
  Build();

  // Lookup result should get the first one.
  EXPECT_EQ(LookupNoBindings("GET", path), first_mock_proc);
}

TEST_F(PathMatcherTest, AllowDuplicate) {
  MethodInfo* id = AddGetPath("/a/{id}");
  EXPECT_NE(nullptr, id);
  // Second call should fail
  EXPECT_EQ(nullptr, AddGetPath("/a/{name}"));
  Build();

  Bindings bindings;
  // Lookup result should get the first one.
  EXPECT_EQ(Lookup("GET", "/a/x", &bindings), id);
}

TEST_F(PathMatcherTest, DuplicatedOptions) {
  MethodInfo* get_id = AddPath("GET", "/a/{id}");
  MethodInfo* post_name = AddPath("POST", "/a/{name}");
  MethodInfo* options_id = AddPath("OPTIONS", "/a/{id}");
  EXPECT_EQ(nullptr, AddPath("OPTIONS", "/a/{name}"));
  Build();

  Bindings bindings;
  // Lookup result should get the first one.
  EXPECT_EQ(Lookup("OPTIONS", "/a/x", &bindings), options_id);

  EXPECT_EQ(Lookup("GET", "/a/x", &bindings), get_id);
  EXPECT_EQ(Lookup("POST", "/a/x", &bindings), post_name);
}

// If a path matches a complete branch of trie, but is longer than the branch
// (ie. the trie cannot match all the way to the end of the path), Lookup
// should return nullptr.
TEST_F(PathMatcherTest, LookupReturnsNullForOverspecifiedPath) {
  EXPECT_NE(nullptr, AddGetPath("/a/b/c"));
  EXPECT_NE(nullptr, AddGetPath("/a/b"));
  Build();
  EXPECT_EQ(LookupNoBindings("GET", "/a/b/c/d"), nullptr);
}

TEST_F(PathMatcherTest, ReturnNullvoidSharedPtrForUnderspecifiedPath) {
  EXPECT_NE(nullptr, AddGetPath("/a/b/c/d"));
  Build();
  EXPECT_EQ(LookupNoBindings("GET", "/a/b/c"), nullptr);
}

TEST_F(PathMatcherTest, DifferentHttpMethod) {
  auto ab = AddGetPath("/a/b");
  Build();
  EXPECT_NE(nullptr, ab);
  EXPECT_EQ(LookupNoBindings("GET", "/a/b"), ab);
  EXPECT_EQ(LookupNoBindings("POST", "/a/b"), nullptr);
}

TEST_F(PathMatcherTest, BodyFieldPathTest) {
  auto a = AddPathWithBodyFieldPath("GET", "/a", "b");
  auto cd = AddPathWithBodyFieldPath("GET", "/c/d", "e.f.g");
  Build();
  EXPECT_NE(nullptr, a);
  EXPECT_NE(nullptr, cd);
  std::string body_field_path;
  EXPECT_EQ(LookupWithBodyFieldPath("GET", "/a", nullptr, &body_field_path), a);
  EXPECT_EQ("b", body_field_path);
  EXPECT_EQ(LookupWithBodyFieldPath("GET", "/c/d", nullptr, &body_field_path),
            cd);
  EXPECT_EQ("e.f.g", body_field_path);
}

TEST_F(PathMatcherTest, VariableBindingsWithQueryParams) {
  MethodInfo* a = AddGetPath("/a");
  MethodInfo* a_b = AddGetPath("/a/{x}/b");
  MethodInfo* a_b_c = AddGetPath("/a/{x}/b/{y}/c");
  Build();

  EXPECT_NE(nullptr, a);
  EXPECT_NE(nullptr, a_b);
  EXPECT_NE(nullptr, a_b_c);

  Bindings bindings;
  EXPECT_EQ(LookupWithParams("GET", "/a", "x=hello", &bindings), a);
  EXPECT_EQ(Bindings({
                Binding{FieldPath{"x"}, "hello"},
            }),
            bindings);

  EXPECT_EQ(LookupWithParams("GET", "/a/book/b", "y=shelf&z=author", &bindings),
            a_b);
  EXPECT_EQ(
      Bindings({
          Binding{FieldPath{"x"}, "book"}, Binding{FieldPath{"y"}, "shelf"},
          Binding{FieldPath{"z"}, "author"},
      }),
      bindings);

  EXPECT_EQ(LookupWithParams("GET", "/a/hello/b/endpoints/c",
                             "z=server&t=proxy", &bindings),
            a_b_c);
  EXPECT_EQ(
      Bindings({
          Binding{FieldPath{"x"}, "hello"},
          Binding{FieldPath{"y"}, "endpoints"},
          Binding{FieldPath{"z"}, "server"}, Binding{FieldPath{"t"}, "proxy"},
      }),
      bindings);
}

TEST_F(PathMatcherTest, VariableBindingsWithQueryParamsEncoding) {
  MethodInfo* a = AddGetPath("/a");
  Build();

  EXPECT_NE(nullptr, a);

  Bindings bindings;
  EXPECT_EQ(LookupWithParams("GET", "/a", "x=Hello%20world", &bindings), a);
  EXPECT_EQ(Bindings({
                Binding{FieldPath{"x"}, "Hello world"},
            }),
            bindings);

  EXPECT_EQ(LookupWithParams("GET", "/a", "x=%24%25%2F%20%0A", &bindings), a);
  EXPECT_EQ(Bindings({
                Binding{FieldPath{"x"}, "$%/ \n"},
            }),
            bindings);
}

TEST_F(PathMatcherTest, VariableBindingsWithQueryParamsAndSystemParams) {
  std::set<std::string> system_params{"key", "api_key"};
  MethodInfo* a_b = AddPathWithSystemParams("GET", "/a/{x}/b", &system_params);
  Build();

  EXPECT_NE(nullptr, a_b);

  Bindings bindings;
  EXPECT_EQ(LookupWithParams("GET", "/a/hello/b", "y=world&api_key=secret",
                             &bindings),
            a_b);
  EXPECT_EQ(
      Bindings({
          Binding{FieldPath{"x"}, "hello"}, Binding{FieldPath{"y"}, "world"},
      }),
      bindings);
  EXPECT_EQ(
      LookupWithParams("GET", "/a/hello/b", "key=secret&y=world", &bindings),
      a_b);
  EXPECT_EQ(
      Bindings({
          Binding{FieldPath{"x"}, "hello"}, Binding{FieldPath{"y"}, "world"},
      }),
      bindings);
}

}  // namespace

}  // namespace api_spec
}  // namespace istio
