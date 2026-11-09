/*
 * Copyright (c) 2026-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package n10n

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/voedger/voedger/pkg/appdef"
	"github.com/voedger/voedger/pkg/appdef/builder"
	"github.com/voedger/voedger/pkg/appdef/filter"
	"github.com/voedger/voedger/pkg/bus"
	"github.com/voedger/voedger/pkg/coreutils"
	"github.com/voedger/voedger/pkg/goutils/logger"
	"github.com/voedger/voedger/pkg/iauthnz"
	"github.com/voedger/voedger/pkg/in10n"
	"github.com/voedger/voedger/pkg/istructs"
	"github.com/voedger/voedger/pkg/sys/authnz"
)

func TestN10NErrorLogging(t *testing.T) {
	logCap := logger.StartCapture(t, logger.LogLevelVerbose)

	t.Run("n10n.error", func(_ *testing.T) {
		logCap.Reset()

		projKey := in10n.ProjectionKey{
			App:        appdef.NewAppQName("test", "app"),
			Projection: appdef.NewQName("test", "View"),
			WS:         istructs.WSID(42),
		}
		wp := newN10nWP("chan-1", projKey)

		reportError(wp, errors.New("test n10n failure"))

		logCap.HasLine("stage=n10n.error", "test n10n failure",
			"channelid=chan-1", "projection=test.View",
			"vapp=test/app", "wsid=42")
	})
}

type noopResponder struct{}

func (noopResponder) StreamJSON(int) bus.IResponseWriter  { return nil }
func (noopResponder) StreamEvents() bus.IResponseWriter   { return nil }
func (noopResponder) Respond(bus.ResponseMeta, any) error { return nil }

func newN10nWP(channelID string, projKey in10n.ProjectionKey) *n10nWorkpiece {
	wp := &n10nWorkpiece{
		channelID:                in10n.ChannelID(channelID),
		requestCtx:               context.Background(),
		logCtx:                   context.Background(),
		appQName:                 projKey.App,
		responder:                noopResponder{},
		subscribedProjectionKeys: []in10n.ProjectionKey{projKey},
	}
	baseCtx := logger.WithContextAttrs(wp.logCtx, map[string]any{
		logAttr_ChannelID: channelID,
	})
	wp.logCtx = n10nProjectionLogCtx(baseCtx, projKey)
	return wp
}

func TestSubscriptionAuthorizationUsesEachWorkspace(t *testing.T) {
	require := require.New(t)
	adb := builder.New()
	adb.AddPackage("test", "test.com/test")
	wsName, descriptor := appdef.NewQName("test", "workspace"), appdef.NewQName("test", "descriptor")
	entity, role := appdef.NewQName("test", "doc"), appdef.NewQName("test", "reader")
	wsb := adb.AddWorkspace(wsName)
	wsb.AddCDoc(descriptor)
	wsb.SetDescriptor(descriptor)
	wsb.AddCDoc(entity)
	wsb.AddRole(role)
	wsb.AddRole(iauthnz.QNameRoleAuthenticatedUser)
	wsb.Grant([]appdef.OperationKind{appdef.OperationKind_Select}, filter.QNames(entity), nil, role)
	app := adb.MustBuild()
	as := &subscriptionApp{app: app, descriptor: descriptor}
	for _, tc := range []struct {
		name                string
		workspaces          []istructs.WSID
		authenticationError error
		expectedStatus      int
		expectedWorkspaces  []istructs.WSID
	}{
		{
			name:       "Subscription is allowed in the authorized Workspace",
			workspaces: []istructs.WSID{100}, expectedWorkspaces: []istructs.WSID{100},
		},
		{
			name:       "Authorization in one Workspace does not grant access to another",
			workspaces: []istructs.WSID{100, 200}, expectedWorkspaces: []istructs.WSID{100, 200},
			expectedStatus: http.StatusForbidden,
		},
		{
			name:       "Authentication failure rejects the subscription and stops further checks",
			workspaces: []istructs.WSID{100, 200}, expectedWorkspaces: []istructs.WSID{100},
			authenticationError: errors.New("Subject lookup failed"), expectedStatus: http.StatusUnauthorized,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var actualRequests []iauthnz.AuthnRequest
			proc := &implIN10NProc{authenticator: subscriptionAuthenticator(func(request iauthnz.AuthnRequest) (iauthnz.AuthnResult, error) {
				actualRequests = append(actualRequests, request)
				if tc.authenticationError != nil {
					return iauthnz.AuthnResult{}, tc.authenticationError
				}
				roleName := iauthnz.QNameRoleAuthenticatedUser
				if request.RequestWSID == 100 {
					roleName = role
				}
				return iauthnz.AuthnResult{Principals: []iauthnz.Principal{{Kind: iauthnz.PrincipalKind_Role, QName: roleName}}}, nil
			})}
			wp := &n10nWorkpiece{appStructs: as, token: "same-token", host: "host"}
			for _, wsid := range tc.workspaces {
				wp.subscriptions = append(wp.subscriptions, subscription{entity: entity, wsid: wsid})
			}
			actualError := proc.authnzEntities(t.Context(), wp)

			// Distinguish missing permissions from authentication failures in the subscription response
			if tc.expectedStatus == 0 {
				require.NoError(actualError)
			} else {
				var actualSysError coreutils.SysError
				require.ErrorAs(actualError, &actualSysError)
				require.Equal(tc.expectedStatus, actualSysError.HTTPStatus)
			}

			// Authenticate each target Workspace with the same credentials; stop after the first failure
			expectedRequests := make([]iauthnz.AuthnRequest, 0, len(tc.expectedWorkspaces))
			for _, wsid := range tc.expectedWorkspaces {
				expectedRequests = append(expectedRequests, iauthnz.AuthnRequest{RequestWSID: wsid, Host: "host", Token: "same-token"})
			}
			require.Equal(expectedRequests, actualRequests)
		})
	}
}

type subscriptionAuthenticator func(iauthnz.AuthnRequest) (iauthnz.AuthnResult, error)

func (f subscriptionAuthenticator) Authenticate(_ context.Context, _ istructs.IAppStructs, _ istructs.IAppTokens, req iauthnz.AuthnRequest) (iauthnz.AuthnResult, error) {
	return f(req)
}

type subscriptionApp struct {
	istructs.IAppStructs
	app        appdef.IAppDef
	descriptor appdef.QName
}

func (a *subscriptionApp) AppDef() appdef.IAppDef { return a.app }
func (a *subscriptionApp) Records() istructs.IRecords {
	return &subscriptionRecords{descriptor: a.descriptor}
}

type subscriptionRecords struct {
	istructs.IRecords
	descriptor appdef.QName
}

func (r *subscriptionRecords) GetSingleton(istructs.WSID, appdef.QName) (istructs.IRecord, error) {
	return &subscriptionDescriptor{descriptor: r.descriptor}, nil
}

type subscriptionDescriptor struct {
	istructs.IRecord
	descriptor appdef.QName
}

func (r *subscriptionDescriptor) QName() appdef.QName { return appdef.QNameCDocWorkspaceDescriptor }
func (r *subscriptionDescriptor) AsQName(field string) appdef.QName {
	if field == authnz.Field_WSKind {
		return r.descriptor
	}
	return appdef.NullQName
}
