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

## 8. First trial run (macOS machine), and what it found

Ran `classify run -limit 5` against `ollama:moondream`, `ollama:gemma3:4b` and
`ollama:qwen2.5vl:7b` over the same five photos (from the repo root, so results
landed in `./classify-results/`, not in the package). It took 20+ minutes, and
the third model never finished; I stopped it.

| model | result | time per photo |
|---|---|---|
| `gemma3:4b` | 5 of 5 good (tomato, band/stage/guitar, wine glass/candle, river/palm trees, river/ice/snow) | 16-22 s |
| `moondream` | 0 of 5 usable: the JSON-schema constraint makes it degenerate (`"tags": [":", "a", "p", "t"...`, `"roses","roses",...`) | ~20 s |
| `qwen2.5vl:7b` | 1 of 5 good (30 s); 3 timed out at the 5 min client timeout; the 5th was still generating at 570+ tokens | 30 s when it worked |

- A `llama-server` using almost no CPU is normal: `ollama ps` showed 100% GPU
  (Metal), so the work is not on the CPU. The real problem was runaway
  generation, seen in ollama's log as `n_gen` past 570 at ~7.6 tokens/s.
- Gemma 3 at ~19 s/photo is ~1.5 h for 296 photos per model.
- Moondream is dropped from the comparison as set up (too small to follow
  schema-constrained output; a plain-text prompt might do better, untried).

Fixes made (not yet re-tested; the user will resume on the Linux box):
- `OllamaVLM` now sends `num_predict` (new `MaxTokens` field, default 300) and
  `repeat_penalty` 1.15, so a model that loops is cut off in seconds, not after
  5 minutes. Greedy decoding (temperature 0) is what makes small models loop.
- `.gitignore` ignores `classify-results/` anywhere (it ignored only the
  package's copy). The tool still writes to `./classify-results` of whatever
  directory it is run from: run it from `pkg/classify`.
- Removed the trial's records from the repo root.

Models: after this, newer ones were looked up. Qwen3-VL (Ollama announced
Oct 14 2025; sizes 2b/4b/8b/30b/32b/235b; needs ollama 0.12.7+) succeeds
Qwen2.5-VL. Qwen2.5-VL's own release date was not found (recollection: late
Jan 2025, unverified). Gemma 4 exists on Ollama (tags e2b, e4b, 26b, 31b per
search results; the user saw `12b` on the library page too). To try:
`ollama pull gemma4:e4b`, `gemma4:12b`, `qwen3-vl:8b` here, and on the Linux
box (8 GiB VRAM, older CPU) `qwen3-vl:4b` and `gemma4:e4b` (~9.6 GB listed, so
it will spill from the GPU to CPU). Sizes are from guides, not verified.

## 9. Machine 2 (Linux, RTX 3070): setup and the 5-photo trial

Machine 2: Linux x86_64, RTX 3070 (8 GiB, driver CUDA 13.2), 62 GB RAM, 20
cores, Go 1.27.2, Python 3.14.4, ollama 0.40.16. Other things hold 1.5 GB or so
of VRAM (desktop, Chrome, Discord), so a model gets about 6 GB at best.

- `make sample ZIP=../../insta/instagram-*.zip`: 296 photos, as on machine 1.
- `make venv`: picked `cu132`, **torch 2.15.0+cu132, CUDA available** (the
  first real run of the CUDA path of `setup_venv.py`: it works).
- Models pulled: moondream, gemma3:4b, gemma4:e4b, qwen3-vl:4b and 8b (already
  there), and `qwen3-vl:4b-instruct` (new).
- **`qwen3-vl:4b` (the plain tag) is the thinking variant** (`ollama show`:
  renderer/parser `qwen3-vl-thinking`): with `MaxTokens` 300 it returned empty
  answers. Use `qwen3-vl:4b-instruct`.
- **SigLIP 2 needs `transformers`** (its tokenizer goes through it, plus
  sentencepiece and protobuf). Added to `requirements.txt`; the worker's weights
  came down from Hugging Face on first use.
- **A model left loaded by ollama (5 min) starves the SigLIP worker of VRAM**
  (CUDA OOM at 20 MiB). `ollama stop MODEL` first, or run them separately.

Trial (5 photos, run from `pkg/classify`, per photo after the first):

| backend | time | notes |
|---|---|---|
| `qwen3-vl:4b-instruct` | 5.6-7.1 s | longest captions, 20-22 labels, includes text in the photo ("Lucia") |
| `gemma3:4b` | 4.5-5.4 s | 17-21 labels; first photo 31 s (load) |
| `gemma4:e4b` | 3.5-4.0 s | fewest labels (13-16), most "abstract" ones (still life, healthy) |
| `siglip2` (ViT-B-16-SigLIP2, no vocab) | 14 ms | 768-d embeddings, search works: "tomato", "snowy river", "band on stage" each put the right photo first (scores 0.13-0.15; no floor yet) |

All three VLMs described all five photos correctly. That is 4-6x faster than
machine 1's Gemma 3 (16-22 s): ~25 min for the 296 photos per model.

Open: first real run over all 296; judge TSV and `compare`; a score floor.

## 10. From experiment to the site (workflow A)

The decision: results live in the gallery's DB; they get there from a machine
that has the photos pulled, not from the one that serves them. Public photos
only (unpublished ones aren't searched, so they aren't classified). Not done:
embedding results in the images' own metadata (XMP), a workflow for local files
before upload. Query embedding on the server stays the python worker for now
(`Classify.Embedder` in the config); a smaller runtime is a later, separate job
(an int8 ONNX text tower would be ~300 MB; unverified, see section 9's sizes).

The pieces:

| where | what |
|---|---|
| `gallery` `GET /gallery/photos.json?after=ID&limit=N` | public; published photos as `{id, sha256, url, width, height}` in id order, with `next` |
| `classify pull -url .../gallery -dir D` | downloads them to `D/<id>.<ext>` + `D/manifest.json`; repeatable, skips what is there |
| `classify run -dir D -b ...` | as before, over the pulled files |
| `classify export -dir D -b ... -o f.json` | the `Export` file (`export.go`): items `{id, sha256, model, caption, labels, embedding}`; embeddings are float16 in base64 |
| admin: Gallery, Classify | imports the file in the browser, 200 items per request; shows models and how many photos each has done |
| table `photo_class` + FTS5 `photo_class_fts` | one row per photo and model; the import checks the sha256 and replaces |
| `gallery/search.go` | `Searcher`: words (FTS5, bm25, caption x1 / labels x2) + vectors (in memory, cosine, floor `MinVector`), fused by reciprocal rank; every match carries a `Why` |
| admin: Gallery, Search | all photos; the score, ranks, cosine and the matched words |
| `/gallery/?q=` | published only; with `Debug` each photo shows its score and why |
| photo modal | what each model said (caption, tags) |

Tried end to end on a scratch site (`monet --load-instagram` of the 296 sample
photos, then pull, run siglip2, export, import, search): works, with the 3.7 GB
of VRAM caveat of section 9. Found while doing it:

- the vector floor of 0.08 was too low (now 0.09, from the user's own searches: "tennis", "hong kong", "china" had their strays under it): "dinosaur" still found two photos
  (cosines 0.086 and 0.081), "snowy river" found five of which two were
  plausible. To tune with the judge file.
- a worker that dies (here: CUDA OOM because a 6.6 GB ollama model was loading
  at the same time) used to leave the search without vectors until a restart;
  the searcher now drops it and tries again after 30 s, and says so in the notes.
- `ollama` and the siglip worker contend for 8 GiB of VRAM: don't run them at
  the same time (`ollama stop MODEL`).

## Open items

- [x] ollama installed and models pulled (section 7)
- [x] rerun the 5-photo trial with the fixes of section 8 (done on machine 2, section 9)
- [ ] first real run over the sample set; `stats` for time per photo
- [ ] SigLIP 2 worker: first run downloads ~400 MB of weights from Hugging Face
      (asked the user; no answer yet) and the worker is untested against a model
- [ ] write ~15 real queries and a judge TSV; `compare` the backends
- [ ] run on the Linux/NVIDIA box (the venv script's CUDA path is untested on a
      GPU) and on other architectures; record results per machine here
- [ ] decide on a score threshold for vector search

## 11. Places (pkg/geo)

Where a photo was taken is metadata, not something a model sees, so it isn't
classified: `pkg/geo` names the coordinates from GeoNames' lists of cities
(`cities1000` and the files that say what the codes in it mean), locally, with
no coordinates sent anywhere.

- `geo.Fetch(dir)` downloads the files, and makes `local-names.tsv` (each city's
  name in the first language of its country that has one, English left out and
  none for countries that are first English speaking; India, Singapore and
  Pakistan excepted) from `alternateNamesV2.zip` (205 MB, not kept; the pass
  over it took about 6 s). The data of a source with bad names is the source's:
  there are no per-country rules.
- A photo's place is the nearest city of any size, of 5000 and of 15000 people,
  once each, with the region, country and continent: `gallery/places.go`,
  table `photo_place`. Its words go in every row of the photo in
  `photo_class_fts` (a `place` column) and in a row of their own (model
  `place`) so that a photo no model has seen is found by where it was.
- Query words match whole words of captions and labels, and the start of a word
  of the place: `tokyo` finds `tokyoto`, `東京` finds `東京都`.
- The server fetches the data when it starts if it isn't in `Geo.Dir`, and
  places the photos that have no place. Uploads and imports place their
  photos as they are added. Gallery, Classify in the admin has Rescan (all the
  photos, with the data there is) and Refresh (fetch the data again, then
  rescan).
- `go run ./pkg/geo/cmd/geo -db prod.db` compares the lists on a gallery.
