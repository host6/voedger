/*
 * Copyright (c) 2025-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package n10n

import (
	"context"
	"encoding/json"
	"time"

	"github.com/voedger/voedger/pkg/appdef"
	"github.com/voedger/voedger/pkg/bus"
	"github.com/voedger/voedger/pkg/in10n"
	"github.com/voedger/voedger/pkg/istructs"
	"github.com/voedger/voedger/pkg/pipeline"
)

type IN10NProc interface {
	Handle(requestCtx context.Context, body []byte, responder bus.IResponder) error
}

type implIN10NProc struct {
	n10nBroker in10n.IN10nBroker
	pipeline   pipeline.IAsyncPipeline
}

type Subscription struct {
	Entity appdef.QName
	WSID   istructs.WSID
}

type n10nWorkpiece struct {
	body                     []byte
	requestCtx               context.Context
	responder                bus.IResponder
	n10nBroker               in10n.IN10nBroker
	channelID                in10n.ChannelID
	subscriptions            []subscription
	expiresIn                time.Duration
	subscribedProjectionKeys []in10n.ProjectionKey
	resultErr                error
	responseWriter           bus.IResponseWriter
	doneCh                   chan (any)
}

type finishResponse struct {
	pipeline.NOOP
}

type n10nArgs struct {
	Subscriptions    []subscriptionJSON `json:"subscriptions"`
	ExpiresInSeconds int64              `json:"expiresIn"`
}

type subscriptionJSON struct {
	Entity     string      `json:"entity"`
	WSIDNumber json.Number `json:"wsid"`
}

type subscription struct {
	entity appdef.QName
	wsid   istructs.WSID
}
