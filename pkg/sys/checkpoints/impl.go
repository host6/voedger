/*
 * Copyright (c) 2026-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package checkpoints

import (
	"github.com/voedger/voedger/pkg/istructs"
)

func recoveryCheckpointProjector(storage IRecoveryCheckpointStorage) func(istructs.IPLogEvent, istructs.IState, istructs.IIntents) error {
	return func(event istructs.IPLogEvent, state istructs.IState, _ istructs.IIntents) error {
		appID := state.AppStructs().ClusterAppID()
		if event.PLogOffset()%partitionCheckpointEventInterval == 0 {
			if err := storage.PutPartitionCheckpoint(appID, event.HandlingPartition(), PartitionCheckpoint{
				LastPLogOffset: event.PLogOffset(),
			}); err != nil {
				return err
			}
		}

		containsNewRecordIDs := false
		event.CUDs(func(row istructs.ICUDRow) bool {
			containsNewRecordIDs = row.IsNew() && row.ID() >= istructs.FirstUserRecordID
			return !containsNewRecordIDs
		})
		if !containsNewRecordIDs {
			return nil
		}
		return storage.PutWorkspaceCheckpoint(appID, event.Workspace(), WorkspaceCheckpoint{
			LastWLogOffsetWithNewRecordIDs: event.WLogOffset(),
		})
	}
}
