---
uspecs.registered_at: 2026-01-20T00:35:10Z
uspecs.change_id: 260120-implement-iappttlstorage
uspecs.baseline: c7f58fd212601dc8106dd7fa68a7f22e31d87017
---

# Change request: Implement IAppTTLStorage interface

- issue: [AIR-2718: link-alpha-code: voedger: implement IAppTTLStorage](https://untill.atlassian.net/browse/AIR-2718)

## Problem

The Air system requires a TTL (Time-To-Live) storage mechanism for temporary data with automatic expiration capabilities. The device linking feature (Alpha Code flow) needs to store temporary key-value pairs that automatically expire after a specified duration:

- Temporary storage of Alpha Code to Device Code mappings (ACAlpha2Device)
- Temporary storage of Device Code to Link Token mappings (ACDevice2Token)
- Automatic expiration of entries after specified TTL periods
- Atomic operations for race-condition-free updates

Without this interface implementation, the device linking feature cannot function properly.

## Background

- [Link Device by Alpha Code](https://github.com/untillpro/airs-design/blob/main/uspecs/specs/prod/devices/link-device-acode--td.md)

## Solution overview

Implement the IAppTTLStorage interface in the voedger project to provide a workspace-agnostic, in-memory TTL storage mechanism:

- Core interface methods:
  - Get(bucket, key string) (value string, exists bool) - Retrieve value by bucket and key
  - InsertIfNotExists(bucket, key, value string, ttlSeconds int) bool - Insert only if key doesn't exist
  - CompareAndSwap(bucket, key, expectedValue, newValue string, ttlSeconds int) bool - Atomic update with TTL reset
  - CompareAndDelete(bucket, key, expectedValue string) bool - Atomic deletion with value verification
- Key features:
  - Workspace-agnostic storage (global storage across all workspaces, isolation enforced at application level)
  - Automatic expiration with background cleanup of expired entries
  - Thread-safe operations with proper synchronization for concurrent access
  - Memory-based non-persistent implementation for performance
  - Atomic operations using compare-and-swap semantics
- Integration points:
  - Accessible via IAppStructs.AppTTLStorage() method
  - Used by device authorization endpoints (c.air.ACDeviceAuthorizationRequest, q.air.ACPollToken, c.air.ACApproveDevice)
  - Supports the RFC 8628 OAuth 2.0 Device Authorization Grant flow implementation
- Security considerations:
  - Storage is workspace-agnostic but security isolation is maintained through application-level token validation
  - TTL ensures automatic cleanup of sensitive temporary data
  - Atomic operations prevent race conditions in multi-threaded environments
