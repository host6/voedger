# Voedger CI/CD Workflow Reference Guide

## Repository Relationships

```
voedger/voedger (Main Project)
├── .github/workflows/
│   ├── ci-pkg-cmd.yml ──────────┐
│   ├── ci-pkg-cmd_pr.yml ───────┤
│   ├── ci-pkg-storage.yml ──────┤
│   ├── ci-full.yml ─────────────┤
│   ├── ci-wf_pr.yml ────────────┤
│   ├── ctool-integration-test.yml
│   ├── cd-voedger.yml
│   ├── ci-vulncheck.yml
│   ├── merge.yml
│   ├── ci_cas.yml
│   └── ci_amazon.yml
│
└── Uses: untillpro/ci-action (Shared Infrastructure)
    ├── .github/workflows/
    │   ├── ci_reuse_go.yml
    │   ├── ci_reuse_go_pr.yml
    │   └── create_issue.yml
    ├── index.js (GitHub Action)
    └── scripts/
        ├── test_subfolders.sh
        ├── check_copyright.sh
        ├── gbash.sh
        ├── execgovuln.sh
        ├── domergepr.sh
        └── checkPR.sh
```

---

## Workflow Execution Paths

### Path 1: Push to Main
```
Push → ci-pkg-cmd.yml
  ├─ call-workflow-ci-pkg (ci_reuse_go.yml)
  │  └─ untillpro/ci-action@main
  ├─ build (set ignore_bp3)
  └─ call-workflow-cd_voeger (cd-voedger.yml)
     └─ Docker push
```

### Path 2: Pull Request
```
PR → ci-pkg-cmd_pr.yml
  ├─ call-workflow-ci-pkg (ci_reuse_go_pr.yml)
  │  └─ untillpro/ci-action@main
  └─ auto-merge-pr (merge.yml)
     └─ domergepr.sh
```

### Path 3: Storage Changes
```
PR/Push → ci-pkg-storage.yml
  ├─ determine_changes
  ├─ trigger_cas (ci_cas.yml)
  │  └─ ScyllaDB tests
  ├─ trigger_amazon (ci_amazon.yml)
  │  └─ DynamoDB tests
  └─ auto-merge-pr (merge.yml)
```

### Path 4: Daily Schedule
```
5 AM UTC → ci-full.yml
  ├─ call-workflow-ci (ci_reuse_go.yml)
  ├─ notify_failure (if failed)
  ├─ call-workflow-create-issue (if failed)
  ├─ call-workflow-vulncheck (ci-vulncheck.yml)
  └─ call-workflow-cd-voeger (cd-voedger.yml)
```

---

## Workflow Configuration Reference

### Common Inputs (ci_reuse_go.yml)
```yaml
test_folder: "pkg"              # Test directory
ignore_copyright: "path/to/file" # Ignore copyright check
ignore_bp3: "true"              # Ignore BP3 check
short_test: "true"              # Run short tests
go_race: "false"                # Enable race detector
ignore_build: "true"            # Skip build step
test_subfolders: "true"         # Test subfolders
```

### Common Secrets
```yaml
reporeading_token: ${{ secrets.REPOREADING_TOKEN }}
codecov_token: ${{ secrets.CODECOV_TOKEN }}
personal_token: ${{ secrets.PERSONAL_TOKEN }}
```

---

## Testing Stages

### Stage 1: Setup
- Checkout code
- Setup Go/Node.js
- Cache dependencies

### Stage 2: Validation
- Check copyright headers
- Reject hidden folders
- Validate go.mod

### Stage 3: Build
- go build ./...
- npm run build

### Stage 4: Testing
- go test ./... (with race detector if enabled)
- npm test
- Test subfolders

### Stage 5: Linting
- golangci-lint
- ESLint (if applicable)

### Stage 6: Security
- govulncheck
- Copyright verification

### Stage 7: Coverage
- Upload to Codecov

---

## Auto-Merge Conditions

PR auto-merges if ALL conditions met:
1. Author in developers team
2. Total changes ≤ 200 lines
3. PR body contains "Resolves #"
4. All tests passed

---

## Failure Handling

### On Test Failure
1. Create GitHub issue
2. Assign to: host6
3. Label: prty/blocker
4. Include: failure URL

### On Merge Failure
1. Log error
2. Notify author
3. Require manual merge

---

## Performance Targets

| Metric | Target | Current |
|--------|--------|---------|
| PR CI | <3 min | 3-5 min |
| Full Suite | <10 min | 15-20 min |
| Cache Hit | >70% | ~20% |
| Success Rate | >99% | 97-98% |

---

## Secrets Management

### Required Secrets
```
REPOREADING_TOKEN    - GitHub token (read private repos)
CODECOV_TOKEN        - Codecov integration
PERSONAL_TOKEN       - PR/issue operations
DOCKER_USERNAME      - Docker Hub auth
DOCKER_PASSWORD      - Docker Hub auth
AWS_ACCESS_KEY_ID    - AWS auth
AWS_SECRET_ACCESS_KEY - AWS auth
AWS_SSH_KEY          - SSH key for AWS
chargebee_token      - Payment service
chargebee_sitename   - Payment config
```

### Secret Rotation
- Quarterly review
- Annual rotation
- Immediate on compromise

---

## Monitoring & Alerts

### Key Metrics
- Build duration (target: <5 min)
- Success rate (target: >99%)
- Cache hit rate (target: >70%)
- Cost per build (target: <$0.50)

### Alert Thresholds
- Build time >15 min
- Success rate <95%
- Cost >$1000/month

---

## Troubleshooting

### Workflow Hangs
- Check job timeout (should be 30 min)
- Review resource usage
- Check for deadlocks

### Tests Fail Intermittently
- Check for race conditions
- Review test isolation
- Check external service availability

### Slow Builds
- Check cache hit rate
- Review dependency downloads
- Profile build steps

---

## Version History

| Version | Date | Changes |
|---------|------|---------|
| 1.0 | 2024 | Initial setup |
| 1.1 | 2024 | Added storage tests |
| 1.2 | 2024 | Added auto-merge |
| 1.3 | 2024 | Added daily schedule |

---

## Support & Escalation

- **Questions**: DevOps team
- **Issues**: GitHub Issues
- **Urgent**: On-call engineer

