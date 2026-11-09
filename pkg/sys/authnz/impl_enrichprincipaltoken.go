/*
 * Copyright (c) 2020-present unTill Pro, Ltd.
 */

package authnz

import (
	"context"

	"github.com/voedger/voedger/pkg/iauthnz"
	"github.com/voedger/voedger/pkg/istructs"
	"github.com/voedger/voedger/pkg/istructsmem"
	"github.com/voedger/voedger/pkg/itokens"
	payloads "github.com/voedger/voedger/pkg/itokens-payloads"
	"github.com/voedger/voedger/pkg/processors"
	"github.com/voedger/voedger/pkg/sys/storages"

	"slices"
)

type enrichPrincipalTokenRR struct {
	istructs.NullObject
	enrichedToken string
}

func (r *enrichPrincipalTokenRR) AsString(string) string {
	return r.enrichedToken
}

// targetApp/parentWS/q.sys.EnrichPrincipalToken
// basic auth, WorkspaceOwner
func provideExecQryEnrichPrincipalToken(atf payloads.IAppTokensFactory) istructsmem.ExecQueryClosure {
	return func(_ context.Context, args istructs.ExecQueryArgs, callback istructs.ExecQueryCallback) (err error) {
		appTokens := atf.New(args.State.App())

		principalToken, err := storages.GetPrincipalTokenFromState(args.State)
		if err != nil {
			return err
		}

		if principalToken == "" {
			return itokens.ErrInvalidToken
		}
		result := args.Workpiece.(processors.IProcessorWorkpiece).GetAuthnResult()
		principalPayload := result.PrincipalPayload
		principalPayload.Roles = slices.Clone(principalPayload.Roles)

		scope := payloads.RoleScopeWorkspacesOwnedBy
		if principalPayload.IsAPIToken {
			scope = payloads.RoleScopeWorkspace
		}
		principals := result.Principals
		for _, prn := range principals {
			if prn.Kind != iauthnz.PrincipalKind_Role {
				continue
			}
			newRole := payloads.RoleType{
				Scope: scope,
				WSID:  args.WSID,
				QName: prn.QName,
			}
			if !slices.Contains(principalPayload.Roles, newRole) {
				principalPayload.Roles = append(principalPayload.Roles, newRole)
			}
		}

		enrichedToken, err := appTokens.IssueToken(DefaultPrincipalTokenExpiration, &principalPayload)
		if err != nil {
			return err
		}
		return callback(&enrichPrincipalTokenRR{enrichedToken: enrichedToken})
	}
}
