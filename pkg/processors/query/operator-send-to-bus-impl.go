/*
 * Copyright (c) 2021-present unTill Pro, Ltd.
 */

package queryprocessor

import (
	"context"
	"time"

	"github.com/voedger/voedger/pkg/coreutils"
	"github.com/voedger/voedger/pkg/goutils/logger"
	"github.com/voedger/voedger/pkg/pipeline"
)

type SendToBusOperator struct {
	pipeline.AsyncNOOP
	elemsSender coreutils.IElementsSender
	metrics     IMetrics
	errCh       chan<- error
}

func (o *SendToBusOperator) DoAsync(_ context.Context, work pipeline.IWorkpiece) (outWork pipeline.IWorkpiece, err error) {
	begin := time.Now()
	defer func() {
		o.metrics.Increase(execSendSeconds, time.Since(begin).Seconds())
	}()
	return work, o.elemsSender.SendElement(work.(rowsWorkpiece).OutputRow().Values())
}

func (o *SendToBusOperator) OnError(_ context.Context, err error) {
	select {
	case o.errCh <- err:
	default:
		logger.Error("failed to send error from rowsProcessor to QP: " + err.Error())
	}
}
