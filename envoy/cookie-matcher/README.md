Demonstrates routing requests based on cookie values using Envoy's `cookie_matchers`.
Requests with `session=foo` are forwarded to `httpbin.com`, while requests with `session=bar` are routed to a local backend service.
