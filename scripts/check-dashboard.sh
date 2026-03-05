#!/bin/bash
set -e

# check-dashboard.sh — Dashboard quality checks (prettier, typecheck, tests)

cd "$(dirname "$0")/.."

BOLD=$'\033[1m'
ACCENT=$'\033[38;2;251;191;36m'
INFO=$'\033[38;2;136;146;176m'
SUCCESS=$'\033[38;2;0;229;204m'
ERROR=$'\033[38;2;230;57;70m'
MUTED=$'\033[38;2;90;100;128m'
NC=$'\033[0m'

ok()   { echo -e "  ${SUCCESS}✓${NC} $1"; }
fail() { echo -e "  ${ERROR}✗${NC} $1"; [ -n "${2:-}" ] && echo -e "    ${MUTED}$2${NC}"; }
hint() { echo -e "    ${MUTED}$1${NC}"; }

section() {
  echo ""
  echo -e "${ACCENT}${BOLD}$1${NC}"
}

if [ ! -d "dashboard" ]; then
  fail "Dashboard directory not found"
  exit 1
fi

cd dashboard

# Detect package runner
RUN=""
if command -v bun &>/dev/null; then
  RUN="bun"
elif command -v npx &>/dev/null; then
  RUN="npx"
else
  fail "Neither bun nor npx found"
  exit 1
fi

# ── Prettier ─────────────────────────────────────────────────────────

section "Prettier"

if $RUN prettier --check "src/**/*.{ts,tsx,css}" 2>&1 | tail -1 | grep -q "All matched files"; then
  ok "All files formatted"
else
  fail "Files not formatted"
  hint "Run: cd dashboard && $RUN prettier --write src/"
  exit 1
fi

# ── TypeScript ───────────────────────────────────────────────────────

section "TypeScript"

if $RUN tsc --noEmit 2>&1; then
  ok "Type check passed"
else
  fail "Type errors found"
  exit 1
fi

# ── Tests ────────────────────────────────────────────────────────────

section "Tests"

if $RUN vitest run 2>&1; then
  ok "All tests passed"
else
  fail "Test failures"
  exit 1
fi

# ── Summary ──────────────────────────────────────────────────────────

section "Summary"
echo ""
echo -e "  ${SUCCESS}${BOLD}Dashboard checks passed!${NC}"
echo ""
