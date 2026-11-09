/*
 * Copyright (c) 2020-present unTill Pro, Ltd.
 */

package storages

import (
	payloads "github.com/voedger/voedger/pkg/itokens-payloads"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/voedger/voedger/pkg/appdef"
	"github.com/voedger/voedger/pkg/istructs"
	"github.com/voedger/voedger/pkg/state"
	"github.com/voedger/voedger/pkg/sys"
)

func TestSubjectStorage_BasicUsage(t *testing.T) {
	for _, tc := range []struct {
		name    string
		subject payloads.PrincipalPayload
	}{
		{"User", payloads.PrincipalPayload{SubjectKind: istructs.SubjectKind_User, Login: "john.doe@acme.com", ProfileWSID: 42}},
		{"Device", payloads.PrincipalPayload{SubjectKind: istructs.SubjectKind_Device, Login: "device-1", ProfileWSID: 43}},
		{"Guest", payloads.PrincipalPayload{SubjectKind: istructs.SubjectKind_User, Login: istructs.SysGuestLogin, ProfileWSID: istructs.GuestWSID}},
		{"API token", payloads.PrincipalPayload{SubjectKind: istructs.SubjectKind_User, Login: "api-owner", ProfileWSID: 44}},
		{"System", payloads.PrincipalPayload{SubjectKind: istructs.SubjectKind_User, Login: "system", ProfileWSID: 45}},
		{"null profile", payloads.PrincipalPayload{SubjectKind: istructs.SubjectKind_User, Login: "system"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require := require.New(t)
			subject, token := tc.subject, "original-token"
			storage := NewSubjectStorage(func() payloads.PrincipalPayload { return subject }, func() string { return token })
			key := storage.NewKeyBuilder(appdef.NullQName, nil)
			require.True(key.Equals(storage.NewKeyBuilder(appdef.NullQName, nil)))
			value, err := storage.(state.IWithGet).Get(key)
			require.NoError(err)
			require.Equal(int32(subject.SubjectKind), value.AsInt32(sys.Storage_RequestSubject_Field_Kind))
			require.Equal(subject.Login, value.AsString(sys.Storage_RequestSubject_Field_Name))
			require.Equal(int64(subject.ProfileWSID), value.AsInt64(sys.Storage_RequestSubject_Field_ProfileWSID))
			require.Equal(token, value.AsString(sys.Storage_RequestSubject_Field_Token))

			subject = payloads.PrincipalPayload{SubjectKind: istructs.SubjectKind_Device, Login: "next-device", ProfileWSID: 91}
			token = "next-token"
			next, err := storage.(state.IWithGet).Get(key)
			require.NoError(err)
			require.Equal(int32(istructs.SubjectKind_Device), next.AsInt32(sys.Storage_RequestSubject_Field_Kind))
			require.Equal("next-device", next.AsString(sys.Storage_RequestSubject_Field_Name))
			require.Equal(int64(91), next.AsInt64(sys.Storage_RequestSubject_Field_ProfileWSID))
			require.Equal("next-token", next.AsString(sys.Storage_RequestSubject_Field_Token))
			require.Equal(tc.subject.Login, value.AsString(sys.Storage_RequestSubject_Field_Name))
		})
	}
}
