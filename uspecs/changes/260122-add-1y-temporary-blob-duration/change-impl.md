# Implementation: Add 1y temporary blob duration support

## Construction

- [x] update: [pkg/iblobstorage/consts.go](../../../pkg/iblobstorage/consts.go)
  - Add `DurationType_1Year = DurationType(365)` constant
- [x] update: [pkg/coreutils/federation/consts.go](../../../pkg/coreutils/federation/consts.go)
  - Add `"1y": iblobstorage.DurationType_1Year` to `TemporaryBLOB_URLTTLToDurationLs` map
  - Add `iblobstorage.DurationType_1Year: "1y"` to `TemporaryBLOBDurationToURLTTL` map
- [x] update: [pkg/processors/blobber/consts.go](../../../pkg/processors/blobber/consts.go)
  - Add `iblobstorage.DurationType_1Year: appdef.NewQName(appdef.SysPackage, "RegisterTempBLOB1y")` to `durationToRegisterFuncs` map
- [x] update: [pkg/processors/blobber/impl_write.go](../../../pkg/processors/blobber/impl_write.go)
  - Update error message from `"1d" is only supported` to `"1d", "1y" are only supported`
- [x] update: [pkg/sys/blobber/provide.go](../../../pkg/sys/blobber/provide.go)
  - Add `RegisterTempBLOB1y` command with `NullCommandExec` in `provideRegisterTempBLOB` function (same pattern as `RegisterTempBLOB1d`)
- [x] update: [pkg/sys/sys.vsql](../../../pkg/sys/sys.vsql)
  - Add `COMMAND RegisterTempBLOB1y WITH Tags=(WorkspaceOwnerFuncTag);` declaration
- [x] update: [pkg/iblobstorage/utils_test.go](../../../pkg/iblobstorage/utils_test.go)
  - Add test case for `DurationType_1Year.Seconds()` returning `86400*365`

## Quick start

Upload a temporary blob with 1-year TTL:

```bash
curl -X POST "https://host/api/v2/apps/owner/app/workspaces/1/tblobs" \
  -H "Authorization: Bearer <token>" \
  -H "TTL: 1y" \
  -H "Content-Type: application/octet-stream" \
  --data-binary @file.bin
```
