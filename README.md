# gpt-image

General-purpose command-line tool for OpenAI's image models (`gpt-image-2` by default): generate images from prompts, edit existing images, and run batches from a JSON manifest.

Written in Go - single static binary, no Python runtime. Started as a hair-color photo-edit workaround ([Arena's image edit leaderboard](https://arena.ai/leaderboard/image-edit) via the API instead of the browser apps); now a general tool. The original hair-color path is `gpt-image hair-color`.

## Install

### From source (Go 1.22+)

```bash
git clone https://github.com/JacobStephens2/gpt-image.git
cd gpt-image
go build -o gpt-image ./cmd/gpt-image
export OPENAI_API_KEY="<your_openai_api_key>"
./gpt-image version
```

Or without cloning:

```bash
go install github.com/JacobStephens2/gpt-image/cmd/gpt-image@latest
```

(`go install` puts the binary in `$(go env GOPATH)/bin` - put that on your `PATH`.)

### API key

```bash
export OPENAI_API_KEY="<your_openai_api_key>"
```

Or keep the key in a local file outside the repo and pass `--api-key-file ~/path/to/key.txt` to any command.

## Usage

Generate:

```bash
gpt-image generate "a lighthouse in a storm, gouache" \
  --output lighthouse.png --quality high --size 1536x1024
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

`--dry-run` prints the plan without calling the API.

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
