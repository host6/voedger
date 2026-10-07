/*
 * Copyright (c) 2021-present unTill Pro, Ltd.
 */

package commandprocessor

import (
	"errors"

	"github.com/voedger/voedger/pkg/appdef"
	"github.com/voedger/voedger/pkg/istructs"
)

var (
	ViewQNamePLogKnownOffsets = appdef.NewQName(appdef.SysPackage, "PLogKnownOffsets")
	ViewQNameWLogKnownOffsets = appdef.NewQName(appdef.SysPackage, "WLogKnownOffsets")
	errRecoveryInProgress     = errors.New("recovery is in progress")
	errRecoveryLimit          = errors.New("recovery concurrency limit is reached")
	errRecoveryFailed         = errors.New("recovery failed")
)

const (
	args                                          = "args"
	workspaceRecoveryRewindEvents istructs.Offset = 10
)
