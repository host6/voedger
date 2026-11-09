/*
 * Copyright (c) 2020-present unTill Pro, Ltd.
 * @author Denis Gribanov
 */

package iauthnz

import (
	"github.com/voedger/voedger/pkg/appdef"
	"github.com/voedger/voedger/pkg/istructs"
	payloads "github.com/voedger/voedger/pkg/itokens-payloads"
)

func IsSystemRole(role appdef.QName) bool {
	return role.Pkg() == appdef.SysPackage
}

// returns NullQName if missing
func QNameAncestor(qName appdef.QName) appdef.QName {
	return rolesInheritance[qName]
}

func IsSystemPrincipal(principals []Principal) bool {
	for _, p := range principals {
		if p.Kind == PrincipalKind_Role && p.QName == QNameRoleSystem {
			return true
		}
	}
	return false
}

func GuestPayload() payloads.PrincipalPayload {
	return payloads.PrincipalPayload{SubjectKind: istructs.SubjectKind_User, Login: istructs.SysGuestLogin, ProfileWSID: istructs.GuestWSID}
}
