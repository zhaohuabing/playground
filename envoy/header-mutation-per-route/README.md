A x-teg-oidcauthentication header has been added to incoming requests and used for RBAC, which proves that header mutation can be done per route and then be used for later processing in other filters in a HTTP filter chain.

To learn about this sandbox and for instructions on how to run it please head over
to the [Envoy docs](https://www.envoyproxy.io/docs/envoy/latest/api-v3/extensions/filters/http/header_mutation/v3/header_mutation.proto#extensions-filters-http-header-mutation-v3-headermutationperroute).
