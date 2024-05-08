/*
 * Copyright (c) 2024-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package blob

import (
	"context"
	"time"

	"github.com/voedger/voedger/pkg/iblobstorage"
	"github.com/voedger/voedger/pkg/pipeline"
	ibus "github.com/voedger/voedger/staging/src/github.com/untillpro/airs-ibus"
)

func Provide(blobStorage iblobstorage.IBLOBStorage, bus ibus.IBus, busTimeout time.Duration, blobMaxSize BLOBMaxSizeType) ServiceFactory {
	return func(sc BLOBProcBus) pipeline.IService {
		return pipeline.NewService(func(vvmCtx context.Context) {
			for vvmCtx.Err() == nil {
				select {
				case mesIntf := <-sc:
					blobMessage := mesIntf.(BLOBMessage)
					switch blobDetails := blobMessage.BLOBDetails.(type) {
					case BLOBReadDetails:
						blobReadMessageHandler(blobMessage.BLOBBaseMessage, blobDetails, blobStorage, bus, busTimeout)
					case BLOBWriteDetailsSingle:
						blobWriteMessageHandlerSingle(blobMessage.BLOBBaseMessage, blobDetails, blobStorage, blobMessage.Header, bus, busTimeout, blobMaxSize)
					case BLOBWriteDetailsMultipart:
						blobWriteMessageHandlerMultipart(blobMessage.BLOBBaseMessage, blobStorage, blobDetails.Boundary, bus, busTimeout, blobMaxSize)
					}
				case <-vvmCtx.Done():
					return
				}
			}
		})
	}
}
