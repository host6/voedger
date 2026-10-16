/*
 * Copyright (c) 2026-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package checkpoints_test

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/voedger/voedger/pkg/checkpoints"
	"github.com/voedger/voedger/pkg/coreutils"
	"github.com/voedger/voedger/pkg/goutils/testingu"
	"github.com/voedger/voedger/pkg/istorage/mem"
	istorageprovider "github.com/voedger/voedger/pkg/istorage/provider"
	"github.com/voedger/voedger/pkg/istructs"
	vvmstorage "github.com/voedger/voedger/pkg/vvm/storage"
)

const initialRecoveryTestAppID istructs.ClusterAppID = 7

func TestInitialRecoveryComputesMissingCheckpoints(t *testing.T) {
	t.Run("recovery: scn: Missing checkpoints are computed for every declared partition", func(t *testing.T) {
		// VIT provisions workspace events in both PLogs and WLogs. This scenario's
		// empty partition must use mocks; deleting only its PLog leaves an impossible history.
		appQName := istructs.AppQName_test1_app1
		appID := initialRecoveryTestAppID
		plog := newLogReadRecorder(t)

		// Given an application declares partition IDs 0 through 2
		const partitionCount = 3

		// And partition 1 has no stored partition checkpoint
		// And partition 1 PLog ends at offset 50 and contains workspace histories:
		// | WSID | latest ID-bearing WLog offset |
		// | 10   | 6                             |
		// | 11   | none                          |
		plog.setEvents(1, nil,
			newPLogEvent(10, 10, 6, newCUDRow(istructs.FirstUserRecordID, true)),
			newPLogEvent(40, 10, 10),
			newPLogEvent(50, 11, 4),
		)
		// And partition 2 has no stored partition checkpoint
		// And partition 2 PLog is empty
		// No workspace history is provisioned for partition 2, so its WLogs are also empty.
		plog.setEvents(2, nil)

		// And partition 0 has stored checkpoint lastPLogOffset 20
		storage := newCheckpointStorageRecorder(t)
		storage.setPartitionCheckpoint(t, appID, 0, checkpoints.PartitionCheckpoint{LastPLogOffset: 20})

		recovery := checkpoints.NewInitialRecovery(newAppStructsProvider(appID, plog.events), storage)

		// When VAOperator starts the VVM
		deployable, failed, err := recovery.InitializeCheckpoints(t.Context(), appQName, partitionCount)
		require.NoError(t, err)
		requireInitialRecoveryOutcomes(t, partitionCount, deployable, failed)
		require.ElementsMatch(t, []istructs.PartitionID{0, 1, 2}, deployable)
		require.Empty(t, failed)

		// Then partition 0 retains checkpoint lastPLogOffset 20
		checkpoint, ok, err := storage.GetPartitionCheckpoint(appID, 0)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, checkpoints.PartitionCheckpoint{LastPLogOffset: 20}, checkpoint)
		require.Equal(t, 0, storage.putPartitionCheckputCount(0), "completed partition must not be rewritten")
		require.Equal(t, 0, plog.readPLogCallsCount(0), "completed partition must not be scanned")

		// And partition checkpoints are persisted:
		// | partition ID | lastPLogOffset |
		// | 1            | 50             |
		// | 2            | 0              |
		require.Equal(t, checkpoints.PartitionCheckpoint{LastPLogOffset: 50}, storage.requirePartitionCheckpoint(t, appID, 1))
		require.Equal(t, checkpoints.PartitionCheckpoint{LastPLogOffset: 0}, storage.requirePartitionCheckpoint(t, appID, 2))

		// And workspace checkpoints for partition 1 are persisted before its partition checkpoint:
		// | WSID | wlogIDBearingOffset |
		// | 10   | 6                   |
		// | 11   | 0                   |
		expected := checkpoints.WorkspaceCheckpoint{WLogIDBearingOffset: 6}
		actual := storage.requireWorkspaceCheckpoint(t, appID, 10)
		require.Equal(t, expected, actual)

		// wsid 11
		expected = checkpoints.WorkspaceCheckpoint{WLogIDBearingOffset: 0}
		actual = storage.requireWorkspaceCheckpoint(t, appID, 11)
		require.Equal(t, expected, actual)

		require.Less(t, storage.checkpointHistoryIndex("workspace:10"), storage.checkpointHistoryIndex("partition:1"))
		require.Less(t, storage.checkpointHistoryIndex("workspace:11"), storage.checkpointHistoryIndex("partition:1"))
		require.Equal(t, 1, plog.readPLogCallsCount(1))
		require.Equal(t, 1, plog.readPLogCallsCount(2), "an empty PLog is scanned without allocating log storage")
	})
}

func TestInitialRecoveryUsesTheLatestIDBearingEvent(t *testing.T) {
	appID := initialRecoveryTestAppID
	plog := newLogReadRecorder(t)

	// Set partition 0 to contain 4 PLog events:
	// workspace 10 creates new records at WLog offsets 6 and 10, then has a CUD-free event at offset 14;
	// workspace 11 only updates an existing record at WLog offset 9.
	plog.setEvents(0, nil,
		newPLogEvent(1, 10, 6, newCUDRow(istructs.FirstUserRecordID, true)),
		newPLogEvent(2, 10, 10, newCUDRow(istructs.FirstUserRecordID+1, true)),
		newPLogEvent(3, 10, 14),
		newPLogEvent(4, 11, 9, newCUDRow(istructs.FirstUserRecordID, false)),
	)
	storage := newCheckpointStorageRecorder(t)
	recovery := checkpoints.NewInitialRecovery(newAppStructsProvider(appID, plog.events), storage)

	// Run the recovery.
	deployable, failed, err := recovery.InitializeCheckpoints(t.Context(), istructs.AppQName_test1_app1, 1)
	require.NoError(t, err)
	requireInitialRecoveryOutcomes(t, 1, deployable, failed)
	require.Equal(t, []istructs.PartitionID{0}, deployable)

	// Workspace 10: checkpoint must be on offset 10 since it is the latest ID-bearing event;
	// the later CUD-free event on offset 14 must not advance it.
	expected := checkpoints.WorkspaceCheckpoint{WLogIDBearingOffset: 10}
	actual := storage.requireWorkspaceCheckpoint(t, appID, 10)
	require.Equal(t, expected, actual)

	// Workspace 11: checkpoint must be on offset 0 since updating an existing record does not bear a new ID.
	expected = checkpoints.WorkspaceCheckpoint{WLogIDBearingOffset: 0}
	actual = storage.requireWorkspaceCheckpoint(t, appID, 11)
	require.Equal(t, expected, actual)

	// Partition 0: checkpoint must be on offset 4 since it is the last PLog event, whether ID-bearing or not.
	expectedPartCP := checkpoints.PartitionCheckpoint{LastPLogOffset: 4}
	actualPartCP := storage.requirePartitionCheckpoint(t, appID, 0)
	require.Equal(t, expectedPartCP, actualPartCP)
}

func TestInitialRecoveryContinuesAfterPartitionFailures(t *testing.T) {
	injectedErr := errors.New("injected failure")
	tests := []struct {
		name             string
		operation        string
		configureStorage func(*checkpointStorageRecorder)
		readError        error
		skipRead         bool
		workspaceData    bool
	}{
		{
			name:      "get partition checkpoint",
			operation: "get partition checkpoint",
			skipRead:  true,
			configureStorage: func(storage *checkpointStorageRecorder) {
				storage.partitionGetErrors[0] = injectedErr
			},
		},
		{
			name:      "read PLog",
			operation: "read PLog",
			readError: injectedErr,
		},
		{
			name:          "put workspace checkpoint",
			operation:     "put workspace checkpoint",
			workspaceData: true,
			configureStorage: func(storage *checkpointStorageRecorder) {
				storage.workspacePutErrors[10] = injectedErr
			},
		},
		{
			name:      "put partition checkpoint",
			operation: "put partition checkpoint",
			configureStorage: func(storage *checkpointStorageRecorder) {
				storage.partitionPutErrors[0] = injectedErr
			},
		},
	}

	appID := initialRecoveryTestAppID

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Set partition 0 to fail at the selected recovery stage and partition 1 to recover successfully.
			reads := newLogReadRecorder(t)
			reads.setEvents(1, nil, newPLogEvent(3, 20, 2))
			if !test.skipRead {
				entries := []*coreutils.MockPLogEvent(nil)
				if test.workspaceData {
					entries = append(entries, newPLogEvent(2, 10, 1))
				}
				reads.setEvents(0, test.readError, entries...)
			}
			storage := newCheckpointStorageRecorder(t)
			if test.configureStorage != nil {
				test.configureStorage(storage)
			}
			recovery := checkpoints.NewInitialRecovery(newAppStructsProvider(appID, reads.events), storage)

			// Run initial recovery.
			deployable, failed, err := recovery.InitializeCheckpoints(t.Context(), istructs.AppQName_test1_app1, 2)
			require.NoError(t, err)
			requireInitialRecoveryOutcomes(t, 2, deployable, failed)
			require.Equal(t, []istructs.PartitionID{1}, deployable)
			require.Len(t, failed, 1)
			require.ErrorIs(t, failed[0], injectedErr)
			require.ErrorContains(t, failed[0], test.operation+": injected failure")

			// Check that the failed partition remains incomplete while the successful partition is checkpointed.
			delete(storage.partitionGetErrors, 0)
			_, failedPartitionMarked, err := storage.GetPartitionCheckpoint(appID, 0)
			require.NoError(t, err)
			require.False(t, failedPartitionMarked)
			require.Equal(t, checkpoints.PartitionCheckpoint{LastPLogOffset: 3}, storage.requirePartitionCheckpoint(t, appID, 1))

			// The initializer returns errors for bootstrap to log; no log assertion belongs here.
			require.Zero(t, storage.putPartitionCheckputCount(0))
		})
	}
}

func requireInitialRecoveryOutcomes(t *testing.T, numPartitions int, deployable []istructs.PartitionID,
	failed map[istructs.PartitionID]error) {
	t.Helper()
	seen := map[istructs.PartitionID]bool{}
	for _, partitionID := range deployable {
		require.Less(t, int(partitionID), numPartitions)
		require.False(t, seen[partitionID], "duplicate deployable partition %d", partitionID)
		seen[partitionID] = true
		_, excluded := failed[partitionID]
		require.False(t, excluded, "partition %d is both deployable and failed", partitionID)
	}
	for partitionID, err := range failed {
		require.Less(t, int(partitionID), numPartitions)
		require.Error(t, err)
		seen[partitionID] = true
	}
	require.Len(t, seen, numPartitions, "every declared partition must have exactly one outcome")
}

func TestInitialRecoveryRetriesOnlyIncompletePartitions(t *testing.T) {
	// Set partition 0 to fail during its first PLog read.
	appID := initialRecoveryTestAppID
	reads := newLogReadRecorder(t)
	entry := newPLogEvent(5, 0, 0)
	reads.setEvents(0, errors.New("temporary read failure"), entry)
	storage := newCheckpointStorageRecorder(t)
	storage.setPartitionCheckpoint(t, appID, 1, checkpoints.PartitionCheckpoint{})
	recovery := checkpoints.NewInitialRecovery(newAppStructsProvider(appID, reads.events), storage)

	// Run initial recovery and check that the failed partition remains incomplete.
	deployable, failed, err := recovery.InitializeCheckpoints(t.Context(), istructs.AppQName_test1_app1, 2)
	require.NoError(t, err)
	requireInitialRecoveryOutcomes(t, 2, deployable, failed)
	require.Equal(t, []istructs.PartitionID{1}, deployable)
	require.Len(t, failed, 1)
	_, ok, err := storage.GetPartitionCheckpoint(appID, 0)
	require.NoError(t, err)
	require.False(t, ok)

	// Make the PLog read succeed and retry initial recovery.
	reads.setEvents(0, nil, entry)
	deployable, failed, err = recovery.InitializeCheckpoints(t.Context(), istructs.AppQName_test1_app1, 2)
	require.NoError(t, err)
	requireInitialRecoveryOutcomes(t, 2, deployable, failed)
	require.ElementsMatch(t, []istructs.PartitionID{0, 1}, deployable)

	// Check that the retried partition is checkpointed after the second read.
	require.Equal(t, checkpoints.PartitionCheckpoint{LastPLogOffset: 5}, storage.requirePartitionCheckpoint(t, appID, 0))
	require.Equal(t, 2, reads.readPLogCallsCount(0))

	// Run initial recovery again and check that the completed partition is skipped.
	deployable, failed, err = recovery.InitializeCheckpoints(t.Context(), istructs.AppQName_test1_app1, 2)
	require.NoError(t, err)
	requireInitialRecoveryOutcomes(t, 2, deployable, failed)
	require.ElementsMatch(t, []istructs.PartitionID{0, 1}, deployable)
	require.Equal(t, 2, reads.readPLogCallsCount(0), "completed partition must not be scanned again")
	require.Equal(t, 1, storage.putPartitionCheckputCount(0), "completed partition must not be written again")
	require.Zero(t, reads.readPLogCallsCount(1), "previously completed sibling must never be scanned")
	require.Zero(t, storage.putPartitionCheckputCount(1))
}

func TestInitialRecoveryHonorsCancellationBeforeScheduling(t *testing.T) {
	appID := initialRecoveryTestAppID
	reads := newLogReadRecorder(t)
	storage := newCheckpointStorageRecorder(t)
	recovery := checkpoints.NewInitialRecovery(newAppStructsProvider(appID, reads.events), storage)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	deployable, failed, err := recovery.InitializeCheckpoints(ctx, istructs.AppQName_test1_app1, 3)

	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, deployable)
	require.Empty(t, failed, "cancellation is fatal, not a partition failure")
	require.Zero(t, reads.readPLogCallsCountTotal())
	require.Empty(t, storage.getCheckpointsHistory())
}

func TestInitialRecoveryHasNoRuntimeRecoveryOrPublicationSideEffects(t *testing.T) {
	appID := initialRecoveryTestAppID
	reads := newLogReadRecorder(t)
	reads.setEvents(0, nil, newPLogEvent(1, 10, 1))
	storage := newCheckpointStorageRecorder(t)
	recovery := checkpoints.NewInitialRecovery(newAppStructsProvider(appID, reads.events), storage)

	deployable, failed, err := recovery.InitializeCheckpoints(t.Context(), istructs.AppQName_test1_app1, 1)
	require.NoError(t, err)
	requireInitialRecoveryOutcomes(t, 1, deployable, failed)

	require.Equal(t, []string{"workspace:10", "partition:0"}, storage.getCheckpointsHistory())
	require.Equal(t, 1, reads.readPLogCallsCount(0))
	reads.events.AssertNotCalled(t, "ReadWLog", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestInitialRecoveryAcceptsPresentZeroCheckpoints(t *testing.T) {
	// Presence, not a nonzero offset, marks completion; no log reads or writes are needed.
	reads := newLogReadRecorder(t)
	storage := newCheckpointStorageRecorder(t)
	for partitionID := istructs.PartitionID(0); partitionID < 3; partitionID++ {
		storage.setPartitionCheckpoint(t, initialRecoveryTestAppID, partitionID, checkpoints.PartitionCheckpoint{})
	}
	recovery := checkpoints.NewInitialRecovery(newAppStructsProvider(initialRecoveryTestAppID, reads.events), storage)
	deployable, failed, err := recovery.InitializeCheckpoints(t.Context(), istructs.AppQName_test1_app1, 3)
	require.NoError(t, err)
	requireInitialRecoveryOutcomes(t, 3, deployable, failed)
	require.ElementsMatch(t, []istructs.PartitionID{0, 1, 2}, deployable)
	require.Zero(t, reads.readPLogCallsCountTotal())
	require.Empty(t, storage.getCheckpointsHistory())
}

func TestInitialRecoveryCheckpointReadFailureExcludesPresentMarker(t *testing.T) {
	// A marker that cannot be read must not authorize deployment, even if it exists in storage.
	reads := newLogReadRecorder(t)
	storage := newCheckpointStorageRecorder(t)
	storage.setPartitionCheckpoint(t, initialRecoveryTestAppID, 0, checkpoints.PartitionCheckpoint{LastPLogOffset: 42})
	injectedErr := errors.New("checkpoint unavailable")
	storage.partitionGetErrors[0] = injectedErr
	recovery := checkpoints.NewInitialRecovery(newAppStructsProvider(initialRecoveryTestAppID, reads.events), storage)
	deployable, failed, err := recovery.InitializeCheckpoints(t.Context(), istructs.AppQName_test1_app1, 1)
	require.NoError(t, err)
	requireInitialRecoveryOutcomes(t, 1, deployable, failed)
	require.Empty(t, deployable)
	require.ErrorIs(t, failed[0], injectedErr)
	require.Zero(t, reads.readPLogCallsCountTotal())
	require.Empty(t, storage.getCheckpointsHistory())
}

func TestInitialRecoveryReturnsEveryPartitionFailure(t *testing.T) {
	// A complete failure map and an empty deployable slice are a successful, nonfatal invocation.
	reads := newLogReadRecorder(t)
	storage := newCheckpointStorageRecorder(t)
	injectedErr := errors.New("PLog unavailable")
	for partitionID := istructs.PartitionID(0); partitionID < 3; partitionID++ {
		reads.setEvents(partitionID, injectedErr)
	}
	recovery := checkpoints.NewInitialRecovery(newAppStructsProvider(initialRecoveryTestAppID, reads.events), storage)
	deployable, failed, err := recovery.InitializeCheckpoints(t.Context(), istructs.AppQName_test1_app1, 3)
	require.NoError(t, err)
	requireInitialRecoveryOutcomes(t, 3, deployable, failed)
	require.Empty(t, deployable)
	for partitionID := istructs.PartitionID(0); partitionID < 3; partitionID++ {
		require.ErrorIs(t, failed[partitionID], injectedErr)
		require.ErrorContains(t, failed[partitionID], "read PLog")
		require.Equal(t, 1, reads.readPLogCallsCount(partitionID))
	}
	require.Empty(t, storage.getCheckpointsHistory())
}

func TestInitialRecoveryReturnsFatalSetupFailure(t *testing.T) {
	setupErr := errors.New("application structures unavailable")
	provider := &coreutils.MockAppStructsProvider{}
	provider.On("BuiltIn", istructs.AppQName_test1_app1).Return(nil, setupErr).Once()
	storage := newCheckpointStorageRecorder(t)
	recovery := checkpoints.NewInitialRecovery(provider, storage)
	deployable, failed, err := recovery.InitializeCheckpoints(t.Context(), istructs.AppQName_test1_app1, 3)
	require.ErrorIs(t, err, setupErr)
	require.Empty(t, deployable)
	require.Empty(t, failed)
	require.Empty(t, storage.getCheckpointsHistory())
	provider.AssertExpectations(t)
}

func TestInitialRecoveryJoinsAttemptsOnCancellation(t *testing.T) {
	// Partition 0 finishes before partition 1 is canceled; partial progress is not a successful outcome.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	completed := make(chan struct{})
	started := make(chan struct{})
	storage := newCheckpointStorageRecorder(t)
	storage.partitionPutHook = func(partitionID istructs.PartitionID) {
		if partitionID == 0 {
			close(completed)
		}
	}
	reads := newLogReadRecorder(t)
	reads.setEvents(0, nil)
	reads.events.On("ReadPLog", mock.Anything, istructs.PartitionID(1), istructs.FirstOffset,
		istructs.ReadToTheEnd, mock.Anything).Run(func(mock.Arguments) {
		select {
		case <-completed:
		case <-ctx.Done():
			return
		}
		close(started)
		<-ctx.Done()
	}).Return(context.Canceled).Once()
	recovery := checkpoints.NewInitialRecovery(newAppStructsProvider(initialRecoveryTestAppID, reads.events), storage)
	done := make(chan error, 1)
	go func() {
		_, _, err := recovery.InitializeCheckpoints(ctx, istructs.AppQName_test1_app1, 2)
		done <- err
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("partition 1 did not start after partition 0 completed")
	}
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	require.Equal(t, checkpoints.PartitionCheckpoint{}, storage.requirePartitionCheckpoint(t, initialRecoveryTestAppID, 0))
	_, ok, err := storage.GetPartitionCheckpoint(initialRecoveryTestAppID, 1)
	require.NoError(t, err)
	require.False(t, ok)
}

func newAppStructsProvider(appID istructs.ClusterAppID, events istructs.IEvents) *coreutils.MockAppStructsProvider {
	asp := &coreutils.MockAppStructsProvider{}
	appStructs := &coreutils.MockAppStructs{}
	appStructs.On("ClusterAppID").Return(appID)
	appStructs.On("Events").Return(events)
	asp.On("BuiltIn", mock.Anything).Return(appStructs, nil)
	return asp
}

func newPLogEvent(plogOffset istructs.Offset, wsid istructs.WSID, wLogOffset istructs.Offset, cuds ...istructs.ICUDRow) *coreutils.MockPLogEvent {
	event := &coreutils.MockPLogEvent{}
	event.On("PLogOffset").Return(plogOffset)
	event.On("Workspace").Return(wsid)
	event.On("WLogOffset").Return(wLogOffset)
	event.On("CUDs", mock.Anything).Run(func(args mock.Arguments) {
		callback := args.Get(0).(func(istructs.ICUDRow) bool)
		for _, cud := range cuds {
			if !callback(cud) {
				return
			}
		}
	}).Return()
	event.On("Release").Return()
	return event
}

func newCUDRow(id istructs.RecordID, isNew bool) istructs.ICUDRow {
	row := &coreutils.MockCUDRow{}
	row.On("ID").Return(id)
	row.On("IsNew").Return(isNew)
	return row
}

type logReadRecorder struct {
	events            *coreutils.MockEvents
	mu                sync.Mutex
	readPLogCallCount map[istructs.PartitionID]int
}

func newLogReadRecorder(t *testing.T) *logReadRecorder {
	t.Helper()
	recorder := &logReadRecorder{
		events:            &coreutils.MockEvents{},
		readPLogCallCount: map[istructs.PartitionID]int{},
	}
	t.Cleanup(func() { recorder.events.AssertExpectations(t) })
	return recorder
}

func (r *logReadRecorder) setEvents(partitionID istructs.PartitionID, errOnReadPLog error,
	events ...*coreutils.MockPLogEvent) {
	var expectation *mock.Call
	expectation = r.events.On("ReadPLog", mock.Anything, partitionID, istructs.FirstOffset,
		istructs.ReadToTheEnd, mock.Anything).Return(errOnReadPLog).Once()
	expectation.Run(func(args mock.Arguments) {
		ctx := args.Get(0).(context.Context)
		callback := args.Get(4).(istructs.PLogEventsReaderCallback)
		r.onReadPLog(partitionID)
		if err := ctx.Err(); err != nil {
			expectation.Return(err)
			return
		}
		if errOnReadPLog != nil {
			return
		}
		for _, event := range events {
			if err := callback(event.PLogOffset(), event); err != nil {
				expectation.Return(err)
				return
			}
		}
		expectation.Return(nil)
	})
}

func (r *logReadRecorder) onReadPLog(partitionID istructs.PartitionID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.readPLogCallCount[partitionID]++
}

func (r *logReadRecorder) readPLogCallsCount(partitionID istructs.PartitionID) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.readPLogCallCount[partitionID]
}

func (r *logReadRecorder) readPLogCallsCountTotal() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	total := 0
	for _, count := range r.readPLogCallCount {
		total += count
	}
	return total
}

type checkpointStorageRecorder struct {
	base                       checkpoints.IRecoveryCheckpointStorage
	mu                         sync.Mutex
	partitionGetErrors         map[istructs.PartitionID]error
	partitionPutErrors         map[istructs.PartitionID]error
	workspacePutErrors         map[istructs.WSID]error
	numPutsPartitionCheckpoint map[istructs.PartitionID]int
	checkpointsHistory         []string
	partitionPutHook           func(istructs.PartitionID)
}

func newCheckpointStorageRecorder(t *testing.T) *checkpointStorageRecorder {
	t.Helper()
	storageProvider := istorageprovider.Provide(mem.Provide(testingu.MockTime))
	sysVVMStorage, err := storageProvider.AppStorage(istructs.AppQName_sys_vvm)
	require.NoError(t, err)
	return &checkpointStorageRecorder{
		base:                       vvmstorage.NewRecoveryCheckpointStorage(sysVVMStorage),
		partitionGetErrors:         map[istructs.PartitionID]error{},
		partitionPutErrors:         map[istructs.PartitionID]error{},
		workspacePutErrors:         map[istructs.WSID]error{},
		numPutsPartitionCheckpoint: map[istructs.PartitionID]int{},
	}
}

func (s *checkpointStorageRecorder) GetPartitionCheckpoint(appID istructs.ClusterAppID,
	partitionID istructs.PartitionID) (checkpoints.PartitionCheckpoint, bool, error) {
	s.mu.Lock()
	injectedErr := s.partitionGetErrors[partitionID]
	s.mu.Unlock()
	if injectedErr != nil {
		return checkpoints.PartitionCheckpoint{}, false, injectedErr
	}
	return s.base.GetPartitionCheckpoint(appID, partitionID)
}

func (s *checkpointStorageRecorder) PutPartitionCheckpoint(appID istructs.ClusterAppID,
	partitionID istructs.PartitionID, checkpoint checkpoints.PartitionCheckpoint) error {
	s.mu.Lock()
	injectedErr := s.partitionPutErrors[partitionID]
	s.mu.Unlock()
	if injectedErr != nil {
		return injectedErr
	}
	if err := s.base.PutPartitionCheckpoint(appID, partitionID, checkpoint); err != nil {
		return err
	}
	if s.partitionPutHook != nil {
		s.partitionPutHook(partitionID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.numPutsPartitionCheckpoint[partitionID]++
	s.checkpointsHistory = append(s.checkpointsHistory, "partition:"+strconv.FormatUint(uint64(partitionID), 10))
	return nil
}

func (s *checkpointStorageRecorder) GetWorkspaceCheckpoint(appID istructs.ClusterAppID,
	wsid istructs.WSID) (checkpoints.WorkspaceCheckpoint, bool, error) {
	return s.base.GetWorkspaceCheckpoint(appID, wsid)
}

func (s *checkpointStorageRecorder) PutWorkspaceCheckpoint(appID istructs.ClusterAppID,
	wsid istructs.WSID, checkpoint checkpoints.WorkspaceCheckpoint) error {
	s.mu.Lock()
	injectedErr := s.workspacePutErrors[wsid]
	s.mu.Unlock()
	if injectedErr != nil {
		return injectedErr
	}
	if err := s.base.PutWorkspaceCheckpoint(appID, wsid, checkpoint); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkpointsHistory = append(s.checkpointsHistory, "workspace:"+strconv.FormatUint(uint64(wsid), 10))
	return nil
}

func (s *checkpointStorageRecorder) setPartitionCheckpoint(t *testing.T, appID istructs.ClusterAppID,
	partitionID istructs.PartitionID, checkpoint checkpoints.PartitionCheckpoint) {
	t.Helper()
	require.NoError(t, s.base.PutPartitionCheckpoint(appID, partitionID, checkpoint))
}

func (s *checkpointStorageRecorder) requirePartitionCheckpoint(t *testing.T, appID istructs.ClusterAppID,
	partitionID istructs.PartitionID) checkpoints.PartitionCheckpoint {
	t.Helper()
	checkpoint, ok, err := s.GetPartitionCheckpoint(appID, partitionID)
	require.NoError(t, err)
	require.True(t, ok, "partition %d checkpoint is absent", partitionID)
	return checkpoint
}

func (s *checkpointStorageRecorder) requireWorkspaceCheckpoint(t *testing.T, appID istructs.ClusterAppID,
	wsid istructs.WSID) checkpoints.WorkspaceCheckpoint {
	t.Helper()
	checkpoint, ok, err := s.GetWorkspaceCheckpoint(appID, wsid)
	require.NoError(t, err)
	require.True(t, ok, "workspace %d checkpoint is absent", wsid)
	return checkpoint
}

func (s *checkpointStorageRecorder) putPartitionCheckputCount(partitionID istructs.PartitionID) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.numPutsPartitionCheckpoint[partitionID]
}

func (s *checkpointStorageRecorder) checkpointHistoryIndex(operation string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index, actual := range s.checkpointsHistory {
		if actual == operation {
			return index
		}
	}
	return -1
}

func (s *checkpointStorageRecorder) getCheckpointsHistory() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.checkpointsHistory...)
}
