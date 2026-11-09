/*
 * Copyright (c) 2022-present unTill Pro, Ltd.
 */

package iauthnzimpl

import (
	"context"
	"fmt"
	"slices"

	"github.com/voedger/voedger/pkg/appdef"
	"github.com/voedger/voedger/pkg/iauthnz"
	"github.com/voedger/voedger/pkg/istructs"
	payloads "github.com/voedger/voedger/pkg/itokens-payloads"
)

func (i *implIAuthenticator) Authenticate(ctx context.Context, as istructs.IAppStructs, appTokens istructs.IAppTokens, req iauthnz.AuthnRequest) (result iauthnz.AuthnResult, err error) {
	defer func() {
		if err != nil {
			result = iauthnz.AuthnResult{}
		} else if !result.Payload.IsAPIToken {
			result.Principals = append(result.Principals, iauthnz.Principal{Kind: iauthnz.PrincipalKind_Host, Name: req.Host})
		}
	}()
	addRole := func(qname appdef.QName) {
		principal := iauthnz.Principal{Kind: iauthnz.PrincipalKind_Role, QName: qname}
		if !slices.Contains(result.Principals, principal) {
			result.Principals = append(result.Principals, principal)
		}
	}
	if req.Token == "" {
		result.Payload = iauthnz.GuestPayload()
		result.Principals = append(result.Principals, iauthnz.Principal{Kind: iauthnz.PrincipalKind_User, Name: istructs.SysGuestLogin})
		addRole(iauthnz.QNameRoleAnonymous)
		roles, err := i.subjectRolesGetter(ctx, istructs.SysGuestLogin, as, req.RequestWSID)
		if err != nil {
			return result, err
		}
		for _, role := range roles {
			addRole(role)
		}
		return result, nil
	}
	pp := &result.Payload
	if _, err = appTokens.ValidateToken(req.Token, pp); err != nil {
		return result, err
	}
	addRole(iauthnz.QNameRoleAuthenticatedUser)
	if pp.IsAPIToken {
		for _, role := range pp.Roles {
			if role.Scope == payloads.RoleScopeWorkspace && role.WSID == req.RequestWSID {
				addRole(role.QName)
			}
		}
		return result, nil
	}
	for _, role := range pp.GlobalRoles {
		addRole(role)
	}
	logins := []string{pp.Login}
	if pp.Alias != "" && pp.Alias != pp.Login {
		logins = append(logins, pp.Alias)
	}
	for _, login := range logins {
		roles, err := i.subjectRolesGetter(ctx, login, as, req.RequestWSID)
		if err != nil {
			return result, err
		}
		for _, role := range roles {
			addRole(role)
		}
	}
	kind := iauthnz.PrincipalKind_User
	name := pp.Login
	switch pp.SubjectKind {
	case istructs.SubjectKind_User:
	case istructs.SubjectKind_Device:
		kind, name = iauthnz.PrincipalKind_Device, ""
	default:
		return result, fmt.Errorf("unsupported subject kind: %v", pp.SubjectKind)
	}
	if iauthnz.IsSystemPrincipal(result.Principals) {
		return result, nil
	}
	if pp.ProfileWSID == istructs.NullWSID {
		addRole(iauthnz.QNameRoleSystem)
		return result, nil
	}
	result.Principals = append(result.Principals, iauthnz.Principal{Kind: kind, Name: name})
	descriptor, err := as.Records().GetSingleton(req.RequestWSID, appdef.QNameCDocWorkspaceDescriptor)
	if err != nil {
		return result, err
	}
	if req.RequestWSID == pp.ProfileWSID {
		addRole(iauthnz.QNameRoleProfileOwner)
	} else if kind == iauthnz.PrincipalKind_User {
		if descriptor.QName() != appdef.NullQName && istructs.WSID(descriptor.AsInt64(field_OwnerWSID)) == pp.ProfileWSID {
			addRole(iauthnz.QNameRoleWorkspaceOwner)
		}
	} else {
		allowed, err := i.isDeviceAllowedFuncs[as.AppQName()](as, req.RequestWSID, pp.ProfileWSID)
		if err != nil {
			return result, err
		}
		if allowed {
			addRole(iauthnz.QNameRoleWorkspaceDevice)
		}
	}
	if descriptor.QName() != appdef.NullQName {
		owner := istructs.WSID(descriptor.AsInt64(field_OwnerWSID))
		for _, role := range pp.Roles {
			if role.Scope == payloads.RoleScopeWorkspacesOwnedBy && role.WSID == owner {
				addRole(role.QName)
			}
		}
	}
	return result, nil
}
