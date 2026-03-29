/*
  - Copyright (c) 2024-present unTill Software Development Group B.V.
    @author Michael Saigachenko
*/
package stateprovide

import (
	"context"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
	"github.com/voedger/voedger/pkg/appdef"
	"github.com/voedger/voedger/pkg/iauthnz"
	"github.com/voedger/voedger/pkg/istructs"
	"github.com/voedger/voedger/pkg/state"
	"github.com/voedger/voedger/pkg/sys"
)

type testQueryParams struct {
	callbackFunc istructs.ExecQueryCallback
}

func (p *testQueryParams) AppStructs() istructs.IAppStructs  { return nil }
func (p *testQueryParams) WSID() istructs.WSID               { return 0 }
func (p *testQueryParams) Principals() []iauthnz.Principal   { return nil }
func (p *testQueryParams) Token() string                     { return "" }
func (p *testQueryParams) PrepareArgs() istructs.PrepareArgs { return istructs.PrepareArgs{} }
func (p *testQueryParams) Arg() istructs.IObject             { return nil }
func (p *testQueryParams) ResultBuilder() istructs.IObjectBuilder {
	return istructs.NewNullObjectBuilder()
}
func (p *testQueryParams) QueryCallback() istructs.ExecQueryCallback { return p.callbackFunc }

func TestQueryProcessorState(t *testing.T) {

	require := require.New(t)
	sentObjects := make([]istructs.IObject, 0)

	params := &testQueryParams{
		callbackFunc: func(object istructs.IObject) error {
			sentObjects = append(sentObjects, object)
			return nil
		},
	}

	qps := ProvideQueryProcessorStateFactory()(context.Background(), params, nil, nil, nil, state.StateOpts{}, nil)
	kb, err := qps.KeyBuilder(sys.Storage_Result, appdef.NullQName)
	require.NoError(err)
	require.NotNil(kb)
	rows := queryProcessorStateMaxIntents + 1
	for i := 0; i < rows; i++ {
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
