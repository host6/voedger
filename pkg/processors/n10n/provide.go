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

func Provide(iN10N in10n.IN10nBroker) ServiceFactory {
	return func(n10nChannel N10NChannel) pipeline.IService {
		return pipeline.NewService(func(vvmCtx context.Context) {
			n10nPipeline := pipeline.NewSyncPipeline(vvmCtx, "Notifications Processor",
				pipeline.WireFunc("getCreateChannelParams", getCreateChannelParams),
				pipeline.WireFunc("newChannel", newChannel),
				pipeline.WireFunc("initResponse", initResponse),
				pipeline.WireFunc("sendChannelIDSSEEvent", sendChannelIDSSEEvent),
				pipeline.WireFunc("subscribe", subscribe),
				pipeline.WireFunc("watchChannel", watchChannel),
				pipeline.WireSyncOperator("finishResponse", &finishResponse{}),
			)
			defer n10nPipeline.Close()

			for vvmCtx.Err() == nil {
				select {
				case intf := <-n10nChannel:
					wp := &n10nWorkpiece{
						IN10NMessage: intf.(IN10NMessage),
						n10nBroker:   iN10N,
					}
					if err := n10nPipeline.SendSync(wp); err != nil {
						// notest: all error must be handled
						panic(err)
					}
					if wp.responseWriter != nil {
						wp.responseWriter.Close(nil)
					}
					wp.Release()
				case <-vvmCtx.Done():
					return
				}
			}
		})
	}
}
