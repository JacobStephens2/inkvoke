# Spec: a machine-callable interface for inkvoke

Status: implemented (P1–P4 in v0.3.0). P5 remains lower-confidence and unbuilt.

## Why

An autonomous agent used `inkvoke` on a locked-down VM (no Go toolchain, no
Python image libraries installed by policy) to produce a banner image for an
internal company release note. The tool worked and the image shipped. This spec
records where the interface cost the caller extra steps, and proposes the
smallest set of changes that removes them.

Vocabulary below follows the deep-module sense of **interface**: everything a
caller must know to use the module correctly. That includes the flag surface,
but also the stdout contract, the exit codes, the error modes, and the
performance characteristics. Where this spec says the interface is thin, it does
not mean the tool lacks features; it means a correct caller cannot learn what it
needs from the interface alone.

## What the session actually showed

Two distinct findings, and the second is the more important one.

**Finding 1: the machine-readable surface is missing.** The result of a
successful run is one human-readable line:

```
Wrote /path/out.png · ~$0.1653 est. · tokens in=142 out=5488 · (text_in=142, img_out=5488) · 99s
```

An agent that must report spend, or record the artifact path, has to regex
prose whose format is not part of any contract. That is an accidental seam: the
caller depends on a formatting decision the tool never promised to keep.

**Finding 2: the caller under-used the tool because the README is not the
interface.** The agent read `README.md`, concluded that arbitrary output
dimensions and local format control were unavailable, generated at
`1536x1024` PNG, and then cropped and re-encoded with Pillow to reach a
1280x640 JPEG under 100KB. Every one of those steps was avoidable:
`--size 1280x640` and `--output-format jpeg` already exist and are visible in
`inkvoke generate --help`. The README's usage section shows a few
representative flags, so a caller who treats it as the interface will
re-implement behaviour the binary already has, and will pull in exactly the
runtime dependency the single-binary design exists to avoid.

The fix for Finding 2 is documentation placement, not code. `AGENTS.md` in this
PR is that fix.

## Proposals

Ordered by value to a machine caller. P1 is the one that matters.

### P1. `--json` on every command

Add an orthogonal `--json` boolean to `generate`, `edit`, `hair-color`, `batch`,
and `version`. When set, stdout carries exactly one JSON document and nothing
else. Heartbeats and warnings continue to go to stderr, so `--quiet` stays
independent and orthogonal.

`generate` / `edit` / `hair-color`:

```json
{
  "ok": true,
  "command": "generate",
  "output": "/abs/path/out.jpg",
  "bytes": 66623,
  "model": "gpt-image-2",
  "size": "1280x640",
  "quality": "high",
  "output_format": "jpeg",
  "background": "auto",
  "elapsed_seconds": 99.2,
  "usage": { "text_input_tokens": 142, "image_output_tokens": 5488 },
  "cost_usd_estimate": 0.1653,
  "revised_prompt": "..."
}
```

`batch` emits one object with a `results` array of the same shape, each with its
manifest `id`, plus `"skipped": true` for `--skip-existing` hits.

On failure, stdout carries the same envelope with `"ok": false` and an `error`
object (`code`, `message`, `retryable`), and the process exits non-zero. An
agent then needs one contract, not two.

Why this is depth and not just more surface: the caller learns one flag and
stops needing to know anything about the human output format, the cost line
layout, or which stream carries what. Leverage rises, and the tool gains
freedom to restyle its human output without breaking anyone.

### P2. A documented exit-code table

Publish and hold to a small set, in `--help` and in `AGENTS.md`:

| Code | Meaning | Retry? |
| --- | --- | --- |
| 0 | Success, output written | - |
| 1 | Usage error (bad flag, missing prompt, unreadable input) | No |
| 2 | Missing or rejected API key | No |
| 3 | API error, non-retryable (content policy, unsupported size) | No |
| 4 | API error, retryable (rate limit, 5xx, timeout) | Yes, with backoff |
| 5 | Local I/O error writing the output | No |

Today a caller can only distinguish success from failure, so any retry policy
it writes is a guess. Exit codes are the cheapest part of an interface to
specify and the most expensive to leave unstated.

### P3. `version` reports the default model

`inkvoke version` prints `inkvoke 0.2.1`. It should also report the default
model, since that is what the next call will spend money on:

```
inkvoke 0.2.1 (default model: gpt-image-2)
```

and under `--json`, `{"version":"0.2.1","default_model":"gpt-image-2"}`.

### P4. `--help-agent`

Print the `AGENTS.md` content, or a condensed form of it, so an agent that has
the binary but not the repository can discover the same guidance. Low cost, and
it closes the loop that Finding 2 opened: the binary becomes self-describing.

### P5 (lower confidence). Post-processing flags

`--max-width <px>` and `--max-bytes <n>` to downscale and re-encode after the
API returns.

State the case honestly: with `--size WIDTHxHEIGHT` and `--output-format jpeg`
already present, most of what the motivating session needed was reachable
without post-processing. What remains is (a) hitting a byte budget, which
matters for email and web delivery and is not something the API guarantees, and
(b) re-cropping a generation you already paid for rather than paying again.

Apply the deletion test before building this. If every caller that needs a
byte budget re-implements resize-and-re-encode, the complexity is real and
belongs behind the interface. If it turns out most callers can simply ask the
API for the right size, this is a shallow addition and should be dropped. Ship
P1 through P4 first and see whether P5 still has a claim.

## Alternatives considered

**A separate `inkvoke api <command>` subtree that always emits JSON.**
Rejected. It duplicates the entire flag surface and creates a second seam to
keep in step with the first. Two adapters over one interface is the goal; two
interfaces is not.

**Overloading `--output-format json`.** Rejected. That flag already names the
image encoding. Collapsing "what the picture is encoded as" and "what the
report is encoded as" into one flag makes both harder to reason about.

**A `--format` template string (Go template over the result struct).**
Rejected. Maximum flexibility, but every caller then invents its own contract
and none of them are testable. `--json` gives one contract that can be
regression-tested.

**Leave parsing to the caller and document the human line as stable.**
Rejected. It freezes the human output, which is the part most likely to want to
change, and it still leaves errors unstructured.

## Out of scope, worth deciding later

- Determinism. Is there a seed, and is a seeded rerun reproducible enough to be
  worth exposing? Affects whether an agent can cache by prompt hash.
- Retry semantics. Does the binary retry internally today, and if so, does the
  reported `elapsed_seconds` cover all attempts? P2's `retryable` flag assumes
  the caller retries; if the tool retries too, that needs saying.
- Concurrency guidance for `batch --workers` against OpenAI rate limits.

## Measured reference

From the motivating run, for anyone sizing timeouts or budgets:

| Parameter | Value |
| --- | --- |
| Command | `generate --quality high --size 1536x1024` |
| Wall clock | 99s |
| Cost (tool estimate) | ~$0.1653 |
| Tokens | text_in 142, img_out 5488 |
| Output | 1536x1024 PNG, 1.28MB |
