/*
 * Copyright (c) 2023-present unTill Software Development Group B.V.
 * @author Michael Saigachenko
 */

package main

import (
	"fmt"

	ext "github.com/voedger/voedger/pkg/exttinygo"
	"github.com/voedger/voedger/pkg/goutils/logger"
)

func main() {}

//export TestEcho
func TestEcho() {
	arg := ext.MustGetValue(ext.KeyBuilder(ext.StorageQueryContext, ext.NullEntity)).AsValue(cmdContext_Argument)
	str := arg.AsString(field_Str)

	result := ext.NewValue(ext.KeyBuilder(ext.StorageResult, ext.NullEntity))
	result.PutString(field_Res, "hello, "+str)
}

//export TestCmdEcho
func TestCmdEcho() {
	arg := ext.MustGetValue(ext.KeyBuilder(ext.StorageCommandContext, ext.NullEntity)).AsValue(cmdContext_Argument)
	str := arg.AsString(field_Str)

	result := ext.NewValue(ext.KeyBuilder(ext.StorageResult, ext.NullEntity))
	result.PutString(field_Res, "hello, "+str)
}

//export ProjectorOnEcho
func ProjectorOnEcho() {
	request := ext.KeyBuilder(ext.StorageHTTP, ext.NullEntity)
	request.PutString("Method", "POST")
	request.PutString("Url", "https://dev.untlklklkill.com")
	request.PutBool("HandleErrors", true)
	var err error
	ext.ReadValues(request, func(key ext.TKey, value ext.TValue) {
		if value.AsInt32("StatusCode") != 200 {
			logger.Error(value.AsInt32("StatusCode"))
			err = fmt.Errorf("GET /masterdata/terminals/ failed: %d: %s, %s", value.AsInt32("StatusCode"),
				value.AsString("Body"), value.AsString("Error"))
			return
		}
	})
	if err != nil {
		panic(err)
	}
}
