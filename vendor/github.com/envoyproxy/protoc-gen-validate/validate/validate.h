#ifndef _VALIDATE_H
#define _VALIDATE_H

#include <functional>
#include <regex>
#include <stdexcept>
#include <string>
#include <typeinfo>
#include <typeindex>
#include <unordered_map>

#if !defined(_WIN32)
#include <arpa/inet.h>
#else
#include <winsock.h>
#include <ws2tcpip.h>
#endif

namespace pgv {
using std::string;

class UnimplementedException : public std::runtime_error {
public:
  UnimplementedException() : std::runtime_error("not yet implemented") {}
  // Thrown by C++ validation code that is not yet implemented.
};

using ValidationMsg = std::string;

class BaseValidator {
protected:
  static std::unordered_map<std::type_index, BaseValidator*>& validators() {
    static auto* validator_map = new std::unordered_map<std::type_index, BaseValidator*>();
    return *validator_map;
  }
};

template <typename T>
class Validator : public BaseValidator {
public:
  Validator(std::function<bool(const T&, ValidationMsg*)> check) : check_(check)
  {
    validators()[std::type_index(typeid(T))] = this;
  }

  static bool CheckMessage(const T& m, ValidationMsg* err)
  {
    auto val = static_cast<Validator<T>*>(validators()[std::type_index(typeid(T))]);
    if (val) {
      return val->check_(m, err);
    }
    return true;
  }

private:
  std::function<bool(const T&, ValidationMsg*)> check_;
};

static inline std::string String(const ValidationMsg& msg)
{
  return std::string(msg);
}

static inline bool IsPrefix(const string& maybe_prefix, const string& search_in)
{
  return search_in.compare(0, maybe_prefix.size(), maybe_prefix) == 0;
}

static inline bool IsSuffix(const string& maybe_suffix, const string& search_in)
{
  return maybe_suffix.size() <= search_in.size() && search_in.compare(search_in.size() - maybe_suffix.size(), maybe_suffix.size(), maybe_suffix) == 0;
}

static inline bool Contains(const string& search_in, const string& to_find)
{
  return search_in.find(to_find) != string::npos;
}

static inline bool IsIpv4(const string& to_validate) {
	struct sockaddr_in sa;
	return !(inet_pton(AF_INET, to_validate.c_str(), &sa.sin_addr) < 1);
}

static inline bool IsIpv6(const string& to_validate) {
  struct sockaddr_in6 sa_six;
	return !(inet_pton(AF_INET6, to_validate.c_str(), &sa_six.sin6_addr) < 1);
}

static inline bool IsIp(const string& to_validate) {
  return IsIpv4(to_validate) || IsIpv6(to_validate);
}

static inline bool IsHostname(const string& to_validate) {
  if (to_validate.length() > 253) {
    return false;
  }

  const std::regex dot_regex{"\\."};
  const auto iter_end = std::sregex_token_iterator();
  auto iter = std::sregex_token_iterator(to_validate.begin(), to_validate.end(), dot_regex, -1);
  for (; iter != iter_end; ++iter) {
    const std::string &part = *iter;
    if (part.empty() || part.length() > 63) {
      return false;
    }
    if (part.at(0) == '-') {
      return false;
    }
    if (part.at(part.length() - 1) == '-') {
      return false;
    }
    for (const auto &character : part) {
      if ((character < 'A' || character > 'Z') && (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-') {
        return false;
      }
    }
  }

  return true;
}

} // namespace pgv

#endif // _VALIDATE_H
