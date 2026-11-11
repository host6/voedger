/*
 * Copyright (c) 2022-present unTill Pro, Ltd.
 */

package stateprovide

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/voedger/voedger/pkg/appdef"
	"github.com/voedger/voedger/pkg/appdef/builder"
	"github.com/voedger/voedger/pkg/coreutils"
	"github.com/voedger/voedger/pkg/istructs"
	payloads "github.com/voedger/voedger/pkg/itokens-payloads"
	"github.com/voedger/voedger/pkg/state"
	"github.com/voedger/voedger/pkg/sys"
	"github.com/voedger/voedger/pkg/sys/authnz"
)

func TestHostState_BasicUsage(t *testing.T) {
	require := require.New(t)

	factory := ProvideQueryProcessorStateFactory()
	hostState := factory(context.Background(), mockedHostStateStructs, state.SimpleWSIDFunc(istructs.WSID(1)), nil, nil, nil, nil, nil, nil, nil, nil, nil, state.NullOpts, nil)

	// Declare simple extension
	extension := func(state istructs.IState) {
		//Create key
		key, err := state.KeyBuilder(sys.Storage_View, testViewRecordQName1)
		require.NoError(err)
		key.PutInt64("pkFld", 64)

		// Call to storage
		require.NoError(state.MustNotExist(key))
	}

	// Run extension
	extension(hostState)

	require.NoError(hostState.ValidateIntents())
	require.NoError(hostState.ApplyIntents())
}

func TestHostState_RequestContext(t *testing.T) {
	require := require.New(t)
	requestCtx := t.Context()
	hostState := newHostState(requestCtx, "ForTest", 10, nil)

	require.Same(requestCtx, hostState.RequestContext())
}

func TestCommandStateRequestSubject(t *testing.T) {
	require := require.New(t)
	for _, tc := range []struct {
		name     string
		expected payloads.PrincipalPayload
	}{
		{"User", payloads.PrincipalPayload{SubjectKind: istructs.SubjectKind_User, Login: "john.doe@acme.com", ProfileWSID: 42}},
		{"Device", payloads.PrincipalPayload{SubjectKind: istructs.SubjectKind_Device, Login: "device-1", ProfileWSID: 43}},
		{"Guest", payloads.PrincipalPayload{SubjectKind: istructs.SubjectKind_User, Login: istructs.SysGuestLogin, ProfileWSID: istructs.GuestWSID}},
		{"User with null profile", payloads.PrincipalPayload{SubjectKind: istructs.SubjectKind_User, Login: "system"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			subject, token := tc.expected, "original-token"
			host := ProvideCommandProcessorStateFactory()(t.Context(), nil, state.SimpleWSIDFunc(200), nil, nil,
				func() payloads.PrincipalPayload { return subject }, func() string { return token }, 1, nil, nil, nil, nil, nil, state.NullOpts, nil)
			key, err := host.KeyBuilder(sys.Storage_RequestSubject, appdef.NullQName)
			require.NoError(err)
			otherKey, err := host.KeyBuilder(sys.Storage_RequestSubject, appdef.NullQName)
			require.NoError(err)
			require.True(key.Equals(otherKey))
			actual, err := host.MustExist(key)
			require.NoError(err)
			// Subject identity comes from the payload callback, independently of request Workspace 200
			require.Equal(tc.expected.Login, actual.AsString(sys.Storage_RequestSubject_Field_Name))
			require.Equal(int32(tc.expected.SubjectKind), actual.AsInt32(sys.Storage_RequestSubject_Field_Kind))
			require.Equal(int64(tc.expected.ProfileWSID), actual.AsInt64(sys.Storage_RequestSubject_Field_ProfileWSID))
			require.Equal(token, actual.AsString(sys.Storage_RequestSubject_Field_Token))

			// Each lookup reads current callbacks without changing the previously returned identity
			subject = payloads.PrincipalPayload{SubjectKind: istructs.SubjectKind_Device, Login: "next-device", ProfileWSID: 91}
			token = "next-token"
			next, err := host.MustExist(key)
			require.NoError(err)
			require.Equal("next-device", next.AsString(sys.Storage_RequestSubject_Field_Name))
			require.Equal(int32(istructs.SubjectKind_Device), next.AsInt32(sys.Storage_RequestSubject_Field_Kind))
			require.Equal(int64(91), next.AsInt64(sys.Storage_RequestSubject_Field_ProfileWSID))
			require.Equal("next-token", next.AsString(sys.Storage_RequestSubject_Field_Token))
			require.Equal(tc.expected.Login, actual.AsString(sys.Storage_RequestSubject_Field_Name))
			require.Equal(int32(tc.expected.SubjectKind), actual.AsInt32(sys.Storage_RequestSubject_Field_Kind))
			require.Equal(int64(tc.expected.ProfileWSID), actual.AsInt64(sys.Storage_RequestSubject_Field_ProfileWSID))
			require.Equal("original-token", actual.AsString(sys.Storage_RequestSubject_Field_Token))
		})
	}
}

func mockedHostStateStructs() istructs.IAppStructs {
	mv := &coreutils.MockStateValue{}
	mv.
		On("AsInt64", "vFld").Return(int64(10)).
		On("AsInt64", state.ColOffset).Return(int64(45))
	mvb1 := &coreutils.MockStateValueBuilder{}
	mvb1.
		On("PutInt64", "vFld", int64(10)).
		On("PutInt64", state.ColOffset, int64(45)).
		On("Build").Return(mv)
	mvb2 := &coreutils.MockStateValueBuilder{}
	mvb2.
		On("PutInt64", "vFld", int64(10)).Once().
		On("PutInt64", state.ColOffset, int64(45)).Once().
		On("PutInt64", "vFld", int64(17)).Once().
		On("PutInt64", state.ColOffset, int64(46)).Once()
	mkb := &coreutils.MockStateKeyBuilder{}
	mkb.
		On("PutInt64", "pkFld", int64(64))
	value := &coreutils.MockStateValue{}
	value.On("AsString", "vk").Return("value")
	viewRecords := &coreutils.MockViewRecords{}
	viewRecords.
		On("KeyBuilder", testViewRecordQName1).Return(mkb).
		On("NewValueBuilder", testViewRecordQName1).Return(mvb1).Once().
		On("NewValueBuilder", testViewRecordQName1).Return(mvb2).Once().
		On("GetBatch", istructs.WSID(1), mock.AnythingOfType("[]istructs.ViewRecordGetBatchItem")).
		Return(nil).
		Run(func(args mock.Arguments) {
			args.Get(1).([]istructs.ViewRecordGetBatchItem)[0].Value = value
		}).
		On("Get", istructs.WSID(1), mock.Anything).Return(nil, nil).
		On("PutBatch", istructs.WSID(1), mock.AnythingOfType("[]istructs.ViewKV")).Return(nil)

	adb := builder.New()

	wsb := adb.AddWorkspace(testWSQName)
	wsDesc := wsb.AddCDoc(testWSDescriptorQName)
	wsDesc.AddField(authnz.Field_WSKind, appdef.DataKind_bytes, false)
	wsb.SetDescriptor(testWSDescriptorQName)

	view := wsb.AddView(testViewRecordQName1)
	view.Key().PartKey().AddField("pkFld", appdef.DataKind_int64)
	view.Key().ClustCols().AddField("ccFld", appdef.DataKind_string)
	view.Value().
		AddField("vFld", appdef.DataKind_int64, false).
		AddField(state.ColOffset, appdef.DataKind_int64, false)

	app := adb.MustBuild()

	mockWorkspaceRecord := &coreutils.MockRecord{}
	mockWorkspaceRecord.On("AsQName", "WSKind").Return(testWSDescriptorQName)
	mockWorkspaceRecord.On("QName").Return(appdef.QNameCDocWorkspaceDescriptor)
	mockedRecords := &coreutils.MockRecords{}
	mockedRecords.On("GetSingleton", istructs.WSID(1), mock.Anything).Return(mockWorkspaceRecord, nil)

	appStructs := &coreutils.MockAppStructs{}
	appStructs.
		On("AppDef").Return(app).
		On("AppQName").Return(testAppQName).
		On("ViewRecords").Return(viewRecords).
		On("Events").Return(&nilEvents{}).
		On("Records").Return(mockedRecords)
	return appStructs
}
func TestHostState_KeyBuilder_Should_return_unknown_storage_ID_error(t *testing.T) {
	require := require.New(t)
	s := hostStateForTest(&mockStorage{})

	_, err := s.KeyBuilder(appdef.NullQName, appdef.NullQName)

	require.ErrorIs(err, ErrUnknownStorage)
}
func TestHostState_CanExist(t *testing.T) {
	require := require.New(t)
	t.Run("Should be ok", func(t *testing.T) {
		ms := &mockStorage{}
		ms.
			On("NewKeyBuilder", appdef.NullQName, nil).Return(newMapKeyBuilder(testStorage, appdef.NullQName)).
			On("GetBatch", mock.AnythingOfType("[]state.GetBatchItem")).
			Return(nil).
			Run(func(args mock.Arguments) {
				args.Get(0).([]state.GetBatchItem)[0].Value = &coreutils.MockStateValue{}
			})
		s := hostStateForTest(ms)
		k, err := s.KeyBuilder(testStorage, appdef.NullQName)
		require.NoError(err)

		_, ok, err := s.CanExist(k)
		require.NoError(err)

		require.True(ok)
	})
	t.Run("Should return error when error occurred", func(t *testing.T) {
		ms := &mockStorage{}
		ms.
			On("NewKeyBuilder", appdef.NullQName, nil).Return(newMapKeyBuilder(testStorage, appdef.NullQName)).
			On("GetBatch", mock.AnythingOfType("[]state.GetBatchItem")).Return(errTest)
		s := hostStateForTest(ms)
		k, err := s.KeyBuilder(testStorage, appdef.NullQName)
		require.NoError(err)

		_, _, err = s.CanExist(k)

		require.ErrorIs(err, errTest)
	})
	t.Run("Should return get batch not supported by storage error", func(t *testing.T) {
		ms := &mockStorage{}
		ms.On("NewKeyBuilder", appdef.NullQName, nil).Return(newMapKeyBuilder(testStorage, appdef.NullQName))
		s, _ := emptyHostStateForTest(ms)
		kb, err := s.KeyBuilder(testStorage, appdef.NullQName)
		require.NoError(err)

		_, _, err = s.CanExist(kb)

		require.ErrorIs(err, ErrGetNotSupportedByStorage)
	})
}
func TestHostState_CanExistAll(t *testing.T) {
	require := require.New(t)
	t.Run("Should be ok", func(t *testing.T) {
		times := 0
		ms := &mockStorage{}
		ms.
			On("NewKeyBuilder", appdef.NullQName, nil).Return(newMapKeyBuilder(testStorage, appdef.NullQName)).
			On("GetBatch", mock.AnythingOfType("[]state.GetBatchItem")).
			Return(nil).
			Run(func(args mock.Arguments) {
				args.Get(0).([]state.GetBatchItem)[0].Value = &coreutils.MockStateValue{}
			})
		s := hostStateForTest(ms)
		k, err := s.KeyBuilder(testStorage, appdef.NullQName)
		require.NoError(err)

		err = s.CanExistAll([]istructs.IStateKeyBuilder{k}, func(key istructs.IKeyBuilder, value istructs.IStateValue, ok bool) (err error) {
			times++
			require.Equal(k, key)
			require.True(ok)
			return
		})
		require.NoError(err)

		require.Equal(1, times)
	})
	t.Run("Should return error when error occurred", func(t *testing.T) {
		ms := &mockStorage{}
		ms.
			On("NewKeyBuilder", appdef.NullQName, nil).Return(newMapKeyBuilder(testStorage, appdef.NullQName)).
			On("GetBatch", mock.AnythingOfType("[]state.GetBatchItem")).Return(errTest)
		s := hostStateForTest(ms)
		k, err := s.KeyBuilder(testStorage, appdef.NullQName)
		require.NoError(err)

		err = s.CanExistAll([]istructs.IStateKeyBuilder{k}, nil)

		require.ErrorIs(err, errTest)
	})
	t.Run("Should return get not supported by storage error", func(t *testing.T) {
		ms := &mockStorage{}
		ms.On("NewKeyBuilder", appdef.NullQName, nil).Return(newMapKeyBuilder(testStorage, appdef.NullQName))
		s, _ := emptyHostStateForTest(ms)
		kb, err := s.KeyBuilder(testStorage, appdef.NullQName)
		require.NoError(err)

		err = s.CanExistAll([]istructs.IStateKeyBuilder{kb}, nil)

		require.ErrorIs(err, ErrGetNotSupportedByStorage)
	})
}
func TestHostState_MustExist(t *testing.T) {
	require := require.New(t)
	t.Run("Should be ok", func(t *testing.T) {
		ms := &mockStorage{}
		ms.
			On("NewKeyBuilder", appdef.NullQName, nil).Return(newMapKeyBuilder(testStorage, appdef.NullQName)).
			On("GetBatch", mock.AnythingOfType("[]state.GetBatchItem")).
			Return(nil).
			Run(func(args mock.Arguments) {
				args.Get(0).([]state.GetBatchItem)[0].Value = &coreutils.MockStateValue{}
			})
		s := hostStateForTest(ms)
		k, err := s.KeyBuilder(testStorage, appdef.NullQName)
		require.NoError(err)

		_, err = s.MustExist(k)

		require.NoError(err)
	})
	t.Run("Should return error when entity not exists", func(t *testing.T) {
		ms := &mockStorage{}
		ms.
			On("NewKeyBuilder", testEntity, nil).Return(newMapKeyBuilder(testStorage, appdef.NullQName)).
			On("GetBatch", mock.AnythingOfType("[]state.GetBatchItem")).
			Return(nil).
			Run(func(args mock.Arguments) {
				args.Get(0).([]state.GetBatchItem)[0].Value = nil
			})
		s := hostStateForTest(ms)
		k, err := s.KeyBuilder(testStorage, testEntity)
		require.NoError(err)

		_, err = s.MustExist(k)

		require.ErrorIs(err, ErrNotExists)
		require.Equal("state ForTest, key {storage:test.testStorage}: not exists", err.Error())
	})
	t.Run("Should return error when error occurred on get batch", func(t *testing.T) {
		ms := &mockStorage{}
		ms.
			On("NewKeyBuilder", appdef.NullQName, nil).Return(newMapKeyBuilder(testStorage, appdef.NullQName)).
			On("GetBatch", mock.AnythingOfType("[]state.GetBatchItem")).Return(errTest)
		s := hostStateForTest(ms)
		k, err := s.KeyBuilder(testStorage, appdef.NullQName)
		require.NoError(err)

		_, err = s.MustExist(k)

		require.ErrorIs(err, errTest)
	})
}
func TestHostState_MustExistAll(t *testing.T) {
	require := require.New(t)
	t.Run("Should be ok", func(t *testing.T) {
		ms := &mockStorage{}
		ms.
			On("NewKeyBuilder", appdef.NullQName, nil).Return(newMapKeyBuilder(testStorage, appdef.NullQName)).
			On("GetBatch", mock.AnythingOfType("[]state.GetBatchItem")).
			Return(nil).
			Run(func(args mock.Arguments) {
				args.Get(0).([]state.GetBatchItem)[0].Value = &coreutils.MockStateValue{}
				args.Get(0).([]state.GetBatchItem)[1].Value = &coreutils.MockStateValue{}
			})
		s := hostStateForTest(ms)
		k1, err := s.KeyBuilder(testStorage, appdef.NullQName)
		require.NoError(err)
		k2, err := s.KeyBuilder(testStorage, appdef.NullQName)
		require.NoError(err)
		kk := make([]istructs.IKeyBuilder, 0, 2)

		err = s.MustExistAll([]istructs.IStateKeyBuilder{k1, k2}, func(key istructs.IKeyBuilder, value istructs.IStateValue, ok bool) (err error) {
			kk = append(kk, key)
			require.True(ok)
			return
		})
		require.NoError(err)

		require.Equal(k1, kk[0])
		require.Equal(k1, kk[1])
	})
	t.Run("Should return error on get batch", func(t *testing.T) {
		ms := &mockStorage{}
		ms.
			On("NewKeyBuilder", appdef.NullQName, nil).Return(newMapKeyBuilder(testStorage, appdef.NullQName)).
			On("GetBatch", mock.AnythingOfType("[]state.GetBatchItem")).Return(errTest)
		s := hostStateForTest(ms)
		k, err := s.KeyBuilder(testStorage, appdef.NullQName)
		require.NoError(err)

		err = s.MustExistAll([]istructs.IStateKeyBuilder{k}, nil)

		require.ErrorIs(err, errTest)
	})
	t.Run("Should return error when entity not exists", func(t *testing.T) {
		ms := &mockStorage{}
		ms.
			On("NewKeyBuilder", appdef.NullQName, nil).Return(newMapKeyBuilder(testStorage, appdef.NullQName)).
			On("GetBatch", mock.AnythingOfType("[]state.GetBatchItem")).
			Return(nil).
			Run(func(args mock.Arguments) {
				args.Get(0).([]state.GetBatchItem)[0].Value = &coreutils.MockStateValue{}
				args.Get(0).([]state.GetBatchItem)[1].Value = nil
			})
		s := hostStateForTest(ms)
		k1, err := s.KeyBuilder(testStorage, appdef.NullQName)
		require.NoError(err)
		k2, err := s.KeyBuilder(testStorage, appdef.NullQName)
		require.NoError(err)

		err = s.MustExistAll([]istructs.IStateKeyBuilder{k1, k2}, nil)

		require.ErrorIs(err, ErrNotExists)
	})
	t.Run("Should return get not supported by storage error", func(t *testing.T) {
		ms := &mockStorage{}
		ms.On("NewKeyBuilder", appdef.NullQName, nil).Return(newMapKeyBuilder(testStorage, appdef.NullQName))
		s, _ := emptyHostStateForTest(ms)
		kb, err := s.KeyBuilder(testStorage, appdef.NullQName)
		require.NoError(err)

		err = s.MustExistAll([]istructs.IStateKeyBuilder{kb}, nil)

		require.ErrorIs(err, ErrGetNotSupportedByStorage)
	})
}
func TestHostState_MustNotExist(t *testing.T) {
	require := require.New(t)
	t.Run("Should be ok", func(t *testing.T) {
		ms := &mockStorage{}
		ms.
			On("NewKeyBuilder", appdef.NullQName, nil).Return(newMapKeyBuilder(testStorage, appdef.NullQName)).
			On("GetBatch", mock.AnythingOfType("[]state.GetBatchItem")).
			Return(nil).
			Run(func(args mock.Arguments) {
				args.Get(0).([]state.GetBatchItem)[0].Value = nil
			})
		s := hostStateForTest(ms)
		k, err := s.KeyBuilder(testStorage, appdef.NullQName)
		require.NoError(err)

		err = s.MustNotExist(k)

		require.NoError(err)
	})
	t.Run("Should return error when entity exists", func(t *testing.T) {
		ms := &mockStorage{}
		ms.
			On("NewKeyBuilder", appdef.NullQName, nil).Return(newMapKeyBuilder(testStorage, appdef.NullQName)).
			On("GetBatch", mock.AnythingOfType("[]state.GetBatchItem")).
			Return(nil).
			Run(func(args mock.Arguments) {
				args.Get(0).([]state.GetBatchItem)[0].Value = &coreutils.MockStateValue{}
			})
		s := hostStateForTest(ms)
		k, err := s.KeyBuilder(testStorage, appdef.NullQName)
		require.NoError(err)

		err = s.MustNotExist(k)

		require.ErrorIs(err, ErrExists)
	})
	t.Run("Should return error when error occurred on get batch", func(t *testing.T) {
		ms := &mockStorage{}
		ms.
			On("NewKeyBuilder", appdef.NullQName, nil).Return(newMapKeyBuilder(testStorage, appdef.NullQName)).
			On("GetBatch", mock.AnythingOfType("[]state.GetBatchItem")).Return(errTest)
		s := hostStateForTest(ms)
		k, err := s.KeyBuilder(testStorage, appdef.NullQName)
		require.NoError(err)

		err = s.MustNotExist(k)

		require.ErrorIs(err, errTest)
	})
}
func TestHostState_MustNotExistAll(t *testing.T) {
	require := require.New(t)
	t.Run("Should be ok", func(t *testing.T) {
		ms := &mockStorage{}
		ms.
			On("NewKeyBuilder", appdef.NullQName, nil).Return(newMapKeyBuilder(testStorage, appdef.NullQName)).
			On("GetBatch", mock.AnythingOfType("[]state.GetBatchItem")).
			Return(nil).
			Run(func(args mock.Arguments) {
				args.Get(0).([]state.GetBatchItem)[0].Value = nil
				args.Get(0).([]state.GetBatchItem)[1].Value = nil
			})
		s := hostStateForTest(ms)
		k1, err := s.KeyBuilder(testStorage, appdef.NullQName)
		require.NoError(err)
		k2, err := s.KeyBuilder(testStorage, appdef.NullQName)
		require.NoError(err)

		err = s.MustNotExistAll([]istructs.IStateKeyBuilder{k1, k2})

		require.NoError(err)
	})
	t.Run("Should return error on get batch", func(t *testing.T) {
		ms := &mockStorage{}
		ms.
			On("NewKeyBuilder", appdef.NullQName, nil).Return(newMapKeyBuilder(testStorage, appdef.NullQName)).
			On("GetBatch", mock.AnythingOfType("[]state.GetBatchItem")).Return(errTest)
		s := hostStateForTest(ms)
		k, err := s.KeyBuilder(testStorage, appdef.NullQName)
		require.NoError(err)

		err = s.MustNotExistAll([]istructs.IStateKeyBuilder{k})

		require.ErrorIs(err, errTest)
	})
	t.Run("Should return error when entity exists", func(t *testing.T) {
		ms := &mockStorage{}
		ms.
			On("NewKeyBuilder", appdef.NullQName, nil).Return(newMapKeyBuilder(testStorage, appdef.NullQName)).
			On("GetBatch", mock.AnythingOfType("[]state.GetBatchItem")).
			Return(nil).
			Run(func(args mock.Arguments) {
				args.Get(0).([]state.GetBatchItem)[0].Value = nil
				args.Get(0).([]state.GetBatchItem)[1].Value = &coreutils.MockStateValue{}
			})
		s := hostStateForTest(ms)
		k1, err := s.KeyBuilder(testStorage, appdef.NullQName)
		require.NoError(err)
		k2, err := s.KeyBuilder(testStorage, appdef.NullQName)
		require.NoError(err)

		err = s.MustNotExistAll([]istructs.IStateKeyBuilder{k1, k2})

		require.ErrorIs(err, ErrExists)
	})
	t.Run("Should return get not supported by storage error", func(t *testing.T) {
		ms := &mockStorage{}
		ms.On("NewKeyBuilder", appdef.NullQName, nil).Return(newMapKeyBuilder(testStorage, appdef.NullQName))
		s, _ := emptyHostStateForTest(ms)
		kb, err := s.KeyBuilder(testStorage, appdef.NullQName)
		require.NoError(err)

		err = s.MustNotExistAll([]istructs.IStateKeyBuilder{kb})

		require.ErrorIs(err, ErrGetNotSupportedByStorage)
	})
}
func TestHostState_Read(t *testing.T) {
	require := require.New(t)
	t.Run("Should be ok", func(t *testing.T) {
		ms := &mockStorage{}
		ms.
			On("NewKeyBuilder", appdef.NullQName, nil).Return(newMapKeyBuilder(testStorage, appdef.NullQName)).
			On("Read", mock.Anything, mock.AnythingOfType("istructs.ValueCallback")).Return(nil)
		s := hostStateForTest(ms)
		k, err := s.KeyBuilder(testStorage, appdef.NullQName)
		require.NoError(err)

		require.NoError(s.Read(k, nil))

		ms.AssertExpectations(t)
	})
	t.Run("Should return read not supported by storage error", func(t *testing.T) {
		ms := &mockStorage{}
		ms.On("NewKeyBuilder", appdef.NullQName, nil).Return(newMapKeyBuilder(testStorage, appdef.NullQName))
		s, _ := emptyHostStateForTest(ms)
		kb, err := s.KeyBuilder(testStorage, appdef.NullQName)
		require.NoError(err)

		err = s.Read(kb, nil)

		require.ErrorIs(err, ErrReadNotSupportedByStorage)
	})
}
func TestHostState_NewValue(t *testing.T) {
	require := require.New(t)
	t.Run("Should return error when intents limit exceeded", func(t *testing.T) {
		ms := &mockStorage{}
		ms.On("NewKeyBuilder", appdef.NullQName, nil).Return(newMapKeyBuilder(testStorage, appdef.NullQName))
		s, i := limitedIntentsHostStateForTest(ms)
		kb, err := s.KeyBuilder(testStorage, appdef.NullQName)
		require.NoError(err)

		_, err = i.NewValue(kb)

		require.ErrorIs(err, ErrIntentsLimitExceeded)
	})
	t.Run("Should return insert not supported by storage error", func(t *testing.T) {
		ms := &mockStorage{}
		ms.On("NewKeyBuilder", appdef.NullQName, nil).Return(newMapKeyBuilder(testStorage, appdef.NullQName))
		s, i := emptyHostStateForTest(ms)
		kb, err := s.KeyBuilder(testStorage, appdef.NullQName)
		require.NoError(err)

		_, err = i.NewValue(kb)

		require.ErrorIs(err, ErrInsertNotSupportedByStorage)
	})
}
func TestHostState_UpdateValue(t *testing.T) {
	require := require.New(t)
	t.Run("Should return error when intents limit exceeded", func(t *testing.T) {
		ms := &mockStorage{}
		ms.On("NewKeyBuilder", appdef.NullQName, nil).Return(newMapKeyBuilder(testStorage, appdef.NullQName))
		s, i := limitedIntentsHostStateForTest(ms)
		kb, err := s.KeyBuilder(testStorage, appdef.NullQName)
		require.NoError(err)

		_, err = i.UpdateValue(kb, nil)

		require.ErrorIs(err, ErrIntentsLimitExceeded)
	})
	t.Run("Should return update not supported by storage error", func(t *testing.T) {
		ms := &mockStorage{}
		ms.On("NewKeyBuilder", appdef.NullQName, nil).Return(newMapKeyBuilder(testStorage, appdef.NullQName))
		s, i := emptyHostStateForTest(ms)
		kb, err := s.KeyBuilder(testStorage, appdef.NullQName)
		require.NoError(err)

		_, err = i.UpdateValue(kb, nil)

		require.ErrorIs(err, ErrUpdateNotSupportedByStorage)
	})
}
func TestHostState_ValidateIntents(t *testing.T) {
	require := require.New(t)
	t.Run("Should be ok", func(t *testing.T) {
		ms := &mockStorage{}
		ms.
			On("NewKeyBuilder", appdef.NullQName, nil).Return(newMapKeyBuilder(testStorage, appdef.NullQName)).
			On("ProvideValueBuilder", mock.Anything, mock.Anything).Return(&coreutils.MockStateValueBuilder{}, nil).
			On("Validate", mock.Anything).Return(nil)
		s := hostStateForTest(ms)
		kb, err := s.KeyBuilder(testStorage, appdef.NullQName)
		require.NoError(err)
		_, err = s.NewValue(kb)
		require.NoError(err)

		err = s.ValidateIntents()

		require.NoError(err)
	})
	t.Run("Should return immediately when intents are empty", func(t *testing.T) {
		ms := &mockStorage{}
		s := hostStateForTest(&mockStorage{})

		require.NoError(s.ValidateIntents())

		ms.AssertNotCalled(t, "Validate", mock.Anything)
	})
	t.Run("Should return validation error", func(t *testing.T) {
		ms := &mockStorage{}
		ms.
			On("NewKeyBuilder", appdef.NullQName, nil).Return(newMapKeyBuilder(testStorage, appdef.NullQName)).
			On("ProvideValueBuilder", mock.Anything, mock.Anything).Return(&coreutils.MockStateValueBuilder{}, nil).
			On("Validate", mock.Anything).Return(errTest)
		s := hostStateForTest(ms)
		kb, err := s.KeyBuilder(testStorage, appdef.NullQName)
		require.NoError(err)
		_, err = s.NewValue(kb)
		require.NoError(err)

		err = s.ValidateIntents()

		require.ErrorIs(err, errTest)
	})
}
func TestHostState_ApplyIntents(t *testing.T) {
	require := require.New(t)
	t.Run("Should be ok", func(t *testing.T) {
		ms := &mockStorage{}
		ms.
			On("NewKeyBuilder", appdef.NullQName, nil).Return(newMapKeyBuilder(testStorage, appdef.NullQName)).
			On("ProvideValueBuilder", mock.Anything, mock.Anything).Return(&coreutils.MockStateValueBuilder{}, nil).
			On("ApplyBatch", mock.Anything).Return(nil)
		s := hostStateForTest(ms)
		kb, err := s.KeyBuilder(testStorage, appdef.NullQName)
		require.NoError(err)
		_, err = s.NewValue(kb)
		require.NoError(err)

		require.NoError(s.ApplyIntents())

		ms.AssertExpectations(t)
	})
	t.Run("Should return apply batch error", func(t *testing.T) {
		ms := &mockStorage{}
		ms.
			On("NewKeyBuilder", appdef.NullQName, nil).Return(newMapKeyBuilder(testStorage, appdef.NullQName)).
			On("ProvideValueBuilder", mock.Anything, mock.Anything).Return(&coreutils.MockStateValueBuilder{}, nil).
			On("ApplyBatch", mock.Anything).Return(errTest)
		s := hostStateForTest(ms)
		kb, err := s.KeyBuilder(testStorage, appdef.NullQName)
		require.NoError(err)
		_, err = s.NewValue(kb)
		require.NoError(err)

		err = s.ApplyIntents()

		require.ErrorIs(err, errTest)
	})
}
func hostStateForTest(s state.IStateStorage) state.IHostState {
	hs := newHostState(context.Background(), "ForTest", 10, nil)
	hs.addStorage(testStorage, s, S_GET_BATCH|S_READ|S_INSERT|S_UPDATE)
	return hs
}
func emptyHostStateForTest(s state.IStateStorage) (istructs.IState, istructs.IIntents) {
	bs := ProvideQueryProcessorStateFactory()(context.Background(), nilAppStructsFunc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, state.NullOpts, nil).(*queryProcessorState)
	bs.addStorage(testStorage, s, math.MinInt)
	return bs, bs
}
func limitedIntentsHostStateForTest(s state.IStateStorage) (istructs.IState, istructs.IIntents) {
	hs := newHostState(context.Background(), "LimitedIntentsForTest", 0, nil)
	hs.addStorage(testStorage, s, S_GET_BATCH|S_READ|S_INSERT|S_UPDATE)
	return hs, hs
}
