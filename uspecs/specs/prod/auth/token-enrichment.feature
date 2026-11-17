Feature: Principal token enrichment

  Clients capture Subject roles for later requests in the same Workspace
  Enrichment and refresh preserve access already carried by the PrincipalToken
  Web-portals uses enriched tokens to administer resellers, payment locations, and their users

  Background:
    Given the Application and all Workspaces in the scenario are initialized and active
    And no role assignments or token grants exist except those stated
    And request bodies are valid and automatic retries are disabled
    And enrichment requests select EnrichedToken and use the canonical Login in args.Login when a bearer token is present

  Rule: Enrichment captures access in the request Workspace

    Scenario: Token enrichment allows to work in a foreign workspace
      Given ProfileWSID 100 ("Alice") owns workspace 200, ProfileWSID 300 owns workspace 400
      And Alice can not work in workspace 400
      And there is a Subject in workspace 200 that says that Alice is WorkspaceOwner in wsid 400
      Wnen Alice's token is enriched in WSID 200
      Then Alice can call funcs in wdid 400 allowed to WorkspaceOwner

    Scenario: Enrichment preserves existing roles of the token
      Given ProfileWSID 100 ("Alice") owns workspace 200, ProfileWSID 300 owns workspace 400
      And Alice has RoleGrant in her token {Scope: RoleScopeWorkspacesOwnedBy, WSID: 100, QName: app1pkg.SpecialAPITokenRole}
      And there is a Subject in workspace 200 that says that Alice is WorkspaceOwner in wsid 400
      When Alice's token is enriched in wsid 200
      Then Alices'enriched token contains exactly these RoleGrants:
        | Scope                      | WSID | QName                       |
        | RoleScopeWorkspacesOwnedBy | 100  | app1pkg.SpecialAPITokenRole |
        | RoleScopeWorkspace         | 400  | sys.WorkspaceOwner          |
      And the returned token preserves the original identity, GlobalRoles, token kind, and Application
      And its issued lifetime is DefaultPrincipalTokenExpiration

    Scenario: A role in an enriched token remains effective after Subject deactivation
      Given ProfileWSID 100 ("Alice") owns workspace 200, ProfileWSID 300 owns workspace 400
      And Alice can not work in workspace 400
      And there is a Subject in workspace 200 that says that Alice is WorkspaceOwner in wsid 400
      And Alice's token is enriched in WSID 200
      When the subject is deactivated
      Then Alice can call funcs in wdid 400 allowed to WorkspaceOwner

      тут продолжить


      

    Scenario: Refresh preserves captured writes and existing reads without extending access
      Given Workspace 100 owns Workspaces 200 and 400, and Workspace 200 owns Workspace 300
      And a valid regular PrincipalToken for User Login "alice" with ProfileWSID 42 has these RoleGrants:
        | Scope                      | WSID | QName                       |
        | RoleScopeWorkspacesOwnedBy | 100  | app1pkg.SpecialAPITokenRole |
        | RoleScopeWorkspace         | 200  | sys.WorkspaceOwner          |
      And enrichment in Workspace 200 captured the WorkspaceOwner grant from a now-deactivated Subject
      When Client sends these requests in order, waiting for each response:
        | Request | Workspace | Operation                            | Token     | Save token as |
        | 1       | 42        | q.sys.RefreshPrincipalToken          | enriched  | refreshed     |
        | 2       | 200       | c.sys.CUD creating an article        | refreshed |               |
        | 3       | 400       | q.sys.Collection of app1pkg.articles | refreshed |               |
        | 4       | 100       | c.sys.CUD creating an article        | refreshed |               |
        | 5       | 300       | c.sys.CUD creating an article        | refreshed |               |
        | 6       | 400       | c.sys.CUD creating an article        | refreshed |               |
        | 7       | 300       | q.sys.Collection of app1pkg.articles | refreshed |               |
      Then the responses are:
        | Request | Status        |
        | 1       | 200 OK        |
        | 2       | 200 OK        |
        | 3       | 200 OK        |
        | 4       | 403 Forbidden |
        | 5       | 403 Forbidden |
        | 6       | 403 Forbidden |
        | 7       | 403 Forbidden |
      And the refreshed token has the same decoded PrincipalPayload, Application, and issued lifetime as the enriched token

    Scenario Outline: Authorization without a Subject assignment adds no grants
      Given a valid regular User PrincipalToken is authorized in Workspace 200 only through <source>
      When Client calls q.sys.EnrichPrincipalToken in Workspace 200 with this token
      Then the response is 200 OK with the original decoded PrincipalPayload unchanged

      Examples:
        | source                                                                                               |
        | ProfileWSID 100 owning Workspace 200                                                                 |
        | ProfileWSID 200                                                                                      |
        | GlobalRoles [sys.WorkspaceOwner] with ProfileWSID 42                                                 |
        | a RoleScopeWorkspace grant for sys.WorkspaceOwner in Workspace 200 with ProfileWSID 42               |
        | a RoleScopeWorkspacesOwnedBy grant for sys.WorkspaceOwner in owner Workspace 100 with ProfileWSID 42 |
        | the System identity with ProfileWSID NullWSID                                                        |

  Rule: Web-portals retains existing child access but cannot create it by enriching in the parent

    Scenario Outline: Portal reads child data with a grant preserved by parent enrichment
      Given Application "untill/airs-bp" has <parent> Workspace 100 owning <child> Workspace 200
      And User Login "alice" is signed in with ProfileWSID 42
      And an active Subject in Workspace 100 assigns <role> to "alice"
      And the original token carries {Scope: RoleScopeWorkspacesOwnedBy, WSID: 100, QName: sys.WorkspaceOwner}
      And Workspace 200 contains a <schema> record
      When Portal sends these requests in order, waiting for each response:
        | Request | Workspace | Operation                    | Token    | Save token as |
        | 1       | 100       | q.sys.EnrichPrincipalToken   | original | enriched      |
        | 2       | 200       | q.sys.Collection of <schema> | enriched |               |
      Then the responses are:
        | Request | Status |
        | 1       | 200 OK |
        | 2       | 200 OK |
      And response 2 contains the record from Workspace 200
      And the enriched token retains the original grant and adds only {Scope: RoleScopeWorkspace, WSID: 100, QName: <role>}

      Examples:
        | parent         | child            | role                       | schema             |
        | Administration | Reseller         | air.ResellersAdmin         | air.Reseller       |
        | Reseller       | Payment location | air.UntillPaymentsReseller | air.UntillPayments |
        | Reseller       | Payment location | air.UntillPaymentsReseller | sys.Invite         |

    Scenario Outline: Parent enrichment alone does not authorize child administration
      Given Application "untill/airs-bp" has Reseller Workspace 100 owning Payment location Workspace 200
      And User Login "alice" is signed in with ProfileWSID 42 and no token grants
      And an active Subject in Workspace 100 assigns air.UntillPaymentsReseller to "alice"
      And Workspace 200 has <data>
      When Portal sends these requests in order, waiting for each response:
        | Request | Workspace | Operation                  | Token    | Save token as |
        | 1       | 100       | q.sys.EnrichPrincipalToken | original | enriched      |
        | 2       | 200       | <operation>                | enriched |               |
      Then the responses are:
        | Request | Status        |
        | 1       | 200 OK        |
        | 2       | 403 Forbidden |
      And the enriched token contains only {Scope: RoleScopeWorkspace, WSID: 100, QName: air.UntillPaymentsReseller}

      Examples:
        | data                              | operation                                            |
        | a payment location record         | q.sys.Collection of air.UntillPayments               |
        | a payment location record         | c.sys.CUD updating its time zone                     |
        | a payment location record         | c.air.UpdateUPLocationRates                          |
        | no Invitation for bob@example.com | c.sys.InitiateInvitationByEMail for bob@example.com  |
        | a sent Invitation 10              | c.sys.CancelSentInvite for Invitation 10             |
        | a joined Invitation 10            | c.sys.InitiateCancelAcceptedInvite for Invitation 10 |
        | an active WorkspaceDescriptor     | c.sys.InitiateDeactivateWorkspace                    |

    Scenario: Administration enrichment alone does not authorize changes to a Reseller user's roles
      Given Application "untill/airs-bp" has Administration Workspace 100 owning Reseller Workspace 200
      And User Login "alice" is signed in with ProfileWSID 42 and no token grants
      And an active Subject in Workspace 100 assigns air.ResellersAdmin to "alice"
      And Workspace 200 has joined Invitation 10 for "bob@example.com"
      When Portal sends these requests in order, waiting for each response:
        | Request | Workspace | Operation                                         | Token    | Save token as |
        | 1       | 100       | q.sys.EnrichPrincipalToken                        | original | enriched      |
        | 2       | 200       | c.sys.InitiateUpdateInviteRoles for Invitation 10 | enriched |               |
      Then the responses are:
        | Request | Status        |
        | 1       | 200 OK        |
        | 2       | 403 Forbidden |
      And the enriched token contains only {Scope: RoleScopeWorkspace, WSID: 100, QName: air.ResellersAdmin}

  Rule: Only Subject assignments produce new grants

    Scenario Outline: Canonical Login and Alias assignments are captured without duplicating grants
      Given Workspace 100 owns Workspace 200
      And a valid regular PrincipalToken identifies <subject_kind> Login "canonical" with Alias "alias" and ProfileWSID 42
      And its GlobalRoles are [test.global, test.shared] and its issued lifetime is 17 minutes
      And it carries these RoleGrants:
        | Scope                      | WSID | QName          |
        | RoleScopeWorkspacesOwnedBy | 100  | test.carried   |
        | RoleScopeWorkspacesOwnedBy | 999  | test.unmatched |
        | RoleScopeWorkspace         | 200  | test.existing  |
      And active Subjects in Workspace 200 assign these roles:
        | Login     | Roles                                                 |
        | canonical | sys.WorkspaceOwner, test.shared, test.subject         |
        | alias     | test.subject, test.alias, test.carried, test.existing |
      When Client sends these requests in order, waiting for each response:
        | Request | Workspace | Operation                  | Token    | Save token as |
        | 1       | 200       | q.sys.EnrichPrincipalToken | original | enriched      |
        | 2       | 200       | q.sys.EnrichPrincipalToken | enriched | repeated      |
      Then the responses are:
        | Request | Status |
        | 1       | 200 OK |
        | 2       | 200 OK |
      And both returned tokens have the same decoded PrincipalPayload
      And both tokens retain every original grant and add exactly one copy of each grant:
        | Scope              | WSID | QName              |
        | RoleScopeWorkspace | 200  | sys.WorkspaceOwner |
        | RoleScopeWorkspace | 200  | test.shared        |
        | RoleScopeWorkspace | 200  | test.subject       |
        | RoleScopeWorkspace | 200  | test.alias         |
        | RoleScopeWorkspace | 200  | test.carried       |
      And the original SubjectKind, canonical Login, Alias, ProfileWSID, GlobalRoles, token kind, and Application are unchanged
      And both issued lifetimes are DefaultPrincipalTokenExpiration

      Examples:
        | subject_kind |
        | User         |
        | Device       |

    Scenario Outline: A Device role is captured only when a Subject also assigns it
      Given a valid regular Device token identifies Login "device-1" with ProfileWSID 100
      And its GlobalRoles include sys.WorkspaceOwner to authorize enrichment in Workspace 200
      And sys.WorkspaceDevice is also supplied by <source> in Workspace 200
      And the Subject assignment of sys.WorkspaceDevice to "device-1" in Workspace 200 is <assignment>
      When Client calls q.sys.EnrichPrincipalToken in Workspace 200 with this token
      Then the response is 200 OK with <grants>
      And the original identity claims, GlobalRoles, and token kind are unchanged

      Examples:
        | source             | assignment | grants                                                                  |
        | GlobalRoles        | active     | only {Scope: RoleScopeWorkspace, WSID: 200, QName: sys.WorkspaceDevice} |
        | Device eligibility | active     | only {Scope: RoleScopeWorkspace, WSID: 200, QName: sys.WorkspaceDevice} |
        | GlobalRoles        | absent     | no RoleGrants                                                           |
        | Device eligibility | absent     | no RoleGrants                                                           |

    Scenario Outline: Enrichment does not delegate a carried role to a child Workspace
      Given Workspace 100 owns Workspace 200 and Workspace 200 owns Workspace 300
      And a valid regular User token with ProfileWSID 42 carries only this RoleGrant:
        | Scope   | WSID         | QName              |
        | <scope> | <grant_wsid> | sys.WorkspaceOwner |
      And the grant's Scope field is <scope_encoding>
      And no Subject in Workspace 200 assigns roles to this User
      When Client sends these requests in order, waiting for each response:
        | Request | Workspace | Operation                     | Token    | Save token as |
        | 1       | 200       | q.sys.EnrichPrincipalToken    | original | enriched      |
        | 2       | 300       | c.sys.CUD creating an article | enriched |               |
      Then the responses are:
        | Request | Status        |
        | 1       | 200 OK        |
        | 2       | 403 Forbidden |
      And the enriched token has the original decoded PrincipalPayload unchanged
      And its RoleGrant has an explicit Scope field

      Examples:
        | scope                      | grant_wsid | scope_encoding |
        | RoleScopeWorkspacesOwnedBy | 100        | explicit       |
        | RoleScopeWorkspacesOwnedBy | 100        | absent         |
        | RoleScopeWorkspace         | 200        | explicit       |

    Scenario Outline: API-token enrichment preserves exact access without capturing Subject roles
      Given Workspace 100 owns Workspace 200
      And a valid API token identifies Login "alice" with Alias "ally" and ProfileWSID 42
      And its GlobalRoles are [sys.WorkspaceOwner]
      And its only RoleGrant is {Scope: RoleScopeWorkspace, WSID: 100, QName: sys.WorkspaceOwner}
      And the grant's Scope field is <scope_encoding>
      And active Subjects assign test.extra to "alice" in Workspace 100 and sys.WorkspaceOwner to "alice" in Workspace 200
      When Client sends these requests in order, waiting for each response:
        | Request | Workspace | Operation                     | Token    | Save token as |
        | 1       | 100       | q.sys.EnrichPrincipalToken    | original | enriched      |
        | 2       | 100       | c.sys.CUD creating an article | enriched |               |
        | 3       | 200       | c.sys.CUD creating an article | enriched |               |
      Then the responses are:
        | Request | Status        |
        | 1       | 200 OK        |
        | 2       | 200 OK        |
        | 3       | 403 Forbidden |
      And the returned token has exactly the original decoded PrincipalPayload with explicit RoleScopeWorkspace
      And its issued lifetime is DefaultPrincipalTokenExpiration

      Examples:
        | scope_encoding |
        | explicit 1     |
        | absent         |

  Rule: Enrichment requires authorized credentials

    Scenario: A User without access cannot obtain an enriched token
      Given Workspace 100 belongs to another User
      And Client has a valid regular token for User Login "alice" with ProfileWSID 42 and no applicable roles
      When Client calls q.sys.EnrichPrincipalToken in Workspace 100 with this token
      Then the response is 403 Forbidden with no EnrichedToken

    Scenario: An anonymous request cannot obtain an enriched token
      Given no Subject assigns roles to sys.Guest in Workspace 100
      When Client calls q.sys.EnrichPrincipalToken in Workspace 100 without a bearer token and with args.Login "alice"
      Then the response is 403 Forbidden with no EnrichedToken

    Scenario Outline: Invalid bearer tokens cannot be enriched
      Given a bearer token identifies User Login "alice" with ProfileWSID 42
      And it carries a sys.WorkspaceOwner grant for Workspace 100
      And its only validation failure is <condition>
      When Client calls q.sys.EnrichPrincipalToken in Workspace 100 with this token
      Then the response is 401 Unauthorized with no EnrichedToken

      Examples:
        | condition                            |
        | an invalid signature                 |
        | an expired lifetime                  |
        | binding to another Application       |
        | an explicit null RoleGrant Scope     |
        | a string RoleGrant Scope "Workspace" |
        | a fractional RoleGrant Scope 1.5     |
        | an unknown RoleGrant Scope 99        |
