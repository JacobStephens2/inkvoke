# gpt-image

General-purpose command-line tool for OpenAI's image models (`gpt-image-2` by default): generate images from prompts, edit existing images, and run batches from a JSON manifest.

Written in Go - single static binary, no Python runtime. Started as a hair-color photo-edit workaround ([Arena's image edit leaderboard](https://arena.ai/leaderboard/image-edit) via the API instead of the browser apps); now a general tool. The original hair-color path is `gpt-image hair-color`.

**If you are an autonomous agent**, read [`AGENTS.md`](AGENTS.md) (or run `gpt-image --help-agent`) instead of this README. Prefer `--json` and the documented exit codes over scraping the human cost line.

## Install

### Binary (recommended)

Download a release from [GitHub Releases](https://github.com/JacobStephens2/gpt-image/releases). Pick the asset that matches **both** your OS and CPU architecture. Using the wrong file (for example a Linux binary on macOS, or `amd64` on Apple Silicon without Rosetta) fails with errors like `exec format error` or "cannot be opened because the developer cannot be verified" / wrong architecture.

#### Which binary? (`amd64` vs `arm64`)

| Suffix | CPU | Typical machines |
|--------|-----|------------------|
| **`amd64`** | Intel/AMD 64-bit (x86_64) | Most Windows/Linux PCs; older Intel Macs |
| **`arm64`** | ARM 64-bit (aarch64) | Apple Silicon Macs (M1/M2/M3/M4…); many Raspberry Pi / AWS Graviton hosts |

- **macOS Apple Silicon** → `gpt-image-darwin-arm64` (not `linux-*`, not `darwin-amd64` unless you run under Rosetta)
- **macOS Intel** → `gpt-image-darwin-amd64`
- **Linux x86_64** → `gpt-image-linux-amd64`
- **Linux ARM64** → `gpt-image-linux-arm64`
- **Windows x86_64** → `gpt-image-windows-amd64.exe`

Check your machine if unsure:

```bash
uname -s   # Darwin = macOS, Linux = Linux
uname -m   # arm64/aarch64 → arm64; x86_64 → amd64
```

Assets also include `SHA256SUMS` for integrity checks.

#### macOS (Apple Silicon — arm64)

```bash
curl -fsSL -o gpt-image \
  https://github.com/JacobStephens2/gpt-image/releases/latest/download/gpt-image-darwin-arm64
chmod +x gpt-image
sudo mv gpt-image /usr/local/bin/gpt-image
gpt-image version
```

#### macOS (Intel — amd64)

```bash
curl -fsSL -o gpt-image \
  https://github.com/JacobStephens2/gpt-image/releases/latest/download/gpt-image-darwin-amd64
chmod +x gpt-image
sudo mv gpt-image /usr/local/bin/gpt-image
gpt-image version
```

#### Linux (amd64)

```bash
curl -fsSL -o gpt-image \
  https://github.com/JacobStephens2/gpt-image/releases/latest/download/gpt-image-linux-amd64
chmod +x gpt-image
sudo mv gpt-image /usr/local/bin/gpt-image
gpt-image version
```

#### Linux (arm64)

```bash
curl -fsSL -o gpt-image \
  https://github.com/JacobStephens2/gpt-image/releases/latest/download/gpt-image-linux-arm64
chmod +x gpt-image
sudo mv gpt-image /usr/local/bin/gpt-image
gpt-image version
```

#### Windows (amd64)

Download [`gpt-image-windows-amd64.exe`](https://github.com/JacobStephens2/gpt-image/releases/latest/download/gpt-image-windows-amd64.exe) from Releases, rename to `gpt-image.exe` if you like, put it on your `PATH`, then run `gpt-image version`.

#### Uninstall

Remove the binary that `gpt-image` is running from:

```bash
gpt-image --uninstall
# or: gpt-image uninstall
```

If the file is root-owned (common for `/usr/local/bin`):

```bash
sudo gpt-image --uninstall
```

That only deletes the installed binary — not API keys, generated images, or shell config. After uninstall, run `hash -r` (zsh/bash) or open a new terminal so the shell drops a cached path.

Manual alternative: `sudo rm /usr/local/bin/gpt-image`, or delete `$(go env GOPATH)/bin/gpt-image` if you used `go install`.

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

Every `generate`, `edit`, and `batch` call needs an OpenAI API key with access to image models. Create one in the [OpenAI dashboard](https://platform.openai.com/api-keys). Resolution order:

1. `--api-key-file /path/to/key.txt` (if passed)
2. `OPENAI_API_KEY` in the environment
3. **Interactive prompt** when stdin is a terminal: `OpenAI API key (not saved):` (typed characters are hidden). The key is held only in process memory for that one command, then discarded when the process exits. It is **not** written to disk and **not** exported into your shell.

```bash
export OPENAI_API_KEY="<your_openai_api_key>"
```

`export` keeps the key in **your shell session** (and child processes inherit it) until you `unset OPENAI_API_KEY` or close the terminal. It is not the same as the interactive prompt, and it only lands on disk if you put the export in a startup file (e.g. `~/.zshrc`).

Or keep the key in a local file outside the repo (preferred if you do not want it in shell history) and pass the file to any command:

```bash
gpt-image generate "a lighthouse in a storm, gouache" \
  --api-key-file ~/path/to/key.txt \
  --output lighthouse.png
```

Agents and CI (non-interactive / no TTY) are not prompted — they must set the env var or pass `--api-key-file`, or the CLI exits with auth error code 2.

Do not commit API keys. The repo `.gitignore` already excludes common local key filenames.

## Usage

### Interactive mode (terminal)

If you run `gpt-image` with no arguments on a terminal, or run a command without its required args (for example `gpt-image generate` with no prompt), the CLI asks **one question at a time** until it has enough to run. Press Enter to accept a shown default. Agents and non-TTY environments are never prompted this way — they still need full flags (and get usage/auth exit codes).

```bash
gpt-image
# Command (generate/edit/batch/hair-color) [generate]:
# Image prompt: …
# Output path [generated.png]:
# …
```

### Generate

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
