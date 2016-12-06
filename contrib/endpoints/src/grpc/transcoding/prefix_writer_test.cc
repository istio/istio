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
#include "src/grpc/transcoding/prefix_writer.h"

#include <memory>
#include <string>

#include "google/protobuf/util/internal/expecting_objectwriter.h"
#include "gtest/gtest.h"

namespace google {
namespace api_manager {

namespace transcoding {
namespace testing {
namespace {

using ::testing::InSequence;

class PrefixWriterTest : public ::testing::Test {
 protected:
  PrefixWriterTest() : mock_(), expect_(&mock_) {}

  std::unique_ptr<PrefixWriter> Create(const std::string& prefix) {
    return std::unique_ptr<PrefixWriter>(new PrefixWriter(prefix, &mock_));
  }

  google::protobuf::util::converter::MockObjectWriter mock_;
  google::protobuf::util::converter::ExpectingObjectWriter expect_;
  InSequence seq_;  // all our expectations must be ordered
};

TEST_F(PrefixWriterTest, EmptyPrefix) {
  expect_.StartObject("");
  expect_.StartObject("A");
  expect_.RenderString("x", "a");
  expect_.RenderBytes("by", "b");
  expect_.RenderInt32("i", google::protobuf::int32(1));
  expect_.RenderUint32("ui", google::protobuf::uint32(2));
  expect_.RenderInt64("i64", google::protobuf::int64(3));
  expect_.RenderUint64("ui64", google::protobuf::uint64(4));
  expect_.RenderBool("b", true);
  expect_.RenderNull("null");
  expect_.StartObject("B");
  expect_.RenderString("y", "b");
  expect_.EndObject();  // B
  expect_.EndObject();  // A
  expect_.EndObject();  // ""

  auto w = Create("");

  w->StartObject("");
  w->StartObject("A");
  w->RenderString("x", "a");
  w->RenderBytes("by", "b");
  w->RenderInt32("i", google::protobuf::int32(1));
  w->RenderUint32("ui", google::protobuf::uint32(2));
  w->RenderInt64("i64", google::protobuf::int64(3));
  w->RenderUint64("ui64", google::protobuf::uint64(4));
  w->RenderBool("b", true);
  w->RenderNull("null");
  w->StartObject("B");
  w->RenderString("y", "b");
  w->EndObject();  // B
  w->EndObject();  // A
  w->EndObject();  // ""
}

TEST_F(PrefixWriterTest, OneLevelPrefix1) {
  expect_.StartObject("");
  expect_.StartObject("A");
  expect_.RenderString("x", "a");
  expect_.StartObject("B");
  expect_.RenderString("y", "b");
  expect_.EndObject();  // B
  expect_.EndObject();  // A
  expect_.EndObject();  // ""

  expect_.StartObject("C");
  expect_.StartObject("A");
  expect_.RenderString("z", "c");
  expect_.EndObject();  // C
  expect_.EndObject();  // A

  auto w = Create("A");

  w->StartObject("");
  w->RenderString("x", "a");
  w->StartObject("B");
  w->RenderString("y", "b");
  w->EndObject();  // B
  w->EndObject();  // A, ""

  w->StartObject("C");
  w->RenderString("z", "c");
  w->EndObject();  // C, A
}

TEST_F(PrefixWriterTest, OneLevelPrefix2) {
  expect_.StartObject("x");
  expect_.RenderString("A", "a");
  expect_.EndObject();  // "A"

  expect_.StartObject("by");
  expect_.RenderBytes("A", "b");
  expect_.EndObject();  // "A"

  expect_.StartObject("i32");
  expect_.RenderInt32("A", google::protobuf::int32(-32));
  expect_.EndObject();  // "A"

  expect_.StartObject("ui32");
  expect_.RenderUint32("A", google::protobuf::uint32(32));
  expect_.EndObject();  // "A"

  expect_.StartObject("i64");
  expect_.RenderInt64("A", google::protobuf::int64(-64));
  expect_.EndObject();  // "A"

  expect_.StartObject("ui64");
  expect_.RenderUint64("A", google::protobuf::uint64(64));
  expect_.EndObject();  // "A"

  expect_.StartObject("b");
  expect_.RenderBool("A", false);
  expect_.EndObject();  // "A"

  expect_.StartObject("nil");
  expect_.RenderNull("A");
  expect_.EndObject();  // "A"

  auto w = Create("A");

  w->RenderString("x", "a");
  w->RenderBytes("by", "b");
  w->RenderInt32("i32", google::protobuf::int32(-32));
  w->RenderUint32("ui32", google::protobuf::uint32(32));
  w->RenderInt64("i64", google::protobuf::int64(-64));
  w->RenderUint64("ui64", google::protobuf::uint64(64));
  w->RenderBool("b", false);
  w->RenderNull("nil");
}

TEST_F(PrefixWriterTest, TwoLevelPrefix) {
  expect_.StartObject("");
  expect_.StartObject("A");
  expect_.StartObject("B");
  expect_.RenderString("x", "a");
  expect_.EndObject();  // B
  expect_.EndObject();  // A
  expect_.EndObject();  // ""

  expect_.StartObject("C");
  expect_.StartObject("A");
  expect_.StartObject("B");
  expect_.RenderString("y", "b");
  expect_.EndObject();  // B
  expect_.EndObject();  // A
  expect_.EndObject();  // C

  auto w = Create("A.B");

  w->StartObject("");
  w->RenderString("x", "a");
  w->EndObject();  // B, A, ""

  w->StartObject("C");
  w->RenderString("y", "b");
  w->EndObject();  // B, A, C
}

TEST_F(PrefixWriterTest, ThreeLevelPrefix) {
  expect_.StartObject("");
  expect_.StartObject("A");
  expect_.StartObject("B");
  expect_.StartObject("C");
  expect_.RenderString("x", "a");
  expect_.EndObject();  // C
  expect_.EndObject();  // B
  expect_.EndObject();  // A
  expect_.EndObject();  // ""

  auto w = Create("A.B.C");

  w->StartObject("");
  w->RenderString("x", "a");
  w->EndObject();  // C, B, A, ""
}

}  // namespace
}  // namespace testing
}  // namespace transcoding

}  // namespace api_manager
}  // namespace google
