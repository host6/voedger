/*
 * Copyright (c) 2026-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package authnz

import (
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

func TestEnrichmentUsesOriginalPayloadAndRequestWorkspace(t *testing.T) {
	require := require.New(t)
	for _, tc := range []struct {
		name          string
		isAPIToken    bool
		expectedScope payloads.RoleScope
	}{
		{"Regular token grants roles in workspaces owned by the request workspace", false, payloads.RoleScopeWorkspacesOwnedBy},
		{"API token grants roles only in the request workspace", true, payloads.RoleScopeWorkspace},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tokens := itokensjwt.ProvideITokens(itokensjwt.SecretKeyExample, timeu.NewITime())
			factory := payloads.ProvideIAppTokensFactory(tokens)
			appTokens := factory.New(istructs.AppQName_test1_app1)
			role := appdef.NewQName("test", "carried")
			original := payloads.RoleType{Scope: tc.expectedScope, WSID: 100, QName: role}
			unmatched := payloads.RoleType{Scope: tc.expectedScope, WSID: 999, QName: appdef.NewQName("test", "unmatched")}
			deviceRole := payloads.RoleType{Scope: tc.expectedScope, WSID: 42, QName: iauthnz.QNameRoleWorkspaceDevice}
			// Spare capacity exposes accidental appends into the original payload's backing array
			roles := make([]payloads.RoleType, 3, 12)
			copy(roles, []payloads.RoleType{original, unmatched, deviceRole})
			expectedBacking := slices.Clone(roles[:cap(roles)])
			originalPayload := payloads.PrincipalPayload{Login: "canonical", Alias: "alias", SubjectKind: istructs.SubjectKind_Device, ProfileWSID: 42,
				IsAPIToken: tc.isAPIToken, GlobalRoles: []appdef.QName{appdef.NewQName("test", "global")}, Roles: roles}
			expected := originalPayload
			expected.Roles = []payloads.RoleType{
				original, unmatched, deviceRole,
				{Scope: tc.expectedScope, WSID: 200, QName: iauthnz.QNameRoleAuthenticatedUser},
				{Scope: tc.expectedScope, WSID: 200, QName: role},
				{Scope: tc.expectedScope, WSID: 200, QName: iauthnz.QNameRoleWorkspaceDevice},
			}
			principals := []iauthnz.Principal{
				{Kind: iauthnz.PrincipalKind_Device},
				{Kind: iauthnz.PrincipalKind_Host, Name: "host"},
				{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleAuthenticatedUser},
				{Kind: iauthnz.PrincipalKind_Role, QName: role},
				{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleWorkspaceDevice},
				{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleWorkspaceDevice},
			}
			token, err := appTokens.IssueToken(17*time.Minute, &originalPayload)
			require.NoError(err)
			wp := &enrichmentWorkpiece{result: iauthnz.AuthnResult{PrincipalPayload: originalPayload, Principals: principals}}
			exec := provideExecQryEnrichPrincipalToken(factory)
			run := func(input string) (payloads.PrincipalPayload, string) {
				t.Helper()
				var output string
				calls := 0
				err := exec(t.Context(), istructs.ExecQueryArgs{Workpiece: wp, WSID: 200, State: newEnrichmentState(t, input)}, func(object istructs.IObject) error {
					calls++
					output = object.AsString("EnrichedToken")
					return nil
				})
				require.NoError(err)
				require.Equal(1, calls)
				var actual payloads.PrincipalPayload
				metadata, err := appTokens.ValidateToken(output, &actual)
				require.NoError(err)
				require.Equal(DefaultPrincipalTokenExpiration, metadata.Duration)
				require.Equal(istructs.AppQName_test1_app1, metadata.AppQName)
				return actual, output
			}
			actual, enrichedToken := run(token)
			// Preserve identity and existing grants, adding only distinct role principals for workspace 200
			require.Equal(expected, actual)
			// Enrichment must leave the workpiece payload and its entire role backing array unchanged
			require.Equal(expectedBacking, roles[:cap(roles)])
			require.Equal(originalPayload, wp.result.PrincipalPayload)

			// Enriching an already enriched token must not duplicate its grants
			wp.result.PrincipalPayload = actual
			actual, _ = run(enrichedToken)
			require.Equal(expected, actual)
		})
	}
}

func TestDeviceEligibilityDoesNotChangeEnrichmentBinding(t *testing.T) {
	require := require.New(t)
	for _, tc := range []struct {
		name            string
		allowed, global bool
	}{
		{"GlobalRole with eligibility denied", false, true},
		{"GlobalRole with eligibility allowed", true, true},
		{"derived role only", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tokens := itokensjwt.ProvideITokens(itokensjwt.SecretKeyExample, timeu.NewITime())
			factory := payloads.ProvideIAppTokensFactory(tokens)
			appTokens := factory.New(istructs.AppQName_test1_app1)
			payload := payloads.PrincipalPayload{Login: "device-1", SubjectKind: istructs.SubjectKind_Device, ProfileWSID: 100,
				GlobalRoles: []appdef.QName{iauthnz.QNameRoleWorkspaceDevice}}
			if !tc.global {
				payload.GlobalRoles = nil
			}
			token, err := appTokens.IssueToken(time.Hour, &payload)
			require.NoError(err)
			calls := 0
			authn := iauthnzimpl.NewDefaultAuthenticator(iauthnzimpl.TestSubjectRolesGetter,
				iauthnzimpl.IsDeviceAllowedFuncs{istructs.AppQName_test1_app1: func(_ istructs.IAppStructs, request, profile istructs.WSID) (bool, error) {
					calls++
					require.Equal(istructs.WSID(200), request)
					require.Equal(istructs.WSID(100), profile)
					return tc.allowed, nil
				}})
			descriptor := &coreutils.TestObject{Name: appdef.QNameCDocWorkspaceDescriptor, Data: map[string]any{"OwnerWSID": int64(900)}}
			records := &coreutils.MockRecords{}
			records.On("GetSingleton", istructs.WSID(200), appdef.QNameCDocWorkspaceDescriptor).Return(descriptor, nil).Once()
			app := &coreutils.MockAppStructs{}
			app.On("AppQName").Return(istructs.AppQName_test1_app1)
			app.On("Records").Return(records).Once()
			t.Cleanup(func() {
				app.AssertExpectations(t)
				records.AssertExpectations(t)
			})
			// Obtain effective device roles through real authentication before testing their enrichment binding
			result, err := authn.Authenticate(t.Context(), app, appTokens, iauthnz.AuthnRequest{RequestWSID: 200, Token: token})
			require.NoError(err)
			require.Equal(1, calls)
			require.Equal(payload, result.PrincipalPayload)
			var output string
			err = provideExecQryEnrichPrincipalToken(factory)(t.Context(), istructs.ExecQueryArgs{
				WSID: 200, Workpiece: &enrichmentWorkpiece{result: result},
				State: newEnrichmentState(t, token)}, func(object istructs.IObject) error { output = object.AsString("EnrichedToken"); return nil })
			require.NoError(err)
			enriched, err := payloads.GetPrincipalPayload(appTokens, output)
			require.NoError(err)
			// Device eligibility changes how the role is obtained, but enrichment still binds it to workspace 200
			require.ElementsMatch([]payloads.RoleType{
				{Scope: payloads.RoleScopeWorkspacesOwnedBy, WSID: 200, QName: iauthnz.QNameRoleAuthenticatedUser},
				{Scope: payloads.RoleScopeWorkspacesOwnedBy, WSID: 200, QName: iauthnz.QNameRoleWorkspaceDevice},
			}, enriched.Roles)
			// Enrichment must use the authentication result without running device eligibility again
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
func (*enrichmentWorkpiece) GetPrincipals() []iauthnz.Principal    { panic("use authentication result") }

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
