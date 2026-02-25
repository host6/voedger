# Implementation plan: VSQL BLOB reading via blobinfo() and blobtext()

## Functional design

- [x] create: [apps/vsql-blob-read.feature](../../specs/prod/apps/vsql-blob-read.feature)
  - add: Scenarios for `blobinfo()` returning JSON metadata (name, mimetype, size, status)
  - add: Scenarios for `blobtext()` returning blob content (base64 for binary, plain text otherwise, limited to 10000 bytes, optional startFrom)
  - add: Constraint scenarios: blob functions require docID or singleton, WHERE clause rejected

## Construction

### Blobber: expose size header

- [x] update: [pkg/coreutils/consts.go](../../pkg/coreutils/consts.go)
  - add: `BlobSize` constant for `X-BLOB-Size` header name
- [x] update: [pkg/processors/blobber/impl_read.go](../../pkg/processors/blobber/impl_read.go)
  - update: `initResponse` to emit `X-BLOB-Size` header from `bw.blobState.Size`

### SqlQuery: parse blob functions and wire blobprocessor

- [x] update: [pkg/sys/sqlquery/provide.go](../../pkg/sys/sqlquery/provide.go)
  - update: `Provide` signature to accept `*blobprocessor.IRequestHandler` and `*bus.RequestHandler`; pass them to `provideExecQrySQLQuery`
- [x] update: [pkg/sys/sysprovide/provide.go](../../pkg/sys/sysprovide/provide.go)
  - update: `ProvideStateless` to accept `*blobprocessor.IRequestHandler` and `*bus.RequestHandler`, pass them to `sqlquery.Provide`
- [x] update: [pkg/sys/sqlquery/impl.go](../../pkg/sys/sqlquery/impl.go)
  - update: `provideExecQrySQLQuery` to accept `*blobprocessor.IRequestHandler` and `*bus.RequestHandler`
  - add: Parse `*sqlparser.FuncExpr` in SELECT walk to detect `blobinfo`/`blobtext` and extract field name + optional `startFrom`
  - add: Reject WHERE clause when blob functions are present
  - add: Validate blob functions require docID or singleton
  - add: Call `IRequestHandler.HandleRead_V2` with a locally-created `bus.IRequestSender` for each blob function; build `blobinfo` JSON / `blobtext` content from captured response headers and writer
- [x] create: [pkg/sys/sqlquery/impl_blobfuncs.go](../../pkg/sys/sqlquery/impl_blobfuncs.go)
  - add: `blobFuncDesc` struct and parsing logic (`parseBlobFuncExpr`)
  - add: `executeBlobFunctions`, `executeBlobInfo`, `executeBlobText` functions
  - add: `limitedBlobWriter` for capped byte capture with offset skip
  - add: `mergeJSONWithBlobResults` for combining record data with blob results

### VVM wiring

- [x] update: [pkg/vvm/provide.go](../../pkg/vvm/provide.go)
  - update: `provideStatelessResources` to accept and pass `*blobprocessor.IRequestHandler` and `*bus.RequestHandler`
- [ ] Review
- [x] update: [pkg/vvm/wire_gen.go](../../pkg/vvm/wire_gen.go)
  - update: Create `blobHandlerPtr` and `requestHandlerPtr` before `provideStatelessResources`; fill them after `NewIRequestHandler` / `provideRequestHandler`

### Tests

- [ ] update: [pkg/sys/it/impl_sqlquery_test.go](../../pkg/sys/it/impl_sqlquery_test.go)
  - add: Integration tests for `blobinfo()` on a doc with blob field
  - add: Integration tests for `blobtext()` with text and binary blobs, with and without `startFrom`
  - add: Integration tests for `blobinfo()`/`blobtext()` on singleton
  - add: Error tests: blob functions without docID on non-singleton, with WHERE clause, with non-existent field

## Quick start

Query blob metadata:

```sql
select blobinfo(Img1) from air.Restaurant.123.air.DocWithBLOBs.456
```

Query blob content (first 10000 bytes):

```sql
select blobtext(Img1) from air.Restaurant.123.air.DocWithBLOBs.456
```

Query blob content starting from byte offset:

```sql
select blobtext(Img1, 5000) from air.Restaurant.123.air.DocWithBLOBs.456
```
