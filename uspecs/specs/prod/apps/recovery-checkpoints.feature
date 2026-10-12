Feature: Partition and workspace recovery from checkpoints

  VAOperator restarts a VVM without losing the next partition-log offset,
  workspace-log offset, or workspace record ID, even when a stored recovery
  checkpoint points to a valid offset or is invalid.

  Rule: Partition checkpoint

    usable checkpoint ──────────────────> resume from checkpoint ──┐
    absent or zero checkpoint ──────────> recover from FirstOffset ├─> actual PLog tail ──> later command succeeds after workspace recovery
    invalid checkpoint ─> log error ────> recover from FirstOffset ┘

    Scenario Outline: Partition recovery starts from a checkpoint
      Given the PLog contains events through offset 50
      And the stored partition checkpoint is <checkpoint>
      When VAOperator restarts the VVM and submits a command
      Then response status is "503 Service Unavailable" while partition recovery runs
      When VAOperator retries the command after partition recovery completes
      Then response status is "503 Service Unavailable" while workspace recovery runs
      When VAOperator retries the command after workspace recovery completes
      Then the command succeeds with a PLog event after offset 50
      And no invalid partition-checkpoint error is logged

      Examples:
        | checkpoint          |
        | absent              |
        | lastPLogOffset 0    |
        | lastPLogOffset 10   |
        | lastPLogOffset 50   |

    Scenario Outline: Invalid partition checkpoint falls back to the PLog beginning
      Given the PLog contains events through offset 50
      And the stored partition checkpoint is <checkpoint>
      When VAOperator restarts the VVM and submits a command
      Then response status is "503 Service Unavailable" while partition recovery runs
      When VAOperator retries the command after partition recovery completes
      Then response status is "503 Service Unavailable" while workspace recovery runs
      When VAOperator retries the command after workspace recovery completes
      Then the command succeeds with a PLog event after offset 50
      And error stage "cp.partition_recovery.checkpoint.invalid" is logged
      And error message is "stored PLog offset <checkpoint_offset> does not identify an existing event; retrying from FirstOffset"

      Examples:
        | checkpoint                                         | checkpoint_offset |
        | lastPLogOffset 1000000 beyond the PLog end         | 1000000           |
        | lastPLogOffset 10 whose first returned event is 11 | 10                |

    Scenario Outline: Empty PLog recovery starts at FirstOffset
      Given the PLog is empty
      And the stored partition checkpoint is <checkpoint>
      When VAOperator restarts the VVM and submits a command
      Then response status is "503 Service Unavailable" while partition recovery runs
      When VAOperator retries the command after partition recovery completes
      Then response status is "503 Service Unavailable" while workspace recovery runs
      When VAOperator retries the command after workspace recovery completes
      Then the command succeeds with a PLog event at FirstOffset
      And no invalid partition-checkpoint error is logged

      Examples:
        | checkpoint       |
        | absent           |
        | lastPLogOffset 0 |

    Scenario: Invalid partition checkpoint over an empty PLog
      Given the PLog is empty
      And the stored partition checkpoint is lastPLogOffset 5
      When VAOperator restarts the VVM and submits a command
      Then response status is "503 Service Unavailable" while partition recovery runs
      When VAOperator retries the command after partition recovery completes
      Then response status is "503 Service Unavailable" while workspace recovery runs
      When VAOperator retries the command after workspace recovery completes
      Then the command succeeds with a PLog event at FirstOffset
      And error stage "cp.partition_recovery.checkpoint.invalid" is logged
      And error message is "stored PLog offset 5 does not identify an existing event; retrying from FirstOffset"

  Rule: Workspace checkpoint

    workspace checkpoint ──> recover actual WLog tail ─────────────> successful retry uses following WLog offset
                          └─> recover latest allocated user record ID ──> successful retry uses following record ID
    non-ID-bearing suffix ──────────────────────────────────────────> advances the WLog offset only

    Scenario Outline: Workspace recovery starts from a checkpoint
      Given the WLog has an ID-bearing event at offset 6 with record ID 200001
      And the WLog has an ID-bearing event at offset 10 with record IDs 200002 through 200005
      And the WLog has a CUD-free event at offset 14
      And the WLog ends at offset 14
      And the stored workspace checkpoint is <checkpoint>
      And the VVM has restarted and completed partition recovery
      When VAOperator submits an insert into the unrecovered workspace
      Then response status is "503 Service Unavailable" while workspace recovery runs
      When VAOperator retries the insert after workspace recovery completes
      Then the insert succeeds with a WLog event at offset 15
      And the inserted record has ID 200006
      And no invalid workspace-checkpoint error is logged

      Examples:
        | checkpoint                |
        | absent                    |
        | wlogIDBearingOffset 0     |
        | wlogIDBearingOffset 6     |
        | wlogIDBearingOffset 10    |

    Scenario: Workspace recovery starts from an ID-bearing checkpoint at the WLog tail
      Given the WLog has allocated record IDs 200001 through 200005
      And its ID-bearing event at offset 10 is the WLog tail
      And the stored workspace checkpoint is wlogIDBearingOffset 10
      And the VVM has restarted and completed partition recovery
      When VAOperator submits an insert into the unrecovered workspace
      Then response status is "503 Service Unavailable" while workspace recovery runs
      When VAOperator retries the insert after workspace recovery completes
      Then the insert succeeds with a WLog event at offset 11
      And the inserted record has ID 200006
      And no invalid workspace-checkpoint error is logged

    Scenario Outline: Non-ID-bearing suffix events advance only the WLog offset
      Given the WLog has allocated record IDs 200001 through 200005 at offset 10
      And the WLog has <suffix_events> from offset 11 through offset 14
      And the stored workspace checkpoint is wlogIDBearingOffset 10
      And the VVM has restarted and completed partition recovery
      When VAOperator submits an insert into the unrecovered workspace
      Then response status is "503 Service Unavailable" while workspace recovery runs
      When VAOperator retries the insert after workspace recovery completes
      Then the insert succeeds with a WLog event at offset 15
      And the inserted record has ID 200006
      And no invalid workspace-checkpoint error is logged

      Examples:
        | suffix_events               |
        | CUD-free events             |
        | singleton-only events       |
        | updates of existing records |

    Scenario Outline: Workspace without user record IDs starts allocation at FirstUserRecordID
      Given the WLog contains CUD-free and singleton-only events through offset 14
      And the WLog contains no newly allocated non-singleton record ID
      And the stored workspace checkpoint is <checkpoint>
      And the VVM has restarted and completed partition recovery
      When VAOperator submits an insert into the unrecovered workspace
      Then response status is "503 Service Unavailable" while workspace recovery runs
      When VAOperator retries the insert after workspace recovery completes
      Then the insert succeeds with a WLog event at offset 15
      And the inserted record has ID 200001
      And no invalid workspace-checkpoint error is logged

      Examples:
        | checkpoint            |
        | absent                |
        | wlogIDBearingOffset 0 |

    Scenario Outline: Invalid workspace checkpoint falls back to the WLog beginning
      Given the WLog has an ID-bearing event at offset 6 with record ID 200001
      And the WLog has a non-ID-bearing event at offset 9
      And the WLog has an ID-bearing event at offset 10 with record IDs 200002 through 200005
      And the WLog has a CUD-free event at offset 12
      And the WLog has a singleton-only event at offset 13
      And the WLog has an update of an existing record at offset 14
      And the WLog ends at offset 14
      And the stored workspace checkpoint is <checkpoint>
      And the VVM has restarted and completed partition recovery
      When VAOperator submits an insert into the unrecovered workspace
      Then response status is "503 Service Unavailable" while workspace recovery runs
      When VAOperator retries the insert after workspace recovery completes
      Then the insert succeeds with a WLog event at offset 15
      And the inserted record has ID 200006
      And error stage "cp.workspace_recovery.checkpoint.invalid" is logged
      And error message is "stored WLog offset <checkpoint_offset> does not identify an ID-bearing event; retrying from FirstOffset"

      Examples:
        | checkpoint                                                   | checkpoint_offset |
        | wlogIDBearingOffset 15 beyond the WLog end                   | 15                |
        | wlogIDBearingOffset 8 whose first returned event is 9        | 8                 |
        | wlogIDBearingOffset 12 pointing to a CUD-free event          | 12                |
        | wlogIDBearingOffset 13 pointing to a singleton-only event    | 13                |
        | wlogIDBearingOffset 14 pointing to an existing-record update | 14                |

  Rule: Empty WLog

    absent or zero checkpoint ─────────────> first successful insert uses FirstOffset and FirstUserRecordID
    invalid checkpoint ─> specified error ──┘

    Scenario Outline: Empty WLog recovery starts at FirstOffset and FirstUserRecordID
      Given the WLog is empty
      And the stored workspace checkpoint is <checkpoint>
      And the VVM has restarted and completed partition recovery
      When VAOperator submits an insert into the unrecovered workspace
      Then response status is "503 Service Unavailable" while workspace recovery runs
      When VAOperator retries the insert after workspace recovery completes
      Then the insert succeeds with a WLog event at FirstOffset
      And the inserted record has ID 200001
      And no invalid workspace-checkpoint error is logged

      Examples:
        | checkpoint            |
        | absent                |
        | wlogIDBearingOffset 0 |

    Scenario: Invalid workspace checkpoint over an empty WLog
      Given the WLog is empty
      And the stored workspace checkpoint is wlogIDBearingOffset 5
      And the VVM has restarted and completed partition recovery
      When VAOperator submits an insert into the unrecovered workspace
      Then response status is "503 Service Unavailable" while workspace recovery runs
      When VAOperator retries the insert after workspace recovery completes
      Then the insert succeeds with a WLog event at FirstOffset
      And the inserted record has ID 200001
      And error stage "cp.workspace_recovery.checkpoint.invalid" is logged
      And error message is "stored WLog offset 5 does not identify an ID-bearing event; retrying from FirstOffset"

  Rule: Workspace recovery admission

    request ── duplicate ───────────────────> shares the running recovery
            ├─ recovery limit available ────> starts one recovery
            └─ recovery limit reached ──────> rejected without queueing
    retained failure ── next request ───────> reports the failure and starts one retry

    Scenario: Duplicate requests share one workspace recovery
      Given workspace 42 is unrecovered after partition recovery
      And its WLog ends at offset 14 with last user record ID 200005
      When VAOperator submits an insert into workspace 42
      Then response status is "503 Service Unavailable" while workspace recovery runs
      When VAOperator submits another insert into workspace 42 before recovery completes
      Then response status is "503 Service Unavailable" while the same recovery runs
      And error stage "cp.error" logs message "workspace 42: recovery is in progress"
      And exactly one workspace-recovery start is logged for workspace 42
      When VAOperator retries the insert after workspace recovery completes
      Then the insert succeeds with a WLog event at offset 15
      And the inserted record has ID 200006

    Scenario: Recovery limit rejects without queueing and permits a later retry
      Given the workspace recovery limit is 1
      And workspaces 42 and 43 are unrecovered after partition recovery
      And both WLogs end at offset 14 with last user record ID 200005
      When VAOperator submits an insert into workspace 42
      Then response status is "503 Service Unavailable" while workspace 42 recovery runs
      When VAOperator submits an insert into workspace 43 before workspace 42 recovery completes
      Then response status is "503 Service Unavailable" because the recovery limit is reached
      And error stage "cp.error" logs message "workspace 43: recovery concurrency limit is reached"
      And no workspace-recovery start is logged for workspace 43
      When workspace 42 recovery completes and VAOperator submits another insert into workspace 43
      Then response status is "503 Service Unavailable" while workspace 43 recovery runs
      When VAOperator retries the insert after workspace 43 recovery completes
      Then the insert succeeds with a WLog event at offset 15
      And the inserted record has ID 200006

    Scenario: Zero recovery limit rejects workspace recovery
      Given the workspace recovery limit is 0
      And workspace 42 is unrecovered after partition recovery
      When VAOperator submits an insert into workspace 42
      Then response status is "503 Service Unavailable" because the recovery limit is reached
      And error stage "cp.error" logs message "workspace 42: recovery concurrency limit is reached"
      And no workspace-recovery start is logged for workspace 42

    Scenario: Failed workspace recovery is reported and retried once
      Given workspace 42 is unrecovered after partition recovery
      And its WLog ends at offset 14 with last user record ID 200005
      And its first workspace recovery fails with "WLog read failed"
      When VAOperator submits an insert into workspace 42
      Then response status is "503 Service Unavailable" while workspace recovery runs
      When VAOperator submits another insert after that recovery fails
      Then response status is "500 Internal Server Error"
      And response error is "workspace 42: recovery failed: WLog read failed"
      And one retry of workspace 42 recovery starts
      When VAOperator submits another insert before that retry completes
      Then response status is "503 Service Unavailable" while the retry runs
      When VAOperator retries the insert after workspace recovery completes
      Then the insert succeeds with a WLog event at offset 15
      And the inserted record has ID 200006
