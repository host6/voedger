/*
 * Copyright (c) 2026-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package oldacl

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/voedger/voedger/pkg/appdef"
	"github.com/voedger/voedger/pkg/goutils/logger"
	"github.com/voedger/voedger/pkg/iauthnz"
	"github.com/voedger/voedger/pkg/istructs"
	payloads "github.com/voedger/voedger/pkg/itokens-payloads"
)

func TestGuestAuthorization(t *testing.T) {
	require := require.New(t)
	for _, tc := range []struct {
		name     string
		subject  payloads.PrincipalPayload
		expected bool
	}{
		{"Guest", payloads.PrincipalPayload{SubjectKind: istructs.SubjectKind_User, Login: istructs.SysGuestLogin, ProfileWSID: istructs.GuestWSID}, true},
		{"ordinary User", payloads.PrincipalPayload{SubjectKind: istructs.SubjectKind_User, Login: "user", ProfileWSID: 42}, false},
		{"Guest name with another profile", payloads.PrincipalPayload{SubjectKind: istructs.SubjectKind_User, Login: istructs.SysGuestLogin, ProfileWSID: 42}, false},
		{"Guest profile with another name", payloads.PrincipalPayload{SubjectKind: istructs.SubjectKind_User, Login: "user", ProfileWSID: istructs.GuestWSID}, false},
		{"Device", payloads.PrincipalPayload{SubjectKind: istructs.SubjectKind_Device, Login: istructs.SysGuestLogin, ProfileWSID: istructs.GuestWSID}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			principals := []iauthnz.Principal{{Kind: iauthnz.PrincipalKind_User, Name: tc.subject.Login}}
			// Guest-only commands must reject identities that only partially match the guest identity
			actual := IsOperationAllowed(t.Context(), appdef.OperationKind_Execute, qNameCmdProvideCertificate, nil, principals, tc.subject)
			require.Equal(tc.expected, actual)
		})
	}
}

func TestLegacyRoleInheritance(t *testing.T) {
	require := require.New(t)
	for _, role := range []appdef.QName{iauthnz.QNameRoleWorkspaceOwner, iauthnz.QNameRoleProfileOwner, iauthnz.QNameRoleWorkspaceDevice} {
		t.Run(role.String(), func(t *testing.T) {
			principals := []iauthnz.Principal{{Kind: iauthnz.PrincipalKind_Role, QName: role}}
			// Authorization must recognize inherited ownership without explicit principal enrichment
			require.True(IsOperationAllowed(t.Context(), appdef.OperationKind_Execute, appdef.NewQName("test", "command"), nil, principals, payloads.PrincipalPayload{}))
		})
	}
}

func TestEnrichPrincipalsAddsLegacyRolesWithoutChangingInput(t *testing.T) {
	require := require.New(t)
	for _, tc := range []struct {
		name      string
		principal iauthnz.Principal
		expected  []iauthnz.Principal
	}{
		{"Profile owner inherits workspace ownership", iauthnz.Principal{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleProfileOwner}, []iauthnz.Principal{{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleWorkspaceOwner}}},
		{"Resellers admin inherits workspace administration", iauthnz.Principal{Kind: iauthnz.PrincipalKind_Role, QName: qNameRoleResellersAdmin}, []iauthnz.Principal{{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleWorkspaceAdmin}}},
		{"Payments reseller inherits workspace administration", iauthnz.Principal{Kind: iauthnz.PrincipalKind_Role, QName: qNameRoleUntillPaymentsReseller}, []iauthnz.Principal{{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleWorkspaceAdmin}}},
		{"User cannot inherit roles through a matching QName", iauthnz.Principal{Kind: iauthnz.PrincipalKind_User, QName: iauthnz.QNameRoleProfileOwner}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			principals := []iauthnz.Principal{tc.principal}
			expected := append(tc.expected, tc.principal)
			actual := EnrichPrincipals(principals)

			// Check enrichment directly so authorization's own ancestry lookup cannot mask missing roles
			require.Equal(expected, actual)
			// Enrichment must preserve the caller's original principals
			require.Equal([]iauthnz.Principal{tc.principal}, principals)
		})
	}
}

func TestLegacyDenyLoggingUsesRequestAndSubjectContext(t *testing.T) {
	require := require.New(t)
	logs := logger.StartCapture(t, logger.LogLevelVerbose)
	ctx := logger.WithContextAttrs(t.Context(), map[string]any{logger.LogAttr_WSID: 200, logger.LogAttr_VApp: istructs.AppQName_test1_app1})
	subject := payloads.PrincipalPayload{SubjectKind: istructs.SubjectKind_User, Login: "canonical", ProfileWSID: 42}
	principals := []iauthnz.Principal{{Kind: iauthnz.PrincipalKind_User, Name: subject.Login}}
	// Denials must identify both the request workspace and the subject profile for diagnosis
	require.False(IsOperationAllowed(ctx, appdef.OperationKind_Execute, appdef.NewQName("test", "denied"), nil, principals, subject))
	logs.HasLine("old-style", "test.denied", "-> deny", "wsid=200", "profilewsid=42")
	logs.NotContains("WSID 42", "ERROR", "WARNING")
	t.Run("deny diagnostics are suppressed below Verbose", func(t *testing.T) {
		logs := logger.StartCapture(t, logger.LogLevelInfo)
		// Ordinary denials must not emit verbose diagnostics at the normal logging level
		IsOperationAllowed(ctx, appdef.OperationKind_Execute, appdef.NewQName("test", "denied"), nil, principals, subject)
		logs.NotContains("old-style", "-> deny")
	})
}
