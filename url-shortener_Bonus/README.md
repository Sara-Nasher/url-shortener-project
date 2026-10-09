# URL Shortener

A small HTTP service written in Go that shortens long URLs and redirects short codes to the original URLs.

The project was implemented in four required parts:

* Part 1 — MVP: shorten and redirect
* Part 2 — API, errors, and Store interface
* Part 3 — Performance and measurement
* Part 4 — Persistence

For the bonus parts, I also added some production-related improvements and documented a scalable architecture for handling a much larger number of requests:

* Part 5 — Millions of Requests
* Part 6 — Production Habits

The application supports both an in-memory store and a file-based persistent store.

---

## Requirements

* Go 1.22 or newer

No external database or additional package is required for the current implementation.

---

## Project Structure

```text
url-shortener/

├── cmd/
│   └── server/
│       └── main.go
├── internal/
│   ├── httpapi/
│   │   ├── handler.go
│   │   ├── handler_test.go
│   │   ├── handler_benchmark_test.go
│   │   ├── file_persistence_test.go
│   │   ├── logging.go
│   │   └── rate_limiter.go
│   ├── shortener/
│   │   ├── service.go
│   │   ├── service_test.go
│   │   └── service_benchmark_test.go
│   └── store/
│       ├── store.go
│       ├── store_test.go
│       ├── file_store.go
│       └── file_store_test.go
├── README.md
├── DECISIONS.md
└── go.mod
```

---

# Part 1 — MVP: Shorten and Redirect

The first part implements the basic URL shortening functionality.

A user sends a long URL to:

```http
POST /api/shorten
```

and receives a generated short code.

The short code can then be used with:

```http
GET /{code}
```

to redirect to the original URL.

The application uses cryptographically random six-character codes containing letters and numbers. Code generation uses rejection sampling to avoid modulo bias.

The store also checks for code collisions before saving a new link.

---

## Running the Application

### Memory Store

The default mode uses an in-memory store:

```powershell
go run ./cmd/server
```

The default address is:

```text
http://localhost:8080
```

The default base URL is:

```text
http://localhost:8080
```

The address and base URL can also be changed:

```powershell
go run ./cmd/server -addr :8080 -base http://localhost:8080
```

### Persistent File Store

The application can also use a file-backed store:

```powershell
go run ./cmd/server -store file -data data/links.jsonl
```

The `-store` flag accepts:

```text
memory
file
```

The `-data` flag specifies the file used by the persistent store.

If the data directory does not exist, the application creates it automatically.

---

## HTTP API

### Create a Short URL

```http
POST /api/shorten
```

Request:

```json
{
  "url": "https://example.com/path"
}
```

Example using PowerShell:

```powershell
Invoke-RestMethod `
  -Method Post `
  -Uri http://localhost:8080/api/shorten `
  -ContentType "application/json" `
  -Body '{"url":"https://example.com/path"}'
```

Example response:

```json
{
  "code": "a1B2c3",
  "short_url": "http://localhost:8080/a1B2c3"
}
```

The response status is:

```text
201 Created
```

The request body is limited to 1 MiB. It must contain exactly one JSON object, and unknown fields are rejected. Invalid JSON returns `400 Bad Request`; a body over the limit returns `413 Request Entity Too Large`.

If the same URL is submitted again, the same code is returned.

### Redirect

```http
GET /{code}
```

For example:

```text
GET /a1B2c3
```

The server returns:

```text
302 Found
```

with the following header:

```text
Location: https://example.com/path
```

---

# Part 2 — API, Errors, and Store Interface

The second part separates the HTTP layer from the shortening logic and the storage layer.

The service uses a Store interface so that different storage implementations can be used without changing the main shortening logic.

The current project has:

* an in-memory store
* a file-based store

The API also returns appropriate HTTP status codes for invalid requests and missing links.

---

## Get Link Information

```http
GET /api/v1/links/{code}
```

Example:

```text
GET /api/v1/links/a1B2c3
```

Response:

```json
{
  "url": "https://example.com/path",
  "created_at": "2026-10-07T10:20:30Z"
}
```

The response status is:

```text
200 OK
```

If the code does not exist, the server returns:

```text
404 Not Found
```

with:

```json
{
  "error": "link not found"
}
```

---

## URL Validation

Only `http` and `https` URLs are accepted.

Invalid examples include:

```text
""

"example.com"

"ftp://example.com"
```

The application does not make an HTTP request to the original URL. It only validates and stores the URL.

The URL is normalized before it is stored. Leading and trailing spaces are removed, the scheme is converted to lowercase, and the host is converted to lowercase.

As part of the bonus production-safety changes, the validation was also extended to reject some obviously unsafe destinations.

The application rejects:

* URLs containing user information
* `localhost`
* literal loopback IP addresses
* literal private IP addresses
* link-local IP addresses
* unspecified IP addresses

For example:

```text
http://localhost:8080
http://127.0.0.1
http://192.168.1.10
http://10.0.0.5
```

are rejected.

The current implementation checks literal IP addresses. It does not perform DNS resolution to detect a public hostname that resolves to a private IP.

---

## Idempotency

The same normalized long URL always gets the same short code.

For example, if the following URL is submitted:

```text
https://example.com/test
```

and the generated code is:

```text
u8RqsO
```

submitting the same URL again returns:

```text
u8RqsO
```

instead of creating another code.

This behavior is preserved when using the persistent store as well.

---

# Part 3 — Performance and Measurement

## Server Timeouts

The HTTP server uses the following timeouts:

```text
ReadTimeout:        5 seconds
ReadHeaderTimeout:  2 seconds
WriteTimeout:       5 seconds
IdleTimeout:        60 seconds
```

These limits prevent slow clients from keeping connections open indefinitely.

## RWMutex

The stores use `sync.RWMutex`.

Reading a code or looking up a URL only requires a read lock, while saving a new link requires a write lock.

This is useful because redirects and lookups are expected to happen more often than creating new short links.

## Benchmarks

I added benchmarks for both the shorten and redirect paths.

The benchmark command used was:

```powershell
go test -run '^$' -bench 'BenchmarkShorten|BenchmarkRedirect' -benchmem ./...
```

### Benchmark Environment

The measurements below were collected on:

```text
Operating system: Windows
CPU: Intel Core i9-13900H
```

### Latest Measured Results

```text
BenchmarkShorten-20    1000000    1028 ns/op     675 B/op     5 allocs/op
BenchmarkRedirect-20    577920    2077 ns/op    6419 B/op    25 allocs/op
```

`ns/op` is the average time per operation, `B/op` is the average number of bytes allocated per operation, and `allocs/op` is the average number of memory allocations per operation. These results are a baseline for this environment, not a guarantee of performance on other machines. Results can vary with Go version, hardware, and system load.

## CPU Profiling

I also ran CPU profiling for the shorten operation:

```powershell
go test -cpuprofile cpu.out -bench BenchmarkShorten -run '^$' ./internal/shortener
```

Then:

```powershell
go tool pprof -top .\cpu.out
```

In the latest profile from the verification environment, map lookups and map growth were visible costs: `runtime.mapaccess2_faststr` accounted for about 20.62% cumulative CPU time and `runtime.evacuate_faststr` for about 18.12%. The random-source system call was also visible. To reduce overhead, code generation reads random bytes in batches while retaining rejection sampling to avoid modulo bias. Profile percentages vary by machine, Go version, and workload.

---

# Part 4 — Persistence

The project has a second Store implementation called `FileStore`.

Instead of keeping the links only in memory, it stores them in a JSON Lines file.

Each line represents one saved link and contains:

```text
url
code
created_at
```

For example:

```json
{"url":"https://example.com","code":"abc123","created_at":"2026-10-07T10:20:30Z"}
```

## Run with Persistent Storage

```powershell
go run ./cmd/server -store file -data data/links.jsonl
```

When the application starts, existing records are loaded from the file.

When a new link is created, the record is written to the file and `Sync()` is called before the operation is considered successful.

## Restart Test

I tested persistence by creating a short URL and then restarting the application.

First request:

```text
https://example.com/restart-test
```

Response:

```text
u8RqsO
```

After stopping and starting the application again with the same data file, I submitted the same URL.

The response was:

```text
u8RqsO
```

This shows that the link was successfully loaded after restart and the same code was returned.

---

# Bonus Improvements Applied to Parts 1–4

Before working on the larger scalability design, I also made several production-related changes directly in the existing Parts 1–4 implementation.

These changes are part of the current codebase.

## Rate Limiting

The `POST /api/shorten` endpoint now has an in-memory rate limiter.

The current limit is:

```text
10 requests per minute per client IP
```

If the limit is exceeded, the server returns:

```text
429 Too Many Requests
```

and includes a `Retry-After` header.

For example:

```text
HTTP/1.1 429 Too Many Requests
Retry-After: 42
```

The rate limiter uses a mutex so that concurrent requests can safely update the counters.

The current implementation is per-process. If multiple application replicas are deployed, the rate-limit state would need to be moved to a shared system such as Redis.

## Graceful Shutdown

The server now listens for:

```text
SIGINT
SIGTERM
```

When a shutdown signal is received, the application uses `http.Server.Shutdown()` with a 5-second timeout.

The basic flow is:

```text
Shutdown signal
      |
      v
Graceful shutdown
      |
      v
Wait for in-flight requests
      |
      v
Stop server
```

This gives active requests a chance to finish instead of stopping the server immediately.

## Request Logging

I also added a small HTTP logging middleware.

The logs contain:

```text
method
path
status
duration
```

For example:

```text
method=POST path=/api/shorten status=201 duration=1.2ms
```

I intentionally do not log the complete URL, query string, request body, or secret values.

This is important because a long URL can contain sensitive information in its query parameters.

---

# Part 5 — Millions of Requests

For this part, I focused on what would be needed if the number of requests became much larger than what one Go process can handle.

The main idea is to keep the application instances stateless and move shared data into a shared store.

The architecture I considered is:

```text
                 Load Balancer
                       |
          +------------+------------+
          |            |            |
        App 1        App 2        App N
          |            |            |
          +------------+------------+
                       |
                 Shared Store
              PostgreSQL / Redis
```

The current project does not run a real cluster with multiple application instances. This is the architecture I would use for a larger deployment.

## Load Balancer and Multiple Go Instances

Instead of:

```text
Client -> Go App -> Store
```

the scalable version would be:

```text
Client
   |
Load Balancer
   |
+--+--+--+
|  |  |  |
Go Go Go
|  |  |  |
+--+--+--+
     |
 Shared Store
```

The load balancer distributes requests between the Go instances.

The important part is that the application instances should not keep the only copy of the links in local memory.

If each instance had its own independent in-memory store, a code created through one instance might not be available when the next request goes to another instance.

For this reason, a shared database such as PostgreSQL would be a suitable source of truth.

Redis could also be used as a shared cache for frequently accessed codes.

The trade-off is that a shared store adds network and operational overhead compared with an in-memory map, but it allows multiple application instances to work with the same data.

## CDN and Edge Caching

Redirects are mostly read operations.

For popular short codes, the redirect response could be cached at the CDN or edge layer.

The flow would become:

```text
Client
  |
 CDN / Edge
  |
  +---- cache hit ----> 302 Redirect
  |
  +---- cache miss ---> Load Balancer
                            |
                         Go App
                            |
                       Shared Store
```

For example:

```text
/a1B2c3 -> https://example.com/path
```

could be cached for a limited TTL.

A longer TTL reduces the number of requests reaching the application and database, but it also means that a changed redirect target would take longer to become visible.

Since the current application does not provide an update operation for links, stale redirect data is less of a problem. I would still use a reasonable TTL instead of caching redirects forever.

CDN caching is an architectural decision for the high-traffic version and is not currently enabled in the local project.

## Write Path Scaling

Creating a short URL is different from redirecting one because it modifies shared state.

The current application already limits requests to:

```text
POST /api/shorten
```

This helps prevent one client from sending an unreasonable number of create requests.

For a multi-instance deployment, I would move the rate-limit state to Redis so all application instances use the same limit.

Another possible improvement for a very high write load would be an asynchronous queue:

```text
Client
  |
 API
  |
Queue
  |
Workers
  |
Shared Store
```

The queue can help absorb sudden write spikes and protect the database.

The trade-off is that the system becomes more complicated and the result may not be available immediately. For the current project, I kept the create operation synchronous because the existing application does not need a queue.

## Sharding and Partitioning

If the number of stored links becomes extremely large, one database may eventually become a bottleneck.

A possible sharding strategy is to partition links using a hash of the short code:

```text
hash(code) % N
```

For example:

```text
Shard 0
Shard 1
Shard 2
...
Shard N
```

The application can calculate the shard from the code and send the lookup to the correct database.

This works well for redirect requests because the short code is already available when the request arrives.

The trade-off is complexity. Sharding makes the system harder to operate and makes some queries more difficult.

I would only introduce it when a single shared database is no longer enough.

## Part 5 Summary

The main scalability decisions are:

* Load balancer in front of multiple Go application instances
* Shared PostgreSQL/Redis storage instead of per-instance memory
* CDN/edge caching for popular redirects
* Rate limiting on the write path
* Optional asynchronous queue for very high write traffic
* Hash-based sharding when a single database is no longer enough

These are architecture decisions for a larger deployment. The current project does not claim to have a real Kubernetes cluster, CDN, or sharded database.

---

# Part 6 — Production Habits

## Graceful Shutdown

Graceful shutdown is implemented in the current application.

The server listens for `SIGINT` and `SIGTERM` and uses:

```go
server.Shutdown(ctx)
```

with a 5-second timeout.

This gives in-flight requests time to finish before the application exits.

## Rate Limit on Create

The current implementation limits:

```text
POST /api/shorten
```

to:

```text
10 requests per minute per client IP
```

When the limit is exceeded:

```text
429 Too Many Requests
```

is returned together with a `Retry-After` header.

This is currently an in-memory limiter.

For a deployment with multiple replicas, I would use a shared Redis-based limiter.

## URL Safety Policy

The URL validation also has a basic safety policy.

The application rejects:

* Empty or invalid URLs
* Non-HTTP schemes
* URLs containing user information
* `localhost`
* Literal loopback, private, link-local, and unspecified IP addresses
* Domains configured in the blocklist

Configure the blocklist before starting the application. The value is a comma-separated list of hostnames (not full URLs):

PowerShell:

```powershell
$env:URL_BLOCKLIST_DOMAINS = "bad.example,phishing.example"
go run ./cmd/server
```

Bash:

```bash
URL_BLOCKLIST_DOMAINS="bad.example,phishing.example" go run ./cmd/server
```

Domain matching is case-insensitive, ignores a trailing dot, and includes subdomains. For example, if `bad.example` is blocked, `shop.bad.example` is blocked too, but `notbad.example` is not matched accidentally. The configuration is read at startup; restart the application after changing it.

This reduces obvious cases where the shortener could be used to redirect users to local/private network addresses and lets an operator block domains known to be unsafe. It is not a complete phishing detector; effectiveness depends on maintaining the configured list.

The current implementation does not perform DNS resolution. A hostname that resolves to a private IP may therefore bypass the literal-IP checks. A production implementation needs carefully designed DNS-aware validation as well.

## Logging

The current HTTP logging middleware records:

```text
method
path
status
duration
```

It does not log:

* Full destination URLs
* Query strings
* Request bodies
* Passwords or tokens
* Other secret values

For example, a request is logged as:

```text
method=POST path=/api/shorten status=201 duration=1.2ms
```

instead of logging the full submitted URL.

This makes the logs useful for debugging and basic performance checking while avoiding unnecessary exposure of user data.

## Production Improvements Not Implemented

Some production features are useful but were intentionally not implemented in this small project.

These include:

* Prometheus metrics
* Distributed Redis rate limiting
* Structured JSON logging
* CDN configuration
* Database sharding
* Asynchronous write queues
* A real multi-instance deployment

These are described in the scalability and production decisions, but I do not consider them implemented features of the current local application.

---

# Testing

Run all tests with:

```powershell
go test ./...
```

Run the tests with the race detector:

```powershell
go test -race ./...
```

Run `go vet`:

```powershell
go vet ./...
```

Run coverage:

```powershell
go test -cover ./...
```

Measure statement coverage across all packages (including `cmd/server`) with:

```powershell
go test "-coverprofile=coverage.out" ./...
go tool cover "-func=coverage.out"
```

The latest verification run performed on Windows reported:

```text
cmd/server:          0.0%
internal/httpapi:    96.3%
internal/shortener:  92.1%
internal/store:      88.2%
total:               78.2%
```

The following checks completed successfully in that run:

```powershell
go vet ./...
go test ./...
go test -race ./...
go test -coverprofile="./coverage.out" ./...
go tool cover -func="./coverage.out"
```

In the latest run reported here, total statement coverage was `78.2%`, above the required `70.0%`. The `cmd/server` entry point has 0% direct unit-test coverage, while the overall total still passes the requirement. Coverage can change when tests or Go versions change, so rerun the commands below after any code changes.

The final verification commands are:

```powershell
gofmt -w .
go vet ./...
go test ./...
go test -race ./...
go test "-coverprofile=coverage.out" ./...
go tool cover "-func=coverage.out"
```

Include the actual results from the final run before submitting.

---

# Current Status

Parts 1–4 are implemented.

The bonus production improvements applied to the existing implementation include:

* Stronger URL validation
* Protection against localhost and literal private/loopback IP destinations
* Rate limiting on `POST /api/shorten`
* `429 Too Many Requests` responses
* `Retry-After` header
* Graceful shutdown
* Basic HTTP request logging
* Avoiding full URLs and query strings in logs

Part 5 scalability decisions are documented for a larger deployment, including:

* Load balancing
* Multiple Go application instances
* Shared storage
* CDN/edge caching
* Write-path scaling
* Sharding and partitioning

The application currently supports:

* Creating short URLs
* Redirecting short URLs
* Looking up link metadata
* URL validation
* Idempotency
* Collision handling
* Concurrent access using `RWMutex`
* HTTP server timeouts
* Benchmarks
* CPU profiling
* In-memory storage
* Persistent file storage
* Loading data after restart
* Persistence tests
* Race detection
* Test coverage
* Graceful shutdown
* Rate limiting
* URL safety checks
* HTTP request logging

The scalability features such as a real load balancer, CDN, Redis, PostgreSQL, asynchronous queue, and database sharding are documented as the next step for a larger deployment and are not claimed as part of the current local implementation.
