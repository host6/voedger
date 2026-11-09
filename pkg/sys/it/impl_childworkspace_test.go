/*
 * Copyright (c) 2020-present unTill Pro, Ltd.
 */

package sys_it

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"github.com/voedger/voedger/pkg/appdef"
	"github.com/voedger/voedger/pkg/goutils/httpu"
	"github.com/voedger/voedger/pkg/iauthnz"
	"github.com/voedger/voedger/pkg/iauthnzimpl"
	"github.com/voedger/voedger/pkg/istructs"
	payloads "github.com/voedger/voedger/pkg/itokens-payloads"
	it "github.com/voedger/voedger/pkg/vit"
)

func TestBasicUsage_ChildWorkspaces(t *testing.T) {
	require := require.New(t)
	vit := it.NewVIT(t, &it.SharedConfig_App1)
	defer vit.TearDown()

	parentWS := vit.WS(istructs.AppQName_test1_app1, "test_ws")
	wsName := vit.NextName()

	t.Run("404 not found on unexisting child workspace query", func(t *testing.T) {
		body := fmt.Sprintf(`
			{
				"args": {
					"WSName": %q
				},
				"elements":[
					{
						"fields":["WSID"]
					}
				]
			}`, wsName)
		resp := vit.PostWS(parentWS, "q.sys.QueryChildWorkspaceByName", body, httpu.Expect404())
		resp.Println()
	})

	t.Run("create child workspace", func(t *testing.T) {
		// init
		// note: creating workspace at non-main cluster is unsupported for now
		// because there are no AppWorkspaces in any cluster but Main (created automatically on VVM launch)
		// also GetAppWSID() uses MainCluser, not the target one
		body := fmt.Sprintf(`
			{
				"args": {
					"WSName": %q,
					"WSKind": "app1pkg.test_ws",
					"WSKindInitializationData": "{\"IntFld\": 10}",
					"TemplateName": "test_template",
					"WSClusterID": 1
				}
			}`, wsName)
		vit.PostWS(parentWS, "c.sys.InitChildWorkspace", body)

		// wait for finish
		childWS := vit.WaitForChildWorkspace(parentWS, wsName)
		require.Empty(childWS.WSError)
		require.Equal(wsName, childWS.Name)
		require.Equal(it.QNameApp1_TestWSKind, childWS.Kind)
		require.Equal(`{"IntFld": 10}`, childWS.InitDataJSON)
		require.Equal("test_template", childWS.TemplateName)
		require.Equal(istructs.ClusterID(1), childWS.WSID.ClusterID())

		t.Run("create a new workspace with an existing name -> 409 conflict", func(t *testing.T) {
			body := fmt.Sprintf(`{"args": {"WSName": %q,"WSKind": "app1pkg.test_ws","WSKindInitializationData": "{\"WorkStartTime\": \"10\"}","TemplateName": "test","WSClusterID": 1}}`, wsName)
			resp := vit.PostWS(parentWS, "c.sys.InitChildWorkspace", body, httpu.Expect409())
			resp.Println()
		})
	})

	t.Run("read child workspaces list", func(t *testing.T) {
		body := `{"args":{"Schema":"sys.ChildWorkspace"},"elements":[{"fields":["WSName","WSKind","WSID","WSError"]}]}`
		resp := vit.PostWS(parentWS, "q.sys.Collection", body)
		// note: wsKind is rendered as {} because q.sys.Collection appends QName to the object to marshal to JSON by value
		// whereas appdef.QName.MarshalJSON() func has pointer receiver
		resp.Println()
	})
}

func TestForeignAuthorization(t *testing.T) {
	require := require.New(t)
	vit := it.NewVIT(t, &it.SharedConfig_App1)
	defer vit.TearDown()

	// sign up a new login
	newLoginName := vit.NextName()
	newLogin := vit.SignUp(newLoginName, "1", istructs.AppQName_test1_app1)
	newPrn := vit.SignIn(newLogin)

	parentWS := vit.WS(istructs.AppQName_test1_app1, "test_ws")
	wsName := vit.NextName()

	// init child workspace
	body := fmt.Sprintf(`{"args": {"WSName": %q,"WSKind": "app1pkg.test_ws","WSKindInitializationData": "{\"IntFld\": 10}","TemplateName": "test_template","WSClusterID": 42}}`, wsName)
	vit.PostWS(parentWS, "c.sys.InitChildWorkspace", body)

	// wait for finish
	childWS := vit.WaitForChildWorkspace(parentWS, wsName)

	t.Run("subjects", func(t *testing.T) {
		// try to execute an operation by the foreign login, expect 403
		cudBody := `{"cuds": [{"fields": {"sys.ID": 1,"sys.QName": "app1pkg.articles","name": "cola","article_manual": 1,"article_hash": 2,"hideonhold": 3,"time_active": 4,"control_active": 5}}]}`
		vit.PostWS(parentWS, "c.sys.CUD", cudBody, httpu.Expect403(), httpu.WithAuthorizeBy(newPrn.Token))

		// make this new foreign login a subject in the existing workspace
		body := fmt.Sprintf(`{"cuds": [{"fields": {"sys.ID": 1,"sys.QName": "sys.Subject","Login": %q,"SubjectKind":%d,"Roles": %q,"ProfileWSID":%d}}]}`,
			newLoginName, istructs.SubjectKind_User, iauthnz.QNameRoleWorkspaceOwner, newPrn.ProfileWSID)
		cdocSubjectID := vit.PostWS(parentWS, "c.sys.CUD", body).NewID()

		// now the foreign login could work in the workspace
		vit.PostWS(parentWS, "c.sys.CUD", cudBody, httpu.WithAuthorizeBy(newPrn.Token))

		// deactivate cdoc.Payload
		body = fmt.Sprintf(`{"cuds": [{"sys.ID": %d,"fields": {"sys.IsActive": false}}]}`, cdocSubjectID)
		vit.PostWS(parentWS, "c.sys.CUD", body)

		// the foreign login can no longer work in the workspace
		vit.PostWS(parentWS, "c.sys.CUD", cudBody, httpu.WithAuthorizeBy(newPrn.Token), httpu.Expect403())
	})

	t.Run("enrich principal token", func(t *testing.T) {
		// 403 forbidden on try to execute a stricted operation in the child workspace using the non-enriched token
		body = `{"cuds": [{"fields": {"sys.ID": 1,"sys.QName": "app1pkg.articles","name": "cola","article_manual": 1,"article_hash": 2,"hideonhold": 3,"time_active": 4,"control_active": 5}}]}`
		vit.PostWS(childWS, "c.sys.CUD", body, httpu.Expect403())

		// create cdoc.sys.Subject with a role the custom func execution could be authorized with
		body = fmt.Sprintf(`{"cuds": [{"fields": {"sys.ID": 1,"sys.QName": "sys.Subject","Login": "login","SubjectKind":%d,"Roles": %q,"ProfileWSID":%d}}]}`,
			istructs.SubjectKind_User, iauthnz.QNameRoleWorkspaceOwner, newPrn.ProfileWSID)
		vit.PostWS(parentWS, "c.sys.CUD", body)

		// enrich the principal token in the parentWS
		// basic auth
		body = `{"args":{"Login":"login"},"elements":[{"fields":["EnrichedToken"]}]}`
		resp := vit.PostWS(parentWS, "q.sys.EnrichPrincipalToken", body)
		enrichedToken := resp.SectionRow()[0].(string)

		// ok to execute a stricted operation in the child workspace using the enriched token
		// expect no errors
		body = `{"cuds": [{"fields": {"sys.ID": 1,"sys.QName": "app1pkg.articles","name": "cola","article_manual": 1,"article_hash": 2,"hideonhold": 3,"time_active": 4,"control_active": 5}}]}`
		vit.PostWS(childWS, "c.sys.CUD", body, httpu.WithAuthorizeBy(enrichedToken))
		enrichedPrincipalPayload, enrichedGenericPayload := decodeScopedPrincipalToken(t, require, vit, enrichedToken)
		var originalPayload payloads.PrincipalPayload
		_, err := vit.ValidateToken(parentWS.Owner.Token, &originalPayload)
		require.NoError(err)
		require.Subset(enrichedPrincipalPayload.Roles, originalPayload.Roles)
		originalPayload.Roles = enrichedPrincipalPayload.Roles
		require.Equal(originalPayload, enrichedPrincipalPayload)
		require.Contains(enrichedPrincipalPayload.Roles, payloads.RoleType{
			Scope: payloads.RoleScopeWorkspacesOwnedBy, WSID: parentWS.WSID, QName: iauthnz.QNameRoleWorkspaceOwner,
		})
		for _, role := range enrichedPrincipalPayload.Roles {
			require.Equal(payloads.RoleScopeWorkspacesOwnedBy, role.Scope)
		}

		enrichBody := `{"args":{"Login":"login"},"elements":[{"fields":["EnrichedToken"]}]}`
		repeatedToken := vit.PostWS(parentWS, "q.sys.EnrichPrincipalToken", enrichBody,
			httpu.WithAuthorizeBy(enrichedToken)).SectionRow()[0].(string)
		repeatedPayload, _ := decodeScopedPrincipalToken(t, require, vit, repeatedToken)
		require.Equal(enrichedPrincipalPayload, repeatedPayload)

		refreshBody := `{"args":{},"elements":[{"fields":["NewPrincipalToken"]}]}`
		refreshedToken := vit.PostProfile(parentWS.Owner, "q.sys.RefreshPrincipalToken", refreshBody,
			httpu.WithAuthorizeBy(repeatedToken)).SectionRow()[0].(string)
		refreshedPayload, refreshedMetadata := decodeScopedPrincipalToken(t, require, vit, refreshedToken)
		require.Equal(repeatedPayload, refreshedPayload)
		require.Equal(enrichedGenericPayload.AppQName, refreshedMetadata.AppQName)
		require.Equal(enrichedGenericPayload.Duration, refreshedMetadata.Duration)
		vit.PostWS(childWS, "c.sys.CUD", body, httpu.WithAuthorizeBy(refreshedToken))

		grandchildName := vit.NextName()
		grandchildBody, err := json.Marshal(map[string]any{"args": map[string]any{
			"WSName": grandchildName, "WSKind": "app1pkg.test_ws",
			"WSKindInitializationData": `{"IntFld":10}`, "WSClusterID": 1,
		}})
		require.NoError(err)
		vit.PostWS(childWS, "c.sys.InitChildWorkspace", string(grandchildBody), httpu.WithAuthorizeBy(refreshedToken))
		grandchildWS := vit.WaitForChildWorkspace(childWS, grandchildName, httpu.WithAuthorizeBy(refreshedToken))
		vit.PostWS(grandchildWS, "c.sys.CUD", body, httpu.WithAuthorizeBy(refreshedToken), httpu.Expect403())
		nextToken := vit.PostWS(childWS, "q.sys.EnrichPrincipalToken", enrichBody,
			httpu.WithAuthorizeBy(refreshedToken)).SectionRow()[0].(string)
		nextPayload, _ := decodeScopedPrincipalToken(t, require, vit, nextToken)
		expectedGrants := append([]payloads.RoleType(nil), refreshedPayload.Roles...)
		expectedGrants = append(expectedGrants,
			payloads.RoleType{Scope: payloads.RoleScopeWorkspacesOwnedBy, WSID: childWS.WSID, QName: iauthnz.QNameRoleAuthenticatedUser},
			payloads.RoleType{Scope: payloads.RoleScopeWorkspacesOwnedBy, WSID: childWS.WSID, QName: iauthnz.QNameRoleWorkspaceOwner})
		require.ElementsMatch(expectedGrants, nextPayload.Roles)
		expectedPayload := refreshedPayload
		expectedPayload.Roles = nextPayload.Roles
		require.Equal(expectedPayload, nextPayload)
		require.Contains(nextPayload.Roles, payloads.RoleType{
			Scope: payloads.RoleScopeWorkspacesOwnedBy, WSID: childWS.WSID, QName: iauthnz.QNameRoleWorkspaceOwner,
		})
		vit.PostWS(grandchildWS, "c.sys.CUD", body, httpu.WithAuthorizeBy(nextToken))
		nextRepeatedToken := vit.PostWS(childWS, "q.sys.EnrichPrincipalToken", enrichBody,
			httpu.WithAuthorizeBy(nextToken)).SectionRow()[0].(string)
		nextRepeatedPayload, _ := decodeScopedPrincipalToken(t, require, vit, nextRepeatedToken)
		require.Equal(nextPayload, nextRepeatedPayload)
	})

	t.Run("API token", func(t *testing.T) {
		// 403 forbidden on try to execute a stricted operation in the child workspace
		body = `{"args":{"Schema":"app1pkg.articles"},"elements":[{"fields":["sys.ID"]}]}`
		vit.PostWS(childWS, "q.sys.Collection", body, httpu.Expect403())

		// Grant the role only in childWS, independently of the token holder's profile
		as, err := vit.IAppStructsProvider.BuiltIn(istructs.AppQName_test1_app1)
		require.NoError(err)
		role := appdef.NewQName("app1pkg", "SpecialAPITokenRole")
		payload := payloads.PrincipalPayload{
			Login:       parentWS.Owner.Name,
			SubjectKind: istructs.SubjectKind_User,
			ProfileWSID: parentWS.Owner.ProfileWSID,
		}
		apiToken, err := iauthnzimpl.IssueAPIToken(as.AppTokens(), time.Hour, []appdef.QName{role}, childWS.WSID, payload)
		require.NoError(err)

		// API token has role.app1pkg.SpecialAPITokenRole, q.sys.Collection is allowed for that role according to grants in vsql -> the request should be successful
		vit.PostWS(childWS, "q.sys.Collection", body, httpu.WithAuthorizeBy(apiToken))
		apiPayload, _ := decodeScopedPrincipalToken(t, require, vit, apiToken)
		require.Equal([]payloads.RoleType{{
			Scope: payloads.RoleScopeWorkspace, WSID: childWS.WSID, QName: role,
		}}, apiPayload.Roles)

		systemToken := vit.GetSystemPrincipal(istructs.AppQName_test1_app1).Token
		grandchildName := vit.NextName()
		vit.InitChildWorkspace(it.SimpleWSParams(grandchildName), childWS, httpu.WithAuthorizeBy(systemToken))
		grandchildWS := vit.WaitForChildWorkspace(childWS, grandchildName, httpu.WithAuthorizeBy(systemToken))
		unrelatedWS := vit.CreateWorkspace(it.SimpleWSParams(vit.NextName()), newPrn)
		for _, tc := range []struct {
			name string
			ws   *it.AppWorkspace
		}{
			{"Role does not apply in the parent Workspace", parentWS},
			{"Role does not apply in a child Workspace", grandchildWS},
			{"Role does not apply in an unrelated Workspace", unrelatedWS},
		} {
			t.Run(tc.name, func(t *testing.T) {
				require.Equal(childWS.Kind, tc.ws.Kind)
				// A token bound to this Workspace proves the same role and query grants are usable here
				controlToken, err := iauthnzimpl.IssueAPIToken(as.AppTokens(), time.Hour, []appdef.QName{role}, tc.ws.WSID, payload)
				require.NoError(err)
				vit.PostWS(tc.ws, "q.sys.Collection", body, httpu.WithAuthorizeBy(controlToken))

				// The original signed token must remain confined to childWS despite identical ACL grants
				vit.PostWS(tc.ws, "q.sys.Collection", body, httpu.WithAuthorizeBy(apiToken), httpu.Expect403())
			})
		}
	})
}

func TestExactWorkspaceRoleDoesNotAuthorizeOwnedOrRelatedWorkspaces(t *testing.T) {
	require := require.New(t)
	vit := it.NewVIT(t, &it.SharedConfig_App1)
	defer vit.TearDown()

	caller := vit.SignIn(vit.SignUp(vit.NextName(), "1", istructs.AppQName_test1_app1))
	ownedWS := vit.CreateWorkspace(it.SimpleWSParams(vit.NextName()), caller)
	foreignWS := vit.WS(istructs.AppQName_test1_app1, "test_ws")
	otherWSOfForeignOwner := vit.WS(istructs.AppQName_test1_app1, "test_ws3")
	require.NotEqual(caller.ProfileWSID, foreignWS.Owner.ProfileWSID)

	as, err := vit.IAppStructsProvider.BuiltIn(istructs.AppQName_test1_app1)
	require.NoError(err)
	issueToken := func(wsid istructs.WSID) string {
		t.Helper()
		// Use a role available in both profile and application Workspaces without changing their schemas
		token, err := as.AppTokens().IssueToken(time.Hour, &payloads.PrincipalPayload{
			Login: caller.Name, SubjectKind: istructs.SubjectKind_User, ProfileWSID: caller.ProfileWSID,
			IsAPIToken: true,
			Roles:      []payloads.RoleType{{Scope: payloads.RoleScopeWorkspace, WSID: wsid, QName: iauthnz.QNameRoleWorkspaceOwner}},
		})
		require.NoError(err)
		return token
	}
	token := issueToken(foreignWS.WSID)
	body := `{"args":{"Schema":"sys.ChildWorkspace"},"elements":[{"fields":["sys.ID"]}]}`

	// The caller has no ownership of this Workspace; only the token's exact grant permits the query
	vit.PostWS(foreignWS, "q.sys.Collection", body, httpu.WithAuthorizeBy(token))
	for _, tc := range []struct {
		name string
		wsid istructs.WSID
	}{
		{"Caller-owned Workspace", ownedWS.WSID},
		{"Foreign Workspace owner", foreignWS.Owner.ProfileWSID},
		{"Caller profile Workspace", caller.ProfileWSID},
		{"Another Workspace of the foreign owner", otherWSOfForeignOwner.WSID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A locally scoped control token confirms that the query and role work in this Workspace
			vit.PostApp(caller.AppQName, tc.wsid, "q.sys.Collection", body, httpu.WithAuthorizeBy(issueToken(tc.wsid)))
			// Neither identity, ownership, nor a shared owner may extend the original token's grant
			vit.PostApp(caller.AppQName, tc.wsid, "q.sys.Collection", body, httpu.WithAuthorizeBy(token), httpu.Expect403())
		})
	}
}

func decodeScopedPrincipalToken(t *testing.T, require *require.Assertions, vit *it.VIT, token string) (payloads.PrincipalPayload, istructs.GenericPayload) {
	t.Helper()
	var payload payloads.PrincipalPayload
	metadata, err := vit.ValidateToken(token, &payload)
	require.NoError(err)
	parsed, _, err := jwt.NewParser().ParseUnverified(token, jwt.MapClaims{})
	require.NoError(err)
	roles := parsed.Claims.(jwt.MapClaims)["Roles"].([]any)
	require.Len(roles, len(payload.Roles))
	for i, role := range payload.Roles {
		claims := roles[i].(map[string]any)
		require.Contains(claims, "Scope")
		require.Equal(float64(role.Scope), claims["Scope"])
	}
	return payload, metadata
}
