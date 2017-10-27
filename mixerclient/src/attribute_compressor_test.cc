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

#include "src/attribute_compressor.h"
#include "include/attributes_builder.h"

#include <time.h>
#include "google/protobuf/text_format.h"
#include "google/protobuf/util/message_differencer.h"
#include "gtest/gtest.h"

using std::string;
using ::istio::mixer::v1::Attributes;
using ::istio::mixer::v1::Attributes_AttributeValue;
using ::istio::mixer::v1::Attributes_StringMap;
using ::istio::mixer::v1::CompressedAttributes;

using ::google::protobuf::TextFormat;
using ::google::protobuf::util::MessageDifferencer;

namespace istio {
namespace mixer_client {
namespace {

const char kAttributes[] = R"(
words: "JWT-Token"
strings {
  key: 2
  value: 127
}
strings {
  key: 6
  value: 101
}
int64s {
  key: 1
  value: 35
}
int64s {
  key: 8
  value: 8080
}
doubles {
  key: 78
  value: 99.9
}
bools {
  key: 71
  value: true
}
timestamps {
  key: 132
  value {
  }
}
durations {
  key: 29
  value {
    seconds: 5
  }
}
bytes {
  key: 0
  value: "text/html; charset=utf-8"
}
string_maps {
  key: 15
  value {
    entries {
      key: 50
      value: -1
    }
    entries {
      key: 58
      value: 104
    }
  }
}
)";

const char kReportAttributes[] = R"(
attributes {
  strings {
    key: 2
    value: 127
  }
  strings {
    key: 6
    value: 101
  }
  int64s {
    key: 1
    value: 35
  }
  int64s {
    key: 8
    value: 8080
  }
  doubles {
    key: 78
    value: 99.9
  }
  bools {
    key: 71
    value: true
  }
  timestamps {
    key: 132
    value {
    }
  }
  durations {
    key: 29
    value {
      seconds: 5
    }
  }
  bytes {
    key: 0
    value: "text/html; charset=utf-8"
  }
  string_maps {
    key: 15
    value {
      entries {
        key: 50
        value: -1
      }
      entries {
        key: 58
        value: 104
      }
    }
  }
}
attributes {
  int64s {
    key: 1
    value: 135
  }
  int64s {
    key: 27
    value: 111
  }
  doubles {
    key: 78
    value: 123.99
  }
  bools {
    key: 71
    value: false
  }
  string_maps {
    key: 15
    value {
      entries {
        key: 32
        value: 90
      }
      entries {
        key: 58
        value: 104
      }
    }
  }
}
default_words: "JWT-Token"
global_word_count: 111
)";

class AttributeCompressorTest : public ::testing::Test {
 protected:
  void SetUp() {
    // Have to use words from global dictionary.
    // Otherwise test is flaky since protobuf::map order is not deterministic
    // if a word has to be in the per-message dictionary, its index depends
    // on the order it created.
    AttributesBuilder builder(&attributes_);
    builder.AddString("source.name", "connection.received.bytes_total");
    builder.AddBytes("source.ip", "text/html; charset=utf-8");
    builder.AddDouble("range", 99.9);
    builder.AddInt64("source.port", 35);
    builder.AddBool("keep-alive", true);
    builder.AddString("source.user", "x-http-method-override");
    builder.AddInt64("target.port", 8080);

    std::chrono::time_point<std::chrono::system_clock> time_point;
    std::chrono::seconds secs(5);
    builder.AddTimestamp("context.timestamp", time_point);
    builder.AddDuration(
        "response.duration",
        std::chrono::duration_cast<std::chrono::nanoseconds>(secs));

    // JWT-token is only word not in the global dictionary.
    std::map<std::string, std::string> string_map = {
        {"authorization", "JWT-Token"}, {"content-type", "application/json"}};
    builder.AddStringMap("request.headers", std::move(string_map));
  }

  Attributes attributes_;
};

TEST_F(AttributeCompressorTest, CompressTest) {
  // A compressor with an empty global dictionary.
  AttributeCompressor compressor;
  ::istio::mixer::v1::CompressedAttributes attributes_pb;
  compressor.Compress(attributes_, &attributes_pb);

  std::string out_str;
  TextFormat::PrintToString(attributes_pb, &out_str);
  GOOGLE_LOG(INFO) << "===" << out_str << "===";

  ::istio::mixer::v1::CompressedAttributes expected_attributes_pb;
  ASSERT_TRUE(
      TextFormat::ParseFromString(kAttributes, &expected_attributes_pb));
  EXPECT_TRUE(
      MessageDifferencer::Equals(attributes_pb, expected_attributes_pb));
}

TEST_F(AttributeCompressorTest, BatchCompressTest) {
  // A compressor with an empty global dictionary.
  AttributeCompressor compressor;
  auto batch_compressor = compressor.CreateBatchCompressor();

  EXPECT_TRUE(batch_compressor->Add(attributes_));

  // modify some attributes
  AttributesBuilder builder(&attributes_);
  builder.AddDouble("range", 123.99);
  builder.AddInt64("source.port", 135);
  builder.AddInt64("response.size", 111);
  builder.AddBool("keep-alive", false);
  builder.AddStringMap("request.headers", {{"content-type", "application/json"},
                                           {":method", "GET"}});

  // Since there is no deletion, batch is good
  EXPECT_TRUE(batch_compressor->Add(attributes_));

  // remove a key
  attributes_.mutable_attributes()->erase("response.size");
  // Batch should fail.
  EXPECT_FALSE(batch_compressor->Add(attributes_));

  auto report_pb = batch_compressor->Finish();

  std::string out_str;
  TextFormat::PrintToString(*report_pb, &out_str);
  GOOGLE_LOG(INFO) << "===" << out_str << "===";

  ::istio::mixer::v1::ReportRequest expected_report_pb;
  ASSERT_TRUE(
      TextFormat::ParseFromString(kReportAttributes, &expected_report_pb));
  report_pb->set_global_word_count(111);
  EXPECT_TRUE(MessageDifferencer::Equals(*report_pb, expected_report_pb));
}

}  // namespace
}  // namespace mixer_client
}  // namespace istio
