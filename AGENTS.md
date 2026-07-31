# gpt-image for agents

You are an autonomous agent and you want an image. Read this file, not the README.

```bash
curl -fsSL -o /usr/local/bin/gpt-image \
  https://github.com/JacobStephens2/gpt-image/releases/latest/download/gpt-image-linux-amd64
chmod +x /usr/local/bin/gpt-image
export OPENAI_API_KEY="..."          # or pass --api-key-file /path/to/key

gpt-image generate "<prompt>" --output out.jpg \
  --size 1280x648 --output-format jpeg --quality high
```

## The whole flag surface

Run `gpt-image <command> --help`. The README's usage section is illustrative, not
exhaustive, so reading only the README will cause you to re-implement things the
binary already does. Specifically, these already exist and you do not need to
shell out to ImageMagick, Pillow, or a wrapper script:

| Need | Flag | Note |
| --- | --- | --- |
| Exact pixel dimensions | `--size WIDTHxHEIGHT` | `gpt-image-2` accepts arbitrary sizes; other models take the fixed set. Ask for the aspect ratio you want rather than cropping afterwards. There is a **minimum pixel budget**: `1024x512` is rejected with `Invalid size '1024x512'. Requested resolution is below the current minimum pixel budget.` after a round trip. `1536x1024` is safe. For a short wide banner, generate at `1536x1024` and crop. |
| JPEG or WebP instead of PNG | `--output-format jpeg\|webp` | Requested from the API. A PNG you convert locally is a wasted step. |
| No progress output | `--quiet` | Heartbeats go to stderr; suppressing them is not required to keep stdout clean. |
| Longer/shorter deadline | `--timeout <seconds>` | Default 300. |
| Suppress the cost line | `--no-cost` | Leave it on if you report spend. |
| Key outside the environment | `--api-key-file <path>` | Avoids the key entering shell history. |
| Many images | `batch <manifest.json>` | `--workers`, `--skip-existing`, `--dry-run`. |

## What to expect at runtime

- **Latency.** A single `--quality high --size 1536x1024` generation took **99s**
  wall clock in one measured run. Budget past 120s. If your harness kills tool
  calls at 120s, run it in the background or raise the limit.
- **Cost.** Printed on success, e.g.
  `Wrote out.png · ~$0.1653 est. · tokens in=142 out=5488`. That run was
  ~$0.17 at high quality, 1536x1024.
- **Progress.** `… still waiting on OpenAI (30s)` lines on stderr every 10s, so a
  slow call is distinguishable from a hang.

## Parsing the result

There is no machine-readable output mode yet. Today you must parse the human
line above, or infer success from the exit code plus the presence of `--output`.
`docs/specs/agent-interface.md` specifies the `--json` contract that would
replace this; until it ships, prefer checking the exit code and stat-ing the
output file over regexing stdout.

## Rules

- Never commit the API key. `.gitignore` already covers common local key filenames.
- Image models mangle text. If the image must contain words, render them in HTML
  or SVG over the generated image instead of prompting for them.
- One image per call. Do not loop `generate` to "try again" without changing the
  prompt; you are paying per attempt.
