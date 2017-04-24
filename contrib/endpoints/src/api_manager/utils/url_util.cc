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

#include "contrib/endpoints/src/api_manager/utils/url_util.h"

namespace google {
namespace api_manager {
namespace utils {
namespace {
const std::string kHttpPrefix = "http://";
const std::string kHttpsPrefix = "https://";
}

std::string GetUrlContent(const std::string &url) {
  std::string result;
  if (url.compare(0, kHttpsPrefix.size(), kHttpsPrefix) == 0) {
    result = url.substr(kHttpsPrefix.size());
  } else if (url.compare(0, kHttpPrefix.size(), kHttpPrefix) == 0) {
    result = url.substr(kHttpPrefix.size());
  } else {
    result = url;
  }
  if (result.back() == '/') {
    result.pop_back();
  }
  return result;
}

bool IsHttpRequest(const std::string &url) {
  return url.compare(0, kHttpPrefix.size(), kHttpPrefix) == 0 ||
         url.compare(0, kHttpsPrefix.size(), kHttpsPrefix) == 0;
}

}  // namespace utils
}  // namespace api_manager
}  // namespace google
