#!/usr/bin/env python3
"""General-purpose CLI for OpenAI's gpt-image models: generate, edit, and batch."""

from __future__ import annotations

import argparse
import base64
import concurrent.futures
import json
import os
import sys
import time
from pathlib import Path

from openai import OpenAI


QUALITIES = ("low", "medium", "high", "auto")
SIZES = ("auto", "1024x1024", "1536x1024", "1024x1536")
FORMATS = ("png", "jpeg", "webp")


def add_common_arguments(parser: argparse.ArgumentParser) -> None:
    parser.add_argument("--model", default="gpt-image-2", help="OpenAI image model.")
    parser.add_argument("--quality", default="auto", choices=QUALITIES)
    parser.add_argument("--size", default="auto", choices=SIZES)
    parser.add_argument("--output-format", default="png", choices=FORMATS)
    parser.add_argument(
        "--api-key-file",
        type=Path,
        help="File containing the OpenAI API key. Falls back to OPENAI_API_KEY.",
    )


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    subcommands = parser.add_subparsers(dest="command", required=True)

    generate = subcommands.add_parser("generate", help="Generate an image from a prompt.")
    generate.add_argument("prompt", help="Image prompt, or '-' to read from stdin.")
    generate.add_argument("--output", type=Path, default=Path("generated.png"))
    add_common_arguments(generate)

    edit = subcommands.add_parser("edit", help="Edit one or more input images with a prompt.")
    edit.add_argument("images", type=Path, nargs="+", help="Input image path(s).")
    edit.add_argument("--prompt", required=True, help="Edit instruction.")
    edit.add_argument("--output", type=Path, default=Path("edited.png"))
    add_common_arguments(edit)

    batch = subcommands.add_parser(
        "batch", help="Generate or edit many images from a JSON manifest."
    )
    batch.add_argument("manifest", type=Path, help="JSON manifest path (see README).")
    batch.add_argument("--output-dir", type=Path, default=Path("outputs"))
    batch.add_argument("--workers", type=int, default=3, help="Concurrent requests.")
    batch.add_argument("--skip-existing", action="store_true", help="Skip outputs that already exist.")
    batch.add_argument("--dry-run", action="store_true", help="Print the plan without calling the API.")
    add_common_arguments(batch)

    return parser.parse_args(argv)


def make_client(api_key_file: Path | None) -> OpenAI:
    api_key = None
    if api_key_file:
        api_key = api_key_file.expanduser().read_text().strip()
    api_key = api_key or os.environ.get("OPENAI_API_KEY")
    if not api_key:
        raise SystemExit(
            "Missing API key. Set OPENAI_API_KEY or pass --api-key-file /path/to/key.txt"
        )
    return OpenAI(api_key=api_key)


def request_with_retry(call, *, attempts: int = 3):
    for attempt in range(1, attempts + 1):
        try:
            return call()
        except Exception as error:  # noqa: BLE001 - retry any API/transport error
            if attempt == attempts:
                raise
            delay = 5 * 2 ** (attempt - 1)
            print(f"  retry {attempt}/{attempts - 1} after error: {error} (waiting {delay}s)")
            time.sleep(delay)
    raise AssertionError("unreachable")


def write_image(result, output: Path) -> Path:
    output = output.expanduser()
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_bytes(base64.b64decode(result.data[0].b64_json))
    return output


def run_generate(client: OpenAI, prompt: str, output: Path, options: dict) -> Path:
    result = request_with_retry(
        lambda: client.images.generate(prompt=prompt, **options)
    )
    return write_image(result, output)


def run_edit(client: OpenAI, images: list[Path], prompt: str, output: Path, options: dict) -> Path:
    for image in images:
        if not image.expanduser().is_file():
            raise SystemExit(f"Input image not found: {image}")
    handles = [image.expanduser().open("rb") for image in images]
    try:
        result = request_with_retry(
            lambda: client.images.edit(
                image=handles if len(handles) > 1 else handles[0],
                prompt=prompt,
                **options,
            )
        )
    finally:
        for handle in handles:
            handle.close()
    return write_image(result, output)


def load_manifest(path: Path) -> list[dict]:
    data = json.loads(path.expanduser().read_text(encoding="utf-8"))
    items = data["images"] if isinstance(data, dict) else data
    if not isinstance(items, list) or not items:
        raise SystemExit("Manifest must be a non-empty list, or an object with an 'images' list.")
    for index, item in enumerate(items):
        if "prompt" not in item:
            raise SystemExit(f"Manifest item {index} is missing 'prompt'.")
        item.setdefault("id", f"image-{index + 1:02d}")
    return items


def run_batch(client: OpenAI, args: argparse.Namespace, base_options: dict) -> int:
    items = load_manifest(args.manifest)
    jobs = []
    for item in items:
        output = Path(item.get("output") or args.output_dir / f"{item['id']}.{args.output_format}")
        if args.skip_existing and output.expanduser().is_file():
            print(f"skip {item['id']} (exists: {output})")
            continue
        options = dict(base_options)
        for field in ("quality", "size"):
            if item.get(field):
                options[field] = item[field]
        jobs.append((item, output, options))

    if args.dry_run:
        for item, output, options in jobs:
            mode = "edit" if item.get("edit_from") else "generate"
            print(f"{mode} {item['id']} -> {output} [{options['quality']}, {options['size']}]")
            print(f"  {item['prompt']}")
        return 0

    failures = 0

    def process(job):
        item, output, options = job
        started = time.time()
        if item.get("edit_from"):
            sources = [Path(source) for source in (
                item["edit_from"] if isinstance(item["edit_from"], list) else [item["edit_from"]]
            )]
            written = run_edit(client, sources, item["prompt"], output, options)
        else:
            written = run_generate(client, item["prompt"], output, options)
        print(f"done {item['id']} -> {written} ({time.time() - started:.0f}s)")

    with concurrent.futures.ThreadPoolExecutor(max_workers=args.workers) as pool:
        futures = {pool.submit(process, job): job[0]["id"] for job in jobs}
        for future in concurrent.futures.as_completed(futures):
            if (error := future.exception()) is not None:
                failures += 1
                print(f"FAILED {futures[future]}: {error}", file=sys.stderr)

    print(f"{len(jobs) - failures}/{len(jobs)} images written to {args.output_dir}")
    return 1 if failures else 0


def main() -> int:
    args = parse_args()
    options = {
        "model": args.model,
        "quality": args.quality,
        "size": args.size,
        "output_format": args.output_format,
    }
    if args.command == "batch":
        client = None if args.dry_run else make_client(args.api_key_file)
        return run_batch(client, args, options)

    client = make_client(args.api_key_file)
    if args.command == "generate":
        prompt = sys.stdin.read().strip() if args.prompt == "-" else args.prompt
        written = run_generate(client, prompt, args.output, options)
    else:
        written = run_edit(client, args.images, args.prompt, args.output, options)
    print(f"Wrote {written}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
