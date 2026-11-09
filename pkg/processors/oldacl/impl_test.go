/*
 * Copyright (c) 2026-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package oldacl

import (
	payloads "github.com/voedger/voedger/pkg/itokens-payloads"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/voedger/voedger/pkg/appdef"
	"github.com/voedger/voedger/pkg/goutils/logger"
	"github.com/voedger/voedger/pkg/iauthnz"
	"github.com/voedger/voedger/pkg/istructs"
)

func TestGuestAuthorization(t *testing.T) {
	for _, tc := range []struct {
		name    string
		subject payloads.PrincipalPayload
		allowed bool
	}{
		{"Guest", payloads.PrincipalPayload{SubjectKind: istructs.SubjectKind_User, Login: istructs.SysGuestLogin, ProfileWSID: istructs.GuestWSID}, true},
		{"ordinary User", payloads.PrincipalPayload{SubjectKind: istructs.SubjectKind_User, Login: "user", ProfileWSID: 42}, false},
		{"Guest name with another profile", payloads.PrincipalPayload{SubjectKind: istructs.SubjectKind_User, Login: istructs.SysGuestLogin, ProfileWSID: 42}, false},
		{"Guest profile with another name", payloads.PrincipalPayload{SubjectKind: istructs.SubjectKind_User, Login: "user", ProfileWSID: istructs.GuestWSID}, false},
		{"Device", payloads.PrincipalPayload{SubjectKind: istructs.SubjectKind_Device, Login: istructs.SysGuestLogin, ProfileWSID: istructs.GuestWSID}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require := require.New(t)
			principals := []iauthnz.Principal{{Kind: iauthnz.PrincipalKind_User, Name: tc.subject.Login}}
			require.Equal(tc.allowed, IsOperationAllowed(t.Context(), appdef.OperationKind_Execute, qNameCmdProvideCertificate, nil, principals, tc.subject))
		})
	}
}

func TestLegacyRoleInheritance(t *testing.T) {
	for _, role := range []appdef.QName{iauthnz.QNameRoleWorkspaceOwner, iauthnz.QNameRoleProfileOwner, iauthnz.QNameRoleWorkspaceDevice} {
		t.Run(role.String(), func(t *testing.T) {
			require := require.New(t)
			principals := []iauthnz.Principal{{Kind: iauthnz.PrincipalKind_Role, QName: role}}
			require.True(IsOperationAllowed(t.Context(), appdef.OperationKind_Execute, appdef.NewQName("test", "command"), nil, EnrichPrincipals(principals), payloads.PrincipalPayload{}))
			require.Equal([]iauthnz.Principal{{Kind: iauthnz.PrincipalKind_Role, QName: role}}, principals)
		})
	}
	t.Run("System kind check remains distinct", func(t *testing.T) {
		require := require.New(t)
		require.True(iauthnz.IsSystemPrincipal([]iauthnz.Principal{{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleSystem}}))
		require.False(iauthnz.IsSystemPrincipal([]iauthnz.Principal{{Kind: iauthnz.PrincipalKind_User, QName: iauthnz.QNameRoleSystem}}))
	})
}

func TestLegacyDenyLoggingUsesRequestAndSubjectContext(t *testing.T) {
	require := require.New(t)
	logs := logger.StartCapture(t, logger.LogLevelVerbose)
	ctx := logger.WithContextAttrs(t.Context(), map[string]any{logger.LogAttr_WSID: 200, logger.LogAttr_VApp: istructs.AppQName_test1_app1})
	subject := payloads.PrincipalPayload{SubjectKind: istructs.SubjectKind_User, Login: "canonical", ProfileWSID: 42}
	principals := []iauthnz.Principal{{Kind: iauthnz.PrincipalKind_User, Name: subject.Login}}
	require.False(IsOperationAllowed(ctx, appdef.OperationKind_Execute, appdef.NewQName("test", "denied"), nil, principals, subject))
	logs.HasLine("old-style", "test.denied", "-> deny", "wsid=200", "profilewsid=42")
	logs.NotContains("Bearer", "atoken", "WSID 42", "ERROR", "WARNING")
	require.NotContains(prnsToString(principals), "WSID")
	t.Run("deny diagnostics are suppressed below Verbose", func(t *testing.T) {
		logs := logger.StartCapture(t, logger.LogLevelInfo)
		IsOperationAllowed(ctx, appdef.OperationKind_Execute, appdef.NewQName("test", "denied"), nil, principals, subject)
		logs.NotContains("old-style", "-> deny")
	})
}
