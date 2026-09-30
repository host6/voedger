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

	// Partition recovery checkpoints and legacy partition sequence storage use
	// distinct four-byte clustering keys within this namespace.
	pKeyPrefix_SeqStorage_Part

	// Workspace recovery checkpoints use a four-byte clustering key within this
	// namespace. Legacy sequence storage uses a two-byte sequence ID.
	pKeyPrefix_SeqStorage_WS

	pKeyPrefix_AppTTL
)

const (
	// PLogOffsetCC identifies the legacy binary partition-offset cell.
	PLogOffsetCC = uint32(0)
)

// recoveryCheckpointCCols identifies JSON recovery checkpoint cells. Keep it
// nonzero to distinguish it from PLogOffsetCC and leave room for additional
// cells under the same partition key.
var recoveryCheckpointCCols = []byte{0, 0, 0, 1}

const (
	MaxKeyLength                = 1024
	MaxValueLength              = 65536
	MaxTTLSeconds               = 31536000
	appTTLPKSize                = 8
	appTTLValidationErrTemplate = "%w: %w"
)
