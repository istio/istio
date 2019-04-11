#ifndef _VALIDATE_H
#define _VALIDATE_H

#include <stdexcept>
#include <string>

#include <google/protobuf/message.h>
#include <google/protobuf/util/time_util.h>

namespace pgv {
using std::string;

namespace protobuf = google::protobuf;
namespace protobuf_wkt = google::protobuf;

class UnimplementedException : public std::runtime_error {
 public:
  UnimplementedException() : std::runtime_error("not yet implemented") {}
  // Thrown by C++ validation code that is not yet implemented.
};

using ValidationMsg = std::string;

static inline std::string String(const ValidationMsg& msg) {
  return std::string(msg);
}

static inline bool IsPrefix(const string& maybe_prefix,
                            const string& search_in) {
  return search_in.compare(0, maybe_prefix.size(), maybe_prefix) == 0;
}

static inline bool IsSuffix(const string& maybe_suffix,
                            const string& search_in) {
  return maybe_suffix.size() <= search_in.size() &&
    search_in.compare(search_in.size() - maybe_suffix.size(),
                       maybe_suffix.size(), maybe_suffix) == 0;
}

static inline bool Contains(const string& search_in, const string& to_find) {
  return search_in.find(to_find) != string::npos;
}

} // namespace pgv

#endif // _VALIDATE_H
