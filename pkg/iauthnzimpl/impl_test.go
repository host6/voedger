/*
 * Copyright (c) 2022-present unTill Pro, Ltd.
 */

package iauthnzimpl

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/voedger/voedger/pkg/goutils/logger"
	"github.com/voedger/voedger/pkg/goutils/timeu"

	"github.com/voedger/voedger/pkg/appdef"
	"github.com/voedger/voedger/pkg/coreutils"
	"github.com/voedger/voedger/pkg/iauthnz"
	"github.com/voedger/voedger/pkg/istructs"
	"github.com/voedger/voedger/pkg/istructsmem"
	payloads "github.com/voedger/voedger/pkg/itokens-payloads"
	"github.com/voedger/voedger/pkg/itokensjwt"
)

const (
	alienWSID     = 3
	nonInitedWSID = 4
)

func TestBasicUsage(t *testing.T) {
	require := require.New(t)
	oldLevel := logger.SetLogLevel(logger.LogLevelVerbose)
	defer func() { logger.SetLogLevel(oldLevel) }()

	tokens := itokensjwt.ProvideITokens(itokensjwt.SecretKeyExample, timeu.NewITime())
	appTokens := payloads.ProvideIAppTokensFactory(tokens).New(istructs.AppQName_test1_app1)
	pp := payloads.PrincipalPayload{
		Login:       "testlogin",
		SubjectKind: istructs.SubjectKind_User,
		Roles: []payloads.RoleType{
			{
				WSID:  42,
				QName: iauthnz.QNameRoleWorkspaceDevice,
			},
		},
		ProfileWSID: 1,
	}
	token, err := appTokens.IssueToken(time.Minute, &pp)
	require.NoError(err)

	appStructs := newAuthenticationApp(t, map[istructs.WSID]int64{2: 1, 3: 2})
	authn := NewDefaultAuthenticator(TestSubjectRolesGetter, TestIsDeviceAllowedFuncs)
	t.Run("authenticate in the profile", func(t *testing.T) {
		req := iauthnz.AuthnRequest{
			Host:        "127.0.0.1",
			RequestWSID: 1,
			Token:       token,
		}
		result, err := authn.Authenticate(context.Background(), appStructs, appTokens, req)
		require.NoError(err)
		principals := result.Principals
		require.Len(principals, 4)
		require.Equal(iauthnz.PrincipalKind_Role, principals[0].Kind)
		require.Equal(iauthnz.QNameRoleAuthenticatedUser, principals[0].QName)

		require.Equal(iauthnz.PrincipalKind_User, principals[1].Kind)
		require.Equal("testlogin", principals[1].Name)

		// request to the profile -> ProfileOwner role got
		require.Equal(iauthnz.PrincipalKind_Role, principals[2].Kind)
		require.Equal(iauthnz.QNameRoleProfileOwner, principals[2].QName)

		require.Equal(iauthnz.PrincipalKind_Host, principals[3].Kind)
		require.Equal("127.0.0.1", principals[3].Name)

		require.Equal(pp.ProfileWSID, result.PrincipalPayload.ProfileWSID)
	})

	t.Run("authenticate in the owned workspace", func(t *testing.T) {
		req := iauthnz.AuthnRequest{
			Host:        "127.0.0.1",
			RequestWSID: 2,
			Token:       token,
		}
		// request to WSID 2, there is a cdoc.sys.WorkspaceDescriptor.OwnerWSID = 1 -> the workspace is owned by the user with ProfileWSID=1
		result, err := authn.Authenticate(context.Background(), appStructs, appTokens, req)
		require.NoError(err)
		principals := result.Principals
		require.Len(principals, 4)
		require.Equal(iauthnz.PrincipalKind_Role, principals[0].Kind)
		require.Equal(iauthnz.QNameRoleAuthenticatedUser, principals[0].QName)

		require.Equal(iauthnz.PrincipalKind_User, principals[1].Kind)
		require.Equal("testlogin", principals[1].Name)

		// request to the owned workspace -> WorkspaceOwner role got
		require.Equal(iauthnz.PrincipalKind_Role, principals[2].Kind)
		require.Equal(iauthnz.QNameRoleWorkspaceOwner, principals[2].QName)

		require.Equal(iauthnz.PrincipalKind_Host, principals[3].Kind)
		require.Equal("127.0.0.1", principals[3].Name)

		require.Equal(pp.ProfileWSID, result.PrincipalPayload.ProfileWSID)
	})

	t.Run("authenticate in the child workspace", func(t *testing.T) {
		pp := payloads.PrincipalPayload{
			Login:       "testlogin",
			SubjectKind: istructs.SubjectKind_User,
			Roles: []payloads.RoleType{
				{
					WSID:  2,
					QName: iauthnz.QNameRoleWorkspaceOwner,
				},
			},
			ProfileWSID: 1,
		}
		token, err := appTokens.IssueToken(time.Minute, &pp)
		require.NoError(err)
		req := iauthnz.AuthnRequest{
			Host:        "127.0.0.1",
			RequestWSID: 3,
			Token:       token,
		}
		// request to WSID 2, there is a cdoc.sys.WorkspaceDescriptor.OwnerWSID = 1 -> the workspace is owned by the user with ProfileWSID=1
		result, err := authn.Authenticate(context.Background(), appStructs, appTokens, req)
		require.NoError(err)
		principals := result.Principals
		require.Len(principals, 4)
		require.Equal(iauthnz.PrincipalKind_Role, principals[0].Kind)
		require.Equal(iauthnz.QNameRoleAuthenticatedUser, principals[0].QName)

		require.Equal(iauthnz.PrincipalKind_User, principals[1].Kind)
		require.Equal("testlogin", principals[1].Name)

		// request to a workspace with a token enriched by WorkspaceOwne role -> WorkspaceOwner role got
		require.Equal(iauthnz.PrincipalKind_Role, principals[2].Kind)
		require.Equal(iauthnz.QNameRoleWorkspaceOwner, principals[2].QName)

		require.Equal(iauthnz.PrincipalKind_Host, principals[3].Kind)
		require.Equal("127.0.0.1", principals[3].Name)
		require.Equal(pp.ProfileWSID, result.PrincipalPayload.ProfileWSID)
	})
}

func TestAuthenticate(t *testing.T) {
	require := require.New(t)

	tokens := itokensjwt.ProvideITokens(itokensjwt.SecretKeyExample, timeu.NewITime())
	appTokens := payloads.ProvideIAppTokensFactory(tokens).New(istructs.AppQName_test1_app1)
	login := "testlogin"
	pp := payloads.PrincipalPayload{
		Login:       login,
		SubjectKind: istructs.SubjectKind_User,
		ProfileWSID: 1,
	}
	userToken, err := appTokens.IssueToken(time.Minute, &pp)
	require.NoError(err)

	testRole := appdef.NewQName("test", "role")
	apiKeyToken, err := IssueAPIToken(appTokens, time.Hour, []appdef.QName{
		testRole,
	}, 2, pp)
	require.NoError(err)

	pp.SubjectKind = istructs.SubjectKind_Device
	deviceToken, err := appTokens.IssueToken(time.Minute, &pp)
	require.NoError(err)

	notIncludedRole := appdef.NewQName("test", "non-included")
	pp = payloads.PrincipalPayload{
		Login:       login,
		SubjectKind: istructs.SubjectKind_User,
		ProfileWSID: 1,
		Roles: []payloads.RoleType{
			{Scope: payloads.RoleScopeWorkspacesOwnedBy, WSID: 1, QName: testRole},
			{Scope: payloads.RoleScopeWorkspacesOwnedBy, WSID: 2, QName: notIncludedRole},
		},
	}
	enrichedToken, err := appTokens.IssueToken(time.Minute, &pp)
	require.NoError(err)
	exactRolePayload := pp
	exactRolePayload.Roles = []payloads.RoleType{{Scope: payloads.RoleScopeWorkspace, WSID: 1, QName: testRole}}
	exactRoleToken, err := appTokens.IssueToken(time.Minute, &exactRolePayload)
	require.NoError(err)
	apiOwnedRolePayload := pp
	apiOwnedRolePayload.IsAPIToken = true
	apiOwnedRolePayload.Roles = []payloads.RoleType{{Scope: payloads.RoleScopeWorkspacesOwnedBy, WSID: 2, QName: testRole}}
	apiOwnedRoleToken, err := appTokens.IssueToken(time.Minute, &apiOwnedRolePayload)
	require.NoError(err)

	appStructs := newAuthenticationApp(t, map[istructs.WSID]int64{2: 1, 3: 2, 5: 1})

	testCases := []struct {
		desc               string
		req                iauthnz.AuthnRequest
		expectedPrincipals []iauthnz.Principal
	}{
		{
			desc: "no auth -> host + Guest user",
			req: iauthnz.AuthnRequest{
				Host:        "127.0.0.1",
				RequestWSID: 1,
			},
			expectedPrincipals: []iauthnz.Principal{
				{Kind: iauthnz.PrincipalKind_User, Name: istructs.SysGuestLogin},
				{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleAnonymous},
				{Kind: iauthnz.PrincipalKind_Host, Name: "127.0.0.1"},
			},
		},
		{
			desc: "request to a non-owned workspace -> AuthenticatedUser + user + host",
			req: iauthnz.AuthnRequest{
				Host:        "127.0.0.1",
				RequestWSID: alienWSID,
				Token:       userToken,
			},
			expectedPrincipals: []iauthnz.Principal{
				{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleAuthenticatedUser},
				{Kind: iauthnz.PrincipalKind_User, Name: login},
				{Kind: iauthnz.PrincipalKind_Host, Name: "127.0.0.1"},
			},
		},
		{
			desc: "request to a non-initialized workspace -> host + user",
			req: iauthnz.AuthnRequest{
				Host:        "127.0.0.1",
				RequestWSID: nonInitedWSID,
				Token:       userToken,
			},
			expectedPrincipals: []iauthnz.Principal{
				{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleAuthenticatedUser},
				{Kind: iauthnz.PrincipalKind_User, Name: login},
				{Kind: iauthnz.PrincipalKind_Host, Name: "127.0.0.1"},
			},
		},
		{
			desc: "device -> AuthenticatedUser + device + ProfileOwner + host",
			req: iauthnz.AuthnRequest{
				Host:        "127.0.0.1",
				RequestWSID: 1,
				Token:       deviceToken,
			},
			expectedPrincipals: []iauthnz.Principal{
				{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleAuthenticatedUser},
				{Kind: iauthnz.PrincipalKind_Device},
				{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleProfileOwner},
				{Kind: iauthnz.PrincipalKind_Host, Name: "127.0.0.1"},
			},
		},
		{
			desc: "API token does not reinterpret an explicit owned-by scope as an exact grant",
			req:  iauthnz.AuthnRequest{Host: "127.0.0.1", RequestWSID: 2, Token: apiOwnedRoleToken},
			expectedPrincipals: []iauthnz.Principal{
				{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleAuthenticatedUser},
			},
		},
		{
			desc: "regular exact grant applies in its target even without a Workspace descriptor",
			req:  iauthnz.AuthnRequest{Host: "127.0.0.1", RequestWSID: 1, Token: exactRoleToken},
			expectedPrincipals: []iauthnz.Principal{
				{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleAuthenticatedUser},
				{Kind: iauthnz.PrincipalKind_User, Name: login},
				{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleProfileOwner},
				{Kind: iauthnz.PrincipalKind_Role, QName: testRole},
				{Kind: iauthnz.PrincipalKind_Host, Name: "127.0.0.1"},
			},
		},
		{
			desc: "regular token does not reinterpret an exact scope as an owned-by grant",
			req:  iauthnz.AuthnRequest{Host: "127.0.0.1", RequestWSID: 2, Token: exactRoleToken},
			expectedPrincipals: []iauthnz.Principal{
				{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleAuthenticatedUser},
				{Kind: iauthnz.PrincipalKind_User, Name: login},
				{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleWorkspaceOwner},
				{Kind: iauthnz.PrincipalKind_Host, Name: "127.0.0.1"},
			},
		},
		{
			desc: "API token does not grant access to a workspace owned by its target",
			req:  iauthnz.AuthnRequest{Host: "127.0.0.1", RequestWSID: 3, Token: apiKeyToken},
			expectedPrincipals: []iauthnz.Principal{
				{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleAuthenticatedUser},
			},
		},
		{
			desc: "owned-by grant applies in a sibling workspace with the same owner",
			req:  iauthnz.AuthnRequest{Host: "127.0.0.1", RequestWSID: 5, Token: enrichedToken},
			expectedPrincipals: []iauthnz.Principal{
				{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleAuthenticatedUser},
				{Kind: iauthnz.PrincipalKind_User, Name: login},
				{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleWorkspaceOwner},
				{Kind: iauthnz.PrincipalKind_Role, QName: testRole},
				{Kind: iauthnz.PrincipalKind_Host, Name: "127.0.0.1"},
			},
		},
		{
			desc: "owned-by grants match only the immediate owner",
			req:  iauthnz.AuthnRequest{Host: "127.0.0.1", RequestWSID: 3, Token: enrichedToken},
			expectedPrincipals: []iauthnz.Principal{
				{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleAuthenticatedUser},
				{Kind: iauthnz.PrincipalKind_User, Name: login},
				{Kind: iauthnz.PrincipalKind_Role, QName: notIncludedRole},
				{Kind: iauthnz.PrincipalKind_Host, Name: "127.0.0.1"},
			},
		},
		{
			desc: "enriched token -> roles from token taken if its wsid == ownerWSID (one of 2 roles is matched by wsid)",
			req: iauthnz.AuthnRequest{
				Host:        "127.0.0.1",
				RequestWSID: 2,
				Token:       enrichedToken,
			},
			expectedPrincipals: []iauthnz.Principal{
				{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleAuthenticatedUser},
				{Kind: iauthnz.PrincipalKind_User, Name: login},
				{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleWorkspaceOwner},
				{Kind: iauthnz.PrincipalKind_Role, QName: testRole},
				{Kind: iauthnz.PrincipalKind_Host, Name: "127.0.0.1"},
			},
		},
		{
			desc: "enriched token -> roles from token taken if its wsid == ownerWSID (no roles matched by wsid)",
			req: iauthnz.AuthnRequest{
				Host:        "127.0.0.1",
				RequestWSID: 42,
				Token:       enrichedToken,
			},
			expectedPrincipals: []iauthnz.Principal{
				{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleAuthenticatedUser},
				{Kind: iauthnz.PrincipalKind_User, Name: login},
				{Kind: iauthnz.PrincipalKind_Host, Name: "127.0.0.1"},
			},
		},
	}
	authn := NewDefaultAuthenticator(TestSubjectRolesGetter, TestIsDeviceAllowedFuncs)
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			actual, err := authn.Authenticate(t.Context(), appStructs, appTokens, tc.req)
			require.NoError(err)
			// Compare all Principals to catch extra roles as well as missing ones
			require.Equal(tc.expectedPrincipals, actual.Principals)
		})
	}
}

func TestIssueAPITokenScopes(t *testing.T) {
	require := require.New(t)
	carriedRole := appdef.NewQName("test", "carried")
	requestedRole := appdef.NewQName("test", "requested")
	roles := make([]payloads.RoleType, 1, 2)
	roles[0] = payloads.RoleType{WSID: 100, QName: carriedRole}
	backing := roles[:cap(roles)]
	backing[1] = payloads.RoleType{WSID: 300, QName: appdef.NewQName("test", "spare")}
	beforeBacking := slices.Clone(backing)
	src := payloads.PrincipalPayload{
		Login: "login", Alias: "alias", SubjectKind: istructs.SubjectKind_User,
		ProfileWSID: 42, Roles: roles,
	}
	before := src
	before.Roles = slices.Clone(src.Roles)
	tokens := itokensjwt.ProvideITokens(itokensjwt.SecretKeyExample, timeu.NewITime())
	appTokens := payloads.ProvideIAppTokensFactory(tokens).New(istructs.AppQName_test1_app1)
	token, err := IssueAPIToken(appTokens, time.Hour, []appdef.QName{requestedRole}, 200, src)
	require.NoError(err)
	var actual payloads.PrincipalPayload
	gp, err := appTokens.ValidateToken(token, &actual)
	require.NoError(err)
	expected := before
	expected.IsAPIToken = true
	expected.Roles = []payloads.RoleType{
		{Scope: payloads.RoleScopeWorkspace, WSID: 100, QName: carriedRole},
		{Scope: payloads.RoleScopeWorkspace, WSID: 200, QName: requestedRole},
	}
	require.Equal(expected, actual)
	require.Equal(time.Hour, gp.Duration)
	require.Equal(before, src)
	require.Equal(beforeBacking, backing)
}

func TestErrors(t *testing.T) {
	require := require.New(t)

	tokens := itokensjwt.ProvideITokens(itokensjwt.SecretKeyExample, timeu.NewITime())
	appTokens := payloads.ProvideIAppTokensFactory(tokens).New(istructs.AppQName_test1_app1)

	appStructs := &coreutils.MockAppStructs{}
	authn := NewDefaultAuthenticator(TestSubjectRolesGetter, TestIsDeviceAllowedFuncs)

	t.Run("wrong token", func(t *testing.T) {
		req := iauthnz.AuthnRequest{
			RequestWSID: 1,
			Token:       "wrong token",
		}
		result, err := authn.Authenticate(context.Background(), appStructs, appTokens, req)
		require.Error(err)
		require.Empty(result)
	})

	t.Run("unsupported subject kind", func(t *testing.T) {

		pp := payloads.PrincipalPayload{
			Login:       "testlogin",
			SubjectKind: istructs.SubjectKind_FakeLast,
			ProfileWSID: 1,
		}
		token, err := appTokens.IssueToken(time.Minute, &pp)
		require.NoError(err)
		req := iauthnz.AuthnRequest{
			RequestWSID: 1,
			Token:       token,
		}
		result, err := authn.Authenticate(context.Background(), appStructs, appTokens, req)
		require.Error(err)
		require.Empty(result)
	})

	t.Run("personal access token for a system role", func(t *testing.T) {
		for _, sysRole := range iauthnz.SysRoles {
			token, err := IssueAPIToken(appTokens, time.Hour, []appdef.QName{sysRole}, 1, payloads.PrincipalPayload{})
			require.ErrorIs(err, ErrPersonalAccessTokenOnSystemRole)
			require.Empty(token)
		}
	})

	t.Run("personal access token for NullWSID", func(t *testing.T) {
		token, err := IssueAPIToken(appTokens, time.Hour, []appdef.QName{appdef.NewQName("test", "role")}, istructs.NullWSID, payloads.PrincipalPayload{})
		require.ErrorIs(err, ErrPersonalAccessTokenOnNullWSID)
		require.Empty(token)
	})
}

func TestAuthenticationPayload(t *testing.T) {
	require := require.New(t)
	for _, tc := range []struct {
		name string
		kind istructs.SubjectKindType
	}{
		{name: "User authentication", kind: istructs.SubjectKind_User},
		{name: "Device authentication", kind: istructs.SubjectKind_Device},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Shared and source-specific roles detect both duplicate Principals and a skipped role source
			role := appdef.NewQName("test", "shared")
			globalRole := appdef.NewQName("test", "global")
			canonicalRole := appdef.NewQName("test", "canonicalSubject")
			aliasRole := appdef.NewQName("test", "aliasSubject")
			tokenRole := appdef.NewQName("test", "token")
			unmatchedRole := appdef.NewQName("test", "unmatched")
			expectedPayload := payloads.PrincipalPayload{
				Login: "canonical", Alias: "alias", SubjectKind: tc.kind, ProfileWSID: 42,
				GlobalRoles: []appdef.QName{role, globalRole},
				Roles: []payloads.RoleType{
					{Scope: payloads.RoleScopeWorkspacesOwnedBy, WSID: 100, QName: role},
					{Scope: payloads.RoleScopeWorkspace, WSID: 200, QName: tokenRole},
					{Scope: payloads.RoleScopeWorkspacesOwnedBy, WSID: 999, QName: unmatchedRole},
				},
			}
			tokens := itokensjwt.ProvideITokens(itokensjwt.SecretKeyExample, timeu.NewITime())
			appTokens := payloads.ProvideIAppTokensFactory(tokens).New(istructs.AppQName_test1_app1)
			token, err := appTokens.IssueToken(time.Hour, &expectedPayload)
			require.NoError(err)
			var actualLogins []string
			getter := func(_ context.Context, login string, _ istructs.IAppStructs, actualWSID istructs.WSID) ([]appdef.QName, error) {
				require.Equal(istructs.WSID(200), actualWSID)
				actualLogins = append(actualLogins, login)
				switch login {
				case "canonical":
					return []appdef.QName{role, canonicalRole}, nil
				case "alias":
					return []appdef.QName{role, aliasRole}, nil
				default:
					return nil, nil
				}
			}
			actualDeviceChecks := 0
			authn := NewDefaultAuthenticator(getter, IsDeviceAllowedFuncs{istructs.AppQName_test1_app1: func(_ istructs.IAppStructs, actualRequestWSID, actualProfileWSID istructs.WSID) (bool, error) {
				actualDeviceChecks++
				require.Equal(istructs.WSID(200), actualRequestWSID)
				require.Equal(istructs.WSID(42), actualProfileWSID)
				return true, nil
			}})
			as := newAuthenticationApp(t, map[istructs.WSID]int64{200: 100})
			actual, err := authn.Authenticate(t.Context(), as, appTokens, iauthnz.AuthnRequest{RequestWSID: 200, Host: "host", Token: token})
			require.NoError(err)

			// Effective role filtering must not remove unmatched grants from the returned token claims
			require.Equal(expectedPayload, actual.PrincipalPayload)

			// Both identifiers must be queried so alias-based subject assignments are considered
			expectedLogins := []string{"canonical", "alias"}
			require.Equal(expectedLogins, actualLogins)
			// Unique roles prove that both lookups contribute results; the shared role must occur once
			require.Equal([]appdef.QName{role, canonicalRole, aliasRole}, actual.SubjectRoles)

			// Only matching grants become Principals, while all distinct sources and the Host remain effective
			expectedPrincipals := []iauthnz.Principal{
				{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleAuthenticatedUser},
				{Kind: iauthnz.PrincipalKind_Role, QName: role},
				{Kind: iauthnz.PrincipalKind_Role, QName: globalRole},
				{Kind: iauthnz.PrincipalKind_Role, QName: canonicalRole},
				{Kind: iauthnz.PrincipalKind_Role, QName: aliasRole},
				{Kind: iauthnz.PrincipalKind_Role, QName: tokenRole},
				{Kind: iauthnz.PrincipalKind_Host, Name: "host"},
			}

			// Device access requires eligibility and must not confer Workspace ownership
			if tc.kind == istructs.SubjectKind_Device {
				require.Equal(1, actualDeviceChecks)
				expectedPrincipals = append(expectedPrincipals,
					iauthnz.Principal{Kind: iauthnz.PrincipalKind_Device},
					iauthnz.Principal{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleWorkspaceDevice})
			} else {
				require.Zero(actualDeviceChecks)
				expectedPrincipals = append(expectedPrincipals, iauthnz.Principal{Kind: iauthnz.PrincipalKind_User, Name: "canonical"})
			}
			require.ElementsMatch(expectedPrincipals, actual.Principals)
		})
	}

	t.Run("Missing token produces Guest identity", func(t *testing.T) {
		// Tokenless requests need an explicit identity for downstream authorization
		as := newAuthenticationApp(t, nil)
		authn := NewDefaultAuthenticator(TestSubjectRolesGetter, TestIsDeviceAllowedFuncs)
		actual, err := authn.Authenticate(t.Context(), as, nil, iauthnz.AuthnRequest{RequestWSID: 200, Host: "host"})
		require.NoError(err)
		expected := payloads.PrincipalPayload{SubjectKind: istructs.SubjectKind_User, Login: istructs.SysGuestLogin, ProfileWSID: istructs.GuestWSID}
		require.Equal(expected, actual.PrincipalPayload)
	})

	for _, source := range []string{"Global System role", "Subject System role", "Null profile", "API token"} {
		t.Run(source+" preserves identity on early return", func(t *testing.T) {
			tokenRole := appdef.NewQName("test", "token")
			expectedPayload := payloads.PrincipalPayload{
				Login: "canonical", Alias: "alias", SubjectKind: istructs.SubjectKind_Device, ProfileWSID: 42,
				Roles: []payloads.RoleType{{Scope: payloads.RoleScopeWorkspace, WSID: 200, QName: tokenRole}},
			}
			getter := TestSubjectRolesGetter
			switch source {
			case "Global System role":
				expectedPayload.GlobalRoles = []appdef.QName{iauthnz.QNameRoleSystem}
			case "Subject System role":
				getter = func(context.Context, string, istructs.IAppStructs, istructs.WSID) ([]appdef.QName, error) {
					return []appdef.QName{iauthnz.QNameRoleSystem}, nil
				}
			case "Null profile":
				expectedPayload.ProfileWSID = istructs.NullWSID
			case "API token":
				expectedPayload.IsAPIToken = true
			}
			actualSubjectLookups := 0
			countedGetter := func(ctx context.Context, login string, as istructs.IAppStructs, wsid istructs.WSID) ([]appdef.QName, error) {
				actualSubjectLookups++
				return getter(ctx, login, as, wsid)
			}
			tokens := itokensjwt.ProvideITokens(itokensjwt.SecretKeyExample, timeu.NewITime())
			appTokens := payloads.ProvideIAppTokensFactory(tokens).New(istructs.AppQName_test1_app1)
			token, err := appTokens.IssueToken(time.Hour, &expectedPayload)
			require.NoError(err)
			// Missing Workspace storage and device checker make accidental work beyond the early return fail
			actual, err := NewDefaultAuthenticator(countedGetter, nil).Authenticate(t.Context(), &coreutils.MockAppStructs{}, appTokens, iauthnz.AuthnRequest{RequestWSID: 200, Host: "host", Token: token})
			require.NoError(err)

			// Every early return must preserve login, alias, subject kind, profile, and token grants
			require.Equal(expectedPayload, actual.PrincipalPayload)

			// API tokens use their matching grant; regular early returns retain the System role and Host
			expectedSubjectLookups := 2
			expectedPrincipals := []iauthnz.Principal{
				{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleAuthenticatedUser},
			}
			if expectedPayload.IsAPIToken {
				expectedSubjectLookups = 0
				expectedPrincipals = append(expectedPrincipals, iauthnz.Principal{Kind: iauthnz.PrincipalKind_Role, QName: tokenRole})
			} else {
				expectedPrincipals = append(expectedPrincipals,
					iauthnz.Principal{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleSystem},
					iauthnz.Principal{Kind: iauthnz.PrincipalKind_Host, Name: "host"})
			}
			require.Equal(expectedSubjectLookups, actualSubjectLookups)
			require.ElementsMatch(expectedPrincipals, actual.Principals)
			if source == "Subject System role" {
				require.Equal([]appdef.QName{iauthnz.QNameRoleSystem}, actual.SubjectRoles)
			} else {
				require.Empty(actual.SubjectRoles)
			}
		})
	}
}

func TestLegacyAuthenticationPayload(t *testing.T) {
	require := require.New(t)
	for _, tc := range []struct {
		name          string
		api           bool
		expectedScope payloads.RoleScope
		grantWSID     istructs.WSID
	}{
		{name: "Regular token infers owned-by scope", expectedScope: payloads.RoleScopeWorkspacesOwnedBy, grantWSID: 100},
		{name: "API token infers Workspace scope", api: true, expectedScope: payloads.RoleScopeWorkspace, grantWSID: 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expected := payloads.PrincipalPayload{
				Login: "canonical", SubjectKind: istructs.SubjectKind_User, ProfileWSID: 42, IsAPIToken: tc.api,
				Roles: []payloads.RoleType{
					{Scope: tc.expectedScope, WSID: tc.grantWSID, QName: appdef.NewQName("test", "first")},
					{Scope: tc.expectedScope, WSID: tc.grantWSID, QName: appdef.NewQName("test", "second")},
				},
			}
			tokens := itokensjwt.ProvideITokens(itokensjwt.SecretKeyExample, timeu.NewITime())
			appTokens := payloads.ProvideIAppTokensFactory(tokens).New(istructs.AppQName_test1_app1)
			token, err := appTokens.IssueToken(time.Hour, &expected)
			require.NoError(err)
			// Remove additive Scope claims and re-sign to reproduce genuine legacy token input
			parsed, _, err := jwt.NewParser().ParseUnverified(token, jwt.MapClaims{})
			require.NoError(err)
			claims := parsed.Claims.(jwt.MapClaims)
			for _, role := range claims["Roles"].([]any) {
				delete(role.(map[string]any), "Scope")
			}
			legacyToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(itokensjwt.SecretKeyExample))
			require.NoError(err)
			as := newAuthenticationApp(t, map[istructs.WSID]int64{200: 100})
			actual, err := NewDefaultAuthenticator(TestSubjectRolesGetter, nil).Authenticate(t.Context(), as, appTokens, iauthnz.AuthnRequest{RequestWSID: 200, Token: legacyToken})
			require.NoError(err)
			// Legacy grants must acquire the scope implied by token kind while preserving identity, role order, and values
			require.Equal(expected, actual.PrincipalPayload)
			// Both normalized grants must also pass through production role matching into the effective result
			expectedPrincipals := []iauthnz.Principal{
				{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleAuthenticatedUser},
				{Kind: iauthnz.PrincipalKind_Role, QName: appdef.NewQName("test", "first")},
				{Kind: iauthnz.PrincipalKind_Role, QName: appdef.NewQName("test", "second")},
			}
			if !tc.api {
				expectedPrincipals = append(expectedPrincipals,
					iauthnz.Principal{Kind: iauthnz.PrincipalKind_User, Name: "canonical"},
					iauthnz.Principal{Kind: iauthnz.PrincipalKind_Host})
			}
			require.ElementsMatch(expectedPrincipals, actual.Principals)
		})
	}
}

func TestDeviceEligibilityIsRequiredWithoutRoleGrant(t *testing.T) {
	require := require.New(t)
	tokens := itokensjwt.ProvideITokens(itokensjwt.SecretKeyExample, timeu.NewITime())
	appTokens := payloads.ProvideIAppTokensFactory(tokens).New(istructs.AppQName_test1_app1)
	expectedPayload := payloads.PrincipalPayload{Login: "device-1", SubjectKind: istructs.SubjectKind_Device, ProfileWSID: 100}
	token, err := appTokens.IssueToken(time.Hour, &expectedPayload)
	require.NoError(err)
	actualDeviceChecks := 0
	authn := NewDefaultAuthenticator(TestSubjectRolesGetter, IsDeviceAllowedFuncs{istructs.AppQName_test1_app1: func(istructs.IAppStructs, istructs.WSID, istructs.WSID) (bool, error) {
		actualDeviceChecks++
		return false, nil
	}})
	actual, err := authn.Authenticate(t.Context(), newAuthenticationApp(t, nil), appTokens, iauthnz.AuthnRequest{RequestWSID: 200, Token: token})
	require.NoError(err)

	// Missing token roles must not bypass the device eligibility policy
	require.Equal(1, actualDeviceChecks)
	require.NotContains(actual.Principals, iauthnz.Principal{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleWorkspaceDevice})

	// Eligibility restricts effective roles without erasing the authenticated device claims
	require.Equal(expectedPayload, actual.PrincipalPayload)
}

func TestSubjectLookupFailureDiscardsAuthenticationResult(t *testing.T) {
	require := require.New(t)
	expectedError := errors.New("Subject storage unavailable")
	tokens := itokensjwt.ProvideITokens(itokensjwt.SecretKeyExample, timeu.NewITime())
	appTokens := payloads.ProvideIAppTokensFactory(tokens).New(istructs.AppQName_test1_app1)
	token, err := appTokens.IssueToken(time.Hour, &payloads.PrincipalPayload{Login: "canonical", Alias: "alias", SubjectKind: istructs.SubjectKind_User, ProfileWSID: 42, GlobalRoles: []appdef.QName{iauthnz.QNameRoleWorkspaceOwner}})
	require.NoError(err)
	authn := NewDefaultAuthenticator(func(_ context.Context, login string, _ istructs.IAppStructs, _ istructs.WSID) ([]appdef.QName, error) {
		if login == "canonical" {
			return []appdef.QName{appdef.NewQName("test", "subject")}, nil
		}
		return nil, expectedError
	}, nil)
	actual, actualError := authn.Authenticate(t.Context(), &coreutils.MockAppStructs{}, appTokens, iauthnz.AuthnRequest{RequestWSID: 200, Host: "host", Token: token})

	// Callers need the original storage failure to diagnose failed authentication
	require.ErrorIs(actualError, expectedError)

	// A failed alias lookup must discard identity, GlobalRoles, and the earlier canonical Subject roles
	require.Equal(iauthnz.AuthnResult{}, actual)
}

func newAuthenticationApp(t *testing.T, workspaceOwners map[istructs.WSID]int64) *coreutils.MockAppStructs {
	t.Helper()
	records := &coreutils.MockRecords{}
	for wsid, owner := range workspaceOwners {
		descriptor := &coreutils.TestObject{
			Name: appdef.QNameCDocWorkspaceDescriptor,
			Data: map[string]any{"OwnerWSID": owner},
		}
		records.On("GetSingleton", wsid, appdef.QNameCDocWorkspaceDescriptor).Return(descriptor, nil).Maybe()
	}
	// An omitted Workspace models a missing descriptor, as in the real record storage
	records.On("GetSingleton", mock.MatchedBy(func(wsid istructs.WSID) bool {
		_, exists := workspaceOwners[wsid]
		return !exists
	}), appdef.QNameCDocWorkspaceDescriptor).Return(istructsmem.NewNullRecord(istructs.NullRecordID), nil).Maybe()
	app := &coreutils.MockAppStructs{}
	app.On("AppQName").Return(istructs.AppQName_test1_app1).Maybe()
	app.On("Records").Return(records).Maybe()
	t.Cleanup(func() {
		app.AssertExpectations(t)
		records.AssertExpectations(t)
	})
	return app
}
