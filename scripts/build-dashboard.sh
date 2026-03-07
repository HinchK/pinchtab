#!/bin/bash
# Build React dashboard and copy to internal/dashboard/dashboard/
set -e

cd "$(dirname "$0")/.."

echo "📦 Building React dashboard..."
cd dashboard

# Install deps if needed (must happen before prettier)
if [ ! -d "node_modules" ]; then
  echo "📥 Installing dependencies..."
  bun install --frozen-lockfile
fi

# Generate TypeScript types from Go structs (ensures types are in sync)
TYGO="${GOPATH:-$HOME/go}/bin/tygo"
if [ -x "$TYGO" ]; then
  echo "🔄 Generating TypeScript types..."
  "$TYGO" generate
elif command -v tygo &> /dev/null; then
  echo "🔄 Generating TypeScript types..."
  tygo generate
else
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo "⚠️  WARNING: tygo not found — skipping TypeScript type generation"
  echo "   Types in the dashboard might fall out of sync with Go structs."
  echo "   Install it with: go install github.com/gzuidhof/tygo@latest"
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
fi

# Normalize tygo output with prettier so generation doesn't dirty git
if [ -f "src/generated/types.ts" ]; then
  npx prettier --write src/generated/types.ts 2>/dev/null || true
fi

bun run build

echo "📋 Copying build to internal/dashboard/dashboard/..."
cd ..

# Clear old dashboard assets (keep favicon.png)
rm -rf internal/dashboard/dashboard/assets/
rm -f internal/dashboard/dashboard/dashboard.html

# Copy React build
cp -r dashboard/dist/* internal/dashboard/dashboard/

# Rename index.html to dashboard.html (Go expects this)
mv internal/dashboard/dashboard/index.html internal/dashboard/dashboard/dashboard.html

echo "✅ Dashboard built: internal/dashboard/dashboard/"
