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

#include "attributes_builder.h"

#include "control/src/attribute_names.h"
#include "google/protobuf/text_format.h"
#include "google/protobuf/util/message_differencer.h"
#include "gtest/gtest.h"
#include "include/attributes_builder.h"
#include "mock_check_data.h"
#include "mock_report_data.h"

using ::google::protobuf::TextFormat;
using ::google::protobuf::util::MessageDifferencer;

using ::testing::_;
using ::testing::Invoke;

namespace istio {
namespace mixer_control {
namespace tcp {
namespace {

const char kCheckAttributes[] = R"(
attributes {
  key: "context.protocol"
  value {
    string_value: "tcp"
  }
}
attributes {
  key: "context.time"
  value {
    timestamp_value {
    }
  }
}
attributes {
  key: "source.ip"
  value {
    bytes_value: "1.2.3.4"
  }
}
attributes {
  key: "source.port"
  value {
    int64_value: 8080
  }
}
attributes {
  key: "source.user"
  value {
    string_value: "test_user"
  }
}
)";

const char kReportAttributes[] = R"(
attributes {
  key: "check.status"
  value {
    int64_value: 0
  }
}
attributes {
  key: "connection.duration"
  value {
    duration_value {
      nanos: 1
    }
  }
}
attributes {
  key: "connection.received.bytes"
  value {
    int64_value: 100
  }
}
attributes {
  key: "connection.received.bytes_total"
  value {
    int64_value: 100
  }
}
attributes {
  key: "connection.sent.bytes"
  value {
    int64_value: 200
  }
}
attributes {
  key: "connection.sent.bytes_total"
  value {
    int64_value: 200
  }
}
attributes {
  key: "context.time"
  value {
    timestamp_value {
    }
  }
}
attributes {
  key: "destination.ip"
  value {
    bytes_value: "1.2.3.4"
  }
}
attributes {
  key: "destination.port"
  value {
    int64_value: 8080
  }
}
)";

void ClearContextTime(RequestContext* request) {
  // Override timestamp with -
  ::istio::mixer_client::AttributesBuilder builder(&request->attributes);
  std::chrono::time_point<std::chrono::system_clock> time0;
  builder.AddTimestamp(AttributeName::kContextTime, time0);
}

TEST(AttributesBuilderTest, TestCheckAttributes) {
  ::testing::NiceMock<MockCheckData> mock_data;
  EXPECT_CALL(mock_data, GetSourceIpPort(_, _))
      .WillOnce(Invoke([](std::string* ip, int* port) -> bool {
        *ip = "1.2.3.4";
        *port = 8080;
        return true;
      }));
  EXPECT_CALL(mock_data, GetSourceUser(_))
      .WillOnce(Invoke([](std::string* user) -> bool {
        *user = "test_user";
        return true;
      }));

  RequestContext request;
  AttributesBuilder builder(&request);
  builder.ExtractCheckAttributes(&mock_data);

  ClearContextTime(&request);

  std::string out_str;
  TextFormat::PrintToString(request.attributes, &out_str);
  GOOGLE_LOG(INFO) << "===" << out_str << "===";

  ::istio::mixer::v1::Attributes expected_attributes;
  ASSERT_TRUE(
      TextFormat::ParseFromString(kCheckAttributes, &expected_attributes));
  EXPECT_TRUE(
      MessageDifferencer::Equals(request.attributes, expected_attributes));
}

TEST(AttributesBuilderTest, TestReportAttributes) {
  ::testing::NiceMock<MockReportData> mock_data;
  EXPECT_CALL(mock_data, GetDestinationIpPort(_, _))
      .WillOnce(Invoke([](std::string* ip, int* port) -> bool {
        *ip = "1.2.3.4";
        *port = 8080;
        return true;
      }));
  EXPECT_CALL(mock_data, GetReportInfo(_))
      .WillOnce(Invoke([](ReportData::ReportInfo* info) {
        info->received_bytes = 100;
        info->send_bytes = 200;
        info->duration = std::chrono::nanoseconds(1);
      }));

  RequestContext request;
  AttributesBuilder builder(&request);
  builder.ExtractReportAttributes(&mock_data);

  ClearContextTime(&request);

  std::string out_str;
  TextFormat::PrintToString(request.attributes, &out_str);
  GOOGLE_LOG(INFO) << "===" << out_str << "===";

  ::istio::mixer::v1::Attributes expected_attributes;
  ASSERT_TRUE(
      TextFormat::ParseFromString(kReportAttributes, &expected_attributes));
  EXPECT_TRUE(
      MessageDifferencer::Equals(request.attributes, expected_attributes));
}

}  // namespace
}  // namespace tcp
}  // namespace mixer_control
}  // namespace istio
