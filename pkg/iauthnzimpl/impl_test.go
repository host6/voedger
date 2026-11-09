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

	appStructs := AppStructsWithTestStorage(istructs.AppQName_test1_app1, map[istructs.WSID]map[appdef.QName]map[istructs.RecordID]map[string]interface{}{
		// WSID 1 is the user profile, not necessary to store docs there

		// workspace owned by the user
		istructs.WSID(2): {
			appdef.QNameCDocWorkspaceDescriptor: {
				// cdoc.sys.WorkspaceDescriptor.ID=1, .OwnerWSID=1
				1: {
					"OwnerWSID": int64(1), // the same as ProfileWSID
				},
			},
		},

		// child workspace. Parent is WSID 2
		istructs.WSID(3): {
			appdef.QNameCDocWorkspaceDescriptor: {
				// cdoc.sys.WorkspaceDescriptor.ID=1, .OwnerWSID=2
				1: {
					"OwnerWSID": int64(2),
				},
			},
		},
	})
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

		require.Equal(pp.ProfileWSID, result.Payload.ProfileWSID)
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

		require.Equal(pp.ProfileWSID, result.Payload.ProfileWSID)
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
		require.Equal(pp.ProfileWSID, result.Payload.ProfileWSID)
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

	pp.ProfileWSID = istructs.NullWSID
	sysToken, err := appTokens.IssueToken(time.Minute, &pp)
	require.NoError(err)
	pp.ProfileWSID = 1

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

	qNameCDocComputers := appdef.NewQName("untill", "computers")

	appStructs := AppStructsWithTestStorage(istructs.AppQName_test1_app1, map[istructs.WSID]map[appdef.QName]map[istructs.RecordID]map[string]interface{}{
		// WSID 1 is the user profile
		istructs.WSID(1): {
			// wrong to store in the user profile wsid, but ok for test
			qNameCDocComputers: {
				2: {
					appdef.SystemField_QName:    qNameCDocComputers,
					appdef.SystemField_IsActive: true,
				},
				5: {
					appdef.SystemField_QName:    qNameCDocComputers,
					appdef.SystemField_IsActive: false,
				},
			},
			// not used for authorization, but keep for an example
			appdef.NewQName("untill", "restaurant_computers"): {
				3: {},
				6: {},
			},
		},

		// workspace owned by the user
		istructs.WSID(2): {
			appdef.QNameCDocWorkspaceDescriptor: {
				// cdoc.sys.WorkspaceDescriptor.ID=1, .OwnerWSID=1
				1: {
					"OwnerWSID": int64(1), // the same as ProfileWSID
				},
			},
		},

		// child workspace. Parent is WSID 2
		istructs.WSID(3): {
			appdef.QNameCDocWorkspaceDescriptor: {
				// cdoc.sys.WorkspaceDescriptor.ID=1, .OwnerWSID=2
				1: {
					"OwnerWSID": int64(2),
				},
			},
		},
		istructs.WSID(5): {
			appdef.QNameCDocWorkspaceDescriptor: {1: {"OwnerWSID": int64(1)}},
		},
	})

	testCases := []struct {
		desc               string
		req                iauthnz.AuthnRequest
		expectedPrincipals []iauthnz.Principal
		subjects           []appdef.QName
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
			desc: "system token -> host and system role",
			req: iauthnz.AuthnRequest{
				Host:        "127.0.0.1",
				RequestWSID: 1,
				Token:       sysToken,
			},
			expectedPrincipals: []iauthnz.Principal{
				{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleAuthenticatedUser},
				{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleSystem},
				{Kind: iauthnz.PrincipalKind_Host, Name: "127.0.0.1"},
			},
		},
		{
			desc: "request to profile -> authenticatedUser + user + profileOwner",
			req: iauthnz.AuthnRequest{
				Host:        "127.0.0.1",
				RequestWSID: 1,
				Token:       userToken,
			},
			expectedPrincipals: []iauthnz.Principal{
				{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleAuthenticatedUser},
				{Kind: iauthnz.PrincipalKind_User, Name: login},
				{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleProfileOwner},
				{Kind: iauthnz.PrincipalKind_Host, Name: "127.0.0.1"},
			},
		},
		{
			desc: "request to an owned workspace -> AuthenticatedUser + user + workspaceOwner + host",
			req: iauthnz.AuthnRequest{
				Host:        "127.0.0.1",
				RequestWSID: 2,
				Token:       userToken,
			},
			expectedPrincipals: []iauthnz.Principal{
				{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleAuthenticatedUser},
				{Kind: iauthnz.PrincipalKind_User, Name: login},
				{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleWorkspaceOwner},
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
			desc: "IsPersonalAccessToken -> principals are built by provided roles only",
			req: iauthnz.AuthnRequest{
				Host:        "127.0.0.1",
				RequestWSID: 2,
				Token:       apiKeyToken,
			},
			expectedPrincipals: []iauthnz.Principal{
				{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleAuthenticatedUser},
				{Kind: iauthnz.PrincipalKind_Role, QName: testRole},
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
	var subjects *[]appdef.QName
	subjectsGetter := func(context.Context, string, istructs.IAppStructs, istructs.WSID) ([]appdef.QName, error) {
		return *subjects, nil
	}
	authn := NewDefaultAuthenticator(subjectsGetter, TestIsDeviceAllowedFuncs)
	for _, tc := range testCases {
		localVarSubjects := &tc.subjects
		t.Run(tc.desc, func(t *testing.T) {
			subjects = localVarSubjects
			result, err := authn.Authenticate(context.Background(), appStructs, appTokens, tc.req)
			require.NoError(err)
			principals := result.Principals
			require.Equal(tc.expectedPrincipals, principals, tc.desc)
		})
	}

	t.Run("API token skips Subject and Workspace descriptor reads", func(t *testing.T) {
		authn := NewDefaultAuthenticator(func(context.Context, string, istructs.IAppStructs, istructs.WSID) ([]appdef.QName, error) {
			panic("unexpected Subject lookup")
		}, TestIsDeviceAllowedFuncs)
		result, err := authn.Authenticate(context.Background(), &implIAppStructs{}, appTokens,
			iauthnz.AuthnRequest{RequestWSID: 2, Token: apiKeyToken})
		require.NoError(err)
		principals := result.Principals
		require.Equal(istructs.WSID(1), result.Payload.ProfileWSID)
		require.Equal([]iauthnz.Principal{
			{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleAuthenticatedUser},
			{Kind: iauthnz.PrincipalKind_Role, QName: testRole},
		}, principals)
	})

	globalSystemToken, err := appTokens.IssueToken(time.Minute, &payloads.PrincipalPayload{
		Login: login, SubjectKind: istructs.SubjectKind_User, ProfileWSID: 1,
		GlobalRoles: []appdef.QName{iauthnz.QNameRoleSystem},
	})
	require.NoError(err)
	for _, tc := range []struct {
		name    string
		token   string
		profile istructs.WSID
	}{
		{"missing profile", sysToken, 0},
		{"global System role", globalSystemToken, 1},
	} {
		t.Run(tc.name+" skips Workspace descriptor reads", func(t *testing.T) {
			authn := NewDefaultAuthenticator(TestSubjectRolesGetter, TestIsDeviceAllowedFuncs)
			result, err := authn.Authenticate(context.Background(), &implIAppStructs{}, appTokens,
				iauthnz.AuthnRequest{RequestWSID: 2, Token: tc.token})
			require.NoError(err)
			principals := result.Principals
			require.Equal(tc.profile, result.Payload.ProfileWSID)
			require.Len(principals, 3)
			require.Contains(principals, iauthnz.Principal{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleSystem})
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

	appStructs := &implIAppStructs{}
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

func TestSubjectRolesResolveAgainstLoginAndAlias(t *testing.T) {
	require := require.New(t)

	tokens := itokensjwt.ProvideITokens(itokensjwt.SecretKeyExample, timeu.NewITime())
	appTokens := payloads.ProvideIAppTokensFactory(tokens).New(istructs.AppQName_test1_app1)

	const canonicalLogin = "jsmith"
	const aliasLogin = "j.smith"
	subjectRole := appdef.NewQName("test", "subjectrole")

	// grants subjectRole only to the subject login string it is configured to match
	rolesGetterFor := func(matchLogin string) SubjectGetterFunc {
		return func(_ context.Context, login string, _ istructs.IAppStructs, _ istructs.WSID) ([]appdef.QName, error) {
			if login == matchLogin {
				return []appdef.QName{subjectRole}, nil
			}
			return nil, nil
		}
	}

	appStructs := AppStructsWithTestStorage(istructs.AppQName_test1_app1, map[istructs.WSID]map[appdef.QName]map[istructs.RecordID]map[string]interface{}{})

	authenticateWith := func(getterMatch string) []iauthnz.Principal {
		pp := payloads.PrincipalPayload{
			Login:       canonicalLogin,
			Alias:       aliasLogin,
			SubjectKind: istructs.SubjectKind_User,
			ProfileWSID: 1,
		}
		token, err := appTokens.IssueToken(time.Minute, &pp)
		require.NoError(err)
		authn := NewDefaultAuthenticator(rolesGetterFor(getterMatch), TestIsDeviceAllowedFuncs)
		result, err := authn.Authenticate(context.Background(), appStructs, appTokens, iauthnz.AuthnRequest{
			Host:        "127.0.0.1",
			RequestWSID: 1,
			Token:       token,
		})
		require.NoError(err)
		require.Equal(canonicalLogin, result.Payload.Login)
		return result.Principals
	}

	hasSubjectRole := func(principals []iauthnz.Principal) bool {
		for _, prn := range principals {
			if prn.Kind == iauthnz.PrincipalKind_Role && prn.QName == subjectRole {
				return true
			}
		}
		return false
	}

	t.Run("subject registered under the canonical login resolves", func(t *testing.T) {
		require.True(hasSubjectRole(authenticateWith(canonicalLogin)))
	})

	t.Run("subject registered under the active alias resolves", func(t *testing.T) {
		require.True(hasSubjectRole(authenticateWith(aliasLogin)))
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
			// Overlapping role sources exercise deduplication; an unmatched grant must remain in the payload
			role := appdef.NewQName("test", "shared")
			expectedPayload := payloads.PrincipalPayload{
				Login: "canonical", Alias: "alias", SubjectKind: tc.kind, ProfileWSID: 42,
				GlobalRoles: []appdef.QName{role},
				Roles: []payloads.RoleType{
					{Scope: payloads.RoleScopeWorkspacesOwnedBy, WSID: 100, QName: role},
					{Scope: payloads.RoleScopeWorkspacesOwnedBy, WSID: 999, QName: role},
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
				if login == "alias" {
					return []appdef.QName{role}, nil
				}
				return nil, nil
			}
			actualDeviceChecks := 0
			authn := NewDefaultAuthenticator(getter, IsDeviceAllowedFuncs{istructs.AppQName_test1_app1: func(_ istructs.IAppStructs, actualRequestWSID, actualProfileWSID istructs.WSID) (bool, error) {
				actualDeviceChecks++
				require.Equal(istructs.WSID(200), actualRequestWSID)
				require.Equal(istructs.WSID(42), actualProfileWSID)
				return true, nil
			}})
			as := AppStructsWithTestStorage(istructs.AppQName_test1_app1, map[istructs.WSID]map[appdef.QName]map[istructs.RecordID]map[string]interface{}{
				200: {appdef.QNameCDocWorkspaceDescriptor: {1: {"OwnerWSID": int64(100)}}},
			})
			actual, err := authn.Authenticate(t.Context(), as, appTokens, iauthnz.AuthnRequest{RequestWSID: 200, Host: "host", Token: token})
			require.NoError(err)

			// Effective role filtering must not remove unmatched grants from the returned token claims
			require.Equal(expectedPayload, actual.Payload)

			// Both identifiers must be queried so alias-based subject assignments are considered
			expectedLogins := []string{"canonical", "alias"}
			require.Equal(expectedLogins, actualLogins)

			// Global, token, and subject grants for the same role must yield one effective principal
			actualRoleCount := 0
			for _, principal := range actual.Principals {
				if principal.Kind == iauthnz.PrincipalKind_Role && principal.QName == role {
					actualRoleCount++
				}
			}
			require.Equal(1, actualRoleCount)

			// Host-based authorization still needs the request host after token authentication
			expected := iauthnz.Principal{Kind: iauthnz.PrincipalKind_Host, Name: "host"}
			require.Contains(actual.Principals, expected)

			// Device access requires eligibility and must not confer Workspace ownership
			if tc.kind == istructs.SubjectKind_Device {
				require.Equal(1, actualDeviceChecks)
				require.Contains(actual.Principals, iauthnz.Principal{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleWorkspaceDevice})
				require.NotContains(actual.Principals, iauthnz.Principal{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleWorkspaceOwner})
			} else {
				require.Zero(actualDeviceChecks)
			}

			// Independent token decoding must agree with the payload returned by authentication
			actualPayload, err := payloads.GetPrincipalPayload(appTokens, token)
			require.NoError(err)
			require.Equal(expectedPayload, actualPayload)
		})
	}

	t.Run("Missing token produces Guest identity", func(t *testing.T) {
		// Tokenless requests need an explicit identity for downstream authorization
		as := AppStructsWithTestStorage(istructs.AppQName_test1_app1, nil)
		authn := NewDefaultAuthenticator(TestSubjectRolesGetter, TestIsDeviceAllowedFuncs)
		actual, err := authn.Authenticate(t.Context(), as, nil, iauthnz.AuthnRequest{RequestWSID: 200, Host: "host"})
		require.NoError(err)
		expected := payloads.PrincipalPayload{SubjectKind: istructs.SubjectKind_User, Login: istructs.SysGuestLogin, ProfileWSID: istructs.GuestWSID}
		require.Equal(expected, actual.Payload)
	})

	for _, source := range []string{"Global System role", "Subject System role", "Null profile", "API token"} {
		t.Run(source+" preserves identity on early return", func(t *testing.T) {
			expectedPayload := payloads.PrincipalPayload{Login: "canonical", Alias: "alias", SubjectKind: istructs.SubjectKind_Device, ProfileWSID: 42}
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
			actual, err := NewDefaultAuthenticator(countedGetter, nil).Authenticate(t.Context(), &implIAppStructs{}, appTokens, iauthnz.AuthnRequest{RequestWSID: 200, Host: "host", Token: token})
			require.NoError(err)

			// Every early return must preserve login, alias, subject kind, profile, and token grants
			require.Equal(expectedPayload, actual.Payload)

			// API tokens skip subjects and host binding; regular tokens retain the System role and host principal
			expectedSubjectLookups := 2
			expectedPrincipals := []iauthnz.Principal{
				{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleAuthenticatedUser},
				{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleSystem},
				{Kind: iauthnz.PrincipalKind_Host, Name: "host"},
			}
			if expectedPayload.IsAPIToken {
				expectedSubjectLookups = 0
				expectedPrincipals = expectedPrincipals[:1]
			}
			require.Equal(expectedSubjectLookups, actualSubjectLookups)
			require.ElementsMatch(expectedPrincipals, actual.Principals)
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
			as := AppStructsWithTestStorage(istructs.AppQName_test1_app1, map[istructs.WSID]map[appdef.QName]map[istructs.RecordID]map[string]interface{}{
				200: {appdef.QNameCDocWorkspaceDescriptor: {1: {"OwnerWSID": int64(100)}}},
			})
			actual, err := NewDefaultAuthenticator(TestSubjectRolesGetter, nil).Authenticate(t.Context(), as, appTokens, iauthnz.AuthnRequest{RequestWSID: 200, Token: legacyToken})
			require.NoError(err)
			// Legacy grants must acquire the scope implied by token kind while preserving identity, role order, and values
			require.Equal(expected, actual.Payload)
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
	actual, err := authn.Authenticate(t.Context(), AppStructsWithTestStorage(istructs.AppQName_test1_app1, nil), appTokens, iauthnz.AuthnRequest{RequestWSID: 200, Token: token})
	require.NoError(err)

	// Missing token roles must not bypass the device eligibility policy
	require.Equal(1, actualDeviceChecks)
	require.NotContains(actual.Principals, iauthnz.Principal{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleWorkspaceDevice})

	// Eligibility restricts effective roles without erasing the authenticated device claims
	require.Equal(expectedPayload, actual.Payload)
}

func TestSubjectLookupFailureDiscardsAuthenticationResult(t *testing.T) {
	require := require.New(t)
	expectedError := errors.New("Subject storage unavailable")
	tokens := itokensjwt.ProvideITokens(itokensjwt.SecretKeyExample, timeu.NewITime())
	appTokens := payloads.ProvideIAppTokensFactory(tokens).New(istructs.AppQName_test1_app1)
	token, err := appTokens.IssueToken(time.Hour, &payloads.PrincipalPayload{Login: "canonical", SubjectKind: istructs.SubjectKind_User, ProfileWSID: 42, GlobalRoles: []appdef.QName{iauthnz.QNameRoleWorkspaceOwner}})
	require.NoError(err)
	authn := NewDefaultAuthenticator(func(context.Context, string, istructs.IAppStructs, istructs.WSID) ([]appdef.QName, error) {
		return nil, expectedError
	}, nil)
	actual, actualError := authn.Authenticate(t.Context(), &implIAppStructs{}, appTokens, iauthnz.AuthnRequest{RequestWSID: 200, Host: "host", Token: token})

	// Callers need the original storage failure to diagnose failed authentication
	require.ErrorIs(actualError, expectedError)

	// A failed lookup must not expose the decoded identity, earlier global roles, or host principal
	require.Equal(iauthnz.AuthnResult{}, actual)
}

func AppStructsWithTestStorage(appQName appdef.AppQName, data map[istructs.WSID]map[appdef.QName]map[istructs.RecordID]map[string]interface{}) istructs.IAppStructs {
	recs := &implIRecords{data: data}
	return &implIAppStructs{records: recs, views: &implIViewRecords{records: recs}, appQName: appQName}
}

type implIAppStructs struct {
	records  *implIRecords
	views    *implIViewRecords
	appQName appdef.AppQName
}

func (as *implIAppStructs) AppDef() appdef.IAppDef                                         { panic("") }
func (as *implIAppStructs) Events() istructs.IEvents                                       { panic("") }
func (as *implIAppStructs) Records() istructs.IRecords                                     { return as.records }
func (as *implIAppStructs) ViewRecords() istructs.IViewRecords                             { return as.views }
func (as *implIAppStructs) ObjectBuilder(appdef.QName) istructs.IObjectBuilder             { panic("") }
func (as *implIAppStructs) Resources() istructs.IResources                                 { panic("") }
func (as *implIAppStructs) ClusterAppID() istructs.ClusterAppID                            { panic("") }
func (as *implIAppStructs) AppQName() appdef.AppQName                                      { return as.appQName }
func (as *implIAppStructs) DescribePackageNames() []string                                 { panic("") }
func (as *implIAppStructs) DescribePackage(string) interface{}                             { panic("") }
func (as *implIAppStructs) SyncProjectors() istructs.Projectors                            { panic("") }
func (as *implIAppStructs) AsyncProjectors() istructs.Projectors                           { panic("") }
func (as *implIAppStructs) CUDValidators() []istructs.CUDValidator                         { panic("") }
func (as *implIAppStructs) EventValidators() []istructs.EventValidator                     { panic("") }
func (as *implIAppStructs) NumAppWorkspaces() istructs.NumAppWorkspaces                    { panic("") }
func (as *implIAppStructs) AppTokens() istructs.IAppTokens                                 { panic("") }
func (as *implIAppStructs) GetEventReapplier(istructs.IPLogEvent) istructs.IEventReapplier { panic("") }
func (as *implIAppStructs) QNameID(appdef.QName) (istructs.QNameID, error)                 { panic("") }
func (as *implIAppStructs) AppTTLStorage() istructs.IAppTTLStorage                         { panic("") }

type implIRecords struct {
	data map[istructs.WSID]map[appdef.QName]map[istructs.RecordID]map[string]interface{}
}

func (*implIRecords) Apply(istructs.IPLogEvent) error                          { panic("") }
func (*implIRecords) Apply2(istructs.IPLogEvent, func(istructs.IRecord)) error { panic("") }
func (*implIRecords) PutJSON(istructs.WSID, map[appdef.FieldName]any) error    { panic("") }
func (r *implIRecords) Get(wsid istructs.WSID, _ bool, id istructs.RecordID) (istructs.IRecord, error) {
	if wsData, ok := r.data[wsid]; ok {
		for qName, qNameRecs := range wsData {
			for recID, recData := range qNameRecs {
				if recID == id {
					return &implIRecord{Data: recData, qName: qName}, nil
				}
			}
		}
	}
	return istructsmem.NewNullRecord(id), nil
}
func (*implIRecords) GetBatch(istructs.WSID, bool, []istructs.RecordGetBatchItem) error { panic("") }
func (r *implIRecords) GetSingleton(wsid istructs.WSID, qName appdef.QName) (record istructs.IRecord, err error) {
	if wsData, ok := r.data[wsid]; ok {
		if qNameRecs, ok := wsData[qName]; ok {
			if len(qNameRecs) > 1 {
				panic(">1 records for a singleton")
			}
			for _, data := range qNameRecs {
				return &implIRecord{qName: qName, Data: data}, nil
			}
		}
	}
	return istructsmem.NewNullRecord(istructs.NullRecordID), nil
}
func (*implIRecords) Read(istructs.WSID, bool, istructs.RecordID) (istructs.IRecord, error) {
	panic("")
}

func (r *implIRecords) GetSingletonID(qName appdef.QName) (istructs.RecordID, error) {
	for _, wsData := range r.data {
		if qNameRecs, ok := wsData[qName]; ok {
			if len(qNameRecs) > 1 {
				panic(">1 records for a singleton")
			}
			for _, data := range qNameRecs {
				iRecord := &implIRecord{qName: qName, Data: data}
				return iRecord.ID(), nil
			}
		}
	}

	return istructs.NullRecordID, nil
}

type implIRecord struct {
	coreutils.TestObject
	qName appdef.QName
}

func (r *implIRecord) QName() appdef.QName                                     { return r.qName }
func (r *implIRecord) ID() istructs.RecordID                                   { return r.AsRecordID(appdef.SystemField_ID) }
func (*implIRecord) Parent() istructs.RecordID                                 { panic("") }
func (*implIRecord) Container() string                                         { panic("") }
func (*implIRecord) RecordIDs(bool) func(func(string, istructs.RecordID) bool) { panic("") }
func (r *implIRecord) Fields(cb func(iField appdef.IField) bool)               { r.TestObject.Fields(cb) }

type implIViewRecords struct {
	records *implIRecords
}

func (*implIViewRecords) KeyBuilder(view appdef.QName) istructs.IKeyBuilder {
	return &implIKeyBuilder{qName: view, Data: map[string]interface{}{}}
}
func (*implIViewRecords) NewValueBuilder(appdef.QName) istructs.IValueBuilder { panic("") }
func (*implIViewRecords) UpdateValueBuilder(appdef.QName, istructs.IValue) istructs.IValueBuilder {
	panic("")
}
func (*implIViewRecords) Put(istructs.WSID, istructs.IKeyBuilder, istructs.IValueBuilder) error {
	panic("")
}
func (*implIViewRecords) PutBatch(istructs.WSID, []istructs.ViewKV) error                  { panic("") }
func (*implIViewRecords) PutJSON(istructs.WSID, map[appdef.FieldName]any) error            { panic("") }
func (*implIViewRecords) Get(istructs.WSID, istructs.IKeyBuilder) (istructs.IValue, error) { panic("") }
func (vr *implIViewRecords) GetBatch(workspace istructs.WSID, kv []istructs.ViewRecordGetBatchItem) error {
	if wsData, ok := vr.records.data[workspace]; ok {
		for biIdx, bi := range kv {
			kb := bi.Key.(*implIKeyBuilder)
			if qNameRecs, ok := wsData[kb.qName]; ok {
				for _, qNameRec := range qNameRecs {
					matchedFields := 0
					for keyField, keyValue := range kb.Data {
						if recFieldValue, ok := qNameRec[keyField]; ok {
							if recFieldValue == keyValue {
								matchedFields++
							}
						}
					}
					if len(kb.Data) == matchedFields {
						kv[biIdx].Ok = true
						kv[biIdx].Value = &implIValue{Data: qNameRec}
						break
					}
				}
			}
		}
	}
	return nil
}
func (*implIViewRecords) Read(context.Context, istructs.WSID, istructs.IKeyBuilder, istructs.ValuesCallback) error {
	panic("")
}

type implIKeyBuilder struct {
	coreutils.TestObject
	qName appdef.QName
}

func (kb *implIKeyBuilder) PartitionKey() istructs.IRowWriter      { return &kb.TestObject }
func (kb *implIKeyBuilder) ClusteringColumns() istructs.IRowWriter { return &kb.TestObject }
func (kb *implIKeyBuilder) Equals(istructs.IKeyBuilder) bool       { panic("implement me") }
func (kb *implIKeyBuilder) ToBytes(istructs.WSID) (pk []byte, cc []byte, err error) {
	return nil, nil, nil
}

type implIValue struct {
	coreutils.TestObject
}

func (v *implIValue) AsRecord(name string) (record istructs.IRecord) { panic("") }
func (v *implIValue) AsEvent(name string) (event istructs.IDbEvent)  { panic("") }
