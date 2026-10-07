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

const partitionCheckpointEventInterval istructs.Offset = 100

var (
	QNameProjectorPartitionRecoveryCheckpoint = appdef.NewQName(appdef.SysPackage, "ProjectorPartitionRecoveryCheckpoint")
	QNameProjectorWorkspaceRecoveryCheckpoint = appdef.NewQName(appdef.SysPackage, "ProjectorWorkspaceRecoveryCheckpoint")
)

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

// Provide registers one asynchronous projector per sequence storage.
func Provide(resources istructsmem.IStatelessResources, storage IRecoveryCheckpointStorage) {
	resources.AddProjectors(appdef.SysPackagePath,
		istructs.Projector{
			Name: QNameProjectorPartitionRecoveryCheckpoint,
			Func: partitionRecoveryCheckpointProjector(storage),
		},
		istructs.Projector{
			Name: QNameProjectorWorkspaceRecoveryCheckpoint,
			Func: workspaceRecoveryCheckpointProjector(storage),
		},
	)
}

func partitionRecoveryCheckpointProjector(storage IRecoveryCheckpointStorage) func(istructs.IPLogEvent, istructs.IState, istructs.IIntents) error {
	return func(event istructs.IPLogEvent, state istructs.IState, _ istructs.IIntents) error {
		if (event.PLogOffset()-istructs.FirstOffset+1)%partitionCheckpointEventInterval != 0 {
			return nil
		}
		return storage.PutPartitionCheckpoint(state.AppStructs().ClusterAppID(), event.HandlingPartition(), PartitionCheckpoint{
			LastHandledPLogOffset: event.PLogOffset(),
		})
	}
}

func workspaceRecoveryCheckpointProjector(storage IRecoveryCheckpointStorage) func(istructs.IPLogEvent, istructs.IState, istructs.IIntents) error {
	return func(event istructs.IPLogEvent, state istructs.IState, _ istructs.IIntents) error {
		return storage.PutWorkspaceCheckpoint(state.AppStructs().ClusterAppID(), event.Workspace(), WorkspaceCheckpoint{
			LastHandledWLogOffset: event.WLogOffset(),
		})
	}
}
