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

#include "quota_config.h"

#include "google/protobuf/text_format.h"
#include "gtest/gtest.h"
#include "include/attributes_builder.h"

using ::istio::mixer::v1::Attributes;
using ::istio::mixer_client::AttributesBuilder;
using ::google::protobuf::TextFormat;
using ::istio::mixer::v1::config::client::QuotaSpec;

namespace Envoy {
namespace Http {
namespace Mixer {
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

using QuotaVector = std::vector<QuotaConfig::Quota>;

class QuotaConfigTest : public ::testing::Test {
 public:
  void SetUp() {}
};

TEST_F(QuotaConfigTest, TestEmptyMatch) {
  QuotaSpec quota_spec;
  ASSERT_TRUE(TextFormat::ParseFromString(kQuotaEmptyMatch, &quota_spec));
  QuotaConfig config(quota_spec);

  Attributes attributes;
  // If match clause is empty, it matches all requests.
  ASSERT_EQ(config.Check(attributes),
            QuotaVector({{"quota1", 1}, {"quota2", 2}}));
}

TEST_F(QuotaConfigTest, TestMatch) {
  QuotaSpec quota_spec;
  ASSERT_TRUE(TextFormat::ParseFromString(kQuotaMatch, &quota_spec));
  QuotaConfig config(quota_spec);

  Attributes attributes;
  AttributesBuilder builder(&attributes);
  ASSERT_EQ(config.Check(attributes), QuotaVector());

  // Wrong http_method
  builder.AddString("request.http_method", "POST");
  builder.AddString("request.path", "/books/1");
  ASSERT_EQ(config.Check(attributes), QuotaVector());

  // Matched
  builder.AddString("request.http_method", "GET");
  ASSERT_EQ(config.Check(attributes), QuotaVector({{"quota-name", 1}}));

  attributes.mutable_attributes()->clear();
  // Wrong api.operation
  builder.AddString("api.operation", "get_shelves");
  ASSERT_EQ(config.Check(attributes), QuotaVector());

  // Matched
  builder.AddString("api.operation", "get_books");
  ASSERT_EQ(config.Check(attributes), QuotaVector({{"quota-name", 1}}));
}

TEST_F(QuotaConfigTest, TestRegexMatch) {
  QuotaSpec quota_spec;
  ASSERT_TRUE(TextFormat::ParseFromString(kQuotaRegexMatch, &quota_spec));
  QuotaConfig config(quota_spec);

  Attributes attributes;
  AttributesBuilder builder(&attributes);
  // Not match
  builder.AddString("request.path", "/shelves/1/bar");
  ASSERT_EQ(config.Check(attributes), QuotaVector());

  // match
  builder.AddString("request.path", "/shelves/10/books");
  ASSERT_EQ(config.Check(attributes), QuotaVector({{"quota-name", 1}}));
}

}  // namespace
}  // namespace Mixer
}  // namespace Http
}  // namespace Envoy
