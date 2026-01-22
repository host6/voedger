---
uspecs.registered_at: 2026-01-22T09:45:19Z
uspecs.change_id: 260122-add-1y-temporary-blob-duration
uspecs.baseline: eb5758df88bea91602507e87adfe5d893297f161
---

# Change request: Add 1y temporary blob duration support

## Problem

Currently, temporary BLOBs only support 1-day (`1d`) duration. Users need the ability to store temporary BLOBs for longer periods, specifically 1 year (`1y`), for use cases requiring extended temporary storage.

## Solution overview

Add support for 1-year temporary blob duration:

- Add `DurationType_1Year` constant to `iblobstorage` package
- Register the new duration in URL TTL mappings (`1y` ↔ `DurationType_1Year`)
- Add corresponding `RegisterTempBLOB1y` function mapping in blobber processor

Add according integration test in sys_it package
