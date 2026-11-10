/*
  - Copyright (c) 2024-present unTill Software Development Group B.V.
    @author Michael Saigachenko
*/
package stateprovide

import (
	"context"
	payloads "github.com/voedger/voedger/pkg/itokens-payloads"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
	"github.com/voedger/voedger/pkg/appdef"
	"github.com/voedger/voedger/pkg/istructs"
	"github.com/voedger/voedger/pkg/state"
	"github.com/voedger/voedger/pkg/sys"
)

func TestQueryProcessorState(t *testing.T) {

	require := require.New(t)
	sentObjects := make([]istructs.IObject, 0)

	execQueryCallbackFunc := func() istructs.ExecQueryCallback {
		return func(object istructs.IObject) error {
			sentObjects = append(sentObjects, object)
			return nil
		}
	}

	qps := ProvideQueryProcessorStateFactory()(context.Background(), nil, nil, nil, nil, nil, nil, nil, nil, istructs.NewNullObjectBuilder, nil, execQueryCallbackFunc, state.StateOpts{}, nil)
	kb, err := qps.KeyBuilder(sys.Storage_Result, appdef.NullQName)
	require.NoError(err)
	require.NotNil(kb)
	rows := queryProcessorStateMaxIntents + 1
	for range rows {
		vb, err := qps.NewValue(kb)
		require.NoError(err)
		require.NotNil(vb)
	}

	intent := qps.FindIntent(kb)
	require.NotNil(intent)

	err = qps.ApplyIntents()
	require.NoError(err)
	require.Len(sentObjects, rows)
	require.NotEqual(unsafe.Pointer(&sentObjects[0]), unsafe.Pointer(&sentObjects[1]))
}

func TestQueryStateRequestSubject(t *testing.T) {
	require := require.New(t)
	subject := payloads.PrincipalPayload{SubjectKind: istructs.SubjectKind_User, Login: "canonical", ProfileWSID: 42}
	token := "original-token"
	host := ProvideQueryProcessorStateFactory()(t.Context(), nil, state.SimpleWSIDFunc(200), nil,
		func() payloads.PrincipalPayload { return subject }, func() string { return token }, nil, nil, nil, nil, nil, nil, state.NullOpts, nil)
	key, err := host.KeyBuilder(sys.Storage_RequestSubject, appdef.NullQName)
	require.NoError(err)
	value, err := host.MustExist(key)
	require.NoError(err)
	// Query state must preserve the profile and original token rather than substituting request Workspace 200
	require.Equal("canonical", value.AsString(sys.Storage_RequestSubject_Field_Name))
	require.Equal(int32(istructs.SubjectKind_User), value.AsInt32(sys.Storage_RequestSubject_Field_Kind))
	require.Equal(int64(42), value.AsInt64(sys.Storage_RequestSubject_Field_ProfileWSID))
	require.Equal(token, value.AsString(sys.Storage_RequestSubject_Field_Token))
}
