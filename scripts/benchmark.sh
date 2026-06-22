#!/usr/bin/env bash
set -euo pipefail

# Benchmark: compare Go binary vs Python reference across all recent sessions.
# Usage: ./scripts/benchmark.sh [days] [min_size_kb]
#   days:        how far back to look (default: 7)
#   min_size_kb: minimum transcript size in KB (default: 10)

DAYS="${1:-7}"
MIN_SIZE="${2:-10}"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
GO_BIN="$ROOT_DIR/bin/cc-session"
PY_BIN="$SCRIPT_DIR/sessions.py"
CLAUDE_PROJECTS="$HOME/.claude/projects"

if [ ! -x "$GO_BIN" ]; then
    echo "Go binary not found at $GO_BIN — run 'go build -o bin/cc-session ./cmd/cc-session/' first"
    exit 1
fi

# Collect session IDs (exclude agent transcripts)
SESSIONS=$(find "$CLAUDE_PROJECTS" -name "*.jsonl" -mtime "-${DAYS}" -size "+${MIN_SIZE}k" \
    -exec basename {} .jsonl \; | grep -v "^agent-" | sort -u)

SESSION_COUNT=$(echo "$SESSIONS" | grep -c . || true)
echo "=== Benchmark: $SESSION_COUNT sessions (last ${DAYS}d, >${MIN_SIZE}KB) ==="
echo ""

# --- Phase 1: Output diff ---
echo "--- Phase 1: Go vs Python output diff ---"
PASS=0
FAIL=0
FAIL_LIST=""

while IFS= read -r SID; do
    [ -z "$SID" ] && continue
    GO_OUT=$("$GO_BIN" read "$SID" 2>/dev/null || true)
    PY_OUT=$(python3 "$PY_BIN" read "$SID" 2>/dev/null || true)

    if [ "$GO_OUT" = "$PY_OUT" ]; then
        PASS=$((PASS + 1))
    else
        FAIL=$((FAIL + 1))
        DIFF_COUNT=$(diff <(echo "$GO_OUT") <(echo "$PY_OUT") | grep -c "^[<>]" || true)
        LINES=$(echo "$GO_OUT" | wc -l | tr -d ' ')
        FAIL_LIST="${FAIL_LIST}\n  ${SID}  (${LINES} lines, ${DIFF_COUNT} diff lines)"
    fi
done <<< "$SESSIONS"

echo "  Pass: $PASS ✅"
echo "  Fail: $FAIL ❌"
if [ "$FAIL" -gt 0 ]; then
    echo -e "  Failed:$FAIL_LIST"
fi
echo ""

# --- Phase 2: Compression stats ---
echo "--- Phase 2: Compression ratio ---"
printf "%-14s %10s %10s %7s\n" "Session" "Raw" "Filtered" "Saved%"
printf "%s\n" "---------------------------------------------"

TMPFILE=$(mktemp)

while IFS= read -r SID; do
    [ -z "$SID" ] && continue
    OUT=$("$GO_BIN" stats "$SID" --no-tokens 2>&1)

    RAW=$(echo "$OUT" | grep "Raw:" | awk '{print $2}' | tr -d ',')
    FILT=$(echo "$OUT" | grep "Filtered:" | awk '{print $2}' | tr -d ',')
    PCT=$(echo "$OUT" | grep "Saved:" | grep -oE '[0-9]+\.[0-9]+%' || echo "0.0%")

    [ -z "$RAW" ] && continue
    [ "$RAW" = "0" ] && continue

    printf "%-14s %10s %10s %7s\n" "${SID:0:14}" "$RAW" "$FILT" "$PCT"
    echo "$RAW $FILT" >> "$TMPFILE"
done <<< "$SESSIONS"

echo ""
echo "--- Summary ---"
python3 -c "
import sys
rows = []
for line in open('$TMPFILE'):
    parts = line.split()
    if len(parts) == 2:
        rows.append((int(parts[0]), int(parts[1])))

if not rows:
    print('No data'); sys.exit()

total_raw = sum(r for r, f in rows)
total_filt = sum(f for r, f in rows)
pcts = [100 * (1 - f/r) for r, f in rows if r > 0]
pcts_nonzero = [p for p in pcts if p > 5]

print(f'Sessions:       {len(rows)}')
print(f'Total raw:      {total_raw:>12,} chars')
print(f'Total filtered: {total_filt:>12,} chars')
print(f'Total saved:    {total_raw - total_filt:>12,} chars ({(total_raw - total_filt)*100/total_raw:.1f}%)')
print()
if pcts_nonzero:
    print(f'Reduction range: {min(pcts_nonzero):.1f}% — {max(pcts_nonzero):.1f}%')
    print(f'Median:          {sorted(pcts_nonzero)[len(pcts_nonzero)//2]:.1f}%')
    print(f'Mean:            {sum(pcts_nonzero)/len(pcts_nonzero):.1f}%')
print(f'No tool calls:   {sum(1 for p in pcts if p < 5)}')
"

rm -f "$TMPFILE"
