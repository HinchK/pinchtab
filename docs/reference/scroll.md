# Scroll

Scroll the current tab or a specific element.

```bash
curl -X POST http://localhost:9867/action \
  -H "Content-Type: application/json" \
  -d '{"kind":"scroll","scrollY":800}'
# Response: {"success":true,"result":{"success":true}}

# CLI Alternative (human-readable by default)
pinchtab scroll down
# Output: OK

pinchtab scroll down --snap        # scroll and output snapshot
pinchtab scroll 800 --snap-diff    # scroll and output snapshot diff
pinchtab scroll 800 --json         # Full JSON response

pinchtab scroll --dy -300          # scroll up 300px
pinchtab scroll --dx -120          # scroll left 120px
```

Notes:

- a negative pixel count uses `--dy` / `--dx`: a leading minus on a positional argument is
  read as a bundle of shorthand flags and refused before the command runs
- give either the flags or one positional, never both, and only one positional is accepted —
  so `--tab` stays a flag and must not be placed after `--`, where it would be read as a
  positional

- use `--snap` to output an interactive snapshot after scrolling
- use `--snap-diff` to output only the changes from the previous snapshot
- the top-level CLI also accepts a pixel value such as `pinchtab scroll 800`
- the raw API uses `scrollY` and `scrollX` for page scrolling
- the raw API can also target an element with `ref` or `selector`
- selector lookup is limited to the current frame scope; the default scope is `main`
- use [`/frame`](./frame.md) or `pinchtab frame` before selector-based iframe scrolling

## Related Pages

- [Frame](./frame.md)
- [Snapshot](./snapshot.md)
- [Text](./text.md)
