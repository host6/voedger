/*
 * Copyright (c) 2026-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package checkpoints_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/voedger/voedger/pkg/checkpoints"
	"github.com/voedger/voedger/pkg/coreutils"
	"github.com/voedger/voedger/pkg/goutils/logger"
	"github.com/voedger/voedger/pkg/goutils/testingu"
	"github.com/voedger/voedger/pkg/istorage/mem"
	istorageprovider "github.com/voedger/voedger/pkg/istorage/provider"
	"github.com/voedger/voedger/pkg/istructs"
	vvmstorage "github.com/voedger/voedger/pkg/vvm/storage"
)

const initialRecoveryTestAppID istructs.ClusterAppID = 7

func TestInitialRecoveryComputesMissingCheckpoints(t *testing.T) {
	appQName := istructs.AppQName_test1_app1
	appID := initialRecoveryTestAppID
	plog := newLogReadRecorder(t)

	// set partition 1 contains 3 plog events,
	plog.setEvents(1, nil,
		newPLogEvent(10, 10, 6, newCUDRow(istructs.FirstUserRecordID, true)),
		newPLogEvent(40, 10, 10, newCUDRow(istructs.FirstUserRecordID+1, true)),
		newPLogEvent(50, 11, 4),
	)
	// set partiton 2 contains no events
	plog.setEvents(2, nil)

	// set partition 0 checkpoint on plog offset 20
	storage := newCheckpointStorageRecorder(t)
	storage.setPartitionCheckpoint(t, appID, 0, checkpoints.PartitionCheckpoint{LastPLogOffset: 20})

	recovery := checkpoints.NewInitialRecovery(newAppStructsProvider(appID, plog.events), storage)

	// run the recovery
	require.NoError(t, recovery.Ensure(t.Context(), appQName, 3))

	// check the resulting checkpoints after initial recovery
	// partiton 0 contains no events but the checkpoint must not be overwritten -> 20 must be kept
	checkpoint, ok, err := storage.GetPartitionCheckpoint(appID, 0)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, checkpoints.PartitionCheckpoint{LastPLogOffset: 20}, checkpoint)
	require.Equal(t, 0, storage.putPartitionCheckputCount(0), "completed partition must not be rewritten")
	require.Equal(t, 0, plog.readPLogCallsCount(0), "completed partition must not be scanned")

	// partition 1: checkpoint must be on offset 50 since it is the offset of the last event we're injected before
	require.Equal(t, checkpoints.PartitionCheckpoint{LastPLogOffset: 50}, storage.requirePartitionCheckpoint(t, appID, 1))

	// partition 2: checkpoint must be on offset 0 since this partiton has no events
	require.Equal(t, checkpoints.PartitionCheckpoint{LastPLogOffset: 0}, storage.requirePartitionCheckpoint(t, appID, 2))

	// check workspace checkpoints
	// wsid 10
	expected := checkpoints.WorkspaceCheckpoint{WLogIDBearingOffset: 10}
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
	require.NoError(t, recovery.Ensure(t.Context(), istructs.AppQName_test1_app1, 1))

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
				storage.partitionGetErrors[0] = errors.New("injected failure")
			},
		},
		{
			name:      "read PLog",
			operation: "read PLog",
			readError: errors.New("injected failure"),
		},
		{
			name:          "put workspace checkpoint",
			operation:     "put workspace checkpoint",
			workspaceData: true,
			configureStorage: func(storage *checkpointStorageRecorder) {
				storage.workspacePutErrors[10] = errors.New("injected failure")
			},
		},
		{
			name:      "put partition checkpoint",
			operation: "put partition checkpoint",
			configureStorage: func(storage *checkpointStorageRecorder) {
				storage.partitionPutErrors[0] = errors.New("injected failure")
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
			logCap := logger.StartCapture(t, logger.LogLevelVerbose)
			recovery := checkpoints.NewInitialRecovery(newAppStructsProvider(appID, reads.events), storage)

			// Run initial recovery.
			require.NoError(t, recovery.Ensure(t.Context(), istructs.AppQName_test1_app1, 2))

			// Check that the failed partition remains incomplete while the successful partition is checkpointed.
			delete(storage.partitionGetErrors, 0)
			_, failedPartitionMarked, err := storage.GetPartitionCheckpoint(appID, 0)
			require.NoError(t, err)
			require.False(t, failedPartitionMarked)
			require.Equal(t, checkpoints.PartitionCheckpoint{LastPLogOffset: 3}, storage.requirePartitionCheckpoint(t, appID, 1))

			// Check that the partition failure is logged exactly once.
			logCap.HasLine("stage=bootstrap.initial_recovery.error", test.operation+": injected failure", "partid=0")
			require.Equal(t, 1, countInitialRecoveryErrorLines(logCap.String(), test.operation+": injected failure", "partid=0"))
		})
	}
}

func countInitialRecoveryErrorLines(logs string, fragments ...string) int {
	count := 0
	for _, line := range strings.Split(logs, "\n") {
		matches := true
		for _, fragment := range fragments {
			if !strings.Contains(line, fragment) {
				matches = false
				break
			}
		}
		if matches {
			count++
		}
	}
	return count
}

func TestInitialRecoveryRetriesOnlyIncompletePartitions(t *testing.T) {
	// Set partition 0 to fail during its first PLog read.
	appID := initialRecoveryTestAppID
	reads := newLogReadRecorder(t)
	entry := newPLogEvent(5, 0, 0)
	reads.setEvents(0, errors.New("temporary read failure"), entry)
	storage := newCheckpointStorageRecorder(t)
	recovery := checkpoints.NewInitialRecovery(newAppStructsProvider(appID, reads.events), storage)

	// Run initial recovery and check that the failed partition remains incomplete.
	require.NoError(t, recovery.Ensure(t.Context(), istructs.AppQName_test1_app1, 1))
	_, ok, err := storage.GetPartitionCheckpoint(appID, 0)
	require.NoError(t, err)
	require.False(t, ok)

	// Make the PLog read succeed and retry initial recovery.
	reads.setEvents(0, nil, entry)
	require.NoError(t, recovery.Ensure(t.Context(), istructs.AppQName_test1_app1, 1))

	// Check that the retried partition is checkpointed after the second read.
	require.Equal(t, checkpoints.PartitionCheckpoint{LastPLogOffset: 5}, storage.requirePartitionCheckpoint(t, appID, 0))
	require.Equal(t, 2, reads.readPLogCallsCount(0))

	// Run initial recovery again and check that the completed partition is skipped.
	require.NoError(t, recovery.Ensure(t.Context(), istructs.AppQName_test1_app1, 1))
	require.Equal(t, 2, reads.readPLogCallsCount(0), "completed partition must not be scanned again")
	require.Equal(t, 1, storage.putPartitionCheckputCount(0), "completed partition must not be written again")
}

func TestInitialRecoveryHonorsCancellationBeforeScheduling(t *testing.T) {
	appID := initialRecoveryTestAppID
	reads := newLogReadRecorder(t)
	storage := newCheckpointStorageRecorder(t)
	recovery := checkpoints.NewInitialRecovery(newAppStructsProvider(appID, reads.events), storage)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := recovery.Ensure(ctx, istructs.AppQName_test1_app1, 3)

	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, reads.readPLogCallsCountTotal())
}

func TestInitialRecoveryHasNoRuntimeRecoveryOrPublicationSideEffects(t *testing.T) {
	appID := initialRecoveryTestAppID
	reads := newLogReadRecorder(t)
	reads.setEvents(0, nil, newPLogEvent(1, 10, 1))
	storage := newCheckpointStorageRecorder(t)
	recovery := checkpoints.NewInitialRecovery(newAppStructsProvider(appID, reads.events), storage)

	require.NoError(t, recovery.Ensure(t.Context(), istructs.AppQName_test1_app1, 1))

	require.Equal(t, []string{"workspace:10", "partition:0"}, storage.getCheckpointsHistory())
	require.Equal(t, 1, reads.readPLogCallsCount(0))
}

func newAppStructsProvider(events istructs.IEvents, appIDs ...istructs.ClusterAppID) *coreutils.MockAppStructsProvider {
	asp := &coreutils.MockAppStructsProvider{}
	for _, appID := range appIDs {
		appStructs := &coreutils.MockAppStructs{}
		appStructs.On("ClusterAppID").Return(appID)
		appStructs.On("Events").Return(events)
		asp.On("BuiltIn", mock.Anything).Return(appStructs, nil).Once()
	}
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
