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
* `os` and `path/filepath` for persistent file storage
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

This makes the shorten operation idempotent.

### Code Generation

For a new URL, I generate a random six-character code.

The allowed characters are:

```text
abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789
```

The code is generated using `crypto/rand`.

Only new URLs get a newly generated code.

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

This allows multiple read operations to happen at the same time.

### Package Layout

The project is divided into three main internal packages:

```text
internal/httpapi
internal/shortener
internal/store
```

The `httpapi` package is responsible for HTTP handlers and JSON responses.

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

### JSON Responses

The API uses JSON for request and response bodies.

The create endpoint returns:

```json
{
  "code": "a1B2c3",
  "short_url": "http://localhost:8080/a1B2c3"
}
```

The metadata endpoint returns:

```json
{
  "url": "https://example.com/path",
  "created_at": "2026-01-15T12:00:00Z"
}
```

Errors are also returned as JSON:

```json
{
  "error": "link not found"
}
```

### Idempotency Through the Store

The idempotency rule from Part 1 was kept after introducing the Store interface.

The service first checks:

```text
GetCode(url)
```

before generating a new code.

Therefore, the same URL still returns the same code even though the service now works through the Store interface.

## Part 3

### Locking Choice

I kept using `sync.RWMutex`.

The application has many read operations such as redirects and metadata lookups, while creating a new short URL is less frequent.

Using `RWMutex` allows concurrent readers while still protecting writes.

Both the in-memory and persistent stores use locks around their shared maps.

### HTTP Server Timeouts

I added the following timeouts to `http.Server`:

```text
ReadTimeout:       5 seconds
ReadHeaderTimeout: 2 seconds
WriteTimeout:      5 seconds
IdleTimeout:       60 seconds
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
BenchmarkShorten-20       1235802       895.9 ns/op       593 B/op      5 allocs/op
BenchmarkRedirect-20       557762      2006 ns/op        6419 B/op     25 allocs/op
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

One of the main things I noticed was that the store operations take a noticeable part of the execution time.

For example:

```text
Store.Save       34.98% cumulative
Store.GetCode    13.58% cumulative
```

The profile also showed time spent in:

```text
net/url.Parse
normalizeURL
makeCode
```

This shows that the shorten operation is not only spending time generating the code, but also in URL parsing and accessing the maps.

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

When loading the file, an incomplete final record can be ignored. This helps with a case where the process stops while the last record is being written.

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

All tests passed successfully.

### Coverage

I checked coverage using:

```powershell
go test -cover ./...
```

The latest coverage run on Windows PowerShell used:

```powershell
go test "-coverprofile=coverage.out" ./...
go tool cover "-func=coverage.out"
```

The measured statement coverage was:

```text
cmd/server:          0.0% (no package tests)
internal/httpapi:   88.2%
internal/shortener:  88.0%
internal/store:     86.9%
Total:              76.0%
```

The project-wide total is above the required 70% statement-coverage threshold. `cmd/server` has no direct tests, so its package coverage is 0.0%; the overall total still passes the project requirement. Coverage percentages can change when code or tests change, so these figures should be regenerated before a final submission.
