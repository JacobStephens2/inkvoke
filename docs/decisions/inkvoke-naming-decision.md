# Naming Decision Record: `gpt-image` → `inkvoke`

**Project:** github.com/JacobStephens2/gpt-image
**Author:** Jacob Stephens
**Date:** 2026-08-01
**Status:** Decided — proceed with rename
**Supersedes:** repo name `gpt-image`, module path `github.com/JacobStephens2/gpt-image`

---

## 1. Decision

Rename the project, repository, Go module, and binary from `gpt-image` to **`inkvoke`**.

Register **`inkvoke.dev`** (Porkbun: $8.75 first year, $12.87 renewal).

Canonical description going forward:

> **inkvoke — single-binary, agent-friendly image generation and editing CLI. Powered by OpenAI gpt-image-2.**

Cut the rename as **v1.0.0** rather than continuing the v0.3.x line.

---

## 2. Why Rename

Three independent problems with `gpt-image`, in descending order of long-term cost.

### 2.1 The name is a moving target that isn't mine

OpenAI's Design Guidelines state plainly: "We do not permit model names in app titles because there is
concern that it confuses end users. It also triggers our enforcement mechanisms," and separately, "we do
not permit our GPT brand to be used in app, product, developer or company names." OpenAI ran an
enforcement wave in 2023, sending compliance messages via a third party with short response windows to
projects using GPT as a name prefix or suffix.

The permitted pattern is descriptive-only: product name first, then "powered by." Never "GPT-4 X."

Nuance worth recording: OpenAI's US trademark application for bare "GPT" ran into USPTO refusal, and the
acronym predates OpenAI as a generic term, so a pure-generic defense exists. But `gpt-image` is not
generic — it is the verbatim OpenAI model family name (`gpt-image-1`, `gpt-image-2`), which is exactly
the case the guidelines target. Practical risk at 0 stars is low. The cost of renaming rises
monotonically with adoption, Homebrew taps, and Go module path history. Rename now or never.

### 2.2 Zero differentiation in a crowded namespace

Everyone who wraps this API names the wrapper after the API:

| Project | What it is |
|---|---|
| `gpt-image-cli` (npm) | CLI for gpt-image-2 generation/editing + Claude Code skill |
| `@cloudwerxlab/gpt-image-1-mcp` | MCP server for the same model |
| `wuyoscar/gpt_image_2_skill` | Prompt gallery shipping a CLI invoked as `gpt-image generate` |
| OpenAI Codex | Uses `imagegen` as its own built-in skill terminology |

Under the old name, every blog post about this tool is indistinguishable from documentation about the
model, and it competes for search real estate against OpenAI's own docs page.

### 2.3 The name under-describes the actual product

The real differentiators are absent from `gpt-image`:

- Single static Go binary, no Python runtime
- Documented agent contract: `--json`, exit codes 1–5, `--help-agent`
- Batch manifests with workers, retries with backoff, `--skip-existing`
- Per-call USD cost estimation from Images API `usage` tokens
- Cross-compiled release matrix with `SHA256SUMS`

The name also hard-couples the project to one vendor at exactly the moment a second backend
(Gemini, FLUX, Replicate) becomes interesting. `inkvoke --provider flux` costs nothing.
`gpt-image --provider flux` is nonsense.

**Counterpoint, recorded for honesty:** `gpt-image` is maximally discoverable, and the binary name matches
the mental model. That value is real and is preserved by keeping the model name in the description and
repo topics rather than the title.

---

## 3. Candidates Evaluated

Three finalists went to full collision check. Two were disqualified.

### 3.1 `litho` — DISQUALIFIED

The lithography metaphor was the strongest conceptual fit: lithography is batch reproduction from a
manifest of plates, which maps directly onto the `batch` + manifest + `--workers` + `--skip-existing`
design. Five characters, no hyphen, no shift key.

Killed by collisions, all recent and all in-lane:

- **girish946/litho** — Rust CLI for flashing disk images and cloning block devices, published on
  crates.io as `litho` (crate `liblitho`), July 2026. A single-binary CLI named `litho` in a package
  registry is the closest possible collision type.
- **sopaco/deepwiki-rs**, explicitly "also known as Litho" — AI-powered C4 architecture documentation
  engine in Rust, crates.io v1.5.0, 6.2k downloads, actively promoted as of July 2026.
- Also occupied: `litho-book` (crates.io), `fastscape-litho` (PyPI).

Adopting it would make this the third `litho` in AI developer tooling in 2026.

### 3.2 `imprint` — DISQUALIFIED

Best plain-English option — covers both generate (make a mark) and edit (stamp onto an existing thing),
with printing-house lineage.

Two problems, the second fatal:

- **Dev collision:** `ilang-ai/Imprint` is an AI agent skill for memory and cross-agent portability
  (April 2026). `imprint-mcp-server` is published on npm. Both sit in agent tooling — the target audience.
- **Permanent SEO burial:** "Imprint" is the standard English rendering of the German *Impressum*, the
  legally required site-identification page. It appears in the footer of an enormous share of European
  dev sites — observed during research on Forgejo, n8n, and assorted engineering blogs. "imprint CLI" and
  "imprint go" will drown in legal footers forever. There is no SEO strategy that beats a word appearing
  on every German-hosted page on the internet.

### 3.3 `inkvoke` — SELECTED

Portmanteau of **ink + invoke**. Communicates invoking visual creation from code, which is precisely
what an agent-operated image CLI does.

```
inkvoke generate "a lighthouse in a storm, gouache" --output lighthouse.png
inkvoke edit photo.jpg --prompt "make the sky golden hour"
inkvoke batch manifest.json --output-dir outputs --workers 3 --json
```

### 3.4 Also considered and rejected

| Name | Reason rejected |
|---|---|
| `imgen` / `imggen` | Dead — casoon/imgen (Replicate), lawrencewzen/imgen, imgen inside alephao/nftool, `imgen-1.5` as a model ID elsewhere |
| `imagegen` | OpenAI/Codex uses this as its own skill terminology |
| `imgctl` | Multiple existing image CLIs, including an agent-first static binary (agent-rt/imgctl) |
| `pixctl` | Taken by an image optimization/upscaling CLI |
| `imago` | AI-image MCP server (Wafle-Imago) plus several image projects |
| `pigment` | npm package is a CLI-UI library |
| `inkwell` | inkwell-cli plus an agent library named Inkwell |
| `atelier` | Two separate agent-runtime projects |
| `sable` | Bloomberg .NET CLI and Libera-Chat's IRC server |
| `gesso`, `daub`, `stipple`, `limn` | All occupied in adjacent dev tooling |
| `imagen` | Google's image model — trades one vendor's trademark for another's |
| `PromptPix`, `ImagePipe` | Multiple existing AI-image products |
| Anything `-Forge` | Generic, clichéd, misaligned with a lightweight Unix CLI |
| `gouache` | Uncontested and a charming callback to the README example, but unspellable. Reserved as a release codename. |

---

## 4. Collision Check Results — `inkvoke`

| Check | Result | Detail |
|---|---|---|
| USPTO trademark | CLEAR | No registration or pending application found |
| GitHub repo | CLEAR | No repository under this name |
| npm | CLEAR | Nothing published |
| crates.io | CLEAR | Nothing published |
| PyPI | CLEAR | Nothing published |
| Go module proxy | CLEAR | Own namespace |
| Homebrew core | CLEAR | No formula; own tap is the correct path anyway |
| Domain `inkvoke.dev` | **AVAILABLE** | Porkbun, $8.75 first year / $12.87 renewal |
| Google Play | Occupied (benign) | See 4.1 |
| Social handles | Partially taken | `inkvoke.tattoo` on Instagram; a marketplace jewelry seller |

### 4.1 The one live software user

**Inkvoke — Lorcana Card Tracker**, by Julien Tonsuso, a solo developer in Annecy, France, publishing
under his own name with a Gmail support address. Android-only, unofficial Disney Lorcana companion app,
explicitly disclaiming affiliation with Disney and Ravensburger. **50+ installs.**

This is about as harmless as a collision gets: different platform, different category, no trademark, no
company entity, negligible install base. The name is also clearly a Lorcana-specific pun — that game's
core mechanic is "ink" and "inkwell," so "Inkvoke" reads as ink + invoke *in that game's vocabulary*. No
claim exists on the general construction, and there is no plausible reason a TCG collection tracker
would care about a Go image CLI.

Precedent: the same profile as Chart35, which cleared all three checks and has held up.

### 4.2 Accepted residual risks

1. **Typo drift toward `invoke`.** Unavoidable. Mitigated with consistent INK·VOKE styling in the README
   header and logo. Note that npm's `invoke` is a dormant 2012 async flow-control micro-library, so even
   a mistyped search does not land users somewhere confusing.
2. **Social handle `@inkvoke` unavailable** — the tattoo shop holds the closest handle. `inkvoke.dev`
   and `getinkvoke` are the standard workarounds. Not blocking for a CLI.

---

## 5. Domain

**Register `inkvoke.dev`** — $8.75 first year, $12.87 renewal at Porkbun.

Rationale:

- `.dev` is semantically correct for a developer CLI and carries automatic HSTS preload.
- Renewal price is stable and unremarkable; no first-year-teaser trap.
- Porkbun exposes a public DNS/domain API, satisfying the standing requirement that registrars be
  automatable from Terraform alongside Cloudflare, Route53, and DigitalOcean.
- Sidesteps the `@inkvoke` social handle collision by making the domain the canonical identity.

`stephens.page/inkvoke/` remains the docs home; `inkvoke.dev` can redirect there initially and be
promoted to primary later without cost.

---

## 6. Migration Plan

### 6.1 Repository and module

1. Rename the repo in GitHub Settings. **Do not delete it and do not create a new empty `gpt-image`
   repo** — GitHub's permanent redirect must stay intact so existing clones and release-download URLs
   keep working.
2. Change the module path. This breaks
   `go install github.com/JacobStephens2/gpt-image/cmd/gpt-image@latest` for new versions, which is
   acceptable: old tagged versions remain resolvable via the module proxy cache, so nothing already
   pinned breaks.
3. **Cut as v1.0.0.** A module path change is a fresh identity — use it to signal stability of the agent
   contract rather than limping along at v0.3.x.

```bash
go mod edit -module github.com/JacobStephens2/inkvoke
grep -rl "JacobStephens2/gpt-image" . | xargs sed -i '' 's|JacobStephens2/gpt-image|JacobStephens2/inkvoke|g'
git mv cmd/gpt-image cmd/inkvoke
go build ./... && go test ./...
git tag v1.0.0 && git push --tags
```

### 6.2 Documentation order (important)

Update in this sequence:

1. `AGENTS.md`
2. `docs/specs/agent-interface.md`
3. `README.md`
4. Repo description and topics

Agents key off the binary name in examples. Stale agent-facing docs cause real failures; a stale README
only causes mild confusion. The agent contract updates first.

### 6.3 Transition release

- Publish both `inkvoke-<os>-<arch>` and `gpt-image-<os>-<arch>` assets for one or two releases. Same
  binary.
- The old-named binary prints a one-line deprecation notice to **stderr only**. Never stdout — the README
  promises "exactly one JSON object on stdout" under `--json`, and a deprecation line there would
  silently corrupt every agent consuming the contract.
- Keep `gpt-image` accepted in argv detection for one version so users who `mv` the binary do not break
  scripts.
- Drop both after the transition window.

### 6.4 Preserve discoverability

- Repo topics stay: `gpt-image-2`, `openai`, `image-generation`, `cli`, `go`, `golang`, `image-editing`.
- "OpenAI gpt-image-2" appears in the GitHub description and the first line of the README.
- 301 `stephens.page/gpt-image/` → `stephens.page/inkvoke/`.
- Description uses the guideline-compliant form — product name first, then "Powered by."

Nothing is lost in search. What is gained is a title that is actually ownable.

### 6.5 Namespace claims to make at rename time

- Register `inkvoke.dev` at Porkbun.
- Create the Homebrew tap: `JacobStephens2/homebrew-tap`, formula `inkvoke`.
- Reserve the npm name if an `npx` installer wrapper is ever wanted — npm is where the collisions
  happened to every other candidate on the shortlist.

---

## 7. Closing Rationale

A repo with a documented agent contract, a cross-compiled release matrix, `SHA256SUMS`, a `docs/specs`
design record, and a project page is not a personal utility. It is a product. Products get product names.

`gpt-image` was an excellent description and a weak brand — legible, unownable, vendor-welded, and
silent about everything that makes the tool good. `inkvoke` is distinctive, provider-neutral, clear
across every registry that matters, available as a `.dev` domain for under $13/year, and reads correctly
for what the tool actually is: a way to invoke image creation from code.
