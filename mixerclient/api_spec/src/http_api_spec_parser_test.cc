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

#include "api_spec/include/http_api_spec_parser.h"
#include "google/protobuf/text_format.h"
#include "google/protobuf/util/message_differencer.h"
#include "gtest/gtest.h"
#include "include/attributes_builder.h"

using ::istio::mixer::v1::Attributes;
using ::istio::mixer_client::AttributesBuilder;
using ::istio::mixer::v1::config::client::HTTPAPISpec;
using ::google::protobuf::TextFormat;
using ::google::protobuf::util::MessageDifferencer;

namespace istio {
namespace api_spec {

const char kSpec[] = R"(  
attributes {
  attributes {
    key: "key0"
    value {
      string_value: "value0"
    }
  }
}
patterns {
  attributes {
    attributes {
      key: "key1"
      value {
        string_value: "value1"
      }
    }                                                                                                                                           
  }
  http_method: "GET"
  uri_template: "/books/{id=*}"
}
patterns {
  attributes {
    attributes {
      key: "key2"
      value {
        string_value: "value2"
      }
    }                                                                                                                                           
  }
  http_method: "GET"
  regex: "/books/.*"
}
)";

const char kResult[] = R"(
attributes {
  key: "key0"
  value {
    string_value: "value0"
  }
}                                                                                                                                           
attributes {
  key: "key1"
  value {
    string_value: "value1"
  }
}                                                                                                                                           
attributes {
  key: "key2"
  value {
    string_value: "value2"
  }
}                                                                                                                                           
)";

TEST(HttpApiSpecParserTest, TestEmptySpec) {
  HTTPAPISpec spec;
  auto parser = HttpApiSpecParser::Create(spec);

  Attributes attributes;
  parser->AddAttributes("GET", "/books", &attributes);
  EXPECT_TRUE(attributes.attributes().size() == 0);
}

TEST(HttpApiSpecParserTest, TestBoth) {
  HTTPAPISpec spec;
  ASSERT_TRUE(TextFormat::ParseFromString(kSpec, &spec));
  auto parser = HttpApiSpecParser::Create(spec);

  Attributes attributes;
  parser->AddAttributes("GET", "/books/10", &attributes);

  Attributes expected;
  ASSERT_TRUE(TextFormat::ParseFromString(kResult, &expected));
  EXPECT_TRUE(MessageDifferencer::Equals(attributes, expected));
}

}  // namespace api_spec
}  // namespace istio
