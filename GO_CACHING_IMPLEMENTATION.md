# Go Module Caching Implementation Guide

## Current Status

### ✅ Already Cached (2 workflows)
- `ci_reuse_go.yml` - Lines 64-70
- `ci_reuse_go_pr.yml` - Lines 74-80

### ⚠️ Missing Cache (3 workflows)
- `ci_amazon.yml` - **NEEDS CACHING**
- `cd-voedger.yml` - **NEEDS CACHING**
- `ci-vulncheck.yml` - **NEEDS CACHING**

### ✅ Already Cached (1 workflow)
- `ci_cas.yml` - Lines 33-39

---

## How Go Module Caching Works

```yaml
- name: Cache Go - Modules
  uses: actions/cache@v4
  with:
    path: ~/go/pkg/mod              # Where Go modules are stored
    key: ${{ runner.os }}-go-${{ hashFiles('**/go.sum') }}  # Unique key based on go.sum
    restore-keys: |
      ${{ runner.os }}-go-          # Fallback key
```

**How it works:**
1. GitHub Actions hashes your `go.sum` file
2. Creates a cache key: `Linux-go-<hash>`
3. On first run: Downloads modules, saves to cache
4. On subsequent runs: Restores from cache if `go.sum` unchanged
5. If `go.sum` changes: Downloads new modules, updates cache

**Expected savings:** 30-40% faster builds (2-4 minutes saved)

---

## Implementation Steps

### Step 1: Add Cache to ci_amazon.yml

**Location:** After "Set up Go" step, before "Run Amazon DynamoDB Implementation Tests"

```yaml
    - name: Set up Go
      uses: actions/setup-go@v5
      with:
        go-version: 'stable'

    # ADD THIS BLOCK
    - name: Cache Go - Modules
      uses: actions/cache@v4
      with:
        path: ~/go/pkg/mod
        key: ${{ runner.os }}-go-${{ hashFiles('**/go.sum') }}
        restore-keys: |
          ${{ runner.os }}-go-

    - name: Run Amazon DynamoDB Implementation Tests
      working-directory: pkg/istorage/amazondb
      run: go test ./... -v -race
```

### Step 2: Add Cache to cd-voedger.yml

**Location:** After "Set up Go" step, before "Build executable"

```yaml
    - name: Set up Go
      uses: actions/setup-go@v5
      with:
        go-version: 'stable'
        cache: false

    # ADD THIS BLOCK
    - name: Cache Go - Modules
      uses: actions/cache@v4
      with:
        path: ~/go/pkg/mod
        key: ${{ runner.os }}-go-${{ hashFiles('**/go.sum') }}
        restore-keys: |
          ${{ runner.os }}-go-

    - name: Build executable
      run: |
        git config --global url."https://${{ secrets.REPOREADING_TOKEN }}:x-oauth-basic@github.com/heeus".insteadOf "https://github.com/heeus"
```

### Step 3: Add Cache to ci-vulncheck.yml

**Location:** After "Set up Go" step, before "Vulnerability management"

```yaml
    - name: Set up Go
      uses: actions/setup-go@v5
      with:
        go-version: 'stable'
        check-latest: true
        cache: false

    # ADD THIS BLOCK
    - name: Cache Go - Modules
      uses: actions/cache@v4
      with:
        path: ~/go/pkg/mod
        key: ${{ runner.os }}-go-${{ hashFiles('**/go.sum') }}
        restore-keys: |
          ${{ runner.os }}-go-

    - name: Vulnerability management
      run: |
        go install golang.org/x/vuln/cmd/govulncheck@latest
        curl -s https://raw.githubusercontent.com/untillpro/ci-action/master/scripts/execgovuln.sh | bash
```

---

## Important Notes

### ✅ DO THIS
- Place cache step **after** "Set up Go"
- Place cache step **before** any `go` commands
- Use `cache: false` in setup-go (we're doing manual caching)
- Use `**/go.sum` to find all go.sum files in repo

### ❌ DON'T DO THIS
- Don't use both `cache: true` in setup-go AND manual cache step
- Don't place cache step after go commands
- Don't change the cache key format
- Don't cache different paths

---

## Cache Key Explanation

```
${{ runner.os }}-go-${{ hashFiles('**/go.sum') }}
     ↓              ↓                    ↓
   Linux      separator          hash of go.sum
   
Example: Linux-go-a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6
```

**Why this works:**
- `runner.os`: Different OS = different cache (Linux vs Windows)
- `hashFiles('**/go.sum')`: Different dependencies = different cache
- Automatic invalidation when go.sum changes

---

## Verification Steps

After implementing, verify caching works:

1. **First run**: Should download modules (slow)
2. **Second run**: Should restore from cache (fast)
3. **Check logs**: Look for "Cache hit" or "Cache miss"

**In GitHub Actions logs, you'll see:**
```
Cache hit: true    ← Cache was used (fast)
Cache hit: false   ← Cache was missed (slow)
```

---

## Expected Performance Improvement

### Before Caching
```
Setup Go:           30 sec
Download modules:   120-180 sec  ← SLOW
Run tests:          60 sec
Total:              210-270 sec (3.5-4.5 min)
```

### After Caching (First Run)
```
Setup Go:           30 sec
Download modules:   120-180 sec
Save to cache:      30 sec
Run tests:          60 sec
Total:              240-300 sec (4-5 min)
```

### After Caching (Subsequent Runs)
```
Setup Go:           30 sec
Restore from cache: 20-30 sec  ← FAST
Run tests:          60 sec
Total:              110-120 sec (1.8-2 min)
```

**Savings: 50-60% faster on cache hits!**

---

## Troubleshooting

### Cache not working?
1. Check `go.sum` exists in repo root
2. Verify cache step is before go commands
3. Check `cache: false` in setup-go
4. Look for "Cache miss" in logs

### Cache too large?
- Go modules cache is typically 200-500 MB
- GitHub allows 5 GB per repo
- Not a concern for most projects

### Want to clear cache?
- Go to Actions → Caches
- Click "Delete" on the cache entry
- Next run will rebuild cache

---

## Files to Modify

1. `.github/workflows/ci_amazon.yml` - Add cache step
2. `.github/workflows/cd-voedger.yml` - Add cache step
3. `.github/workflows/ci-vulncheck.yml` - Add cache step

**Total changes:** 3 files, ~8 lines added per file

---

## Implementation Checklist

- [ ] Add cache to ci_amazon.yml
- [ ] Add cache to cd-voedger.yml
- [ ] Add cache to ci-vulncheck.yml
- [ ] Commit changes
- [ ] Push to main
- [ ] Monitor first 3 runs for cache hits
- [ ] Verify performance improvement
- [ ] Document in team wiki

---

## Expected Results

After implementation:
- ✅ 30-40% faster builds overall
- ✅ 50-60% faster on cache hits
- ✅ Reduced GitHub Actions minutes usage
- ✅ Lower CI/CD costs
- ✅ Better developer experience

---

## References

- [GitHub Actions Cache Documentation](https://docs.github.com/en/actions/using-workflows/caching-dependencies-to-speed-up-workflows)
- [Go Module Documentation](https://golang.org/ref/mod)
- [actions/cache@v4](https://github.com/actions/cache)

