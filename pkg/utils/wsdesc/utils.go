/*
 * Copyright (c) 2024-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package wsdescutil

import (
	"github.com/voedger/voedger/pkg/appdef"
	"github.com/voedger/voedger/pkg/istructs"
	"github.com/voedger/voedger/pkg/state"
	"github.com/voedger/voedger/pkg/sys/authnz"
)

func IWorkspaceFromCurrentState(s istructs.IState, appDef appdef.IAppDef) (appdef.IWorkspace, error) {
	skbCDocWorkspaceDescriptor, err := s.KeyBuilder(state.Record, authnz.QNameCDocWorkspaceDescriptor)
	if err != nil {
		return nil, err
	}
	skbCDocWorkspaceDescriptor.PutBool(state.Field_IsSingleton, true)
	svCDocWorkspaceDescriptor, err := s.MustExist(skbCDocWorkspaceDescriptor)
	if err != nil {
		return nil, err
	}
	wsKind := svCDocWorkspaceDescriptor.AsQName(authnz.Field_WSKind)
	return appDef.WorkspaceByDescriptor(wsKind), nil
}
