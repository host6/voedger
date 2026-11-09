/*
 * Copyright (c) 2023-present unTill Pro, Ltd.
 */

package storages

import (
	"github.com/voedger/voedger/pkg/appdef"

	"github.com/voedger/voedger/pkg/istructs"
	"github.com/voedger/voedger/pkg/state"
	"github.com/voedger/voedger/pkg/sys"
)

type subjectStorage struct {
	payloadFunc state.PrincipalPayloadFunc
	tokenFunc   state.TokenFunc
}

func NewSubjectStorage(payloadFunc state.PrincipalPayloadFunc, tokenFunc state.TokenFunc) state.IStateStorage {
	return &subjectStorage{
		payloadFunc: payloadFunc,
		tokenFunc:   tokenFunc,
	}
}

type subjectKeyBuilder struct {
	baseKeyBuilder
}

func (b *subjectKeyBuilder) Equals(src istructs.IKeyBuilder) bool {
	_, ok := src.(*subjectKeyBuilder)
	return ok
}

func (s *subjectStorage) NewKeyBuilder(_ appdef.QName, _ istructs.IStateKeyBuilder) istructs.IStateKeyBuilder {
	return &subjectKeyBuilder{
		storage: sys.Storage_RequestSubject,
	}
}
func (s *subjectStorage) Get(_ istructs.IStateKeyBuilder) (istructs.IStateValue, error) {
	ssv := &requestSubjectValue{
		token: s.tokenFunc(),
	}
	if s.payloadFunc != nil {
		payload := s.payloadFunc()
		ssv.profileWSID = int64(payload.ProfileWSID)
		ssv.name = payload.Login
		ssv.kind = int32(payload.SubjectKind)
	}

	return ssv, nil
}

type requestSubjectValue struct {
	baseStateValue
	kind        int32
	profileWSID int64
	name        string
	token       string
}

func (v *requestSubjectValue) AsInt64(name string) int64 {
	switch name {
	case sys.Storage_RequestSubject_Field_ProfileWSID:
		return v.profileWSID
	default:
		return v.baseStateValue.AsInt64(name)
	}
}
func (v *requestSubjectValue) AsInt32(name string) int32 {
	switch name {
	case sys.Storage_RequestSubject_Field_Kind:
		return v.kind
	default:
		return v.baseStateValue.AsInt32(name)
	}
}
func (v *requestSubjectValue) AsString(name string) string {
	switch name {
	case sys.Storage_RequestSubject_Field_Name:
		return v.name
	case sys.Storage_RequestSubject_Field_Token:
		return v.token
	default:
		return v.baseStateValue.AsString(name)
	}
}
