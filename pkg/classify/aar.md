# classify: running log (after-action record)

A blow-by-blow of the work on `pkg/classify`, kept up to date as it goes, to be
boiled down into a smaller summary at the end. Entries are in order. "Verified"
means I ran it or read it from a source; "unverified" means it's from memory or
inference. `research.md` has the research itself; this has what was done and
what was learned doing it.

Machine 1 (where this was started): Apple M3, 24 GB, macOS 14.6 (Sonoma),
arm64, Go 1.26.0, Python 3.14.7 (Homebrew), Homebrew 7.0.13, tesseract
installed, ollama not installed at the start.

## 1. The ask

Search photos by content ("pizza" finds photos with pizza), locally, without
sending them to a hosted AI. Which technique works best depends on resource
use and output size, and on the user's own photos, so: research the options,
and build `pkg/classify` as a tool/library for trying several, with the
research and tradeoffs written to `pkg/classify/research.md`.

## 2. Research (web searches, then writing it up)

Searched for: SigLIP 2 vs CLIP retrieval; RAM++ tagging; ollama vision models
on Apple Silicon; Go ML runtimes (hugot, onnxruntime); Immich's search and its
ML service; PhotoPrism's vision docs; sqlite-vec and brute force vector search.

Findings that shaped the design:

- Two families of output: **embeddings** (one vector per photo; query embedded
  with the same model; nearest wins) and **words** (caption + tags from a
  vision-language model, searched as text with FTS5). Embeddings are cheap and
  open-vocabulary but give no readable labels and no "no results"; VLMs are
  readable and give text-in-photo and detail but cost seconds per photo.
- Immich searches with CLIP-family embeddings; its docs table has
  `ViT-B-16-SigLIP2__webli` as a good small model and
  `ViT-SO400M-16-SigLIP2-384__webli` as the best English one (only a point or
  two of recall apart). I read these figures from a search summary, not the
  page: unverified in detail.
- Only speed datapoint found for VLMs: Gemma 3 4B via ollama about 4 s/photo on
  an M4 Pro (one user's blog). That is ~11 h per 10,000 photos: fine as a
  background job, not as something to re-run casually.
- Brute-force vector search is fine at personal scale (sqlite-vec: 100k x 384
  in 68 ms on an M1 Pro). No vector database needed.
- Go has no good in-process path to CLIP-style search that I could confirm.
  hugot (ONNX Runtime / pure Go / GoMLX backends) exists but I did not confirm
  a CLIP/SigLIP image+text pipeline. So the package drives other things:
  ollama over HTTP, and any program over a line-delimited JSON "worker"
  protocol.
- Immich's ML container has a `/predict` endpoint but its request format is
  undocumented and has changed between versions: tempting, fragile, not used.

## 3. The package (`pkg/classify`)

Built (all standard library plus `golang.org/x/image`, already in go.mod):

| file | what |
|---|---|
| `classify.go` | `Classifier` / `TextEmbedder` interfaces, `Result{Labels, Caption, Embedding}`, `Prepare` (decode, downscale, JPEG) |
| `ollama.go` | `OllamaVLM`: `/api/chat`, image as base64, JSON-schema constrained `{caption, tags}`, `temperature 0`, `think:false`, `MaxEdge` default 896 |
| `worker.go` | `Worker`: a long-lived process; `{"op":"image","path"}` / `{"op":"text","text"}` in, a Result out, one JSON line each; also a `TextEmbedder` |
| `run.go` | `Run`: classify files, append `Record`s to a JSONL file; resumable; failed photos retried next run |
| `search.go` | `Index.Lexical` (every query word must be in labels or caption; label > caption; crude plural stemming) and `Index.Vector` (cosine, brute force) |
| `spec.go` | backend spec strings: `ollama:MODEL[@HOST]`, `worker:COMMAND`, optional `name=` prefix; names the results file |
| `cmd/classify` | `run`, `search`, `compare` (with `-judge` TSV for precision/recall), `stats` |
| `scripts/worker_openclip.py` | worker for open_clip models (CLIP/SigLIP/SigLIP 2), optional `--vocab` for zero-shot labels |
| tests | httptest for ollama; the test binary re-exec'd as a fake worker; lexical, vector, resume, spec parsing |

Verified: `go vet` and `go test ./pkg/classify/...` pass; the CLI was smoke
tested end to end (run, resume, search, compare with judging, stats) against a
fake python worker. Not verified: anything against a real model.

Design choices worth remembering:
- Results are per backend, in `classify-results/<name>.jsonl`, so backends are
  compared on the same photos. Embeddings are stored as JSON text (~8 KB/photo
  at 768 dims): fine for experiments, a BLOB for real.
- Vector search has no score floor yet, so it always returns N hits, even for
  things you have no photos of. Known gap, noted in research.md.
- The tool does not apply EXIF orientation (Instagram's files are already
  upright; for others use the gallery's thumbnails).

## 4. Python environment

Ask: a virtualenv inside the package, plus a script that works out which
PyTorch to install; the user pointed at pytorch.org/get-started/locally/
(PyTorch supports Python 3.10-3.14).

- **Correction to my own earlier claim:** I had said Python 3.14 might not be
  supported by PyTorch wheels yet. Wrong: the PyTorch index has `cp314` wheels
  (checked), and the user's page reference said 3.10-3.14.
- The install page is JavaScript-rendered; a fetch could not read the commands
  (the fetched page also showed a stale default, "2.7.0"). So no table was
  written from it. Instead, `download.pytorch.org/whl/` is a plain HTML index
  that can be read at run time, and `scripts/setup_venv.py` (stdlib only) uses
  it:
  - macOS: plain PyPI wheels (Apple GPU / MPS is built in; no index URL).
  - Linux/Windows + NVIDIA: parse `nvidia-smi`'s "CUDA Version", consider
    `cuNNN` builds no newer than that, fetch each one's `torch/` page, and pick
    the one with the newest torch that has a wheel for this python tag and
    platform. (Newest torch beats newest CUDA: a newer toolkit that only has an
    older torch loses.)
  - ROCm (Linux): the same with `rocmX.Y`. Otherwise the CPU index (Linux's
    PyPI default is the multi-GB CUDA wheel; Windows's PyPI default is CPU).
  - If a CUDA machine's driver is too old for any build with this python it
    falls back to CPU and **warns**, suggesting a driver update or
    `--python python3.12`.
  - Flags: `--dry-run`, `--device`, `--toolkit X.Y`, `--python`, `--no-torch`.
- Checked the selection logic against the live index with faked platforms
  (Linux x86_64 CUDA 12.8 -> `cu126`, torch 2.14.1, because `cu128` lacks the
  newest torch; CUDA 13.0 -> `cu130`; Windows CUDA 12.6 -> `cu126`; Linux
  aarch64 CPU -> `cpu` 2.15.0; ROCm 7.0 -> `rocm7.0`, torch 2.10.0). **Not
  tested on a real GPU machine.** The user plans to run on a Linux box with an
  NVIDIA GPU.
- `make -C pkg/classify venv` ran here: Python 3.14.7, **torch 2.15.0, MPS
  available**, open_clip_torch 4.2.2, in `pkg/classify/.venv` (about 1.5 min).
  `requirements.txt` deliberately leaves torch out; the script installs it
  first. `.gitignore` gained the venv, `__pycache__`, the sample set and
  `classify-results/`.

## 5. Sample set

The user's Instagram export (`instagram-jmoiron-2026-03-23-*.zip`, repo root,
untracked) is the sample corpus: 296 photos (240 JPEG + 56 WebP), 54 MB, in
`pkg/classify/sample/instagram` (4 videos skipped). `make -C pkg/classify
sample ZIP=...` re-extracts. Not committed.

## 6. Installing ollama (in progress)

The user ran `brew install ollama` and it started compiling everything from
source (cmake, go, z3, ...). Looked at it without interrupting it:

- macOS is 14.6 (Sonoma). `brew info --json=v2` lists **no bottles at all** for
  `ollama` 0.40.16, `z3` and `mlx-c` (z3 is a common formula that surely has
  bottles somewhere), which is consistent with Homebrew no longer publishing or
  serving bottles for this macOS version. That would be why it builds from
  source. This is an inference: I did not confirm Homebrew's support policy.
- The `ollama` formula depends on `mlx-c` (build deps: ccache, cmake, go), so
  the source build is large.
- There is a prebuilt alternative: `brew install --cask ollama-app` (the
  Ollama.app, which includes the server and CLI). Suggested stopping the source
  build and using that instead. Not yet done / confirmed by the user.

### 6a. macOS upgrade

The user saw macOS 27.2.1 on offer and suspected version-number inflation.
Not so: Apple moved to year-based numbers with macOS 26 (2025); the line is 14
(2023), 15 (2024), 26 (2025), 27 (2026), so macOS 14 is three releases back.
That is consistent with Homebrew having no bottles for it (still an inference).
Plan: upgrade, update the Command Line Tools, `brew update`, then
`brew install --cask ollama-app`. If the upgrade breaks `.venv` (it points at
Homebrew's python 3.14), delete and re-run `make venv` (~90 s). Not yet done.

## 7. ollama installed, models pulled

After the macOS upgrade (now Darwin 27.2) and new Command Line Tools, ollama
0.40.16 was installed and its server was answering on `localhost:11434`.
Pulled, one after another, with plain `ollama pull` (about 4 min in all, not
the hours I had feared; the tags I guessed from memory were all right):

```
ollama pull moondream       # 1.7 GB
ollama pull gemma3:4b       # 3.4 GB
ollama pull qwen2.5vl:7b    # 6.0 GB
```

The same three commands work on any other machine with ollama running.

## Open items

- [ ] ollama installed and a model pulled (gemma3:4b, qwen2.5vl:7b, moondream:
      tags to be checked against ollama's library)
- [ ] first real run over the sample set; `stats` for time per photo
- [ ] SigLIP 2 worker: first run downloads ~400 MB of weights from Hugging Face
      (asked the user; no answer yet) and the worker is untested against a model
- [ ] write ~15 real queries and a judge TSV; `compare` the backends
- [ ] run on the Linux/NVIDIA box (the venv script's CUDA path is untested on a
      GPU) and on other architectures; record results per machine here
- [ ] decide on a score threshold for vector search
