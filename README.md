# gpt-image

General-purpose command-line tool for OpenAI's image models (`gpt-image-2` by default): generate images from prompts, edit existing images, and run batches from a JSON manifest.

Started as a hair-color photo-edit workaround ([Arena's image edit leaderboard](https://arena.ai/leaderboard/image-edit) via the API instead of the browser apps, worked through with Codex CLI using GPT-5.5); now a general tool. The original `edit_hair_color.py` still works as before.

## Setup

```bash
python3 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
export OPENAI_API_KEY="<your_openai_api_key>"
```

Or keep the key in a local file outside the repo and pass `--api-key-file ~/path/to/key.txt` to any command.

## Usage

Generate:

```bash
python gpt_image.py generate "a lighthouse in a storm, gouache" \
  --output lighthouse.png --quality high --size 1536x1024
```

Edit (one or more input images):

```bash
python gpt_image.py edit photo.jpg --prompt "make the sky golden hour" --output golden.png
```

Batch from a manifest:

```bash
python gpt_image.py batch manifest.json --output-dir outputs \
  --quality high --size 1536x1024 --workers 3 --skip-existing
```

`--dry-run` prints the plan without calling the API.

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
- Failed items retry twice with backoff; the exit code is non-zero if any item still fails.

## Hair-color legacy script

```bash
python edit_hair_color.py photo.jpg --output edited.png --color "strawberry blonde"
```

## Defaults

- Model `gpt-image-2`, quality `auto`, size `auto`, format `png`.

## Cost in CLI output

After each generate/edit (and each batch item), the CLI prints an **estimated** USD cost from the Images API `usage` tokens and published OpenAI per-token rates (standard tier). The API does not return a dollar amount directly.

Example:

```text
Wrote lighthouse.png · ~$0.0531 est. · tokens in=18 out=1760 (text_in=18, img_out=1760) · 41s
```

Use `--no-cost` to suppress usage/cost lines. Rates live in `PRICING_PER_1M` in `gpt_image.py` and may lag OpenAI's pricing page.

## Privacy

This repo stays code-only. The `.gitignore` excludes common image formats, `inputs/`, `outputs/`, local key files, and response JSON. Do not commit inputs, outputs, or API keys.

## License

MIT
