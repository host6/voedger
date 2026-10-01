/*
 * Copyright (c) 2026-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package checkpoints

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/voedger/voedger/pkg/appdef"
	"github.com/voedger/voedger/pkg/istructs"
	"github.com/voedger/voedger/pkg/istructsmem"
)

func TestRecoveryCheckpointProjector(t *testing.T) {
	require := require.New(t)
	storage := &testCheckpointStorage{}
	projector := recoveryCheckpointProjector(storage)
	event := &testPLogEvent{
		partition:  4,
		pLogOffset: 10,
		wsid:       5,
		wLogOffset: 20,
		cuds: []istructs.ICUDRow{
			&testCUDRow{id: istructs.FirstUserRecordID + 100, isNew: true},
			&testCUDRow{id: istructs.FirstUserRecordID + 500},
		},
		argument: istructs.NewNullObject(),
	}
	state := &testState{appStructs: &testAppStructs{
		clusterAppID: 7,
		appDef:       &testAppDef{},
	}}

	require.NoError(projector(event, state, nil))
	require.Equal([]string{"workspace", "partition"}, storage.calls)
	require.Equal(istructs.ClusterAppID(7), storage.appID)
	require.Equal(istructs.WSID(5), storage.wsid)
	require.Equal(WorkspaceCheckpoint{
		NextWLogOffset: 21,
		NextRecordID:   istructs.FirstUserRecordID + 101,
	}, storage.workspace)
	require.Equal(istructs.PartitionID(4), storage.partitionID)
	require.Equal(PartitionCheckpoint{NextPLogOffset: 11}, storage.partition)
}

func TestRecoveryCheckpointProjectorDoesNotAdvancePartitionWhenWorkspaceWriteFails(t *testing.T) {
	require := require.New(t)
	injectedErr := errors.New("injected workspace checkpoint failure")
	storage := &testCheckpointStorage{workspaceErr: injectedErr}
	projector := recoveryCheckpointProjector(storage)
	event := &testPLogEvent{
		partition:  4,
		pLogOffset: 10,
		wsid:       5,
		wLogOffset: 20,
		argument:   istructs.NewNullObject(),
	}
	state := &testState{appStructs: &testAppStructs{
		clusterAppID: 7,
		appDef:       &testAppDef{},
	}}

	require.ErrorIs(projector(event, state, nil), injectedErr)
	require.Equal([]string{"workspace"}, storage.calls)
}

func TestWorkspaceCheckpointIncludesODocRecordIDs(t *testing.T) {
	require := require.New(t)
	oDocQName := appdef.NewQName("test", "Order")
	argument := &testObject{
		qname: oDocQName,
		id:    istructs.FirstUserRecordID + 200,
		children: map[string][]istructs.IObject{
			"Items": {&testObject{id: istructs.FirstUserRecordID + 202}},
		},
	}
	event := &testPLogEvent{
		wLogOffset: 30,
		argument:   argument,
		cuds:       []istructs.ICUDRow{&testCUDRow{id: istructs.FirstUserRecordID + 201, isNew: true}},
	}
	findType := func(name appdef.QName) appdef.IType {
		if name == oDocQName {
			return &testType{kind: appdef.TypeKind_ODoc}
		}
		return appdef.NullType
	}

	require.Equal(WorkspaceCheckpoint{
		NextWLogOffset: 31,
		NextRecordID:   istructs.FirstUserRecordID + 203,
	}, workspaceCheckpoint(event, findType))
}

func TestProvideRegistersStandardAsyncProjector(t *testing.T) {
	require := require.New(t)
	resources := istructsmem.NewStatelessResources()
	storage := &testCheckpointStorage{}
	Provide(resources, storage)

	found := false
	resources.Projectors(func(path string, projector istructs.Projector) bool {
		require.Equal(appdef.SysPackagePath, path)
		require.Equal(QNameProjectorRecoveryCheckpoint, projector.Name)
		found = true
		return true
	})
	require.True(found)
}

type testCheckpointStorage struct {
	calls        []string
	workspaceErr error
	partitionErr error
	appID        istructs.ClusterAppID
	partitionID  istructs.PartitionID
	wsid         istructs.WSID
	partition    PartitionCheckpoint
	workspace    WorkspaceCheckpoint
}

func (*testCheckpointStorage) GetPartitionCheckpoint(istructs.ClusterAppID, istructs.PartitionID) (PartitionCheckpoint, bool, error) {
	return PartitionCheckpoint{}, false, nil
}

func (s *testCheckpointStorage) PutPartitionCheckpoint(appID istructs.ClusterAppID, partitionID istructs.PartitionID, checkpoint PartitionCheckpoint) error {
	s.calls = append(s.calls, "partition")
	s.appID = appID
	s.partitionID = partitionID
	s.partition = checkpoint
	return s.partitionErr
}

func (*testCheckpointStorage) GetWorkspaceCheckpoint(istructs.ClusterAppID, istructs.WSID) (WorkspaceCheckpoint, bool, error) {
	return WorkspaceCheckpoint{}, false, nil
}

func (s *testCheckpointStorage) PutWorkspaceCheckpoint(appID istructs.ClusterAppID, wsid istructs.WSID, checkpoint WorkspaceCheckpoint) error {
	s.calls = append(s.calls, "workspace")
	s.appID = appID
	s.wsid = wsid
	s.workspace = checkpoint
	return s.workspaceErr
}

type testState struct {
	istructs.IState
	appStructs istructs.IAppStructs
}

func (s *testState) AppStructs() istructs.IAppStructs { return s.appStructs }

type testAppStructs struct {
	istructs.IAppStructs
	clusterAppID istructs.ClusterAppID
	appDef       appdef.IAppDef
}

func (s *testAppStructs) ClusterAppID() istructs.ClusterAppID { return s.clusterAppID }
func (s *testAppStructs) AppDef() appdef.IAppDef              { return s.appDef }

type testAppDef struct {
	appdef.IAppDef
	types map[appdef.QName]appdef.IType
}

func (d *testAppDef) Type(name appdef.QName) appdef.IType {
	if typ := d.types[name]; typ != nil {
		return typ
	}
	return appdef.NullType
}

type testType struct {
	appdef.IType
	kind appdef.TypeKind
}

func (t *testType) Kind() appdef.TypeKind { return t.kind }

type testPLogEvent struct {
	istructs.IPLogEvent
	partition  istructs.PartitionID
	pLogOffset istructs.Offset
	wsid       istructs.WSID
	wLogOffset istructs.Offset
	cuds       []istructs.ICUDRow
	argument   istructs.IObject
}

func (e *testPLogEvent) HandlingPartition() istructs.PartitionID { return e.partition }
func (e *testPLogEvent) PLogOffset() istructs.Offset             { return e.pLogOffset }
func (e *testPLogEvent) Workspace() istructs.WSID                { return e.wsid }
func (e *testPLogEvent) WLogOffset() istructs.Offset             { return e.wLogOffset }
func (e *testPLogEvent) ArgumentObject() istructs.IObject        { return e.argument }
func (e *testPLogEvent) CUDs(yield func(istructs.ICUDRow) bool) {
	for _, row := range e.cuds {
		if !yield(row) {
			return
		}
	}
}

type testCUDRow struct {
	istructs.ICUDRow
	id    istructs.RecordID
	isNew bool
}

func (r *testCUDRow) ID() istructs.RecordID { return r.id }
func (r *testCUDRow) IsNew() bool           { return r.isNew }

type testObject struct {
	istructs.IObject
	qname    appdef.QName
	id       istructs.RecordID
	children map[string][]istructs.IObject
}

func (o *testObject) QName() appdef.QName { return o.qname }
func (o *testObject) AsRecordID(name appdef.FieldName) istructs.RecordID {
	if name == appdef.SystemField_ID {
		return o.id
	}
	return istructs.NullRecordID
}
func (o *testObject) Containers(yield func(string) bool) {
	for name := range o.children {
		if !yield(name) {
			return
		}
	}
}
func (o *testObject) Children(containers ...string) func(func(istructs.IObject) bool) {
	return func(yield func(istructs.IObject) bool) {
		for _, container := range containers {
			for _, child := range o.children[container] {
				if !yield(child) {
					return
				}
			}
		}
	}
}
