/*
 * Copyright (c) 2026-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package checkpoints

import (
	"github.com/voedger/voedger/pkg/appdef"
	"github.com/voedger/voedger/pkg/istructs"
	"github.com/voedger/voedger/pkg/istructsmem"
)

var QNameProjectorRecoveryCheckpoint = appdef.NewQName(appdef.SysPackage, "ProjectorRecoveryCheckpoint")

type PartitionCheckpoint struct {
	LastHandledPLogOffset istructs.Offset `json:"lastHandledPLogOffset"`
}

type WorkspaceCheckpoint struct {
	LastHandledWLogOffset istructs.Offset `json:"lastHandledWLogOffset"`
}

type IRecoveryCheckpointStorage interface {
	GetPartitionCheckpoint(appID istructs.ClusterAppID, partitionID istructs.PartitionID) (PartitionCheckpoint, bool, error)
	PutPartitionCheckpoint(appID istructs.ClusterAppID, partitionID istructs.PartitionID, checkpoint PartitionCheckpoint) error
	GetWorkspaceCheckpoint(appID istructs.ClusterAppID, wsid istructs.WSID) (WorkspaceCheckpoint, bool, error)
	PutWorkspaceCheckpoint(appID istructs.ClusterAppID, wsid istructs.WSID, checkpoint WorkspaceCheckpoint) error
}

// Provide registers recovery checkpointing as a regular built-in asynchronous
// projector. Its lifecycle, PLog position, retries, and shutdown are therefore
// managed by the standard actualizer infrastructure.
func Provide(resources istructsmem.IStatelessResources, storage IRecoveryCheckpointStorage) {
	resources.AddProjectors(appdef.SysPackagePath, istructs.Projector{
		Name: QNameProjectorRecoveryCheckpoint,
		Func: recoveryCheckpointProjector(storage),
	})
}

func recoveryCheckpointProjector(storage IRecoveryCheckpointStorage) func(istructs.IPLogEvent, istructs.IState, istructs.IIntents) error {
	return func(event istructs.IPLogEvent, state istructs.IState, _ istructs.IIntents) error {
		appID := state.AppStructs().ClusterAppID()

		// The partition checkpoint is a durability barrier. Persist the workspace
		// offset covered by this event before allowing recovery to skip it.
		if err := storage.PutWorkspaceCheckpoint(appID, event.Workspace(), WorkspaceCheckpoint{
			LastHandledWLogOffset: event.WLogOffset(),
		}); err != nil {
			return err
		}
		return storage.PutPartitionCheckpoint(appID, event.HandlingPartition(), PartitionCheckpoint{
			LastHandledPLogOffset: event.PLogOffset(),
		})
	}
}
