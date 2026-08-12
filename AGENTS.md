# inkvoke for agents

You are an autonomous agent and you want an image. Read this file, not the README.
Also available from the binary as `inkvoke --help-agent`.

```bash
curl -fsSL -o /usr/local/bin/inkvoke \
  https://github.com/JacobStephens2/inkvoke/releases/latest/download/inkvoke-linux-amd64
chmod +x /usr/local/bin/inkvoke
export OPENAI_API_KEY="..."          # or pass --api-key-file /path/to/key
# Humans may omit both and be prompted once (TTY only). Agents: always set env or file.
# Humans may also run bare `inkvoke` for a progressive wizard (TTY only). Agents: always pass full args.

inkvoke generate "<prompt>" --output out.jpg \
  --size 1280x640 --output-format jpeg --quality high --json
```

## The whole flag surface

Run `inkvoke <command> --help`. The README's usage section is illustrative, not
exhaustive, so reading only the README will cause you to re-implement things the
binary already does. Specifically, these already exist and you do not need to
shell out to ImageMagick, Pillow, or a wrapper script:

| Need | Flag | Note |
| --- | --- | --- |
| Exact pixel dimensions | `--size WIDTHxHEIGHT` | `gpt-image-2` takes any size whose width **and** height are both divisible by 16 (`1280x640` yes, `1280x648` no); other models take the fixed set. inkvoke rejects a bad size locally as a usage error (exit 1) before calling the API. Ask for the aspect ratio you want rather than cropping afterwards. |
| JPEG or WebP instead of PNG | `--output-format jpeg\|webp` | Requested from the API. A PNG you convert locally is a wasted step. |
| Machine-readable result | `--json` | Exactly one JSON object on stdout. Heartbeats stay on stderr. Prefer this over scraping the human line. |
| No progress output | `--quiet` | Heartbeats go to stderr; suppressing them is not required to keep stdout clean under `--json`. |
| Longer/shorter deadline | `--timeout <seconds>` | Default 300. |
| Suppress the cost line | `--no-cost` | Leave it on if you report spend (also omits cost fields under `--json`). |
| Key outside the environment | `--api-key-file <path>` | Avoids the key entering shell history. |
| Interactive key (humans only) | (none) | If no env/file and stdin is a TTY, prompts once; key is process-memory only. Agents must set env or file — non-TTY never prompts. |
| Uninstall this binary | `--uninstall` / `uninstall` | Deletes the running binary only. Prefer manual `rm` in agent sandboxes. |
| Many images | `batch <manifest.json>` | `--workers`, `--skip-existing`, `--dry-run`. |
| Default model in use | `version` / `version --json` | Reports CLI version and default model (`gpt-image-2`). |

## Exit codes

| Code | Meaning | Retry? |
| --- | --- | --- |
| 0 | Success, output written | - |
| 1 | Usage error (bad flag, missing prompt, unreadable invocation) | No |
| 2 | Missing or rejected API key | No |
| 3 | API error, non-retryable (content policy, unsupported size) | No |
| 4 | API error, retryable (rate limit, 5xx, timeout) | Yes, with backoff |
| 5 | Local I/O error reading input or writing output | No |

Under `--json`, failures still use these codes and also emit
`{"ok":false,"error":{"code","message","retryable"}}` on stdout.

## What to expect at runtime

- **Latency.** A single `--quality high --size 1536x1024` generation took **99s**
  wall clock in one measured run. Budget past 120s. If your harness kills tool
  calls at 120s, run it in the background or raise the limit.
- **Cost.** On success without `--json`:
  `Wrote out.png · ~$0.1653 est. · tokens in=142 out=5488`. That run was
  ~$0.17 at high quality, 1536x1024. With `--json`, read `cost_usd_estimate`.
- **Progress.** `… still waiting on OpenAI (30s)` lines on stderr every 10s, so a
  slow call is distinguishable from a hang.

## Parsing the result

Always pass `--json`. Successful `generate` / `edit` / `hair-color` stdout:

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
  "elapsed_seconds": 99.2,
  "usage": { "text_input_tokens": 142, "image_output_tokens": 5488 },
  "cost_usd_estimate": 0.1653
}
```

`batch --json` returns one object with a `results` array (same item shape, plus
`id`, and `"skipped": true` for `--skip-existing`). Full contract:
`docs/specs/agent-interface.md`.

If you cannot pass `--json`, treat exit code 0 plus a present `--output` file as
success. Do not regex the human cost line; it is not a contract.

## Rules

- Never commit the API key. `.gitignore` already covers common local key filenames.
- Image models mangle text. If the image must contain words, render them in HTML
  or SVG over the generated image instead of prompting for them.
- One image per call. Do not loop `generate` to "try again" without changing the
  prompt; you are paying per attempt.
