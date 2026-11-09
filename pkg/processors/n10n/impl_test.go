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
	var requests []iauthnz.AuthnRequest
	var failure error
	proc := &implIN10NProc{authenticator: subscriptionAuthenticator(func(req iauthnz.AuthnRequest) (iauthnz.AuthnResult, error) {
		requests = append(requests, req)
		if failure != nil {
			return iauthnz.AuthnResult{}, failure
		}
		if req.RequestWSID == 100 {
			return iauthnz.AuthnResult{Principals: []iauthnz.Principal{{Kind: iauthnz.PrincipalKind_Role, QName: role}}}, nil
		}
		return iauthnz.AuthnResult{Principals: []iauthnz.Principal{{Kind: iauthnz.PrincipalKind_Role, QName: iauthnz.QNameRoleAuthenticatedUser}}}, nil
	})}
	wp := &n10nWorkpiece{appStructs: as, token: "same-token", host: "host", subscriptions: []subscription{{entity: entity, wsid: 100}}}
	require.NoError(proc.authnzEntities(t.Context(), wp))
	wp.subscriptions = append(wp.subscriptions, subscription{entity: entity, wsid: 200})
	err := proc.authnzEntities(t.Context(), wp)
	var sysErr coreutils.SysError
	require.ErrorAs(err, &sysErr)
	require.Equal(http.StatusForbidden, sysErr.HTTPStatus)
	require.Equal([]iauthnz.AuthnRequest{{RequestWSID: 100, Host: "host", Token: "same-token"}, {RequestWSID: 100, Host: "host", Token: "same-token"}, {RequestWSID: 200, Host: "host", Token: "same-token"}}, requests)
	failure = errors.New("Subject lookup failed")
	err = proc.authnzEntities(t.Context(), wp)
	require.ErrorAs(err, &sysErr)
	require.Equal(http.StatusUnauthorized, sysErr.HTTPStatus)
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
