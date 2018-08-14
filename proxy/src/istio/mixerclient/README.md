# Istio Mixerclient

The Istio Mixerclient is a C++ library to support the mixer API with following features:

- Uses simple struct Attributes to pass attributes. The library will convert them to the request proto message by using the global dictionary and per message dictionary.

- Supports combining multiple quota calls into one single Check call together with precondition check.

- Supports cache for precondition check result. Attributes used to calculate cache key are specified by the Mixer. By default, check cache is enabled unless CheckOptions.num_entries is 0.

- Supports quota cache and prefetch. Attributes used to calculate quota cache key are specified by the Mixer too. By default, quota cache is enabled unless QuotaOptions.num_entries is 0.

- Supports batch for Reports. All report requests are batched up to ReportOptions.max_batch_entries, or up to ReportOptions.max_match_time_ms.


