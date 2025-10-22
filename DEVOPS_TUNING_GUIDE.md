# DevOps CI/CD Tuning Guide for Voedger Project

## Quick Reference: Workflow Triggers

| Workflow | Trigger | Duration | Purpose |
|----------|---------|----------|---------|
| ci-pkg-cmd | Push main | 5-10m | Test pkg changes |
| ci-pkg-cmd_pr | PR | 3-5m | Test PR changes |
| ci-pkg-storage | Push/PR storage | 10-15m | Storage tests |
| ci-full | Daily 5 AM | 15-20m | Full suite |
| ci-wf_pr | PR workflows | 2-3m | Workflow changes |
| ctool-integration | Manual | 30-45m | Integration tests |

---

## Critical Issues & Fixes

### Issue #1: Unsafe Script Fetching
**Problem**: Scripts fetched without version pinning
```yaml
# ❌ CURRENT (Risky)
curl -s https://raw.githubusercontent.com/untillpro/ci-action/main/scripts/check_copyright.sh | bash

# ✅ RECOMMENDED
curl -s https://raw.githubusercontent.com/untillpro/ci-action/v1.2.3/scripts/check_copyright.sh | bash
```
**Action**: Create releases in ci-action repo, update all workflows

### Issue #2: Missing Go Module Cache
**Problem**: Go modules downloaded on every run
```yaml
# ✅ ADD TO ALL GO WORKFLOWS
- uses: actions/cache@v4
  with:
    path: ~/go/pkg/mod
    key: ${{ runner.os }}-go-${{ hashFiles('**/go.sum') }}
    restore-keys: |
      ${{ runner.os }}-go-
```
**Expected Savings**: 30-40% faster builds

### Issue #3: No Concurrency Control
**Problem**: Multiple runs consume resources
```yaml
# ✅ ADD TO WORKFLOW TOP LEVEL
concurrency:
  group: ${{ github.workflow }}-${{ github.ref }}
  cancel-in-progress: true
```
**Expected Savings**: 40% cost reduction

### Issue #4: Missing Job Timeouts
**Problem**: Workflows can hang indefinitely
```yaml
# ✅ ADD TO EACH JOB
jobs:
  build:
    timeout-minutes: 30
    runs-on: ubuntu-22.04
```

### Issue #5: Complex Secret Management
**Problem**: 10+ secrets scattered across workflows
**Solution**: Use GitHub Environments
```yaml
# ✅ RECOMMENDED STRUCTURE
environment: production
secrets:
  REPOREADING_TOKEN: ${{ secrets.REPOREADING_TOKEN }}
```

---

## Performance Optimization Checklist

- [ ] Pin ci-action script versions (v1.2.3+)
- [ ] Add Go module caching to all workflows
- [ ] Implement concurrency control
- [ ] Set job timeouts (30 min default)
- [ ] Consolidate secrets to environments
- [ ] Add workflow status badges
- [ ] Monitor build times weekly
- [ ] Review failed workflows monthly

---

## Workflow Optimization Examples

### Before (ci-pkg-cmd.yml)
```yaml
jobs:
  call-workflow-ci-pkg:
    uses: untillpro/ci-action/.github/workflows/ci_reuse_go.yml@main
    # No caching, no timeout, no concurrency control
```

### After (Optimized)
```yaml
concurrency:
  group: ci-pkg-cmd-${{ github.ref }}
  cancel-in-progress: true

jobs:
  call-workflow-ci-pkg:
    timeout-minutes: 30
    uses: untillpro/ci-action/.github/workflows/ci_reuse_go.yml@v1.2.3
    with:
      # ... existing config
```

---

## Monitoring & Metrics

### Key Metrics to Track
1. **Build Duration**: Target <5 min for PR, <10 min for full
2. **Success Rate**: Target >99%
3. **Cache Hit Rate**: Target >70%
4. **Cost per Build**: Target <$0.50

### Monitoring Tools
- GitHub Actions dashboard
- Workflow run history
- Cost analysis in Settings

---

## Secrets Audit

### Current Secrets (10 total)
- REPOREADING_TOKEN (5 workflows)
- CODECOV_TOKEN (2 workflows)
- PERSONAL_TOKEN (7 workflows)
- DOCKER_USERNAME (2 workflows)
- DOCKER_PASSWORD (2 workflows)
- AWS_ACCESS_KEY_ID (1 workflow)
- AWS_SECRET_ACCESS_KEY (1 workflow)
- AWS_SSH_KEY (1 workflow)
- chargebee_token (1 workflow)
- chargebee_sitename (1 workflow)

### Recommendation
Group by environment:
- **dev**: REPOREADING_TOKEN, CODECOV_TOKEN
- **prod**: DOCKER_*, AWS_*, PERSONAL_TOKEN
- **payment**: chargebee_*

---

## Implementation Timeline

**Week 1**: Script versioning + Go caching
**Week 2**: Concurrency control + timeouts
**Week 3**: Secret consolidation
**Week 4**: Monitoring setup

---

## Cost Optimization

### Current Estimate
- 14 workflows × 20 runs/day × 8 min avg = ~1,866 min/day
- At $0.008/min = ~$15/day = ~$450/month

### With Optimizations
- 40% faster builds = 1,120 min/day
- 40% fewer runs (concurrency) = 672 min/day
- Estimated savings: ~$200/month (45% reduction)

---

## References

- [GitHub Actions Documentation](https://docs.github.com/en/actions)
- [Workflow Syntax](https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions)
- [Caching Dependencies](https://docs.github.com/en/actions/using-workflows/caching-dependencies-to-speed-up-workflows)
- [Concurrency](https://docs.github.com/en/actions/using-jobs/using-concurrency)

