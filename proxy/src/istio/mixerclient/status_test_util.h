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

#ifndef ISTIO_UTILS_STATUS_TEST_UTIL_H__
#define ISTIO_UTILS_STATUS_TEST_UTIL_H__

#include "gmock/gmock.h"
#include "google/protobuf/stubs/status.h"
#include "gtest/gtest.h"

// Macros for testing the results of functions that return
// ::google::protobuf::util::Status.

// EXPECT_OK is defined in google/protobuf/stubs/status.h
//#define EXPECT_OK(statement) EXPECT_EQ(::google::protobuf::util::Status::OK,
//(statement))
#define ASSERT_OK(statement) \
  ASSERT_EQ(::google::protobuf::util::Status::OK, (statement))

// Test that a util::Status is not OK.  NOTE: It is preferable to use
// {ASSERT,EXPECT}_ERROR_SUBSTR() and check for a specific error string.
#define EXPECT_NOT_OK(cmd) \
  EXPECT_NE(::google::protobuf::util::Status::OK, (cmd))
#define ASSERT_NOT_OK(cmd) \
  ASSERT_NE(::google::protobuf::util::Status::OK, (cmd))

namespace google {
namespace util {
namespace status_macros {

// StatusErrorCodeMatcher is a gMock matcher that tests that a util::Status
// has a certain error code and ErrorSpace.
//
// Specifying the ErrorSpace is optional - it can be inferred from an
// enum error code type using ErrorCodeOptions from status_macros.h.
//
// Example usage:
//   EXPECT_THAT(transaction->Commit(),
//               HasErrorCode(NOT_FOUND));
class StatusErrorCodeMatcher : public ::testing::MatcherInterface<
                                   const ::google::protobuf::util::Status&> {
 public:
  StatusErrorCodeMatcher(int error_code) : error_code_(error_code) {}

  virtual bool MatchAndExplain(const ::google::protobuf::util::Status& status,
                               ::testing::MatchResultListener* listener) const {
    if (status.ok()) {
      // Code 0 always means OK, and the ErrorSpace is discarded.
      return error_code_ == 0;
    } else {
      return status.error_code() == error_code_;
    }
  }
  virtual void DescribeTo(::std::ostream* os) const {
    *os << "util::Status has error code " << error_code_;
  }
  virtual void DescribeNegationTo(::std::ostream* os) const {
    *os << "util::Status does not have error code " << error_code_;
  }

 private:
  const int error_code_;
};

inline ::testing::Matcher<const ::google::protobuf::util::Status&> HasErrorCode(
    int error_code) {
  return ::testing::MakeMatcher(new StatusErrorCodeMatcher(error_code));
}

// Test that a util::Status has a specific error code, in the right ErrorSpace
// as defined by ErrorCodeOptions.
#define EXPECT_ERROR_CODE(code, cmd) \
  EXPECT_THAT((cmd), ::google::util::status_macros::HasErrorCode(code))
#define ASSERT_ERROR_CODE(code, cmd) \
  ASSERT_THAT((cmd), ::google::util::status_macros::HasErrorCode(code))

}  // namespace status_macros
}  // namespace util
}  // namespace google

#endif  // ISTIO_UTILS_STATUS_TEST_UTIL_H__
