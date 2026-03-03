# AI Code Review Setup

Automated code review using Claude after CI checks pass.

## How It Works

1. PR is opened/updated and marked "Ready for review"
2. CI checks run (tests, lint, etc.)
3. **After CI completes**, Claude reviews the PR diff
4. Claude posts review as a PR comment

## Setup

### 1. Add Anthropic API Key

Get an API key from https://console.anthropic.com/

Add to repo secrets:
```bash
# Via GitHub CLI
gh secret set ANTHROPIC_API_KEY --body "sk-ant-..."

# Or via web UI:
# Settings → Secrets and variables → Actions → New repository secret
# Name: ANTHROPIC_API_KEY
# Value: sk-ant-...
```

### 2. Enable the Workflow

The workflow is already configured in `.github/workflows/ai-code-review.yml`.

It runs automatically on:
- PR opened
- PR synchronized (new commits)
- PR reopened
- PR marked "Ready for review"

**Skips draft PRs** — no review until you mark as ready.

## Cost Estimation

**Per PR review:**
- Input: ~5K-50K tokens (depending on diff size)
- Output: ~1K tokens (review text)
- Cost: ~$0.05-$0.30 per review (Sonnet 4.5 pricing)

**Monthly estimate:**
- 50 PRs/month × $0.15 average = ~$7.50/month

**Large diffs (>100KB) are automatically skipped** to avoid excessive costs.

## Customization

### Change Model

Edit `.github/scripts/ai-review.js`:
```javascript
model: 'claude-sonnet-4-5',  // Fast, cheaper
// OR
model: 'claude-opus-4',      // More thorough, expensive
```

### Change Review Focus

Edit the prompt in `.github/scripts/ai-review.js`:
```javascript
content: `Review this PR focusing on:
1. Security issues
2. Performance
3. Maintainability
...`
```

### Skip Certain Files

Edit `.github/workflows/ai-code-review.yml`:
```yaml
- name: Get PR diff
  run: |
    # Exclude generated files, vendor, etc.
    gh pr diff ${{ github.event.pull_request.number }} \
      -- ':!vendor/**' ':!*.gen.go' ':!dashboard/dist/**' > pr.diff
```

## Disabling

### Temporarily (per PR)

Add `[skip ai-review]` to PR title or description.

Edit workflow to check for this:
```yaml
if: |
  github.event.pull_request.draft == false &&
  !contains(github.event.pull_request.title, '[skip ai-review]')
```

### Permanently

Delete or rename `.github/workflows/ai-code-review.yml`

## Example Output

```markdown
## 🤖 AI Code Review (Claude)

**Security:** Line 327 - exec.Command with user-controlled input. The #nosec 
directive is appropriate here since chromeBinary comes from config/env vars, 
but consider validating the path exists before execution.

**Performance:** Consider caching the Chrome binary lookup result in 
findChromeBinary() to avoid repeated filesystem checks.

**Code Quality:** Well-documented! The goroutine+timer pattern for startup 
timeout is elegant and avoids the context cancellation issue.

Overall this is solid work. The fallback strategy for Chrome 145 is robust 
and backwards compatible.

---
*Automated review using Claude Sonnet 4.5*
```

## Troubleshooting

### "ANTHROPIC_API_KEY not set"

Secret not configured or workflow doesn't have access.
- Check: Settings → Secrets → ANTHROPIC_API_KEY exists
- Verify workflow has `permissions: pull-requests: write`

### "Diff too large, skipping AI review"

PR changes >100KB. Options:
1. Split into smaller PRs
2. Increase limit in workflow (line 26)
3. Review manually

### "Claude API error: rate_limit_error"

API rate limit hit. Options:
1. Wait a few minutes and re-run workflow
2. Upgrade Anthropic plan
3. Reduce review frequency (skip draft PRs, etc.)

## Alternative: OpenAI

Don't have Anthropic API? Use OpenAI instead:

Edit `.github/scripts/ai-review.js`:
```javascript
// Change API endpoint
hostname: 'api.openai.com',
path: '/v1/chat/completions',

// Change headers
headers: {
  'Authorization': `Bearer ${OPENAI_API_KEY}`,
  'Content-Type': 'application/json'
}

// Change payload
const payload = JSON.stringify({
  model: 'gpt-4-turbo',
  messages: [{ role: 'user', content: `...` }]
});
```

Update secret name in workflow: `OPENAI_API_KEY`

## Further Reading

- [Anthropic API Docs](https://docs.anthropic.com)
- [GitHub Actions Docs](https://docs.github.com/actions)
- [Definition of Done](../../DEFINITION_OF_DONE.md) — What Claude checks for
