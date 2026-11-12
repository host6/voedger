Feature: Principal token enrichment

  Rule: Token enrichment does not require Principal.WSID

    Scenario: Enrichment preserves initial grants and binds Subject roles to the request Workspace
      Given Workspace 100 owns Workspace 200 and Workspace 200 owns Workspace 300
      And a valid regular PrincipalToken identifies User Login "alice" with ProfileWorkspace 42
      And its only RoleGrant is {Scope: RoleScopeWorkspacesOwnedBy, WSID: 100, QName: sys.WorkspaceOwner}
      And a Subject in Workspace 200 also grants sys.WorkspaceOwner to Login "alice"
      And authentication produces effective sys.AuthenticatedUser and sys.WorkspaceOwner Principals without Workspace identifiers
      When Client calls q.sys.EnrichPrincipalToken in Workspace 200 with this token
      Then the returned PrincipalToken contains exactly these RoleGrants:
        | Scope                      | WSID | QName              |
        | RoleScopeWorkspacesOwnedBy | 100  | sys.WorkspaceOwner |
        | RoleScopeWorkspace         | 200  | sys.WorkspaceOwner |
      And its canonical Login remains "alice", Subject kind remains User, and ProfileWorkspace remains 42
      And its sys.WorkspaceOwner grants authorize Workspace 200 but not Workspace 300

  Rule: Only roles from Subjects in the current Workspace create new grants

    Scenario Outline: Enrichment preserves initial grants and adds only Subject roles from the current Workspace
      Given Workspace 42 owns Workspace 200
      And a valid regular PrincipalToken for Application "test1/app1" has an issued lifetime of 17 minutes
      And its Subject kind is <subject_kind>, canonical Login is "canonical", Alias is "alias", and ProfileWorkspace is 42
      And its GlobalRoles are [test.global, test.shared]
      And it contains these initial RoleGrants:
        | Scope                      | WSID | QName               |
        | RoleScopeWorkspacesOwnedBy | 42   | test.carried        |
        | RoleScopeWorkspacesOwnedBy | 999  | test.unmatched      |
        | RoleScopeWorkspacesOwnedBy | 42   | sys.WorkspaceDevice |
        | RoleScopeWorkspace         | 200  | test.existing       |
      And Subjects in Workspace 200 grant these roles:
        | Login     | Roles                                                 |
        | canonical | test.shared, test.subject                             |
        | alias     | test.subject, test.alias, test.carried, test.existing |
      When Client calls q.sys.EnrichPrincipalToken in Workspace 200 with this token
      Then the returned PrincipalToken retains all initial RoleGrants unchanged and adds exactly these RoleGrants:
        | Scope              | WSID | QName        |
        | RoleScopeWorkspace | 200  | test.shared  |
        | RoleScopeWorkspace | 200  | test.subject |
        | RoleScopeWorkspace | 200  | test.alias   |
        | RoleScopeWorkspace | 200  | test.carried |
      And its canonical Login, Alias, Subject kind, ProfileWorkspace, GlobalRoles, and IsAPIToken flag remain unchanged
      And the test.existing RoleGrant occurs exactly once
      And the returned token belongs to Application "test1/app1" with issued lifetime DefaultPrincipalTokenExpiration

      Examples:
        | subject_kind |
        | User         |
        | Device       |

    Scenario: Enriching an API token preserves its grants without capturing Subject roles
      Given a valid API token authorizes q.sys.EnrichPrincipalToken in Workspace 200
      And a Subject in Workspace 200 grants its Login a role absent from the token
      When Client calls q.sys.EnrichPrincipalToken in Workspace 200 with this API token
      Then the returned token has exactly the same decoded PrincipalPayload as the initial token
      And the returned token has issued lifetime DefaultPrincipalTokenExpiration

  Rule: Repeated enrichment does not duplicate grants

    Scenario: Enriching the token again in the same Workspace preserves its payload
      Given a valid regular PrincipalToken has already been enriched in Workspace 200
      And its RoleGrants are distinct
      And the Subject roles used for enrichment have not changed
      When Client calls q.sys.EnrichPrincipalToken in Workspace 200 with the enriched token
      Then the returned token has exactly the same decoded PrincipalPayload as the previously enriched token

  Rule: Device roles use the request Workspace, not the Device profile

    Scenario Outline: Enrichment captures WorkspaceDevice only when it comes from a Subject
      Given a valid regular PrincipalToken identifies Device "device-1" with ProfileWorkspace 100
      And its GlobalRoles are <global_roles> and it has no RoleGrants
      And Workspace 900 owns Workspace 200
      And a Subject grants sys.WorkspaceDevice to "device-1" in Workspace 200 with result <subject_role>
      And authentication checks Device allowed to work in Workspace 200 with result <device_allowed>
      When Client calls q.sys.EnrichPrincipalToken in Workspace 200 with this token
      Then RoleGrant {Scope: RoleScopeWorkspace, WSID: 200, QName: sys.WorkspaceDevice} is added if and only if <subject_role>
      And no other RoleGrant is added
      And its GlobalRoles remain <global_roles>
      And its canonical Login remains "device-1", Subject kind remains Device, and ProfileWorkspace remains 100

      Examples:
        | global_roles          | device_allowed | subject_role |
        | [sys.WorkspaceDevice] | false          | false        |
        | []                    | true           | false        |
        | [sys.WorkspaceDevice] | false          | true         |
        | []                    | true           | true         |

  Rule: Added roles grant no access outside the request Workspace

    Scenario: A captured role authorizes its target after Subject removal without authorizing other Workspaces
      Given Workspace 100 owns Workspace 200 and Workspace 200 owns Workspace 300
      And Workspace 100 also owns Workspace 201
      And a valid regular PrincipalToken with ProfileWorkspace 42 initially contained only app1pkg.SpecialAPITokenRole with scope RoleScopeWorkspacesOwnedBy and WSID 100
      And its GlobalRoles are empty and its User has no other Subject roles in these Workspaces
      And a Subject granted sys.WorkspaceOwner to its User in Workspace 200
      And the token was enriched in Workspace 200 before that Subject was deactivated
      And app1pkg.SpecialAPITokenRole permits reading Articles but not creating them
      When Client sends these requests in order with the enriched token and without automatic retries:
        | Request | Workspace | Operation         |
        | 1       | 200       | Create an Article |
        | 2       | 100       | Create an Article |
        | 3       | 300       | Create an Article |
        | 4       | 201       | Create an Article |
        | 5       | 300       | Read Articles     |
        | 6       | 201       | Read Articles     |
      Then the responses are:
        | Request | Status        |
        | 1       | 200 OK        |
        | 2       | 403 Forbidden |
        | 3       | 403 Forbidden |
        | 4       | 403 Forbidden |
        | 5       | 403 Forbidden |
        | 6       | 200 OK        |

  Rule: Enrichment requires a bearer token

    Scenario: Enrichment refuses to issue a token for a synthetic Guest identity without a bearer token
      Given the request has a synthetic Guest identity without a bearer token
      When token enrichment is invoked for this request
      Then enrichment returns ErrInvalidToken
      And no enriched token is returned
