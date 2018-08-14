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

#include "proxy/include/istio/api_spec/http_api_spec_parser.h"
#include "google/protobuf/text_format.h"
#include "google/protobuf/util/message_differencer.h"
#include "gtest/gtest.h"
#include "proxy/include/istio/utils/attributes_builder.h"
#include "proxy/src/istio/control/http/mock_check_data.h"

using ::google::protobuf::TextFormat;
using ::google::protobuf::util::MessageDifferencer;
using ::istio::control::http::MockCheckData;
using ::istio::mixer::v1::Attributes;
using ::istio::mixer::v1::config::client::HTTPAPISpec;
using ::istio::utils::AttributesBuilder;

using ::testing::Invoke;
using ::testing::_;

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

TEST(HttpApiSpecParserTest, TestDefaultApiKey) {
  HTTPAPISpec spec;
  auto parser = HttpApiSpecParser::Create(spec);

  // Failed
  ::testing::NiceMock<MockCheckData> mock_data0;
  std::string api_key0;
  EXPECT_FALSE(parser->ExtractApiKey(&mock_data0, &api_key0));

  // "key" query
  ::testing::NiceMock<MockCheckData> mock_data1;
  EXPECT_CALL(mock_data1, FindQueryParameter(_, _))
      .WillRepeatedly(
          Invoke([](const std::string& name, std::string* value) -> bool {
            if (name == "key") {
              *value = "this-is-a-test-api-key";
              return true;
            }
            return false;
          }));

  std::string api_key1;
  EXPECT_TRUE(parser->ExtractApiKey(&mock_data1, &api_key1));
  EXPECT_EQ(api_key1, "this-is-a-test-api-key");

  // "api_key" query
  ::testing::NiceMock<MockCheckData> mock_data2;
  EXPECT_CALL(mock_data2, FindQueryParameter(_, _))
      .WillRepeatedly(
          Invoke([](const std::string& name, std::string* value) -> bool {
            if (name == "api_key") {
              *value = "this-is-a-test-api-key";
              return true;
            }
            return false;
          }));

  std::string api_key2;
  EXPECT_TRUE(parser->ExtractApiKey(&mock_data2, &api_key2));
  EXPECT_EQ(api_key2, "this-is-a-test-api-key");

  // "x-api-key" header
  ::testing::NiceMock<MockCheckData> mock_data3;
  EXPECT_CALL(mock_data3, FindHeaderByName(_, _))
      .WillRepeatedly(
          Invoke([](const std::string& name, std::string* value) -> bool {
            if (name == "x-api-key") {
              *value = "this-is-a-test-api-key";
              return true;
            }
            return false;
          }));

  std::string api_key3;
  EXPECT_TRUE(parser->ExtractApiKey(&mock_data3, &api_key3));
  EXPECT_EQ(api_key3, "this-is-a-test-api-key");
}

TEST(HttpApiSpecParserTest, TestCustomApiKey) {
  HTTPAPISpec spec;
  spec.add_api_keys()->set_query("api_key_query");
  spec.add_api_keys()->set_header("Api-Key-Header");
  spec.add_api_keys()->set_cookie("Api-Key-Cookie");
  auto parser = HttpApiSpecParser::Create(spec);

  // Failed
  ::testing::NiceMock<MockCheckData> mock_data0;
  std::string api_key0;
  EXPECT_FALSE(parser->ExtractApiKey(&mock_data0, &api_key0));

  // "api_key_query"
  ::testing::NiceMock<MockCheckData> mock_data1;
  EXPECT_CALL(mock_data1, FindQueryParameter(_, _))
      .WillRepeatedly(
          Invoke([](const std::string& name, std::string* value) -> bool {
            if (name == "api_key_query") {
              *value = "this-is-a-test-api-key";
              return true;
            }
            return false;
          }));

  std::string api_key1;
  EXPECT_TRUE(parser->ExtractApiKey(&mock_data1, &api_key1));
  EXPECT_EQ(api_key1, "this-is-a-test-api-key");

  // "api-key-header" header
  ::testing::NiceMock<MockCheckData> mock_data2;
  EXPECT_CALL(mock_data2, FindHeaderByName(_, _))
      .WillRepeatedly(
          Invoke([](const std::string& name, std::string* value) -> bool {
            if (name == "Api-Key-Header") {
              *value = "this-is-a-test-api-key";
              return true;
            }
            return false;
          }));

  std::string api_key2;
  EXPECT_TRUE(parser->ExtractApiKey(&mock_data2, &api_key2));
  EXPECT_EQ(api_key2, "this-is-a-test-api-key");

  // "Api-Key-Cookie" cookie
  ::testing::NiceMock<MockCheckData> mock_data3;
  EXPECT_CALL(mock_data3, FindCookie(_, _))
      .WillRepeatedly(
          Invoke([](const std::string& name, std::string* value) -> bool {
            if (name == "Api-Key-Cookie") {
              *value = "this-is-a-test-api-key";
              return true;
            }
            return false;
          }));

  std::string api_key3;
  EXPECT_TRUE(parser->ExtractApiKey(&mock_data3, &api_key3));
  EXPECT_EQ(api_key3, "this-is-a-test-api-key");
}

}  // namespace api_spec
}  // namespace istio
