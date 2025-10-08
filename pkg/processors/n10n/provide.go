/*
 * Copyright (c) 2025-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package n10n

import (
	"context"

	"github.com/voedger/voedger/pkg/iauthnz"
	"github.com/voedger/voedger/pkg/in10n"
	payloads "github.com/voedger/voedger/pkg/itokens-payloads"
	"github.com/voedger/voedger/pkg/pipeline"
)

func NewIN10NProc(vvmCtx context.Context, n10nBroker in10n.IN10nBroker, authenticator iauthnz.IAuthenticator, appTokensFactory payloads.IAppTokensFactory) IN10NProc {
	proc := &implIN10NProc{
		n10nBroker:       n10nBroker,
		authenticator:    authenticator,
		appTokensFactory: appTokensFactory,
	}
	proc.pipeline = pipeline.NewAsyncPipeline(vvmCtx, "Notifications Processor",
		pipeline.WireAsyncFunc("getSubjectLogin", proc.getSubjectLogin),
		pipeline.WireAsyncFunc("getCreateChannelParams", parseRequest),
		pipeline.WireAsyncFunc("newChannel", proc.newChannel),
		pipeline.WireAsyncFunc("initResponse", initResponse),
		pipeline.WireAsyncFunc("sendChannelIDSSEEvent", sendChannelIDSSEEvent),
		pipeline.WireAsyncFunc("subscribe", proc.subscribe),
		pipeline.WireAsyncFunc("watchChannel", proc.watchChannel),
		pipeline.WireAsyncOperator("finishResponse", &finishResponse{}),
	)
	return proc
}
