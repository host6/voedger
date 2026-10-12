/*
 * Copyright (c) 2026-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package checkpoints

import (
	"github.com/voedger/voedger/pkg/appdef"
	"github.com/voedger/voedger/pkg/istructs"
)

const partitionCheckpointEventInterval istructs.Offset = 100

var QNameProjectorRecoveryCheckpoint = appdef.NewQName(appdef.SysPackage, "ProjectorRecoveryCheckpoint")
