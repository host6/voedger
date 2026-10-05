/*
 * Copyright (c) 2025-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package storage

const (
	// nolint: unused
	pKeyPrefix_null pKeyPrefix = iota

	// [~server.design.orch/KeyPrefix_VVMLeader~impl]
	pKeyPrefix_VVMLeader

	// Partition recovery checkpoints use the partition ID as their clustering column.
	pKeyPrefix_SeqStorage_Part

	// Workspace recovery checkpoints use the WSID as their clustering column.
	pKeyPrefix_SeqStorage_WS

	pKeyPrefix_AppTTL
)

const (
	MaxKeyLength                = 1024
	MaxValueLength              = 65536
	MaxTTLSeconds               = 31536000
	appTTLPKSize                = 8
	appTTLValidationErrTemplate = "%w: %w"
)
