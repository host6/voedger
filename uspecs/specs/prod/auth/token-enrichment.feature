Feature: Principal token enrichment

  Background:
    Given Application "test1/app1" and the Workspaces in the scenario are initialized
    And protected operations require sys.WorkspaceOwner
    And request bodies are valid and requests do not retry errors
    And no GlobalRoles, carried grants, Subject assignments, or Device permissions exist except those stated

  Rule: Access to child Workspaces

    @E1 @E2
    Scenario Outline: Enrichment grants access to child Workspaces
      Given Workspace <owner_wsid> owns Workspace 100 and Workspace 100 owns Workspace 200
      And a valid regular PrincipalToken identifies User Login "alice" with Alias "ally" and ProfileWSID 42
      And sys.WorkspaceOwner in Workspace 100 comes only from <source>
      And a protected operation in Workspace 200 with this token returned 403 Forbidden
      When Client calls q.sys.EnrichPrincipalToken in Workspace 100 with this token
      Then the response is 200 OK with an enriched token that authorizes the protected operation in Workspace 200 with 200 OK
      And the enriched token contains exactly RoleGrant {Scope: RoleScopeWorkspacesOwnedBy, WSID: 100, QName: sys.WorkspaceOwner}

      Examples:
        | source                       | owner_wsid |
        | Workspace ownership          | 42         |
        | a Subject with Login "alice" | 900        |
        | a Subject with Login "ally"  | 900        |

    @E3
    Scenario Outline: Enrichment includes roles without Subject assignments
      Given Workspace 900 owns Workspace 100
      And a valid regular User token has ProfileWSID <profile_wsid>
      And <source> supplies <role> in Workspace 100
      When Client calls q.sys.EnrichPrincipalToken in Workspace 100 with this token
      Then the returned token contains exactly RoleGrant {Scope: RoleScopeWorkspacesOwnedBy, WSID: 100, QName: <role>}

      Examples:
        | source            | profile_wsid | role               |
        | GlobalRoles       | 42           | sys.WorkspaceOwner |
        | Profile ownership | 100          | sys.ProfileOwner   |
        | System derivation | NullWSID     | sys.System         |

    @E4
    Scenario Outline: An independently supplied Device role uses the request Workspace
      Given Workspace 900 owns Workspace 200
      And a valid regular PrincipalToken identifies Device "device-1" with ProfileWSID 100
      And sys.WorkspaceDevice is independently supplied only by <source> in Workspace 200
      And the token carries <carried_grants> for sys.WorkspaceDevice
      When Client calls q.sys.EnrichPrincipalToken in Workspace 200 with this token
      Then the returned token retains its original RoleGrants and adds exactly RoleGrant {Scope: RoleScopeWorkspacesOwnedBy, WSID: 200, QName: sys.WorkspaceDevice}
      And its identity, GlobalRoles, and token kind remain unchanged

      Examples:
        | source               | carried_grants                   |
        | a Subject assignment | no grants                        |
        | GlobalRoles          | no grants                        |
        | Device eligibility   | no grants                        |
        | a Subject assignment | an exact grant for Workspace 200 |

  Rule: Enrichment preserves carried grant boundaries

    @E5
    Scenario Outline: A carried role is delegated further only with an independent assignment
      Given Workspace 100 owns Workspace 200 and Workspace 200 owns Workspace 300
      And a valid regular PrincipalToken identifies User Login "alice" with ProfileWSID 42
      And its only RoleGrant is {Scope: <scope>, WSID: <grant_wsid>, QName: sys.WorkspaceOwner}
      And the Subject assignment of sys.WorkspaceOwner to "alice" in Workspace 200 is <assignment>
      When Client sends these requests in order, waiting for each response:
        | Request | Workspace | Operation                   | Token     | Save token as |
        | 1       | 300       | Protected operation         | original  |               |
        | 2       | 200       | q.sys.EnrichPrincipalToken  | original  | enriched      |
        | 3       | 200       | Protected operation         | enriched  |               |
        | 4       | 300       | Protected operation         | enriched  |               |
        | 5       | 42        | q.sys.RefreshPrincipalToken | enriched  | refreshed     |
        | 6       | 300       | Protected operation         | refreshed |               |
      Then the responses are:
        | Request | Status              |
        | 1       | 403 Forbidden       |
        | 2       | 200 OK              |
        | 3       | 200 OK              |
        | 4       | <grandchild_status> |
        | 5       | 200 OK              |
        | 6       | <grandchild_status> |
      And the enriched token retains the original grant and adds exactly RoleGrant {Scope: RoleScopeWorkspacesOwnedBy, WSID: 200, QName: sys.WorkspaceOwner} when the assignment is active, otherwise it adds no grant
      And the refreshed token has the same decoded PrincipalPayload as the enriched token

      Examples:
        | scope                      | grant_wsid | assignment | grandchild_status |
        | RoleScopeWorkspacesOwnedBy | 100        | absent     | 403 Forbidden     |
        | RoleScopeWorkspace         | 200        | absent     | 403 Forbidden     |
        | RoleScopeWorkspacesOwnedBy | 100        | active     | 200 OK            |
        | RoleScopeWorkspace         | 200        | active     | 200 OK            |

    @E6 @E8
    Scenario Outline: Foreign access keeps its boundaries through enrichment and refresh
      Given ProfileWorkspace 100 owns Workspace 200
      And ProfileWorkspace 300 owns Workspace 400 and Workspace 400 owns Workspace 500
      And the ownership branches of ProfileWorkspaces 100 and 300 are disjoint
      And a valid regular PrincipalToken identifies User Login "alice" with ProfileWSID 100
      And its authority in Workspace 400 comes only from <source>
      When Client sends these requests in order, waiting for each response:
        | Request | Workspace | Operation                   | Token      | Save token as |
        | 1       | 100       | q.sys.EnrichPrincipalToken  | original   | enriched      |
        | 2       | 400       | Protected operation         | enriched   |               |
        | 3       | 300       | Protected operation         | enriched   |               |
        | 4       | 500       | Protected operation         | enriched   |               |
        | 5       | 100       | q.sys.RefreshPrincipalToken | enriched   | refreshed     |
        | 6       | 400       | q.sys.EnrichPrincipalToken  | refreshed  | reenriched    |
        | 7       | 500       | Protected operation         | reenriched |               |
      Then the responses are:
        | Request | Status                    |
        | 1       | 200 OK                    |
        | 2       | 200 OK                    |
        | 3       | 403 Forbidden             |
        | 4       | 403 Forbidden             |
        | 5       | 200 OK                    |
        | 6       | 200 OK                    |
        | 7       | <reenriched_child_status> |
      And the enriched token retains its original grants and adds exactly {Scope: RoleScopeWorkspacesOwnedBy, WSID: 100, QName: sys.ProfileOwner}
      And the refreshed token has the same decoded PrincipalPayload as the enriched token
      And re-enrichment adds exactly RoleGrant {Scope: RoleScopeWorkspacesOwnedBy, WSID: 400, QName: sys.WorkspaceOwner} for a Subject assignment, otherwise its decoded PrincipalPayload remains unchanged
      And all returned tokens preserve the original identity claims, GlobalRoles, and token kind

      Examples:
        | source                                                       | reenriched_child_status |
        | an exact WorkspaceOwner grant for 400 in the original token  | 403 Forbidden           |
        | a WorkspaceOwner Subject assignment in 400 for Login "alice" | 200 OK                  |

    @E7
    Scenario: A Subject identity reference does not select a grant target
      Given ProfileWorkspace 100 belongs to User Login "alice" and owns Workspace 200
      And ProfileWorkspace 400 belongs to User Login "bob" outside the ownership branch of 100
      And a Subject in Workspace 100 assigns sys.WorkspaceOwner to Login "bob" with ProfileWSID 400
      And a valid regular PrincipalToken identifies User Login "alice" with ProfileWSID 100
      When Client sends these requests in order, waiting for each response:
        | Request | Workspace | Operation                  | Token    | Save token as |
        | 1       | 100       | q.sys.EnrichPrincipalToken | original | enriched      |
        | 2       | 400       | Protected operation        | enriched |               |
      Then the responses are:
        | Request | Status        |
        | 1       | 200 OK        |
        | 2       | 403 Forbidden |
      And the enriched token contains exactly RoleGrant {Scope: RoleScopeWorkspacesOwnedBy, WSID: 100, QName: sys.ProfileOwner}

  Rule: Enrichment preserves token claims and issued grants

    @E9 @E10
    Scenario Outline: Repeated enrichment preserves claims and distinct grant tuples
      Given Workspace 900 owns Workspace 200
      And a valid regular PrincipalToken for Application "test1/app1" has an issued lifetime of 17 minutes
      And its SubjectKind is <subject_kind>, canonical Login is "canonical", Alias is "alias", and ProfileWSID is 42
      And its GlobalRoles are [test.global, test.shared] and IsAPIToken is false
      And it contains these RoleGrants with the stated multiplicities:
        | Scope                      | WSID | QName          | Count |
        | RoleScopeWorkspacesOwnedBy | 900  | test.shared    | 2     |
        | RoleScopeWorkspace         | 200  | test.shared    | 1     |
        | RoleScopeWorkspace         | 300  | test.foreign   | 1     |
        | RoleScopeWorkspacesOwnedBy | 999  | test.unmatched | 1     |
        | RoleScopeWorkspace         | 200  | test.exact     | 1     |
      And the owned-by grants serialize Scope as <scope_encoding>
      And Subjects in Workspace 200 grant these roles:
        | Login     | Roles                                         |
        | canonical | sys.WorkspaceOwner, test.subject, test.shared |
        | alias     | test.subject, test.alias                      |
      And these GlobalRoles and Subject assignments are the only independent role sources in Workspace 200
      When Client sends these requests in order, waiting for each response:
        | Request | Workspace | Operation                  | Token    | Save token as |
        | 1       | 200       | q.sys.EnrichPrincipalToken | original | enriched      |
        | 2       | 200       | q.sys.EnrichPrincipalToken | enriched | repeated      |
      Then the responses are:
        | Request | Status |
        | 1       | 200 OK |
        | 2       | 200 OK |
      And both returned tokens have the same decoded PrincipalPayload
      And each returned token contains every initial grant with its original multiplicity plus exactly one copy of each of these grants:
        | Scope                      | WSID | QName              |
        | RoleScopeWorkspacesOwnedBy | 200  | test.global        |
        | RoleScopeWorkspacesOwnedBy | 200  | test.shared        |
        | RoleScopeWorkspacesOwnedBy | 200  | sys.WorkspaceOwner |
        | RoleScopeWorkspacesOwnedBy | 200  | test.subject       |
        | RoleScopeWorkspacesOwnedBy | 200  | test.alias         |
      And its canonical Login, Alias, SubjectKind, ProfileWSID, GlobalRoles, and IsAPIToken remain unchanged
      And it belongs to Application "test1/app1" with issued lifetime DefaultPrincipalTokenExpiration

      Examples:
        | subject_kind | scope_encoding  |
        | User         | explicit zero   |
        | Device       | explicit zero   |
        | User         | an absent field |
        | Device       | an absent field |

    @E11
    Scenario: An issued delegation remains valid after its Subject assignment is removed
      Given Workspace 900 owns Workspaces 100 and 400
      And Workspace 100 owns Workspace 200 and Workspace 200 owns Workspace 300
      And a regular PrincipalToken for User Login "alice" with ProfileWSID 42 was enriched in Workspace 100
      And a Subject in Workspace 100 supplied its only role sys.WorkspaceOwner during enrichment
      And that Subject has since been deactivated and the enriched token is unexpired
      When Client sends these requests in order with the enriched token, waiting for each response:
        | Request | Workspace | Operation           |
        | 1       | 200       | Protected operation |
        | 2       | 100       | Protected operation |
        | 3       | 300       | Protected operation |
        | 4       | 400       | Protected operation |
      Then the responses are:
        | Request | Status        |
        | 1       | 200 OK        |
        | 2       | 403 Forbidden |
        | 3       | 403 Forbidden |
        | 4       | 403 Forbidden |

    @E12
    Scenario Outline: API-token enrichment preserves its payload and exact access boundaries
      Given Workspace 900 owns Workspace 100 and Workspace 100 owns Workspace 200
      And a valid API token identifies User Login "alice" with Alias "ally" and ProfileWSID 42
      And its GlobalRoles are [sys.WorkspaceOwner]
      And its only RoleGrant is {Scope: RoleScopeWorkspace, WSID: 100, QName: sys.WorkspaceOwner}
      And that grant serializes Scope as <scope_encoding>
      And a Subject in Workspace 100 assigns test.extra to Login "alice" with ProfileWSID 42
      And a Subject in Workspace 200 assigns sys.WorkspaceOwner to Login "alice" with ProfileWSID 42
      When Client sends these requests in order, waiting for each response:
        | Request | Workspace | Operation                  | Token    | Save token as |
        | 1       | 100       | q.sys.EnrichPrincipalToken | original | enriched      |
        | 2       | 100       | Protected operation        | enriched |               |
        | 3       | 200       | Protected operation        | enriched |               |
      Then the responses are:
        | Request | Status        |
        | 1       | 200 OK        |
        | 2       | 200 OK        |
        | 3       | 403 Forbidden |
      And the returned token has exactly the same decoded PrincipalPayload as the original token
      And its RoleGrant retains Scope RoleScopeWorkspace and WSID 100
      And its issued lifetime is DefaultPrincipalTokenExpiration

      Examples:
        | scope_encoding  |
        | explicit one    |
        | an absent field |

  Rule: Enrichment requires valid authorized credentials

    @E13
    Scenario: Enrichment refuses to issue a token without a bearer token
      Given an enrichment execution request in Workspace 100 has a synthetic Guest identity and no bearer token
      When the enrichment executor is invoked for that request
      Then it returns ErrInvalidToken and publishes no token

    @E14 @E15
    Scenario Outline: Enrichment rejects unauthorized or invalid bearer tokens
      Given Workspace 900 owns Workspace 100
      And a bearer token identifies User Login "alice" with ProfileWSID 42
      And its condition is <condition>, with all other token validation checks passing
      And any carried RoleGrant names sys.WorkspaceOwner in Workspace 100
      When Client calls q.sys.EnrichPrincipalToken in Workspace 100 with this token
      Then the response status is <status> and no EnrichedToken is returned

      Examples:
        | condition                                 | status           |
        | valid with no applicable role             | 403 Forbidden    |
        | an invalid signature                      | 401 Unauthorized |
        | an expired lifetime                       | 401 Unauthorized |
        | an Application binding of "test1/app2"    | 401 Unauthorized |
        | a RoleGrant with an explicit null Scope   | 401 Unauthorized |
        | a RoleGrant with string Scope "Workspace" | 401 Unauthorized |
        | a RoleGrant with fractional Scope 1.5     | 401 Unauthorized |
        | a RoleGrant with unknown Scope 99         | 401 Unauthorized |
