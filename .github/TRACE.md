# GitHub Workflow Execution Trace

## EVENT: Push to main branch (excluding pkg/istorage)

**Voedger Workflow:** [`.github/workflows/ci-pkg-cmd.yml`](.github/workflows/ci-pkg-cmd.yml#L3-L9)

- Triggered on: `push` to `main` branch, paths-ignore: `pkg/istorage/**`
- Condition: `github.repository == 'voedger/voedger'`

- [**Step 1: Call CI Reuse Go Workflow**](.github/workflows/ci-pkg-cmd.yml#L11-L25): Calls `untillpro/ci-action/.github/workflows/ci_reuse_go.yml@main` with `test_folder: pkg`, `ignore_copyright`, `short_test: true`, `go_race: false`, `ignore_build: true`, `test_subfolders: true`

- [**Step 2: Set Ignore Build BP3**](.github/workflows/ci-pkg-cmd.yml#L26-L41): If `github.repository == 'voedger/voedger'`: `ignore_bp3=false`, else `ignore_bp3=true`

- [**Step 3: Build & Push Docker**](.github/workflows/ci-pkg-cmd.yml#L43-L51): Calls `voedger/voedger/.github/workflows/cd-voedger.yml@main` (condition: `github.repository == 'voedger/voedger'`)

---

## EVENT: Pull Request to pkg-cmd (excluding pkg/istorage)

**Voedger Workflow:** [`.github/workflows/ci-pkg-cmd_pr.yml`](.github/workflows/ci-pkg-cmd_pr.yml#L3-L6)

- Triggered on: `pull_request_target`, paths-ignore: `pkg/istorage/**`
- Condition: `github.repository == 'voedger/voedger'`

- [**Step 1: Call CI Reuse Go PR Workflow**](.github/workflows/ci-pkg-cmd_pr.yml#L9-L24): Calls `untillpro/ci-action/.github/workflows/ci_reuse_go_pr.yml@main` with `test_folder: pkg`, `ignore_copyright`, `short_test: true`, `go_race: false`, `ignore_build: true`, `test_subfolders: true`

- [**Step 2: Auto-merge PR**](.github/workflows/ci-pkg-cmd_pr.yml#L25-L29): Calls `./.github/workflows/merge.yml`
  - [**Merge PR**](https://github.com/voedger/voedger/blob/main/.github/workflows/merge.yml#L15-L22): Run `domergepr.sh` script from ci-action with PR number and branch name

---

## EVENT: Pull Request to .github/workflows

**Voedger Workflow:** [`.github/workflows/ci-wf_pr.yml`](.github/workflows/ci-wf_pr.yml#L3-L6)

- Triggered on: `pull_request_target`, paths: `.github/workflows/**`

- [**Step 1: Auto-merge PR**](.github/workflows/ci-wf_pr.yml#L9-L12): Calls `voedger/voedger/.github/workflows/merge.yml@main`
  - [**Merge PR**](https://github.com/voedger/voedger/blob/main/.github/workflows/merge.yml#L15-L22): Run `domergepr.sh` script from ci-action with PR number and branch name

---

## EVENT: Daily test suite (scheduled or manual)

**Voedger Workflow:** [`.github/workflows/ci-full.yml`](.github/workflows/ci-full.yml#L3-L6)

- Triggered on: `workflow_dispatch` or `schedule: cron "0 5 * * *"` (daily at 5 AM UTC)
- Condition: `github.repository == 'voedger/voedger'`

- [**Step 1: Call CI Reuse Go Workflow**](.github/workflows/ci-full.yml#L9-L21): Calls `untillpro/ci-action/.github/workflows/ci_reuse_go.yml@main` with `ignore_copyright`, `go_race: true`, `short_test: false`, `ignore_build: true`, `test_subfolders: true`

- [**Step 2: Notify Failure (if failed)**](.github/workflows/ci-full.yml#L23-L32): Condition `failure()` - Sets output `failure_url` with workflow run URL

- [**Step 3: Create Issue (if failed)**](.github/workflows/ci-full.yml#L34-L45): Condition `failure()` - Calls `untillpro/ci-action/.github/workflows/create_issue.yml@main` to create issue "Daily Test failed on" with label "prty/blocker"

- [**Step 4: Vulnerability Check**](.github/workflows/ci-full.yml#L47-L49): Calls `voedger/voedger/.github/workflows/ci-vulncheck.yml@main`
  - [**Set up Go**](https://github.com/voedger/voedger/blob/main/.github/workflows/ci-vulncheck.yml#L11-L16): Go stable version, cache disabled
  - [**Checkout**](https://github.com/voedger/voedger/blob/main/.github/workflows/ci-vulncheck.yml#L18-L19): Checkout code
  - [**Vulnerability management**](https://github.com/voedger/voedger/blob/main/.github/workflows/ci-vulncheck.yml#L21-L24): Install `govulncheck@latest`, run `execgovuln.sh` script

- [**Step 5: Build & Push Docker**](.github/workflows/ci-full.yml#L50-L58): Calls `voedger/voedger/.github/workflows/cd-voedger.yml@main`
  - [**Checkout**](https://github.com/voedger/voedger/blob/main/.github/workflows/cd-voedger.yml#L21-L22): Checkout code
  - [**Set up Go**](https://github.com/voedger/voedger/blob/main/.github/workflows/cd-voedger.yml#L24-L28): Go stable version, cache disabled
  - [**Build executable**](https://github.com/voedger/voedger/blob/main/.github/workflows/cd-voedger.yml#L30-L38): Configure git for private repos (heeus, untillpro, voedger), `go build -o ./cmd/voedger ./cmd/voedger`
  - [**Log in to Docker Hub**](https://github.com/voedger/voedger/blob/main/.github/workflows/cd-voedger.yml#L40-L44): Authenticate with Docker Hub credentials
  - [**Build and push Docker image**](https://github.com/voedger/voedger/blob/main/.github/workflows/cd-voedger.yml#L46-L52): Build from `./cmd/voedger/Dockerfile`, push as `voedger/voedger:0.0.1-alpha`

---

## EVENT: Issue opened with title starting with cprc or cprelease

**Voedger Workflow:** [`.github/workflows/cp.yml`](.github/workflows/cp.yml#L3-L5)

- Triggered on: `issues` type `opened`
- Condition: Issue title starts with `cprc` or `cprelease`

- [**Step 1: Cherry Pick Commits**](.github/workflows/cp.yml#L8-L25): Calls `untillpro/ci-action/.github/workflows/cp.yml@main` with `org: voedger`, `repo: voedger`, `team: DevOps_cp`, `user`, `issue`, `issue-title`, `issue-body`

---

## CI-ACTION Reusable Workflows

When ci-action workflows are called, they execute the following:

### ci_reuse_go.yml (Full test suite)

**CI-Action Workflow:** `untillpro/ci-action/.github/workflows/ci_reuse_go.yml@main`

- [**Checkout**](https://github.com/untillpro/ci-action/blob/main/.github/workflows/ci_reuse_go.yml#L42-L43): Checkout code
- [**Set up Go**](https://github.com/untillpro/ci-action/blob/main/.github/workflows/ci_reuse_go.yml#L45-L49): Go version 1.24, cache disabled
- [**Install TinyGo**](https://github.com/untillpro/ci-action/blob/main/.github/workflows/ci_reuse_go.yml#L52-L55): Download and install TinyGo v0.37.0
- [**Check PR file size**](https://github.com/untillpro/ci-action/blob/main/.github/workflows/ci_reuse_go.yml#L57-L62): If pull_request event, run `checkPR.sh`
- [**Cache Go Modules**](https://github.com/untillpro/ci-action/blob/main/.github/workflows/ci_reuse_go.yml#L64-L70): Cache `~/go/pkg/mod` based on `go.sum`
- [**CI: Run ci-action with parameters**](https://github.com/untillpro/ci-action/blob/main/.github/workflows/ci_reuse_go.yml#L72-L83) → See [Phase 1-4 substeps below](#phase-1-initialization--context)
- [**Test subfolders**](https://github.com/untillpro/ci-action/blob/main/.github/workflows/ci_reuse_go.yml#L85-L92): If `test_subfolders == 'true'`, run `test_subfolders.sh` or `test_subfolders_full.sh` based on `short_test`
- [**Check copyright**](https://github.com/untillpro/ci-action/blob/main/.github/workflows/ci_reuse_go.yml#L94-L95): Run `check_copyright.sh`
- [**Linters**](https://github.com/untillpro/ci-action/blob/main/.github/workflows/ci_reuse_go.yml#L97-L100): Run `gbash.sh` for linting

---

### ci_reuse_go_pr.yml (PR test suite)

**CI-Action Workflow:** `untillpro/ci-action/.github/workflows/ci_reuse_go_pr.yml@main`

- [**Set up Go**](https://github.com/untillpro/ci-action/blob/main/.github/workflows/ci_reuse_go_pr.yml#L42-L46): Go version 1.24, cache disabled
- [**Install TinyGo**](https://github.com/untillpro/ci-action/blob/main/.github/workflows/ci_reuse_go_pr.yml#L49-L52): Download and install TinyGo v0.37.0
- [**Checkout**](https://github.com/untillpro/ci-action/blob/main/.github/workflows/ci_reuse_go_pr.yml#L54-L58): Checkout PR head commit with full history
- [**Check PR file size**](https://github.com/untillpro/ci-action/blob/main/.github/workflows/ci_reuse_go_pr.yml#L60-L64): Run `checkPR.sh` to validate PR file size
- [**Cancel other workflows**](https://github.com/untillpro/ci-action/blob/main/.github/workflows/ci_reuse_go_pr.yml#L66-L72): Run `cancelworkflow.sh` to cancel other running workflows on same branch
- [**Cache Go Modules**](https://github.com/untillpro/ci-action/blob/main/.github/workflows/ci_reuse_go_pr.yml#L74-L80): Cache `~/go/pkg/mod` based on `go.sum`
- [**CI: Run ci-action with parameters**](https://github.com/untillpro/ci-action/blob/main/.github/workflows/ci_reuse_go_pr.yml#L82-L93) → See [Phase 1-4 substeps below](#phase-1-initialization--context)
- [**Test subfolders**](https://github.com/untillpro/ci-action/blob/main/.github/workflows/ci_reuse_go_pr.yml#L95-L98): If `test_subfolders == 'true'`, run `test_subfolders.sh`
- [**Check copyright**](https://github.com/untillpro/ci-action/blob/main/.github/workflows/ci_reuse_go_pr.yml#L100-L101): Run `check_copyright.sh`
- [**Linters**](https://github.com/untillpro/ci-action/blob/main/.github/workflows/ci_reuse_go_pr.yml#L103-L106): Run `gbash.sh` for linting

---

## CI-ACTION JavaScript Execution Details

When ci-action workflows execute, they run the following JavaScript logic:

### [Phase 1: Initialization & Context](https://github.com/untillpro/ci-action/blob/main/index.js#L15-L69)

- [**Parse Inputs**](https://github.com/untillpro/ci-action/blob/main/index.js#L15-L37): Extract all configuration parameters
- [**Print Context**](https://github.com/untillpro/ci-action/blob/main/index.js#L57-L69): Log repository, organization, actor, event info

### [Phase 2: Source Code Validation](https://github.com/untillpro/ci-action/blob/main/index.js#L71-L76)

- [**Reject Hidden Folders**](https://github.com/untillpro/ci-action/blob/main/index.js#L72): Validates no unexpected hidden folders (except `.git`, `.github`, `.husky`, `.augment`)
- [**Check Source Files**](https://github.com/untillpro/ci-action/blob/main/index.js#L76): Validates all source files have Copyright in first comment (unless ignored), no LICENSE word if file missing, skips "DO NOT EDIT" files

### [Phase 3: Language Detection & Build](https://github.com/untillpro/ci-action/blob/main/index.js#L78-L147)

- [**Detect Language**](https://github.com/untillpro/ci-action/blob/main/index.js#L78): Checks for `go.mod` → Go project, scans for `.go` files → Go, scans for `.js`/`.ts` → Node.js

#### [IF Go Project](https://github.com/untillpro/ci-action/blob/main/index.js#L79-L147)

- [**Setup Go Environment**](https://github.com/untillpro/ci-action/blob/main/index.js#L88-L93): Configure GOPRIVATE and git credentials for private repositories
- [**go mod tidy**](https://github.com/untillpro/ci-action/blob/main/index.js#L99-L101): If `run-mod-tidy !== "false"`
- [**Build**](https://github.com/untillpro/ci-action/blob/main/index.js#L103-L105): `go build ./...` if `ignore-build !== "true"`
- [**Tests & Coverage**](https://github.com/untillpro/ci-action/blob/main/index.js#L107-L138):
  - **If Codecov Token**: `go test` with coverage flags, upload to Codecov
  - **If No Token**: `go test` with optional `-race` and `-short` flags
- [**Custom Build Command**](https://github.com/untillpro/ci-action/blob/main/index.js#L142-L144): Execute if provided

#### [IF Node.js Project](https://github.com/untillpro/ci-action/blob/main/index.js#L149-L170)

- [**npm install**](https://github.com/untillpro/ci-action/blob/main/index.js#L155)
- [**npm run build**](https://github.com/untillpro/ci-action/blob/main/index.js#L156)
- [**npm test**](https://github.com/untillpro/ci-action/blob/main/index.js#L157)
- [**Codecov**](https://github.com/untillpro/ci-action/blob/main/index.js#L160-L165): If token provided, run coverage and upload

### [Phase 4: Publish Release](https://github.com/untillpro/ci-action/blob/main/index.js#L174-L186)

**Condition:** `branchName === mainBranch && publishAsset`

- [**Validate Asset & deployer.url**](https://github.com/untillpro/ci-action/blob/main/publish.js#L53-L57)
- [**Generate Version**](https://github.com/untillpro/ci-action/blob/main/publish.js#L59): Format `yyyyMMdd.HHmmss.SSS` (UTC timestamp)
- [**Prepare Zip**](https://github.com/untillpro/ci-action/blob/main/publish.js#L60): Create zip if directory or not .zip
- [**Create Release & Tag**](https://github.com/untillpro/ci-action/blob/main/publish.js#L64-L70): GitHub API call with version tag
- [**Upload Asset**](https://github.com/untillpro/ci-action/blob/main/publish.js#L85-L90): Upload zipped asset as `${repositoryName}.zip`
- [**Upload deploy.txt**](https://github.com/untillpro/ci-action/blob/main/publish.js#L104-L109): Combine asset URL + deployer.url content
- [**Delete Old Releases**](https://github.com/untillpro/ci-action/blob/main/publish.js#L121-L139): Keep only `publishKeep` releases matching pattern `^\d{8}\.\d{6}\.\d{3}$`
- [**Set Outputs**](https://github.com/untillpro/ci-action/blob/main/index.js#L184-L186): `release_id`, `release_name`, `release_html_url`, `release_upload_url`, `asset_browser_download_url`

---

## External Scripts

Called from `https://raw.githubusercontent.com/untillpro/ci-action/main/scripts/`:

- `add-issue-commit.sh` - Add comment to GitHub issue
- `cp.sh` - Cherry pick commits to target branch
- `close-issue.sh` - Close GitHub issue
- `rc.sh` - Create release candidate branch
- `createissue.sh` - Create new GitHub issue
- `domergepr.sh` - Auto-merge pull request
