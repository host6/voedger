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
	for _, api := range []bool{false, true} {
		name := "regular"
		if api {
			name = "API"
		}
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			tokens := itokensjwt.ProvideITokens(itokensjwt.SecretKeyExample, timeu.NewITime())
			factory := payloads.ProvideIAppTokensFactory(tokens)
			appTokens := factory.New(istructs.AppQName_test1_app1)
			scope := payloads.RoleScopeWorkspacesOwnedBy
			if api {
				scope = payloads.RoleScopeWorkspace
			}
			role := appdef.NewQName("test", "carried")
			original := payloads.RoleType{Scope: scope, WSID: 100, QName: role}
			unmatched := payloads.RoleType{Scope: scope, WSID: 999, QName: appdef.NewQName("test", "unmatched")}
			roles := make([]payloads.RoleType, 3, 12)
			copy(roles, []payloads.RoleType{original, unmatched, {Scope: scope, WSID: 42, QName: iauthnz.QNameRoleWorkspaceDevice}})
			beforeBacking := slices.Clone(roles[:cap(roles)])
			pp := payloads.PrincipalPayload{Login: "canonical", Alias: "alias", SubjectKind: istructs.SubjectKind_Device, ProfileWSID: 42,
				IsAPIToken: api, GlobalRoles: []appdef.QName{appdef.NewQName("test", "global")}, Roles: roles}
			expected := pp
			expected.Roles = slices.Clone(pp.Roles)
			names := []appdef.QName{iauthnz.QNameRoleAuthenticatedUser, role, iauthnz.QNameRoleWorkspaceDevice}
			principals := []iauthnz.Principal{{Kind: iauthnz.PrincipalKind_Device}, {Kind: iauthnz.PrincipalKind_Host, Name: "host"}}
			for _, name := range names {
				principals = append(principals, iauthnz.Principal{Kind: iauthnz.PrincipalKind_Role, QName: name})
				expected.Roles = append(expected.Roles, payloads.RoleType{Scope: scope, WSID: 200, QName: name})
			}
			principals = append(principals, principals[len(principals)-1])
			token, err := appTokens.IssueToken(17*time.Minute, &pp)
			require.NoError(err)
			wp := &enrichmentWorkpiece{result: iauthnz.AuthnResult{Payload: pp, Principals: principals}}
			exec := provideExecQryEnrichPrincipalToken(factory)
			run := func(input string) payloads.PrincipalPayload {
				t.Helper()
				var output string
				calls := 0
				err := exec(t.Context(), istructs.ExecQueryArgs{Workpiece: wp, WSID: 200, State: &enrichmentState{token: input}}, func(object istructs.IObject) error {
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
				require.Equal(expected, actual)
				return actual
			}
			actual := run(token)
			require.Equal(beforeBacking, roles[:cap(roles)])
			require.Equal(pp, wp.result.Payload)
			for _, grant := range actual.Roles {
				require.NotEqual(iauthnz.QNameRoleWorkspaceOwner, grant.QName)
			}
			repeated, err := appTokens.IssueToken(time.Minute, &actual)
			require.NoError(err)
			wp.result.Payload = actual
			require.Equal(actual, run(repeated))
			decoded, err := payloads.GetPrincipalPayload(appTokens, token)
			require.NoError(err)
			require.Equal(pp, decoded)
		})
	}
}

func TestDeviceEligibilityDoesNotChangeEnrichmentBinding(t *testing.T) {
	for _, tc := range []struct {
		name            string
		allowed, global bool
	}{
		{"GlobalRole with eligibility denied", false, true},
		{"GlobalRole with eligibility allowed", true, true},
		{"derived role only", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require := require.New(t)
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
			result, err := authn.Authenticate(t.Context(), &deviceEnrichmentApp{}, appTokens, iauthnz.AuthnRequest{RequestWSID: 200, Token: token})
			require.NoError(err)
			require.Equal(1, calls)
			require.Equal(payload, result.Payload)
			var output string
			err = provideExecQryEnrichPrincipalToken(factory)(t.Context(), istructs.ExecQueryArgs{
				WSID: 200, Workpiece: &enrichmentWorkpiece{result: result},
				State: &enrichmentState{token: token}}, func(object istructs.IObject) error { output = object.AsString("EnrichedToken"); return nil })
			require.NoError(err)
			enriched, err := payloads.GetPrincipalPayload(appTokens, output)
			require.NoError(err)
			require.ElementsMatch([]payloads.RoleType{
				{Scope: payloads.RoleScopeWorkspacesOwnedBy, WSID: 200, QName: iauthnz.QNameRoleAuthenticatedUser},
				{Scope: payloads.RoleScopeWorkspacesOwnedBy, WSID: 200, QName: iauthnz.QNameRoleWorkspaceDevice},
			}, enriched.Roles)
			require.Equal(1, calls)
		})
	}
}

type deviceEnrichmentApp struct{ istructs.IAppStructs }

func (*deviceEnrichmentApp) AppQName() appdef.AppQName  { return istructs.AppQName_test1_app1 }
func (*deviceEnrichmentApp) Records() istructs.IRecords { return &deviceEnrichmentRecords{} }

type deviceEnrichmentRecords struct{ istructs.IRecords }

func (*deviceEnrichmentRecords) GetSingleton(istructs.WSID, appdef.QName) (istructs.IRecord, error) {
	return &deviceEnrichmentDescriptor{}, nil
}

type deviceEnrichmentDescriptor struct{ istructs.IRecord }

func (*deviceEnrichmentDescriptor) QName() appdef.QName  { return appdef.QNameCDocWorkspaceDescriptor }
func (*deviceEnrichmentDescriptor) AsInt64(string) int64 { return 900 }

func TestEnrichmentRejectsMissingBearerToken(t *testing.T) {
	require := require.New(t)
	tokens := itokensjwt.ProvideITokens(itokensjwt.SecretKeyExample, timeu.NewITime())
	factory := payloads.ProvideIAppTokensFactory(tokens)
	called := false
	err := provideExecQryEnrichPrincipalToken(factory)(t.Context(), istructs.ExecQueryArgs{State: &enrichmentState{},
		Workpiece: &enrichmentWorkpiece{result: iauthnz.AuthnResult{Payload: iauthnz.GuestPayload()}}}, func(istructs.IObject) error { called = true; return nil })
	require.ErrorIs(err, itokens.ErrInvalidToken)
	require.False(called)
}

type enrichmentWorkpiece struct {
	processors.IProcessorWorkpiece
	result iauthnz.AuthnResult
}

func (w *enrichmentWorkpiece) GetAuthnResult() iauthnz.AuthnResult { return w.result }
func (*enrichmentWorkpiece) GetPrincipals() []iauthnz.Principal    { panic("use authentication result") }

type enrichmentState struct {
	istructs.IState
	token string
}

func (*enrichmentState) App() appdef.AppQName { return istructs.AppQName_test1_app1 }
func (*enrichmentState) KeyBuilder(storage, entity appdef.QName) (istructs.IStateKeyBuilder, error) {
	if storage != sys.Storage_RequestSubject || entity != appdef.NullQName {
		panic("unexpected storage")
	}
	return nil, nil
}
func (s *enrichmentState) MustExist(istructs.IStateKeyBuilder) (istructs.IStateValue, error) {
	return &enrichmentTokenValue{token: s.token}, nil
}

type enrichmentTokenValue struct {
	istructs.IStateValue
	token string
}

func (v *enrichmentTokenValue) AsString(field string) string {
	if field != sys.Storage_RequestSubject_Field_Token {
		panic("unexpected field")
	}
	return v.token
}

var _ processors.IProcessorWorkpiece = (*enrichmentWorkpiece)(nil)
