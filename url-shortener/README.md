# URL Shortener

A small HTTP service written in Go that shortens long URLs and redirects short codes to the original URLs.

The project was implemented in four required parts:

* Part 1 — MVP: shorten and redirect
* Part 2 — API, errors, and Store interface
* Part 3 — Performance and measurement
* Part 4 — Persistence

The application supports both an in-memory store and a file-based persistent store.

## Requirements

* Go 1.22 or newer

No external database or additional package is required.

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
│   │   └── file_persistence_test.go
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

### Get Link Information

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

## Part 3 — Performance

### Server Timeouts

The HTTP server uses the following timeouts:

```text
ReadTimeout:       5 seconds
ReadHeaderTimeout: 2 seconds
WriteTimeout:      5 seconds
IdleTimeout:       60 seconds
```

These limits prevent slow clients from keeping connections open indefinitely.

### RWMutex

The stores use `sync.RWMutex`.

Reading a code or looking up a URL only requires a read lock, while saving a new link requires a write lock.

This is useful because redirects and lookups are expected to happen more often than creating new short links.

### Benchmarks

I added benchmarks for both the shorten and redirect paths.

The benchmark command used was:

```powershell
go test -run '^$' -bench 'BenchmarkShorten|BenchmarkRedirect' -benchmem ./internal/shortener ./internal/httpapi
```

The measured results were:

```text
BenchmarkShorten-20       1235802       895.9 ns/op       593 B/op      5 allocs/op
BenchmarkRedirect-20       557762      2006 ns/op        6419 B/op     25 allocs/op
```

The exact results can change depending on the computer and system load.

### CPU Profiling

I also ran CPU profiling for the shorten operation:

```powershell
go test -cpuprofile cpu.out -bench BenchmarkShorten -run '^$' ./internal/shortener
```

Then:

```powershell
go tool pprof -top .\cpu.out
```

One of the main observations was that the store operations and map access take a significant part of the execution time.

For example, `Store.Save` had about 34.98% cumulative time and `Store.GetCode` had about 13.58% cumulative time.

The profile also showed time spent in URL parsing and code generation.

## Part 4 — Persistence

The project has a second Store implementation called `FileStore`.

Instead of keeping the links only in memory, it stores them in a JSON Lines file.

The store loads these records at startup. If a crash leaves an invalid, unterminated final JSONL record, startup truncates that incomplete tail before accepting new writes. Invalid records earlier in the file are treated as corruption and cause startup to fail.

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

### Run with Persistent Storage

```powershell
go run ./cmd/server -store file -data data/links.jsonl
```

When the application starts, existing records are loaded from the file.

When a new link is created, the record is written to the file and `Sync()` is called before the operation is considered successful.

### Restart Test

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

## Testing

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

Coverage was re-run after the persistence recovery test was added. The measured results in the development environment were:

```text
cmd/server:          0.0% (no package tests)
internal/httpapi:   88.2%
internal/shortener:  88.0%
internal/store:     86.9%
Total:              76.0%
```

The total statement coverage is above the required 70%. These numbers can vary when tests change, so re-run the command below before submission and update this section if the results differ:

```powershell
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```

In the latest verification run on Windows PowerShell, `go test ./...`, `go vet ./...`, and `go test -race ./...` all passed. The measured total statement coverage was 76.0%. The Go version used for that local run should be recorded with `go version` if this README is updated for a formal submission.

## Current Status

Parts 1–4 are implemented.

The application supports:

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
