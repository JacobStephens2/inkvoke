# gpt-image

General-purpose command-line tool for OpenAI's image models (`gpt-image-2` by default): generate images from prompts, edit existing images, and run batches from a JSON manifest.

Written in Go - single static binary, no Python runtime. Started as a hair-color photo-edit workaround ([Arena's image edit leaderboard](https://arena.ai/leaderboard/image-edit) via the API instead of the browser apps); now a general tool. The original hair-color path is `gpt-image hair-color`.

**If you are an autonomous agent**, read [`AGENTS.md`](AGENTS.md) (or run `gpt-image --help-agent`) instead of this README. Prefer `--json` and the documented exit codes over scraping the human cost line.

## Install

### Binary (recommended)

Download a release from [GitHub Releases](https://github.com/JacobStephens2/gpt-image/releases) (linux/macOS/Windows). Example for Linux amd64:

```bash
curl -fsSL -o gpt-image \
  https://github.com/JacobStephens2/gpt-image/releases/latest/download/gpt-image-linux-amd64
chmod +x gpt-image
sudo mv gpt-image /usr/local/bin/gpt-image
gpt-image version
```

Assets: `gpt-image-linux-amd64`, `gpt-image-linux-arm64`, `gpt-image-darwin-amd64`, `gpt-image-darwin-arm64`, `gpt-image-windows-amd64.exe`, plus `SHA256SUMS`.

### With Go 1.22+

```bash
go install github.com/JacobStephens2/gpt-image/cmd/gpt-image@latest
# or pin: ...@v0.3.0
```

(`go install` puts the binary in `$(go env GOPATH)/bin` - put that on your `PATH`.)

From a clone:

```bash
git clone https://github.com/JacobStephens2/gpt-image.git
cd gpt-image
go build -o gpt-image ./cmd/gpt-image
```

### API key (required)

Every `generate`, `edit`, and `batch` call needs an OpenAI API key with access to image models. Create one in the [OpenAI dashboard](https://platform.openai.com/api-keys). Without a key the CLI exits with `missing API key`.

```bash
export OPENAI_API_KEY="<your_openai_api_key>"
```

Or keep the key in a local file outside the repo (preferred if you do not want it in shell history) and pass the file to any command:

```bash
gpt-image generate "a lighthouse in a storm, gouache" \
  --api-key-file ~/path/to/key.txt \
  --output lighthouse.png
```

Do not commit API keys. The repo `.gitignore` already excludes common local key filenames.

## Usage

Generate:

```bash
gpt-image generate "a lighthouse in a storm, gouache" \
  --output lighthouse.png --quality high --size 1536x1024
```

Generate with machine-readable stdout (agents):

```bash
gpt-image generate "a lighthouse in a storm, gouache" \
  --output lighthouse.jpg --size 1280x648 --output-format jpeg \
  --quality high --json
```

Edit (one or more input images):

```bash
gpt-image edit photo.jpg --prompt "make the sky golden hour" --output golden.png
```

Batch from a manifest:

```bash
gpt-image batch manifest.json --output-dir outputs \
  --quality high --size 1536x1024 --workers 3 --skip-existing
```

`--dry-run` prints the plan without calling the API. `gpt-image version` reports the default model; add `--json` for a structured version envelope.

Hair-color convenience (legacy Arena path):

```bash
gpt-image hair-color photo.jpg --output edited.png --color "strawberry blonde"
```

## Manifest format

A JSON list (or an object with an `"images"` list). Each item needs a `prompt`; everything else is optional:

```json
[
  {"id": "cover", "prompt": "a stone house on a sea cliff at dawn"},
  {"id": "recolor", "prompt": "make the door red", "edit_from": "inputs/house.png",
   "quality": "medium", "size": "1024x1024", "output": "outputs/red-door.png"}
]
```

- `id` names the output file (`<output-dir>/<id>.<format>`) when `output` is not set.
- `edit_from` (path or list of paths) switches that item from generate to edit.
- Per-item `quality`/`size` override the command-line flags.
- Failed items retry twice with backoff; the exit code is non-zero if any still fail.

## Defaults

- Model `gpt-image-2`, quality `auto`, size `auto`, format `png`.
- `hair-color` defaults quality to `medium`.

## Cost in CLI output

After each generate/edit (and each batch item), the CLI prints an **estimated** USD cost from the Images API `usage` tokens and published OpenAI per-token rates (standard tier). The API does not return a dollar amount directly.

Example:

```text
Wrote lighthouse.png · ~$0.0531 est. · tokens in=18 out=1760 (text_in=18, img_out=1760) · 41s
```

Use `--no-cost` to suppress usage/cost lines. Rates live in `cmd/gpt-image/cost.go` and may lag OpenAI's pricing page.

## Machine-readable output (agents)

Pass `--json` on any command for exactly one JSON object on stdout (progress heartbeats stay on stderr). Exit codes distinguish usage (1), auth (2), permanent API (3), retryable API (4), and local I/O (5). See `AGENTS.md` or `gpt-image --help-agent` for the full agent contract; `docs/specs/agent-interface.md` is the design record.

## Long-running requests

High-quality `gpt-image-2` calls often take 30–120+ seconds. The CLI prints stderr heartbeats (`… still waiting on OpenAI (Ns)`) so it does not look hung. Use `--quiet` to silence them, and `--timeout 300` (seconds, default) if a run is cut off early.

## Cross-compile

```bash
GOOS=linux   GOARCH=amd64 go build -o dist/gpt-image-linux-amd64   ./cmd/gpt-image
GOOS=darwin  GOARCH=arm64 go build -o dist/gpt-image-darwin-arm64  ./cmd/gpt-image
GOOS=windows GOARCH=amd64 go build -o dist/gpt-image-windows-amd64.exe ./cmd/gpt-image
```

## Privacy

This repo stays code-only. The `.gitignore` excludes common image formats, `inputs/`, `outputs/`, local key files, and response JSON. Do not commit inputs, outputs, or API keys.

## License

MIT
