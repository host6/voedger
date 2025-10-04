/*
 * Copyright (c) 2025-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package n10n

import (
	"context"
	"time"

	"github.com/voedger/voedger/pkg/appdef"
	"github.com/voedger/voedger/pkg/in10n"
	"github.com/voedger/voedger/pkg/in10nmem"
	"github.com/voedger/voedger/pkg/iprocbus"
	"github.com/voedger/voedger/pkg/istructs"
	"github.com/voedger/voedger/pkg/pipeline"
)

type ISSEResponseIniter interface {
	SendChannelIDSSEMessage(mes string) (ISSEMessenger, error)
}

type ISSEMessenger interface {
	SendSSEMessage(mes string) error
}

type N10NChannel iprocbus.ServiceChannel
type ServiceFactory func(n10nChannel N10NChannel) pipeline.IService
type N10NMessage struct {
	expiresIn     time.Duration
	subscriptions []Subscription
	urlPayload    string
}
type IN10NMessage interface {
	ExpiresIn() time.Duration
	Subscriptions() []Subscription
	URLPayload() string
	RequestCtx() context.Context
	SSEResponseIniter() ISSEResponseIniter
}

type Subscription struct {
	Entity appdef.QName
	WSID   istructs.WSID
}

type n10nWorkpiece struct {
	IN10NMessage
	n10nBroker               in10n.IN10nBroker
	channelID                in10n.ChannelID
	createChannelParams      in10nmem.CreateChannelParamsType
	subscribedProjectionKeys []in10n.ProjectionKey
	vvmAndRequestCombinedCtx context.Context
	resultErr                error
	sseMessenger             ISSEMessenger
}

type finishResponse struct {
	pipeline.NOOP
}
