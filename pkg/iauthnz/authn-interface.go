/*
* Copyright (c) 2022-present unTill Pro, Ltd.
* @author Maxim Geraskin
 */

package iauthnz

import (
	"context"

	"github.com/voedger/voedger/pkg/istructs"
)

type IAuthenticator interface {
	Authenticate(requestContext context.Context, app istructs.IAppStructs, appTokens istructs.IAppTokens, req AuthnRequest) (AuthnResult, error)
}
