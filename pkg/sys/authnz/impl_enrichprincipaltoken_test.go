/*
 * Copyright (c) 2026-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package authnz

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/voedger/voedger/pkg/appdef"
	"github.com/voedger/voedger/pkg/coreutils"
	"github.com/voedger/voedger/pkg/goutils/timeu"
	"github.com/voedger/voedger/pkg/iauthnz"
	"github.com/voedger/voedger/pkg/iauthnzimpl"
	"github.com/voedger/voedger/pkg/istructs"
	"github.com/voedger/voedger/pkg/itokens"
	payloads "github.com/voedger/voedger/pkg/itokens-payloads"
	"github.com/voedger/voedger/pkg/itokensjwt"
	"github.com/voedger/voedger/pkg/processors"
	"github.com/voedger/voedger/pkg/sys"
)

func TestEnrichmentPreservesInitialGrantsAndAddsOnlySubjectRoles(t *testing.T) {
	require := require.New(t)
	for _, tc := range []struct {
		name       string
		kind       istructs.SubjectKindType
		isAPIToken bool
	}{
		{"User captures Subject roles", istructs.SubjectKind_User, false},
		{"Device captures Subject roles", istructs.SubjectKind_Device, false},
		{"API token retains grants without capturing Subjects", istructs.SubjectKind_User, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tokens := itokensjwt.ProvideITokens(itokensjwt.SecretKeyExample, timeu.NewITime())
			factory := payloads.ProvideIAppTokensFactory(tokens)
			appTokens := factory.New(istructs.AppQName_test1_app1)
			carriedRole := appdef.NewQName("test", "carried")
			sharedRole := appdef.NewQName("test", "shared")
			subjectRole := appdef.NewQName("test", "subject")
			aliasRole := appdef.NewQName("test", "alias")
			existingRole := appdef.NewQName("test", "existing")
			initialScope := payloads.RoleScopeWorkspacesOwnedBy
			if tc.isAPIToken {
				initialScope = payloads.RoleScopeWorkspace
			}
			initialGrants := []payloads.RoleType{
				{Scope: initialScope, WSID: 42, QName: carriedRole},
				{Scope: initialScope, WSID: 999, QName: appdef.NewQName("test", "unmatched")},
				{Scope: initialScope, WSID: 42, QName: iauthnz.QNameRoleWorkspaceDevice},
				{Scope: payloads.RoleScopeWorkspace, WSID: 200, QName: existingRole},
			}
			// Spare capacity exposes accidental appends into the original payload's backing array
			roles := make([]payloads.RoleType, len(initialGrants), 12)
			copy(roles, initialGrants)
			expectedBacking := slices.Clone(roles[:cap(roles)])
			originalPayload := payloads.PrincipalPayload{Login: "canonical", Alias: "alias", SubjectKind: tc.kind, ProfileWSID: 42,
				IsAPIToken: tc.isAPIToken, GlobalRoles: []appdef.QName{appdef.NewQName("test", "global"), sharedRole}, Roles: roles}
			token, err := appTokens.IssueToken(17*time.Minute, &originalPayload)
			require.NoError(err)
			var actualLogins []string
			authn := iauthnzimpl.NewDefaultAuthenticator(func(_ context.Context, login string, _ istructs.IAppStructs, wsid istructs.WSID) ([]appdef.QName, error) {
				require.Equal(istructs.WSID(200), wsid)
				actualLogins = append(actualLogins, login)
				if login == "canonical" {
					return []appdef.QName{sharedRole, subjectRole}, nil
				}
				return []appdef.QName{subjectRole, aliasRole, carriedRole, existingRole}, nil
			}, iauthnzimpl.TestIsDeviceAllowedFuncs)
			result, err := authn.Authenticate(t.Context(), newEnrichmentApp(t, tc.isAPIToken, 42), appTokens,
				iauthnz.AuthnRequest{RequestWSID: 200, Host: "host", Token: token})
			require.NoError(err)
			expected := originalPayload
			expected.Roles = slices.Clone(initialGrants)
			if tc.isAPIToken {
				require.Empty(actualLogins)
			} else {
				require.Equal([]string{"canonical", "alias"}, actualLogins)
				expected.Roles = append(expected.Roles,
					payloads.RoleType{Scope: payloads.RoleScopeWorkspace, WSID: 200, QName: sharedRole},
					payloads.RoleType{Scope: payloads.RoleScopeWorkspace, WSID: 200, QName: subjectRole},
					payloads.RoleType{Scope: payloads.RoleScopeWorkspace, WSID: 200, QName: aliasRole},
					payloads.RoleType{Scope: payloads.RoleScopeWorkspace, WSID: 200, QName: carriedRole})
			}
			// Keep spare capacity in the workpiece payload to check enrichment's slice isolation
			result.PrincipalPayload = originalPayload
			wp := &enrichmentWorkpiece{result: result}
			exec := provideExecQryEnrichPrincipalToken(factory)
			run := func(input string) (payloads.PrincipalPayload, string) {
				t.Helper()
				var output string
				err := exec(t.Context(), istructs.ExecQueryArgs{Workpiece: wp, WSID: 200, State: newEnrichmentState(t, input)}, func(object istructs.IObject) error {
					output = object.AsString("EnrichedToken")
					return nil
				})
				require.NoError(err)
				var actual payloads.PrincipalPayload
				metadata, err := appTokens.ValidateToken(output, &actual)
				require.NoError(err)
				require.Equal(DefaultPrincipalTokenExpiration, metadata.Duration)
				require.Equal(istructs.AppQName_test1_app1, metadata.AppQName)
				return actual, output
			}
			actual, enrichedToken := run(token)
			// Only Subject roles become new exact grants, including roles shared with other sources
			require.Equal(expected, actual)
			require.Equal(expectedBacking, roles[:cap(roles)])
			require.Equal(originalPayload, wp.result.PrincipalPayload)

			if tc.isAPIToken {
				return
			}
			// Repeating enrichment must not duplicate grants
			wp.result.PrincipalPayload = actual
			actual, _ = run(enrichedToken)
			require.Equal(expected, actual)
		})
	}
}

func TestDeviceRolesAreCapturedOnlyFromSubjects(t *testing.T) {
	require := require.New(t)
	for _, tc := range []struct {
		name                     string
		allowed, global, subject bool
	}{
		{"GlobalRole alone is not captured", false, true, false},
		{"Device eligibility alone is not captured", true, false, false},
		{"Subject role shared with GlobalRoles is captured", false, true, true},
		{"Subject role shared with Device eligibility is captured", true, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tokens := itokensjwt.ProvideITokens(itokensjwt.SecretKeyExample, timeu.NewITime())
			factory := payloads.ProvideIAppTokensFactory(tokens)
			appTokens := factory.New(istructs.AppQName_test1_app1)
			payload := payloads.PrincipalPayload{Login: "device-1", SubjectKind: istructs.SubjectKind_Device, ProfileWSID: 100}
			if tc.global {
				payload.GlobalRoles = []appdef.QName{iauthnz.QNameRoleWorkspaceDevice}
			}
			token, err := appTokens.IssueToken(time.Hour, &payload)
			require.NoError(err)
			calls := 0
			authn := iauthnzimpl.NewDefaultAuthenticator(func(_ context.Context, login string, _ istructs.IAppStructs, wsid istructs.WSID) ([]appdef.QName, error) {
				require.Equal("device-1", login)
				require.Equal(istructs.WSID(200), wsid)
				if tc.subject {
					return []appdef.QName{iauthnz.QNameRoleWorkspaceDevice}, nil
				}
				return nil, nil
			}, iauthnzimpl.IsDeviceAllowedFuncs{istructs.AppQName_test1_app1: func(_ istructs.IAppStructs, request, profile istructs.WSID) (bool, error) {
				calls++
				require.Equal(istructs.WSID(200), request)
				require.Equal(istructs.WSID(100), profile)
				return tc.allowed, nil
			}})
			result, err := authn.Authenticate(t.Context(), newEnrichmentApp(t, false, 900), appTokens, iauthnz.AuthnRequest{RequestWSID: 200, Token: token})
			require.NoError(err)
			// All cases have an effective Device role; its origin determines whether enrichment captures it
			require.Contains(result.Principals, iauthnz.Principal{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleWorkspaceDevice})
			var output string
			err = provideExecQryEnrichPrincipalToken(factory)(t.Context(), istructs.ExecQueryArgs{
				WSID: 200, Workpiece: &enrichmentWorkpiece{result: result},
				State: newEnrichmentState(t, token)}, func(object istructs.IObject) error { output = object.AsString("EnrichedToken"); return nil })
			require.NoError(err)
			actual, err := payloads.GetPrincipalPayload(appTokens, output)
			require.NoError(err)
			expected := payload
			if tc.subject {
				expected.Roles = []payloads.RoleType{{Scope: payloads.RoleScopeWorkspace, WSID: 200, QName: iauthnz.QNameRoleWorkspaceDevice}}
			}
			require.Equal(expected, actual)
			// Enrichment must not repeat the Device eligibility check
			require.Equal(1, calls)
		})
	}
}

func TestEnrichmentRejectsMissingBearerToken(t *testing.T) {
	require := require.New(t)
	tokens := itokensjwt.ProvideITokens(itokensjwt.SecretKeyExample, timeu.NewITime())
	factory := payloads.ProvideIAppTokensFactory(tokens)
	called := false
	err := provideExecQryEnrichPrincipalToken(factory)(t.Context(), istructs.ExecQueryArgs{State: newEnrichmentState(t, ""),
		Workpiece: &enrichmentWorkpiece{result: iauthnz.AuthnResult{PrincipalPayload: iauthnz.GuestPrincipalPayload()}}}, func(istructs.IObject) error { called = true; return nil })
	// Missing credentials must fail without publishing an enriched token
	require.ErrorIs(err, itokens.ErrInvalidToken)
	require.False(called)
}

type enrichmentWorkpiece struct {
	processors.IProcessorWorkpiece
	result iauthnz.AuthnResult
}

func (w *enrichmentWorkpiece) GetAuthnResult() iauthnz.AuthnResult { return w.result }

func newEnrichmentState(t *testing.T, token string) *coreutils.MockState {
	t.Helper()
	key := &coreutils.MockStateKeyBuilder{}
	value := &coreutils.MockStateValue{}
	value.On("AsString", sys.Storage_RequestSubject_Field_Token).Return(token).Once()
	state := &coreutils.MockState{}
	state.On("App").Return(istructs.AppQName_test1_app1).Once()
	state.On("KeyBuilder", sys.Storage_RequestSubject, appdef.NullQName).Return(key, nil).Once()
	state.On("MustExist", key).Return(value, nil).Once()
	t.Cleanup(func() {
		state.AssertExpectations(t)
		value.AssertExpectations(t)
	})
	return state
}

var _ processors.IProcessorWorkpiece = (*enrichmentWorkpiece)(nil)

func newEnrichmentApp(t *testing.T, isAPIToken bool, ownerWSID int64) *coreutils.MockAppStructs {
	t.Helper()
	app := &coreutils.MockAppStructs{}
	records := &coreutils.MockRecords{}
	if !isAPIToken {
		descriptor := &coreutils.TestObject{Name: appdef.QNameCDocWorkspaceDescriptor, Data: map[string]any{"OwnerWSID": ownerWSID}}
		records.On("GetSingleton", istructs.WSID(200), appdef.QNameCDocWorkspaceDescriptor).Return(descriptor, nil).Once()
		app.On("Records").Return(records).Once()
		app.On("AppQName").Return(istructs.AppQName_test1_app1).Maybe()
	}
	t.Cleanup(func() {
		app.AssertExpectations(t)
		records.AssertExpectations(t)
	})
	return app
}
