/*
 * Copyright (c) 2025-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package n10n

import (
	"context"

	"github.com/voedger/voedger/pkg/in10n"
	"github.com/voedger/voedger/pkg/pipeline"
)

func NewIN10NProc(vvmCtx context.Context, n10nBroker in10n.IN10nBroker) IN10NProc {
	n10nPipeline := pipeline.NewAsyncPipeline(vvmCtx, "Notifications Processor",
		pipeline.WireAsyncFunc("getCreateChannelParams", getCreateChannelParams),
		pipeline.WireAsyncFunc("newChannel", newChannel),
		pipeline.WireAsyncFunc("initResponse", initResponse),
		pipeline.WireAsyncFunc("sendChannelIDSSEEvent", sendChannelIDSSEEvent),
		pipeline.WireAsyncFunc("subscribe", subscribe),
		pipeline.WireAsyncFunc("watchChannel", watchChannel),
		pipeline.WireAsyncOperator("finishResponse", &finishResponse{}),
	)
	return &implIN10NProc{
		n10nBroker: n10nBroker,
		pipeline:   n10nPipeline,
	}
}
