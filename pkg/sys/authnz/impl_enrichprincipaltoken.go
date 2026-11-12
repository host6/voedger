/*
 * Copyright (c) 2020-present unTill Pro, Ltd.
 */

package authnz

import (
	"context"
	"slices"

	"github.com/voedger/voedger/pkg/istructs"
	"github.com/voedger/voedger/pkg/istructsmem"
	"github.com/voedger/voedger/pkg/itokens"
	payloads "github.com/voedger/voedger/pkg/itokens-payloads"
	"github.com/voedger/voedger/pkg/processors"
	"github.com/voedger/voedger/pkg/sys/storages"
)

// targetApp/requestWS/q.sys.EnrichPrincipalToken
// bearer token, WorkspaceOwner
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

		for _, role := range result.SubjectRoles {
			newRole := payloads.RoleType{
				Scope: payloads.RoleScopeWorkspace,
				WSID:  args.WSID,
				QName: role,
			}
			if !slices.Contains(principalPayload.Roles, newRole) {
				principalPayload.Roles = append(principalPayload.Roles, newRole)
			}
		}

		enrichedToken, err := appTokens.IssueToken(DefaultPrincipalTokenExpiration, &principalPayload)
		if err != nil {
			return err
		}
		return callback(&issuePrincipalTokenRR{principalToken: enrichedToken})
	}
}
