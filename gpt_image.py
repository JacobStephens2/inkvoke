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
from dataclasses import dataclass
from pathlib import Path

from openai import OpenAI


QUALITIES = ("low", "medium", "high", "auto")
SIZES = ("auto", "1024x1024", "1536x1024", "1024x1536")
FORMATS = ("png", "jpeg", "webp")

# Standard-tier USD per 1M tokens (OpenAI image generation pricing page).
# API returns usage tokens only; dollar cost is estimated from these rates.
# Keys: model id (prefix match falls back to gpt-image-2).
PRICING_PER_1M: dict[str, dict[str, float]] = {
    "gpt-image-2": {
        "text_input": 5.0,
        "image_input": 8.0,
        "image_output": 30.0,
        "text_output": 0.0,
    },
    "gpt-image-1.5": {
        "text_input": 5.0,
        "image_input": 8.0,
        "image_output": 32.0,
        "text_output": 10.0,
    },
    "chatgpt-image-latest": {
        "text_input": 5.0,
        "image_input": 8.0,
        "image_output": 32.0,
        "text_output": 10.0,
    },
    "gpt-image-1": {
        "text_input": 5.0,
        "image_input": 10.0,
        "image_output": 40.0,
        "text_output": 0.0,
    },
    "gpt-image-1-mini": {
        "text_input": 2.0,
        "image_input": 2.5,
        "image_output": 8.0,
        "text_output": 0.0,
    },
}


@dataclass
class UsageSummary:
    input_tokens: int = 0
    output_tokens: int = 0
    text_input: int = 0
    image_input: int = 0
    image_output: int = 0
    text_output: int = 0
    cost_usd: float | None = None
    model: str = ""

    def format_line(self, *, seconds: float | None = None) -> str:
        parts = []
        if self.cost_usd is not None:
            parts.append(f"~${self.cost_usd:.4f} est.")
        if self.input_tokens or self.output_tokens:
            parts.append(f"tokens in={self.input_tokens} out={self.output_tokens}")
            detail = []
            if self.text_input:
                detail.append(f"text_in={self.text_input}")
            if self.image_input:
                detail.append(f"img_in={self.image_input}")
            if self.image_output:
                detail.append(f"img_out={self.image_output}")
            if self.text_output:
                detail.append(f"text_out={self.text_output}")
            if detail:
                parts.append("(" + ", ".join(detail) + ")")
        if seconds is not None:
            parts.append(f"{seconds:.0f}s")
        return " · ".join(parts) if parts else ""


def pricing_for(model: str) -> dict[str, float]:
    model = (model or "").strip().lower()
    if model in PRICING_PER_1M:
        return PRICING_PER_1M[model]
    for key, rates in PRICING_PER_1M.items():
        if model.startswith(key):
            return rates
    return PRICING_PER_1M["gpt-image-2"]


def summarize_usage(result, model: str) -> UsageSummary:
    """Pull token usage from an Images API response and estimate USD cost."""
    summary = UsageSummary(model=model)
    usage = getattr(result, "usage", None)
    if usage is None and isinstance(result, dict):
        usage = result.get("usage")
    if usage is None:
        return summary

    def _get(obj, *names, default=0):
        for name in names:
            if isinstance(obj, dict) and name in obj:
                val = obj[name]
            else:
                val = getattr(obj, name, None)
            if val is not None:
                return val
        return default

    summary.input_tokens = int(_get(usage, "input_tokens") or 0)
    summary.output_tokens = int(_get(usage, "output_tokens") or 0)

    in_details = _get(usage, "input_tokens_details", default=None)
    out_details = _get(usage, "output_tokens_details", default=None)
    if in_details is not None:
        summary.text_input = int(_get(in_details, "text_tokens") or 0)
        summary.image_input = int(_get(in_details, "image_tokens") or 0)
    else:
        summary.text_input = summary.input_tokens
    if out_details is not None:
        summary.image_output = int(_get(out_details, "image_tokens") or 0)
        summary.text_output = int(_get(out_details, "text_tokens") or 0)
    else:
        summary.image_output = summary.output_tokens

    rates = pricing_for(model)
    million = 1_000_000.0
    summary.cost_usd = (
        summary.text_input * rates["text_input"]
        + summary.image_input * rates["image_input"]
        + summary.image_output * rates["image_output"]
        + summary.text_output * rates["text_output"]
    ) / million
    return summary


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
    parser.add_argument(
        "--no-cost",
        action="store_true",
        help="Do not print estimated USD cost / token usage.",
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


def run_generate(
    client: OpenAI, prompt: str, output: Path, options: dict
) -> tuple[Path, object]:
    result = request_with_retry(
        lambda: client.images.generate(prompt=prompt, **options)
    )
    return write_image(result, output), result


def run_edit(
    client: OpenAI, images: list[Path], prompt: str, output: Path, options: dict
) -> tuple[Path, object]:
    for image in images:
        if not image.expanduser().is_file():
            raise SystemExit(f"Input image not found: {image}")
    handles = [image.expanduser().open("rb") for image in images]
    try:
        # Re-open on each attempt: a failed read leaves file pointers at EOF.
        def call():
            for handle in handles:
                handle.seek(0)
            return client.images.edit(
                image=handles if len(handles) > 1 else handles[0],
                prompt=prompt,
                **options,
            )

        result = request_with_retry(call)
    finally:
        for handle in handles:
            handle.close()
    return write_image(result, output), result


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
    total_cost = 0.0
    cost_count = 0
    show_cost = not getattr(args, "no_cost", False)

    def process(job):
        item, output, options = job
        started = time.time()
        if item.get("edit_from"):
            sources = [Path(source) for source in (
                item["edit_from"] if isinstance(item["edit_from"], list) else [item["edit_from"]]
            )]
            written, result = run_edit(client, sources, item["prompt"], output, options)
        else:
            written, result = run_generate(client, item["prompt"], output, options)
        elapsed = time.time() - started
        usage = summarize_usage(result, options.get("model", args.model))
        line = f"done {item['id']} -> {written}"
        if show_cost:
            detail = usage.format_line(seconds=elapsed)
            if detail:
                line = f"{line} · {detail}"
            else:
                line = f"{line} ({elapsed:.0f}s)"
        else:
            line = f"{line} ({elapsed:.0f}s)"
        print(line)
        return usage

    with concurrent.futures.ThreadPoolExecutor(max_workers=args.workers) as pool:
        futures = {pool.submit(process, job): job[0]["id"] for job in jobs}
        for future in concurrent.futures.as_completed(futures):
            if (error := future.exception()) is not None:
                failures += 1
                print(f"FAILED {futures[future]}: {error}", file=sys.stderr)
            else:
                usage = future.result()
                if usage and usage.cost_usd is not None:
                    total_cost += usage.cost_usd
                    cost_count += 1

    summary = f"{len(jobs) - failures}/{len(jobs)} images written to {args.output_dir}"
    if show_cost and cost_count:
        summary += f" · total ~${total_cost:.4f} est."
    print(summary)
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
    started = time.time()
    if args.command == "generate":
        prompt = sys.stdin.read().strip() if args.prompt == "-" else args.prompt
        written, result = run_generate(client, prompt, args.output, options)
    else:
        written, result = run_edit(client, args.images, args.prompt, args.output, options)
    elapsed = time.time() - started
    line = f"Wrote {written}"
    if not args.no_cost:
        usage = summarize_usage(result, args.model)
        detail = usage.format_line(seconds=elapsed)
        if detail:
            line = f"{line} · {detail}"
        else:
            line = f"{line} ({elapsed:.0f}s)"
    print(line)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
