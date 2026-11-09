/*
 * Copyright (c) 2020-present unTill Pro, Ltd.
 * @author Denis Gribanov
 */

package iauthnz

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/voedger/voedger/pkg/appdef"
)

func TestIsSystemRole(t *testing.T) {
	require := require.New(t)
	require.True(IsSystemRole(QNameRoleProfileOwner))
	require.True(IsSystemRole(QNameRoleSystem))
	require.True(IsSystemRole(QNameRoleWorkspaceAdmin))
	require.True(IsSystemRole(QNameRoleWorkspaceDevice))
	require.True(IsSystemRole(appdef.NewQName(appdef.SysPackage, "test")))
	require.True(IsSystemRole(QNameRoleEveryone))
	require.True(IsSystemRole(QNameRoleAnonymous))
	require.True(IsSystemRole(QNameRoleAuthenticatedUser))
	require.False(IsSystemRole(appdef.NewQName("test", "role")))
}

func TestRolesInheritance(t *testing.T) {
	for qName := range rolesInheritance {
		require.NotEqual(t, appdef.NullQName, QNameAncestor(qName))
	}
	require.Equal(t, appdef.NullQName, QNameAncestor(appdef.NewQName("missing", "missing")))
}

func TestIsSystemPrincipal(t *testing.T) {
	require := require.New(t)
	require.True(IsSystemPrincipal([]Principal{{Kind: PrincipalKind_Role, QName: QNameRoleSystem}}))
	require.False(IsSystemPrincipal([]Principal{{Kind: PrincipalKind_Group, QName: QNameRoleSystem}}))
	require.False(IsSystemPrincipal([]Principal{{Kind: PrincipalKind_User, Name: QNameRoleSystem.String()}}))
	require.False(IsSystemPrincipal([]Principal{{Kind: PrincipalKind_Role, QName: QNameRoleWorkspaceAdmin}}))
	require.False(IsSystemPrincipal(nil))
	require.False(IsSystemPrincipal([]Principal{}))
}
