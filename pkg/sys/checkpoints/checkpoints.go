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
	NextPLogOffset istructs.Offset `json:"nextPLogOffset"`
}

type WorkspaceCheckpoint struct {
	NextWLogOffset istructs.Offset   `json:"nextWLogOffset"`
	NextRecordID   istructs.RecordID `json:"nextRecordID"`
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
		workspace := workspaceCheckpoint(event, state.AppStructs().AppDef().Type)

		// The partition checkpoint is a durability barrier. Persist the workspace
		// sequence state covered by this event before allowing recovery to skip it.
		if err := storage.PutWorkspaceCheckpoint(appID, event.Workspace(), workspace); err != nil {
			return err
		}
		return storage.PutPartitionCheckpoint(appID, event.HandlingPartition(), PartitionCheckpoint{
			NextPLogOffset: event.PLogOffset() + 1,
		})
	}
}

func workspaceCheckpoint(event istructs.IPLogEvent, findType appdef.FindType) WorkspaceCheckpoint {
	nextRecordID := istructs.FirstUserRecordID
	advanceRecordID := func(id istructs.RecordID) {
		if candidate := id + 1; candidate > nextRecordID {
			nextRecordID = candidate
		}
	}

	for record := range event.CUDs {
		if record.IsNew() {
			advanceRecordID(record.ID())
		}
	}

	argument := event.ArgumentObject()
	if argument != nil && argument.QName() != appdef.NullQName {
		if typ := findType(argument.QName()); typ != nil && typ.Kind() == appdef.TypeKind_ODoc {
			visitObjectRecordIDs(argument, advanceRecordID)
		}
	}

	return WorkspaceCheckpoint{
		NextWLogOffset: event.WLogOffset() + 1,
		NextRecordID:   nextRecordID,
	}
}

func visitObjectRecordIDs(object istructs.IObject, visit func(istructs.RecordID)) {
	visit(object.AsRecordID(appdef.SystemField_ID))
	for container := range object.Containers {
		for child := range object.Children(container) {
			visitObjectRecordIDs(child, visit)
		}
	}
}
