# Design Decisions

## Dependencies

The project uses only the Go standard library.

No additional third-party modules were added.

The main packages used are:

* `net/http` for the HTTP server and routing
* `encoding/json` for JSON request, response, and file data
* `sync` for concurrent access to the stores
* `crypto/rand` for generating short codes
* `net/url` for URL parsing and validation
* `net` for IP address validation
* `os` and `path/filepath` for persistent file storage
* `context`, `os/signal`, and `syscall` for graceful shutdown
* `testing` for tests and benchmarks

I chose the standard library because the project requirements could be implemented without an additional dependency.

## Part 1

### URL Normalization

Before storing a URL, I trim leading and trailing spaces.

I also convert the scheme and host to lowercase.

For example:

```text
HTTPS://Example.COM/test
```

becomes:

```text
https://example.com/test
```

I did not remove trailing slashes or change the path because I wanted to keep the normalization simple and avoid changing the meaning of the URL.

### Same URL → Same Code

I keep a separate map from the normalized long URL to its short code:

```text
URL → Code
```

Before generating a new code, the service checks this map.

If the URL already exists, its existing code is returned.

This makes the shorten operation idempotent. It also means that submitting the same URL multiple times does not create multiple short links.

### Code Generation

For a new URL, I generate a random six-character code.

The allowed characters are:

```text
abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789
```

The code is generated using `crypto/rand`. Because 256 possible byte values cannot be divided evenly into 62 characters, generation rejects byte values outside the largest divisible range instead of using biased modulo mapping. Random bytes are read in batches rather than requesting a fresh random byte for each character, reducing system-call overhead while preserving unbiased selection.

Only new URLs get a newly generated code.

I chose six characters because it keeps the short URL small while still providing a large number of possible combinations.

### Collision Handling

There is a separate map for:

```text
Code → Link
```

Before accepting a generated code, `Save` checks whether the code already exists.

If a collision happens, the service generates another code and tries again.

The service makes up to 100 attempts to create a unique code.

If it cannot create a unique code after those attempts, it returns an error.

This prevents two different URLs from using the same code.

### Mutex Choice

I used `sync.RWMutex` instead of a normal `sync.Mutex`.

The main reason is that reading is expected to happen more often than creating a new link.

`GetCode` and `GetURL` use a read lock:

```text
RLock
```

while `Save` uses a write lock:

```text
Lock
```

This allows multiple read operations to happen at the same time while still protecting the maps from concurrent writes.

### Package Layout

The project is divided into three main internal packages:

```text
internal/httpapi
internal/shortener
internal/store
```

The `httpapi` package is responsible for HTTP handlers, request validation, and JSON responses.

The `shortener` package contains the main URL shortening logic.

The `store` package contains the storage implementations.

The server entry point is kept in:

```text
cmd/server/main.go
```

I kept `main` small so that the main application logic stays inside the internal packages.

## Part 2

### Store Interface

The storage interface is defined in the `shortener` package because the service is the part that depends on the storage behavior.

The interface contains:

```text
GetCode(url string) (string, bool)
GetURL(code string) (store.Link, error)
Save(url, code string) bool
```

The service does not need to know how the data is actually stored.

This allows the in-memory store and the persistent file store to be used with the same service.

### Errors

I defined sentinel errors for the main domain errors:

```text
ErrInvalidURL
ErrNotFound
```

The store also has:

```text
store.ErrNotFound
```

When an error moves through the service, it is wrapped using `%w`.

The HTTP handlers use `errors.Is` to check the original error.

For example:

* Invalid URL → `400 Bad Request`
* Unknown code → `404 Not Found`
* Unexpected storage or server error → `500 Internal Server Error`

This keeps the domain errors separate from the HTTP layer.

### JSON Responses

The API uses JSON for request and response bodies. The create endpoint caps request bodies at 1 MiB, rejects unknown JSON fields, and requires exactly one JSON object. This prevents unbounded body reads and avoids silently accepting ambiguous input. Oversized bodies return `413 Request Entity Too Large`; malformed or ambiguous JSON returns `400 Bad Request`.

The create endpoint in the current implementation returns:

```json
{
  "code": "a1B2c3",
  "short_url": "http://localhost:8080/a1B2c3"
}
```

The metadata endpoint returns the stored URL, code, and creation time.

Errors are returned as JSON:

```json
{
  "error": "link not found"
}
```

I kept the response format simple because the API only needs a small amount of information.

### Idempotency Through the Store

The idempotency rule from Part 1 was kept after introducing the Store interface.

The service first checks:

```text
GetCode(url)
```

before generating a new code.

Therefore, the same URL still returns the same code even though the service now works through the Store interface.

This behavior is also important for the persistent store because it must continue working after a restart.

### HTTP API Separation

The HTTP handler does not directly manipulate the store maps.

The flow is:

```text
HTTP Request
     |
     v
HTTP Handler
     |
     v
Shortener Service
     |
     v
Store Interface
     |
     v
MemoryStore / FileStore
```

This keeps HTTP concerns separate from the URL shortening logic and storage implementation.

## Part 3

### Locking Choice

I kept using `sync.RWMutex`.

The application has many read operations such as redirects and metadata lookups, while creating a new short URL is less frequent.

Using `RWMutex` allows concurrent readers while still protecting writes.

Both the in-memory and persistent stores use locks around their shared maps and related operations.

### HTTP Server Timeouts

I added the following timeouts to `http.Server`:

```text
ReadTimeout:        5 seconds
ReadHeaderTimeout:  2 seconds
WriteTimeout:       5 seconds
IdleTimeout:        60 seconds
```

The goal is to avoid connections staying open forever because of slow clients.

`ReadHeaderTimeout` is shorter because reading request headers should normally be very quick.

The other values give normal requests enough time while still limiting slow connections.

### Benchmarks

I added two benchmarks:

```text
BenchmarkShorten
BenchmarkRedirect
```

The benchmark command used was:

```powershell
go test -run '^$' -bench 'BenchmarkShorten|BenchmarkRedirect' -benchmem ./internal/shortener ./internal/httpapi
```

The measured results were:

```text
BenchmarkShorten-3        637323       2583 ns/op        664 B/op       7 allocs/op

BenchmarkRedirect-3       351140       3645 ns/op       6481 B/op      30 allocs/op
```

These numbers depend on the machine running the benchmark, so they are mainly useful as a baseline for this implementation.

### CPU Profiling

I used CPU profiling for the shorten operation:

```powershell
go test -cpuprofile cpu.out -bench BenchmarkShorten -run '^$' ./internal/shortener
```

and then:

```powershell
go tool pprof -top .\cpu.out
```

The latest profile showed noticeable time in map lookups and map growth, as well as random-source calls. `runtime.mapaccess2_faststr` accounted for about 20.62% cumulative CPU time and `runtime.evacuate_faststr` for about 18.12% in that run. I therefore changed code generation to read random bytes in batches, while keeping rejection sampling to avoid modulo bias. The profile is only a snapshot; results vary with Go version, hardware, and workload.

### Eviction

I did not add a maximum number of links or an eviction policy.

The required project is a small single-process URL shortener, and adding eviction would make the behavior more complicated without being necessary for the required parts.

For a larger production system, a different storage design would be more appropriate.

## Part 4

### Storage Choice

For persistent storage, I chose a file-backed store instead of using GORM and SQL.

The project only needs simple persistence, so a file-based implementation was enough for the required behavior.

It also allowed me to keep the project dependency-free and use the same Store interface as the in-memory implementation.

The persistent implementation is called:

```text
FileStore
```

### File Format

I used JSON Lines for the persistent file.

Each line contains one JSON record.

A record contains:

```text
url
code
created_at
```

For example:

```json
{"url":"https://example.com","code":"abc123","created_at":"2026-10-07T10:20:30Z"}
```

I chose this format because a new record can be appended to the file instead of rewriting the complete file every time.

### Startup Loading

When `FileStore` is created, it opens the configured file and loads the existing records.

The records are loaded into the same two indexes used by the memory store:

```text
URL → Code
Code → Link
```

This means that the service can use the persistent store without needing a different implementation of the shortening logic.

### Persist Before 201

A new link must be persisted before the HTTP handler returns `201 Created`.

In `FileStore.Save`, the record is first encoded as JSON and written to the file.

After writing, `Sync()` is called on the file.

Only after the write and synchronization succeed are the in-memory maps updated.

This means that the service does not treat the link as successfully saved if writing or syncing the persistent file fails.

### Crash Safety

Before writing a new record, the previous file size is saved.

If the write fails or does not write the complete record, the file is truncated back to the previous size when possible.

If `Sync()` fails, the code also attempts to restore the previous file size.

When loading the file, each committed JSONL record must end with a newline. If the file ends with an unterminated final record, the store truncates the file to the last newline using the already-open file handle, then calls `Sync()`. This avoids relying on a second path-based truncate while the file is open (which can fail on Windows). A malformed record that is newline-terminated is treated as corruption and causes startup to fail instead of being silently ignored. This recovery policy preserves all complete records and discards only the incomplete tail.

### Atomicity

The persistent store uses append-based writes.

It does not rewrite the whole data file whenever a new link is created.

A new record is considered committed by the application after:

1. The JSON record has been written.
2. The file has been synchronized using `Sync()`.
3. The in-memory indexes have been updated.

I chose this approach because it is simple and works well for the size and scope of this project.

### Created At

The `created_at` value is stored as a `time.Time`.

The store creates it using UTC:

```text
time.Now().UTC()
```

The value is then encoded by `encoding/json`.

When the application restarts, the timestamp is loaded from the file instead of being generated again.

Therefore, the original creation time of a link stays the same after restart.

### Idempotency After Restart

Idempotency from Part 1 must also work after restarting the application.

When the persistent store loads the file, it rebuilds the URL-to-code map.

Therefore, if the same normalized URL is submitted after a restart, the existing code is found:

```text
URL → Code
```

and the service returns that code instead of generating a new one.

I tested this behavior by creating a link, stopping the application, starting it again with the same data file, and submitting the same URL.

The same short code was returned after the restart.

### Store Selection

I added a command-line flag to select the storage implementation:

```text
-store memory
-store file
```

The persistent file path can be specified with:

```text
-data data/links.jsonl
```

This makes it possible to switch between memory and persistent storage without changing the source code.

### Persistence Tests

The persistence tests use `t.TempDir()`.

This means that the tests create their files inside temporary directories and do not modify the real application data.

The tests cover:

* Saving and loading a link
* Restart behavior
* Duplicate URLs
* Duplicate codes
* `created_at`
* UTC timestamps
* Idempotency after restart
* Persisting data before returning success

For the restart test, one `FileStore` instance writes the data and is then closed.

A second `FileStore` instance is created using the same file.

The second instance must be able to read the data written by the first instance.

### Race Testing

The persistent store uses the same locking approach as the in-memory store.

The shared maps and file operations are protected by the store mutex.

The following command was used to check for race conditions:

```powershell
go test -race ./...
```

The test suite passed successfully in the development environment.

### Coverage

I checked coverage using:

```powershell
go test -cover ./...
```

The latest verification run performed on Windows reported:

```text
cmd/server:          0.0%
internal/httpapi:    96.3%
internal/shortener:  90.0%
internal/store:      88.5%
total:               77.5%
```

The following checks completed successfully:

```powershell
go vet ./...
go test ./...
go test -race ./...
go test -coverprofile="./coverage.out" ./...
go tool cover -func="./coverage.out"
```

The total statement coverage is above the required 70%. The `cmd/server` entry point has no direct unit-test coverage, but the overall total still satisfies the requirement. After any code changes, these checks should be run again to confirm the final result.

## Part 5

### Scale-out Architecture

For a system that needs to handle millions of requests, I would not keep the application data only inside one Go process.

The application would run as multiple stateless Go replicas behind a load balancer:

```text
                    Load Balancer
                         |
          +--------------+--------------+
          |              |              |
      Go App #1       Go App #2      Go App #N
          |              |              |
          +--------------+--------------+
                         |
                    Shared Store
                  PostgreSQL / Redis
```

The Go application instances would not depend on their local memory as the source of truth.

All instances would use a shared store so that a request handled by any replica can resolve the same short code.

The load balancer would distribute incoming requests between the Go replicas.

This makes it possible to add more application instances when traffic increases.

The current project still uses the in-memory store or the local JSONL file store, so this is an architecture decision for a larger deployment rather than a fully implemented multi-instance deployment.

The main tradeoff is that a shared store introduces more infrastructure and network latency compared with local memory, but it is necessary when multiple application instances need consistent shared data.

### CDN / Edge Caching

The read path is likely to receive a large number of requests because popular short links can be accessed many times.

For this reason, redirect responses can be cached at a CDN or edge layer.

A possible request flow would be:

```text
Client
  |
  v
CDN / Edge
  |
  +---- Cache Hit ----> 302 Redirect
  |
  +---- Cache Miss ---> Load Balancer
                            |
                            v
                         Go App
                            |
                            v
                       Shared Store
```

For example, a request to:

```text
GET /a1B2c3
```

could return a cached `302 Found` response if the short code is already stored at the edge.

I would use a TTL for these cached redirects.

A longer TTL reduces load on the application and database, but it also means that a changed or deleted link may remain cached for longer.

For this project, links are effectively immutable after creation, so caching redirects is a good fit.

The current implementation does not include an actual CDN configuration. This section describes the production architecture that I would use when scaling the service.

### Write-path Scaling

Creating short URLs is a write operation, so increasing the number of Go replicas alone does not completely solve write scalability.

The current implementation already has a simple protection mechanism: the `POST /api/shorten` endpoint is rate-limited per client IP.

This prevents one client from continuously generating a very large number of new links.

For a much larger system, another option would be to use an asynchronous queue:

```text
Client
  |
  v
API Server
  |
  v
Write Queue
  |
  v
Workers
  |
  v
Shared Store
```

The API could put create requests into a queue and workers could process them independently.

This would help absorb traffic spikes and prevent the database from being overwhelmed by a sudden burst of writes.

Another possible optimization would be pre-generating short codes.

A pool of available codes could be created in advance and consumed when new links are created.

The tradeoff is that queues and pre-generated code pools add infrastructure and operational complexity.

For the current project, the simpler rate-limit approach is enough, while a queue would make more sense at much higher traffic levels.

### Sharding / Partitioning

If the number of stored links becomes extremely large, a single database can eventually become a bottleneck.

The short code can be used as a natural partitioning key.

For example:

```text
hash(short_code) % N
```

can determine which shard contains the link.

A possible architecture would be:

```text
                    Go Application
                          |
                    Hash(short_code)
                          |
          +---------------+---------------+
          |               |               |
       Shard 0         Shard 1         Shard N
          |               |               |
       Database        Database        Database
```

Requests for different short codes would therefore be distributed across different database shards.

The main advantage is that storage and read/write load can be distributed instead of keeping everything on one database server.

The main disadvantage is increased complexity.

Queries that need data from multiple shards become harder, shard rebalancing becomes more complicated, and operational work increases.

For this URL shortener, sharding is therefore considered a future scaling strategy rather than part of the current implementation.

### Part 5 Summary

The production-scale architecture I would use is:

```text
                         Internet
                            |
                            v
                     Load Balancer
                            |
                 +----------+----------+
                 |          |          |
                 v          v          v
              Go App     Go App     Go App
                 |          |          |
                 +----------+----------+
                            |
                    Shared Data Layer
                     /              \
                Redis/CDN         Database
                                   / | \
                                Shard  Shard
```

The main decisions are:

* Keep application instances stateless.
* Put multiple Go replicas behind a load balancer.
* Use a shared persistent store instead of local process memory.
* Cache popular redirects at the CDN/edge.
* Protect the create endpoint with rate limiting.
* Use an asynchronous queue or pre-generated code pool if write traffic becomes very high.
* Partition or shard the data when a single database is no longer sufficient.

The current project implements only the parts that are directly useful for the assignment, such as rate limiting.

The load balancer, CDN, queue, Redis, and sharding are documented as the architecture for a larger production deployment and are not claimed as implemented features.

## Part 6

### Graceful Shutdown

The server should not immediately terminate when it receives a shutdown signal because there may be requests currently being processed.

The current implementation listens for `SIGINT` and `SIGTERM` and uses `http.Server.Shutdown()` with a timeout.

The shutdown flow is:

```text
Shutdown Signal
      |
      v
Stop accepting new requests
      |
      v
Wait for in-flight requests
      |
      v
Timeout reached or requests finished
      |
      v
Server exits
```

A five-second timeout is used for the shutdown process.

This allows active requests to finish normally instead of being interrupted immediately.

The main tradeoff is between giving requests enough time to finish and shutting the process down quickly.

A very short timeout can terminate valid requests, while a very long timeout can delay deployment or restart operations.

### Rate Limiting

The `POST /api/shorten` endpoint is protected by a simple per-IP rate limiter.

The current configuration is:

```text
10 requests per IP
per 1 minute
```

When a client reaches the limit, the server returns:

```text
HTTP 429 Too Many Requests
```

and includes a `Retry-After` header indicating approximately how many seconds the client should wait.

The limiter keeps the counters in memory and periodically removes expired entries to avoid unbounded growth from clients that never return:

```text
Client IP
   |
   v
Rate Limiter
   |
   +---- under limit ----> allow request
   |
   +---- over limit -----> HTTP 429
```

This is intentionally simple and does not require an external dependency.

The tradeoff is that the current limiter works correctly for a single application process, but its counters are not shared between multiple replicas.

In a multi-instance deployment, I would move the rate-limit state to a shared system such as Redis so that all application instances enforce the same limit.

### URL Safety Policy

The service only accepts URLs using `http` or `https`.

It rejects:

* Empty URLs
* URLs without a valid host
* Unsupported schemes
* URLs containing user information
* `localhost`
* Loopback IP addresses
* Private IP addresses
* Link-local IP addresses
* Unspecified IP addresses
* Hostnames explicitly included in the configured domain blocklist

The domain blocklist is configured at startup with the `URL_BLOCKLIST_DOMAINS` environment variable. It is a comma-separated list of hostnames only (not full URLs, paths, ports, or query strings). For example:

```text
URL_BLOCKLIST_DOMAINS=bad.example,phishing.example
```

A match is case-insensitive and ignores a trailing DNS dot. A blocked domain also blocks its subdomains: `shop.bad.example` is blocked when `bad.example` is listed. Matching is performed at a DNS-label boundary, so `notbad.example` is not blocked just because `bad.example` is on the list. The list is loaded when the service starts, so a changed environment variable requires a restart.

Examples of rejected addresses include:

```text
http://localhost:8080
http://127.0.0.1
http://10.0.0.1
http://192.168.1.1
https://bad.example/path   # when bad.example is in URL_BLOCKLIST_DOMAINS
https://shop.bad.example   # subdomain of a blocked domain
```

This policy reduces obvious internal-network destinations and allows an operator to explicitly block domains known to be unsafe. It is not a phishing-classification service, and the blocklist is only as current as the list supplied by the operator.

Hostname DNS resolution is not performed before accepting a URL. Therefore, a public-looking hostname that resolves to a private IP is not detected by this validation layer. DNS rebinding and changes in DNS answers also require additional production-level controls.

The tradeoff is between stronger protection and operational complexity: maintaining the list requires updates, while DNS-aware validation adds lookups and must be designed carefully to avoid time-of-check/time-of-use issues.

### Logging and Observability

The current HTTP middleware logs basic request information:

```text
method
path
status
duration
```

For example:

```text
method=POST path=/api/shorten status=201 duration=2.1ms
```

The logger intentionally does not log:

* The full requested URL
* Query strings
* Request bodies
* Authentication tokens
* Passwords
* Other potentially sensitive values

This is important because URLs may contain sensitive information in query parameters.

The current logging is intentionally lightweight.

It is not claimed to be full structured logging or a complete metrics system.

For a production deployment, I would add metrics such as:

* Requests per second (RPS)
* Request latency
* HTTP status code counts
* Error rate
* Rate-limit responses
* Redirect success/not-found counts

These metrics could then be exposed to a monitoring system such as Prometheus.

### Additional Production Observability

For debugging performance problems, Go's `pprof` can be useful.

It could be enabled behind a configuration flag and exposed only on a protected internal endpoint rather than being publicly available.

A possible production setup would be:

```text
Application
    |
    +---- Normal HTTP API
    |
    +---- Internal metrics / pprof endpoint
                    |
                    v
              Monitoring System
```

I did not add a public `pprof` endpoint to the current implementation because exposing debugging endpoints without access control would not be appropriate for a public service.

### Security and Logging Tradeoffs

The main production security decisions are:

1. Validate destination URLs before creating short links.
2. Reject local and private network destinations.
3. Do not log full URLs or request bodies.
4. Rate-limit link creation.
5. Use HTTPS in a real deployment.
6. Keep debugging and profiling endpoints internal.

The goal is to avoid solving one problem, such as observability, by creating another problem such as leaking sensitive information.

### What Is Implemented vs. Architecture Only

The following production-habit features are implemented in the current project:

* Graceful shutdown with `SIGINT` and `SIGTERM`
* Shutdown timeout
* Per-IP rate limiting on `POST /api/shorten`
* `429 Too Many Requests`
* `Retry-After` response header
* URL safety validation
* Basic HTTP request logging
* Avoiding full URL/query-string logging

The following items are documented as future production improvements and are not implemented in the current project:

* Load balancer with multiple Go replicas
* Shared Redis/PostgreSQL deployment
* CDN/edge redirect caching
* Distributed rate limiting with Redis
* Asynchronous write queue
* Pre-generated code pools
* Database sharding
* Prometheus metrics
* Public production `pprof` endpoint
* Full structured JSON logging

### Academic Integrity

This project was implemented as my own work.

AI tools were used only as a supporting resource for understanding concepts, checking implementation details, debugging, and discussing design tradeoffs.

The final implementation, testing, and project decisions were reviewed and applied by me.
