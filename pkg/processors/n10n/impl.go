/*
 * Copyright (c) 2025-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package n10n

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/voedger/voedger/pkg/appdef"
	"github.com/voedger/voedger/pkg/bus"
	"github.com/voedger/voedger/pkg/coreutils"
	"github.com/voedger/voedger/pkg/goutils/logger"
	"github.com/voedger/voedger/pkg/in10n"
	"github.com/voedger/voedger/pkg/istructs"
	payloads "github.com/voedger/voedger/pkg/itokens-payloads"
	"github.com/voedger/voedger/pkg/pipeline"
)

func (p *implIN10NProc) Handle(requestCtx context.Context, body []byte, responder bus.IResponder, token string, appQName appdef.AppQName) {
	wp := &n10nWorkpiece{
		body:       body,
		requestCtx: requestCtx,
		responder:  responder,
		token:      token,
		appQName:   appQName,
	}
	if err := p.pipeline.SendAsync(wp); err != nil {
		// notest: cannot happen, all errors are handled
		panic(err)
	}
	// if err := <-wp.doneAndErr; err != nil {
	// 	if wp.responseWriter == nil {
	// 		if _, err := initResponse(wp.requestCtx, wp); err != nil {
	// 			// notest: no error possible here
	// 			panic(err)
	// 		}
	// 	}
	// 	wp.responseWriter.Close(err)
	// }
	logger.Info("")
}

func (p *implIN10NProc) getSubjectLogin(ctx context.Context, work pipeline.IWorkpiece) (outWork pipeline.IWorkpiece, err error) {
	n10nWP := work.(*n10nWorkpiece)
	appTokens := p.appTokensFactory.New(n10nWP.appQName)
	principalPayload := payloads.PrincipalPayload{}
	_, err = appTokens.ValidateToken(n10nWP.token, &principalPayload)
	if err != nil {
		return work, coreutils.NewHTTPError(http.StatusUnauthorized, err)
	}
	n10nWP.subjectLogin = istructs.SubjectLogin(principalPayload.Login)
	return work, nil
}

func parseRequest(ctx context.Context, work pipeline.IWorkpiece) (outWork pipeline.IWorkpiece, err error) {
	n10nWP := work.(*n10nWorkpiece)
	n10nArgs := n10nArgs{}
	if err := coreutils.JSONUnmarshalDisallowUnknownFields(n10nWP.body, &n10nArgs); err != nil {
		return work, fmt.Errorf("failed to unmarshal request body: %w", err)
	}
	if n10nArgs.ExpiresInSeconds == 0 {
		n10nArgs.ExpiresInSeconds = defaultN10NExpiresInSeconds
	} else if n10nArgs.ExpiresInSeconds < 0 {
		return work, fmt.Errorf("invalid expiresIn value %d", n10nArgs.ExpiresInSeconds)
	}
	n10nWP.expiresIn = time.Duration(n10nArgs.ExpiresInSeconds) * time.Second
	if len(n10nArgs.Subscriptions) == 0 {
		return work, errors.New("no subscriptions provided")
	}
	for i, subscr := range n10nArgs.Subscriptions {
		if len(subscr.Entity) == 0 || len(subscr.WSIDNumber.String()) == 0 {
			return work, fmt.Errorf("subscriptions[%d]: entity and\\or wsid is not provided", i)
		}
		wsid, err := coreutils.ClarifyJSONWSID(subscr.WSIDNumber)
		if err != nil {
			return work, err
		}
		entity, err := appdef.ParseQName(subscr.Entity)
		if err != nil {
			return work, fmt.Errorf("subscriptions[%d]: failed to parse entity %s as a QName: %w", i, subscr.Entity, err)
		}
		n10nWP.subscriptions = append(n10nWP.subscriptions, subscription{
			entity: entity,
			wsid:   wsid,
		})
	}
	return work, nil
}

func (p *implIN10NProc) newChannel(ctx context.Context, work pipeline.IWorkpiece) (outWork pipeline.IWorkpiece, err error) {
	n10nWP := work.(*n10nWorkpiece)
	n10nWP.channelID, err = p.n10nBroker.NewChannel(n10nWP.subjectLogin, n10nWP.expiresIn)
	return work, err
}

func initResponse(ctx context.Context, work pipeline.IWorkpiece) (outWork pipeline.IWorkpiece, err error) {
	n10nWP := work.(*n10nWorkpiece)
	n10nWP.responseWriter = n10nWP.responder.InitEventStream()
	return work, nil
}

func sendChannelIDSSEEvent(ctx context.Context, work pipeline.IWorkpiece) (outWork pipeline.IWorkpiece, err error) {
	n10nWP := work.(*n10nWorkpiece)
	return work, n10nWP.responseWriter.Write(
		fmt.Sprintf("event: channelId\ndata: %s\n\n", n10nWP.channelID),
	)
}

func (p *implIN10NProc) subscribe(ctx context.Context, work pipeline.IWorkpiece) (outWork pipeline.IWorkpiece, err error) {
	n10nWP := work.(*n10nWorkpiece)
	for _, sub := range n10nWP.subscriptions {
		projectionKey := in10n.ProjectionKey{
			App:        n10nWP.appQName,
			Projection: sub.entity,
			WS:         sub.wsid,
		}
		if err = p.n10nBroker.Subscribe(n10nWP.channelID, projectionKey); err != nil {
			return work, coreutils.NewHTTPErrorf(n10nErrorToStatusCode(err), "subscribe failed: %w", err)
		}
		n10nWP.subscribedProjectionKeys = append(n10nWP.subscribedProjectionKeys, projectionKey)
	}
	return work, nil
}

func (p *implIN10NProc) watchChannel(ctx context.Context, work pipeline.IWorkpiece) (outWork pipeline.IWorkpiece, err error) {
	n10nWP := work.(*n10nWorkpiece)
	// RequestCtx tracks both http request and VVM contexts
	p.n10nBroker.WatchChannel(n10nWP.requestCtx, n10nWP.channelID, func(projection in10n.ProjectionKey, offset istructs.Offset) {
		sseMessage := fmt.Sprintf("event: %s\ndata: %d\n\n", projection.ToJSON(), offset)
		if err := n10nWP.responseWriter.Write(sseMessage); err != nil {
			// could happen if router stopped to listen for bus
			// more likely request ctx is closed
			// WatchChannel will exit in this case
			logger.Error("failed to send sse message:", sseMessage)
		}
	})
	return work, nil
}

func (rs *finishResponse) DoAsync(_ context.Context, work pipeline.IWorkpiece) (outWork pipeline.IWorkpiece, err error) {
	n10nWP := work.(*n10nWorkpiece)
	n10nWP.responseWriter.Close(n10nWP.resultErr)
	return work, nil
}

func (rs *finishResponse) OnErr(err error, work interface{}, context pipeline.IWorkpieceContext) (newErr error) {
	logger.Error(err)
	return nil
}

func (rs *finishResponse) OnError(_ context.Context, err error) {
	logger.Error(err)
	// n10nWP := work.(*n10nWorkpiece)
	// for _, subscribedKey := range n10nWP.subscribedProjectionKeys {
	// 	if err = n10nWP.n10nBroker.Unsubscribe(n10nWP.channelID, subscribedKey); err != nil {
	// 		logger.Error(fmt.Sprintf("failed to unsubscribe key %#v: %s", subscribedKey, err))
	// 	}
	// }
	// n10nWP.resultErr = coreutils.WrapSysError(err, http.StatusBadRequest)
}

func (rs *finishResponse) Flush(callback pipeline.OpFuncFlush) (err error) {
	logger.Info("")
	return nil
}

func (m *n10nWorkpiece) Release() {}
