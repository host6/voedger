/*
* Copyright (c) 2022-present unTill Pro, Ltd.
* @author Maxim Geraskin
 */

package iauthnz

import (
	"context"

	"github.com/voedger/voedger/pkg/istructs"
)

// Proposed NewDefaultAuthenticator() signature
// One per VVM
type NewDefaultAuthenticatorType func() IAuthenticator

type IAuthenticator interface {
	Authenticate(requestContext context.Context, app istructs.IAppStructs, appTokens istructs.IAppTokens, req AuthnRequest) (AuthnResult, error)
}
