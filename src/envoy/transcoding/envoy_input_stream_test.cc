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

#include "src/envoy/transcoding/envoy_input_stream.h"

#include "gtest/gtest.h"

namespace Envoy {
namespace Grpc {
namespace {

class EnvoyInputStreamTest : public testing::Test {
 public:
  EnvoyInputStreamTest() {
    Buffer::OwnedImpl buffer{"abcd"};
    stream_.Move(buffer);
  }

  std::string slice_data_{"abcd"};
  EnvoyInputStream stream_;

  const void *data_;
  int size_;
};

TEST_F(EnvoyInputStreamTest, Move) {
  Buffer::OwnedImpl buffer{"abcd"};
  stream_.Move(buffer);

  EXPECT_EQ(0, buffer.length());
  EXPECT_EQ(8, stream_.BytesAvailable());
}

TEST_F(EnvoyInputStreamTest, Next) {
  EXPECT_TRUE(stream_.Next(&data_, &size_));
  EXPECT_EQ(4, size_);
  EXPECT_EQ(0, memcmp(slice_data_.data(), data_, size_));
}

TEST_F(EnvoyInputStreamTest, TwoSlices) {
  Buffer::OwnedImpl buffer("efgh");

  stream_.Move(buffer);

  EXPECT_TRUE(stream_.Next(&data_, &size_));
  EXPECT_EQ(4, size_);
  EXPECT_EQ(0, memcmp(slice_data_.data(), data_, size_));
  EXPECT_TRUE(stream_.Next(&data_, &size_));
  EXPECT_EQ(4, size_);
  EXPECT_EQ(0, memcmp("efgh", data_, size_));
}

TEST_F(EnvoyInputStreamTest, BackUp) {
  EXPECT_TRUE(stream_.Next(&data_, &size_));
  EXPECT_EQ(4, size_);

  stream_.BackUp(3);
  EXPECT_EQ(3, stream_.BytesAvailable());
  EXPECT_EQ(1, stream_.ByteCount());

  EXPECT_TRUE(stream_.Next(&data_, &size_));
  EXPECT_EQ(3, size_);
  EXPECT_EQ(0, memcmp("bcd", data_, size_));
  EXPECT_EQ(4, stream_.ByteCount());
}

TEST_F(EnvoyInputStreamTest, ByteCount) {
  EXPECT_EQ(0, stream_.ByteCount());
  EXPECT_TRUE(stream_.Next(&data_, &size_));
  EXPECT_EQ(4, stream_.ByteCount());
}

TEST_F(EnvoyInputStreamTest, Finish) {
  EXPECT_TRUE(stream_.Next(&data_, &size_));
  EXPECT_TRUE(stream_.Next(&data_, &size_));
  EXPECT_EQ(0, size_);
  stream_.Finish();
  EXPECT_FALSE(stream_.Next(&data_, &size_));

  Buffer::OwnedImpl buffer("efgh");
  stream_.Move(buffer);

  EXPECT_EQ(4, buffer.length());
}

}  // namespace
}  // namespace Grpc
}  // namespace Envoy
