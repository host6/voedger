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

	// Partition recovery checkpoints use this namespace with an empty
	// clustering key. Legacy sequence storage uses a four-byte clustering key.
	pKeyPrefix_SeqStorage_Part

	// Workspace recovery checkpoints use this namespace with an empty
	// clustering key. Legacy sequence storage uses a two-byte sequence ID.
	pKeyPrefix_SeqStorage_WS

	pKeyPrefix_AppTTL
)

const (
	// PLogOffsetCC identifies the legacy binary partition-offset cell. Recovery
	// checkpoint JSON deliberately uses an empty clustering key instead.
	PLogOffsetCC = uint32(0)
)

const (
	MaxKeyLength                = 1024
	MaxValueLength              = 65536
	MaxTTLSeconds               = 31536000
	appTTLPKSize                = 8
	appTTLValidationErrTemplate = "%w: %w"
)
