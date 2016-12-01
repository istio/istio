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
#include "src/api_manager/cloud_trace/cloud_trace.h"

#include "google/devtools/cloudtrace/v1/trace.pb.h"
#include "gtest/gtest.h"
#include "src/api_manager/mock_api_manager_environment.h"

using google::devtools::cloudtrace::v1::TraceSpan;

namespace google {
namespace api_manager {
namespace cloud_trace {
namespace {

class CloudTraceTest : public ::testing::Test {
 public:
  void SetUp() {
    env_.reset(new ::testing::NiceMock<MockApiManagerEnvironmentWithLog>());
    sa_token_ = std::unique_ptr<auth::ServiceAccountToken>(
        new auth::ServiceAccountToken(env_.get()));
  }

  std::unique_ptr<ApiManagerEnvInterface> env_;
  std::unique_ptr<auth::ServiceAccountToken> sa_token_;
};

TEST_F(CloudTraceTest, TestCloudTrace) {
  std::unique_ptr<CloudTrace> cloud_trace(
      CreateCloudTrace("e133eacd437d8a12068fd902af3962d8;o=1", "root-span"));
  ASSERT_TRUE(cloud_trace);

  // After created, there should be a root span.
  ASSERT_EQ(cloud_trace->trace()->spans_size(), 1);
  ASSERT_EQ(cloud_trace->trace()->spans(0).name(), "root-span");
  // End time should be empty now.
  ASSERT_EQ(cloud_trace->trace()->spans(0).end_time().DebugString(), "");

  TraceSpan *trace_span = cloud_trace->trace()->add_spans();
  trace_span->set_name("Span1");

  ASSERT_EQ(cloud_trace->trace()->spans_size(), 2);
  ASSERT_EQ(cloud_trace->trace()->spans(1).name(), "Span1");

  std::shared_ptr<CloudTraceSpan> cloud_trace_span(
      CreateSpan(cloud_trace.get(), "Span2"));
  TRACE(cloud_trace_span) << "Message";
  cloud_trace_span.reset();

  ASSERT_EQ(cloud_trace->trace()->spans_size(), 3);
  ASSERT_EQ(cloud_trace->trace()->spans(2).name(), "Span2");
  ASSERT_EQ(cloud_trace->trace()->spans(2).labels().size(), 1);
  ASSERT_EQ(cloud_trace->trace()->spans(2).labels().find("000")->second,
            "Message");

  cloud_trace->EndRootSpan();
  // After EndRootSpan, end time should not be empty.
  ASSERT_NE(cloud_trace->trace()->spans(0).end_time().DebugString(), "");
}

TEST_F(CloudTraceTest, TestCloudTraceSpanDisabled) {
  std::shared_ptr<CloudTraceSpan> cloud_trace_span(CreateSpan(nullptr, "Span"));
  // Ensure no core dump calling TRACE when cloud_trace_span is nullptr.
  TRACE(cloud_trace_span) << "Message";
  ASSERT_FALSE(cloud_trace_span);
}

TEST_F(CloudTraceTest, TestParseContextHeader) {
  // Disabled if empty.
  ASSERT_EQ(nullptr, CreateCloudTrace("", ""));
  // Disabled for malformed prefix
  ASSERT_EQ(nullptr, CreateCloudTrace("o=1", ""));
  ASSERT_EQ(nullptr, CreateCloudTrace("o=", ""));
  ASSERT_EQ(nullptr, CreateCloudTrace("o=1foo", ""));
  ASSERT_EQ(nullptr, CreateCloudTrace("o=foo1", ""));
  ASSERT_EQ(nullptr, CreateCloudTrace("o=113471230948140", ""));
  ASSERT_EQ(nullptr, CreateCloudTrace("o=1;foo=bar", ""));
  ASSERT_EQ(nullptr, CreateCloudTrace(";o=", ""));
  ASSERT_EQ(nullptr, CreateCloudTrace(";o=1", ""));
  ASSERT_EQ(nullptr, CreateCloudTrace(";o=1foo", ""));
  ASSERT_EQ(nullptr, CreateCloudTrace(";o=foo1", ""));
  ASSERT_EQ(nullptr, CreateCloudTrace(";o=113471230948140", ""));
  ASSERT_EQ(nullptr, CreateCloudTrace(";o=1;foo=bar", ""));
  // Disabled if trace id length < 32
  ASSERT_EQ(nullptr, CreateCloudTrace("123;o=1", ""));
  // Disabled if trace id is not hex string ('q' in last position)
  ASSERT_EQ(nullptr,
            CreateCloudTrace("e133eacd437d8a12068fd902af3962dq;o=1", ""));
  // Disabled if no option invalid or not provided.
  ASSERT_EQ(nullptr, CreateCloudTrace("e133eacd437d8a12068fd902af3962d8", ""));
  ASSERT_EQ(nullptr, CreateCloudTrace("e133eacd437d8a12068fd902af3962d8;", ""));
  ASSERT_EQ(nullptr,
            CreateCloudTrace("e133eacd437d8a12068fd902af3962d8;o=", ""));
  ASSERT_EQ(nullptr,
            CreateCloudTrace("e133eacd437d8a12068fd902af3962d8;o=4", ""));
  ASSERT_EQ(nullptr,
            CreateCloudTrace("e133eacd437d8a12068fd902af3962d8;o=-1", ""));
  ASSERT_EQ(nullptr,
            CreateCloudTrace("e133eacd437d8a12068fd902af3962d8;o=12345", ""));
  ASSERT_EQ(nullptr,
            CreateCloudTrace("e133eacd437d8a12068fd902af3962d8;o=1foo", ""));
  ASSERT_EQ(nullptr,
            CreateCloudTrace("e133eacd437d8a12068fd902af3962d8;o=foo1", ""));
  ASSERT_EQ(nullptr,
            CreateCloudTrace(
                "e133eacd437d8a12068fd902af3962d8;o=113471230948140", ""));
  ASSERT_EQ(nullptr, CreateCloudTrace("e133eacd437d8a12068fd902af3962d8", ""));
  // Disabled if option explicitly says so. Note: first bit of number "o"
  // indicated whether trace is enabled.
  ASSERT_EQ(nullptr,
            CreateCloudTrace("e133eacd437d8a12068fd902af3962d8;o=0", ""));
  ASSERT_EQ(nullptr,
            CreateCloudTrace("e133eacd437d8a12068fd902af3962d8;o=2", ""));
  ASSERT_EQ(nullptr,
            CreateCloudTrace("e133eacd437d8a12068fd902af3962d8;o=0;o=1", ""));
  // Disabled if span id is illegal
  ASSERT_EQ(nullptr,
            CreateCloudTrace("e133eacd437d8a12068fd902af3962d8/xx;o=1", ""));
  ASSERT_EQ(nullptr,
            CreateCloudTrace("e133eacd437d8a12068fd902af3962d8/1xx;o=1", ""));
  ASSERT_EQ(nullptr,
            CreateCloudTrace("e133eacd437d8a12068fd902af3962d8/xx1;o=1", ""));
  ASSERT_EQ(
      nullptr,
      CreateCloudTrace(
          "e133eacd437d8a12068fd902af3962d8/18446744073709551616;o=1", ""));

  std::unique_ptr<CloudTrace> cloud_trace;

  // parent trace id should be 0(default) if span id is not provided.
  cloud_trace.reset(
      CreateCloudTrace("e133eacd437d8a12068fd902af3962d8;o=1", ""));
  ASSERT_TRUE(cloud_trace);
  ASSERT_EQ(0, cloud_trace->root_span()->parent_span_id());
  ASSERT_EQ(1, cloud_trace->trace()->spans_size());
  ASSERT_EQ("o=1", cloud_trace->options());

  // Should also be enabled for "o=3"
  cloud_trace.reset(
      CreateCloudTrace("e133eacd437d8a12068fd902af3962d8;o=3", ""));
  ASSERT_TRUE(cloud_trace);
  ASSERT_EQ("o=3", cloud_trace->options());

  cloud_trace.reset(
      CreateCloudTrace("e133eacd437d8a12068fd902af3962d8;o=1;", ""));
  ASSERT_TRUE(cloud_trace);
  ASSERT_EQ("o=1;", cloud_trace->options());

  cloud_trace.reset(
      CreateCloudTrace("e133eacd437d8a12068fd902af3962d8;o=1;o=0", ""));
  ASSERT_TRUE(cloud_trace);
  ASSERT_EQ("o=1;o=0", cloud_trace->options());

  // Verify capital hex digits should pass
  cloud_trace.reset(
      CreateCloudTrace("46F1ADB8573CC0F3C4156B5EA7E0E3DC;o=1", ""));
  ASSERT_TRUE(cloud_trace);

  // Parent trace id should be set if span id is provided.
  cloud_trace.reset(
      CreateCloudTrace("e133eacd437d8a12068fd902af3962d8/12345;o=1", ""));
  ASSERT_TRUE(cloud_trace);
  ASSERT_EQ(12345, cloud_trace->root_span()->parent_span_id());
  // Parent trace id is max uint64
  cloud_trace.reset(CreateCloudTrace(
      "e133eacd437d8a12068fd902af3962d8/18446744073709551615;o=1", ""));
  ASSERT_TRUE(cloud_trace);
  ASSERT_EQ(18446744073709551615U, cloud_trace->root_span()->parent_span_id());

  // Should not crash if unrecognized option is provided.
  cloud_trace.reset(
      CreateCloudTrace("e133eacd437d8a12068fd902af3962d8;foo=bar;o=1", ""));
  ASSERT_TRUE(cloud_trace);
  ASSERT_EQ("foo=bar;o=1", cloud_trace->options());

  cloud_trace.reset(
      CreateCloudTrace("e133eacd437d8a12068fd902af3962d8;x;o=1", ""));
  ASSERT_TRUE(cloud_trace);
  ASSERT_EQ("x;o=1", cloud_trace->options());

  cloud_trace.reset(
      CreateCloudTrace("e133eacd437d8a12068fd902af3962d8;o=1;foo=bar", ""));
  ASSERT_TRUE(cloud_trace);
  ASSERT_EQ("o=1;foo=bar", cloud_trace->options());
}

}  // namespace

}  // cloud_trace
}  // namespace api_manager
}  // namespace google
