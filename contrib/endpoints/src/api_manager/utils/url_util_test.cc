// Copyright 2016 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
////////////////////////////////////////////////////////////////////////////////
//
#include "src/api_manager/utils/url_util.h"

#include "gtest/gtest.h"

namespace google {
namespace api_manager {
namespace utils {

TEST(UrlUtil, UrlContentTest) {
  EXPECT_EQ("a.com", GetUrlContent("https://a.com/"));
  EXPECT_EQ("xyz", GetUrlContent("http://xyz"));
  EXPECT_EQ("xyz", GetUrlContent("https://xyz"));
  EXPECT_EQ("a.com.", GetUrlContent("http://a.com./"));
  EXPECT_EQ("xyz", GetUrlContent("xyz/"));
}

}  // namespace utils
}  // namespace api_manager
}  // namespace google
