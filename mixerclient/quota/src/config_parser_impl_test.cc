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

#include "quota/include/config_parser.h"

#include "google/protobuf/text_format.h"
#include "gtest/gtest.h"
#include "include/attributes_builder.h"

using ::istio::mixer::v1::Attributes;
using ::istio::mixer_client::AttributesBuilder;
using ::google::protobuf::TextFormat;
using ::istio::mixer::v1::config::client::QuotaSpec;

namespace istio {
namespace quota {
namespace {

const char kQuotaEmptyMatch[] = R"(
rules {
  quotas {
    quota: "quota1"
    charge: 1
  }
  quotas {
    quota: "quota2"
    charge: 2
  }
}
)";

const char kQuotaMatch[] = R"(
rules {
  match {
    clause {
      key: "request.http_method"
      value {
        exact: "GET"
      }
    }
    clause {
      key: "request.path"
      value {
        prefix: "/books"
      }
    }
  }
  match {
    clause {
      key: "api.operation"
      value {
        exact: "get_books"
      }
    }
  }
  quotas {
    quota: "quota-name"
    charge: 1
  }
}
)";

const char kQuotaRegexMatch[] = R"(
rules {
  match {
    clause {
      key: "request.path"
      value {
        regex: "/shelves/.*/books"
      }
    }
  }
  quotas {
    quota: "quota-name"
    charge: 1
  }
}
)";

// Define similar data structure for quota requirement
// But this one has operator== for comparison so that EXPECT_EQ
// can directly use its vector.
struct Quota {
  std::string quota;
  int64_t charge;

  bool operator==(const Quota& v) const {
    return quota == v.quota && charge == v.charge;
  }
};

// A short name for the vector.
using QV = std::vector<Quota>;

// Converts the vector of Requirement to vector of Quota
std::vector<Quota> ToQV(const std::vector<Requirement>& src) {
  std::vector<Quota> v;
  for (const auto& it : src) {
    v.push_back({it.quota, it.charge});
  }
  return v;
}

TEST(ConfigParserTest, TestEmptyMatch) {
  QuotaSpec quota_spec;
  ASSERT_TRUE(TextFormat::ParseFromString(kQuotaEmptyMatch, &quota_spec));
  auto parser = ConfigParser::Create(quota_spec);

  Attributes attributes;
  // If match clause is empty, it matches all requests.
  ASSERT_EQ(ToQV(parser->GetRequirements(attributes)),
            QV({{"quota1", 1}, {"quota2", 2}}));
}

TEST(ConfigParserTest, TestMatch) {
  QuotaSpec quota_spec;
  ASSERT_TRUE(TextFormat::ParseFromString(kQuotaMatch, &quota_spec));
  auto parser = ConfigParser::Create(quota_spec);

  Attributes attributes;
  AttributesBuilder builder(&attributes);
  ASSERT_EQ(ToQV(parser->GetRequirements(attributes)), QV());

  // Wrong http_method
  builder.AddString("request.http_method", "POST");
  builder.AddString("request.path", "/books/1");
  ASSERT_EQ(ToQV(parser->GetRequirements(attributes)), QV());

  // Matched
  builder.AddString("request.http_method", "GET");
  ASSERT_EQ(ToQV(parser->GetRequirements(attributes)), QV({{"quota-name", 1}}));

  attributes.mutable_attributes()->clear();
  // Wrong api.operation
  builder.AddString("api.operation", "get_shelves");
  ASSERT_EQ(ToQV(parser->GetRequirements(attributes)), QV());

  // Matched
  builder.AddString("api.operation", "get_books");
  ASSERT_EQ(ToQV(parser->GetRequirements(attributes)), QV({{"quota-name", 1}}));
}

TEST(ConfigParserTest, TestRegexMatch) {
  QuotaSpec quota_spec;
  ASSERT_TRUE(TextFormat::ParseFromString(kQuotaRegexMatch, &quota_spec));
  auto parser = ConfigParser::Create(quota_spec);

  Attributes attributes;
  AttributesBuilder builder(&attributes);
  // Not match
  builder.AddString("request.path", "/shelves/1/bar");
  ASSERT_EQ(ToQV(parser->GetRequirements(attributes)), QV());

  // match
  builder.AddString("request.path", "/shelves/10/books");
  ASSERT_EQ(ToQV(parser->GetRequirements(attributes)), QV({{"quota-name", 1}}));
}

}  // namespace
}  // namespace quota
}  // namespace istio
