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
| admin: Gallery, Classify | the file is dropped on the same drop area as the other uploads (`POST /admin/gallery/classify/`, up to 256 MiB, one request); shows models and how many photos each has done |
| table `photo_class` + FTS5 `photo_class_fts` | one row per photo and model; the import checks the sha256 and replaces |
| `gallery/search.go` | `Searcher`: words (FTS5, bm25, caption x1 / labels x2) + vectors (in memory, cosine, floor `MinVector`), fused by reciprocal rank; every match carries a `Why` |
| admin: Gallery, Search | all photos; the score, ranks, cosine and the matched words |
| `/gallery/?q=` | published only; with `Debug` each photo shows its score and why |
| photo modal | what each model said (caption, tags) |

Tried end to end on a scratch site (`monet --load-instagram` of the 296 sample
photos, then pull, run siglip2, export, import, search): works, with the 3.7 GB
of VRAM caveat of section 9. Found while doing it:

- the vector floor of 0.08 was too low: "dinosaur" still found two photos
  (cosines 0.086 and 0.081), "snowy river" found five of which two were
  plausible. It is 0.09 now, see section 12.
- a worker that dies (here: CUDA OOM because a 6.6 GB ollama model was loading
  at the same time) used to leave the search without vectors until a restart;
  the searcher now drops it and tries again after 30 s, and says so in the notes.
- `ollama` and the siglip worker contend for 8 GiB of VRAM: don't run them at
  the same time (`ollama stop MODEL`).

### 10a. Learned running it on the real photos (Linux machine, 296 photos)

- **The SigLIP 2 weights are 1.4 GB** (the cached file), fp32, not the 400 MB
  written in section 9's first version. The worker keeps both towers loaded:
  ~1.7 GB of VRAM idle (1.4 GB of weights + ~0.3 GB of CUDA context), 3.7 GB at
  the peak while embedding images (activations, and the allocator keeps what it
  freed). Ways to shrink it, none done: `precision=fp16` (half; re-embed
  everything after, so stored and query vectors agree), a text-only worker for
  the server (drops the image tower, ~40% of the parameters), or the text tower
  on the CPU (about a second a query). The server starts the worker in the
  background when it starts listening (`App.WarmSearch`), so the first search
  doesn't wait ~5 s for it. This is still PyTorch; there is no ONNX anywhere in
  the code.
- **ollama answers were cut off** on a text heavy photo (a flyer: the model kept
  listing the dates and product names it could read) at `MaxTokens` 300, which
  left unparseable JSON. A repair that closed the JSON was written and removed
  as brittle. What stayed: the schema asks for at most 25 tags (`maxItems`,
  which ollama enforces when it generates, so the list can't run on and the
  JSON always closes), `MaxTokens` is 600 (still a guard against a model
  looping at temperature 0), and `done_reason == "length"` is an error that
  says so; `run` retries photos with errors. Tried on that photo: gemma4:e4b
  (3.3 s, 12 labels), gemma3:4b (14.7 s, 25), qwen3-vl:4b-instruct (19.6 s, 25).
- `classify run` prints what a backend produced for each photo ("embedding 768
  dims", "13 labels, caption 212 chars"), so an embedder doesn't look like it
  found nothing.
- **Which VLM:** the user found qwen3-vl:4b-instruct and gemma similar in
  quality and time, and qwen's output longer, so likely to search better; the
  user leaned to running qwen alone. Not compared with a judge file. Watch for
  longer output bringing false word matches (a generic tag like "city" on every
  skyline would bring back the "hong kong" problem through the words).
- **The SigLIP floor.** SigLIP 2 over-identifies: it always has a nearest photo.
  The user's searches: below ~0.09 a photo with no word match was nearly always
  unrelated ("tennis" gave a beach volleyball; "hong kong" gave skylines that
  weren't there; "china" included a Hong Kong photo at 0.081 and many of India
  at higher cosines). The user accepts losing the odd related photo to keep out
  2-3x as many unrelated ones, so `DefaultMinVector` is 0.09 (config:
  `Classify.MinVector`). A photo with a word match stays at any cosine.
- **Other embedding models** (a web search; the sources were vendor-adjacent
  roundups that disagree, no MetaCLIP 2 or Perception Encoder data found; treat
  as pointers): SigLIP 2 is the usual open default; a bigger one
  (`ViT-SO400M-16-SigLIP2-384`) is the easiest trial (the worker takes `--model`,
  but its text tower is too big for the 1 GiB server); Jina CLIP v2 and Nomic
  Embed Vision (open weights); Qwen3-VL-2B as an embedder (reported strong, far
  too big for the server, one secondary source); Gemini Embedding 2 and Cohere
  Embed v4 are hosted APIs. **Cosines don't carry over between models**: each
  model needs its own floor.
- **Not built, discussed:** the floor tunable in the admin (a per-search `?min=`
  field is the cheap start; a stored setting needs a table the gallery doesn't
  have); special search terms such as `year:2022` or `camera:Canon`, meant as
  something the UI links to (click an EXIF value), later; date search stays the
  admin's date dropdown.

## 11. Places (pkg/geo)

Where a photo was taken is metadata, not something a model sees, so it isn't
classified: `pkg/geo` names the coordinates, locally, with no coordinates sent
anywhere. The search then treats where a photo was taken as fact.

### Decisions

| decision | why | what else was possible |
|---|---|---|
| names come from **GeoNames** city lists, looked up offline | a public geocoder (Nominatim) allows about 1 request a second and no heavy use, and would be sent the coordinates of every photo, home included; the data is small | Nominatim: street level detail; could be added behind the same job |
| the place is the **nearest city of any size, of 5000 and of 15000 people**, each once, plus region, country, continent | compared on the 158 photos of `prod.db` with `go run ./pkg/geo/cmd/geo`: the lists agree on the city for 97 of 158; the small list gives neighbourhoods ("Shibuya", "Mid Levels") and the large the town ("Itō" for "Inatori"), and people search by either | one list; every city within N km (rejected: no example needed it, "Hoboken" is tagged Hoboken and not New York) |
| **no per-country rules**: trust the data | the user's call: a source with bad names is the source's, and it may get better; Refresh re-reads it | rules by country and script (a Taiwan and a Vietnam rule were written and removed) |
| names in the **local language** are kept for the city, region and country, and the English ones too | to show "Bangkok, Thailand" with กรุงเทพมหานคร as flavour, and to search either; the UX (a hover or option, not prominent) is later | none stored |
| the **English first** rule: no local name for a country whose first language is English (US, UK, Australia...), except India, Singapore and Pakistan | otherwise New York gets "Nueva York" (es-US) | an allowlist is a guess; edit `localDespiteEnglish` in `pkg/geo/local.go` |
| places are **looked up on the server**, as photos are added | the lookup takes about a millisecond and the data is a few tens of MB in memory | in classify (rejected: it isn't pixels, it needs no GPU and no pulled copy, it is deterministic) |

### Data (https://download.geonames.org/export/dump/)

- `cities1000.zip` 11.1 MB (172k cities; `cities5000` 5.7 MB and `cities15000`
  3.4 MB are subsets, so one list serves all three by population),
  `countryInfo.txt` 32 KB (names, continent, languages, geonameid),
  `admin1CodesASCII.txt` 152 KB (region names and geonameids).
- **`alternateNamesV2.zip` is 205 MB** (787 MB unzipped, ~13M rows: every name
  of every feature in every language, with the language and a preferred flag).
  It is the only place a name's language is said: the cities files list names
  with no language (the Tokyo row has 東京, 东京 and 도쿄 together). It is read
  once to make `local-names.tsv` (40,356 names; about 6 s) and is not kept.
  GeoNames also has it split by country under `export/dump/alternatenames/`
  (Japan 3.8 MB, Thailand 6.2 MB): a way to fetch less, not used.
- A name's language: the first of the country's languages (from `countryInfo`,
  English left out) that has a name for it, preferring the preferred flag;
  historic and colloquial names skipped, short names kept (`東京` is flagged
  short); a name equal to the English one is not a local name.

### How it works

- `geo.Fetch(dir)` downloads the files and makes `local-names.tsv`;
  `geo.Open(dir)` loads them (172k cities in ~250 ms); `Index.Around(lat, lng,
  0, 5000, 15000)` is the nearest city for each size, once each.
- `gallery/places.go`: table `photo_place` (city, region, country, code,
  continent, `near`: the other cities, local names of city, region and country,
  km). Its words go in **every row of the photo in `photo_class_fts`** (a
  `place` column) **and a row of their own** (model `place`): the first so
  that "tokyo sunset" finds a caption with the one and a place with the other,
  the second so a photo that no model has seen is found by where it was.
  `reindex` rebuilds a photo's rows from `photo_class` and `photo_place`.
- The server (`Geo.Dir` in the config, `pkg/geo/data` in `dev.cfg.json`)
  fetches the data when it starts if it isn't there (the 205 MB download takes
  a while) and places photos that have no place. Uploads and imports place the
  photo as it is added; a photo added before the data is loaded is placed by
  the next scan. Gallery, Classify has **Rescan** (every photo again) and
  **Refresh** (fetch the data again, then rescan, in the background).
  Neither is periodic yet.
- Migrations: `photo_place` and the `place` column of the FTS table (rebuilt
  from `photo_class`), then `region_local` and `country_local`. Photos placed
  before the last one have those empty until Rescan.
- Measured on 294 photos (158 with GPS): data loaded in 240-270 ms, all
  placed in about 0.6 s.

### Known gaps in the data (not fixed, by decision)

- Many places have no local name (neighbourhoods and villages: Saiyad-ul-ajaib
  in Delhi, Mid Levels, Sha Tin, French and Portuguese villages). 88 of the 158
  photos have at least one local name in the tuple.
- Singapore gets Tamil, there being no Chinese or Malay name. Taiwan gets the
  region in Traditional and the country in Simplified characters
  (`台北市, 台灣, 台湾`). Keelung gets `Ke-lâng-chhī [nan]`: a romanization in
  Taiwanese Hokkien (`nan`, a Chinese language brought by Han settlers, not an
  indigenous Taiwanese one), the only name GeoNames has for it in a language of
  Taiwan. A Taiwan rule (`zh-tw`, `zh`, Han script only) and a Vietnam rule
  (`vi`, Latin script only, so as not to get the old Han names) fixed these
  and were removed when the user decided to trust the data; they are a table
  of about ten lines if wanted again.
- For a country with several languages, the first listed that has a name wins
  (India: Hindi before Malayalam), so Kerala shows Hindi where there is a name
  in both.
- Region and country names are not in any language but English and the
  country's; "Tokyo" the region is `東京都`, so a search for `東京` works only
  by prefix.

## 12. Search and ranking

`gallery/search.go`. A query is searched two ways and the lists are fused.

- **Words** (FTS5, `porter unicode61`): every word must match, each as a whole
  word of a caption or a label, or as the **start of a word of the place**
  (`("w" OR place : "w" *)`): `tokyo` finds `tokyoto`, `東京` finds `東京都`,
  `china` finds `Chinatown`. Nobody searching `ind` is hurt by the Taj Mahal.
  Substring and trigram matching were rejected: `京都` would match `東京都`,
  queries under 3 characters fail, and stemming is lost. bm25 weights:
  caption 1, labels 2, place 2. All the words must be in one row, which is why
  the place words are in every row.
- **Vectors:** the query is embedded by the same model as the photos
  (`Classify.Embedder`, a python worker), cosine against every photo's
  embedding, kept at or above `MinVector` (0.09 for SigLIP 2).
- **Fusion:** reciprocal rank fusion, `1/(60+rank)` from each list a photo is
  in, so only the order in each list counts, not bm25 or the cosine; ties by id.
  The best a photo can get from the two is ~2/61 = 0.033.
- **Place boost:** a photo whose place matched the query gets **+0.05** (more
  than the best from words and vectors together), so a photo taken in Tokyo
  comes before the sakura in Central Park that only feels like Tokyo. The user's
  reasoning: places are accurate and reflect intent ("china" is photos in
  China, not dishes). Prefix and whole word matches count the same, so
  Chinatown in New York is boosted like Guangzhou; an exact match could earn
  more if that annoys. Among place matches the order is the vector and bm25 rank,
  which for place only rows is close to arbitrary (bm25 depends on how short the
  place text is); a date tie-break would be better and isn't built.
- **Dev display** (`Debug`, and always in the admin): score, `words #n`,
  `vector #n, cosine c`, `place`, and per matching row the words with `[matches]`.
  A model's row that matched only in the place is hidden (the `place` row says
  it), and models that gave only an embedding have no row at all.
- Searching published photos (`/gallery/?q=`) and all photos (admin) share it.

## 12a. The query embedder without PyTorch (ONNX, hugot)

Only the *text* tower is needed on the server (the photos' embeddings come from
the classify run), so it is exported alone and run by hugot (v0.8.6) over ONNX
Runtime, CPU. `make -C pkg/classify onnx onnxruntime`, build with `--tags=ORT`
(the Makefile does), and set `Classify.Embedder` to
`siglip2=onnx:pkg/classify/onnx/siglip2@pkg/classify/onnx/lib`. The `worker:`
spec still works, and is what makes the photos' embeddings.

What was measured (RTX 3070 box, CPU only for ONNX; 5 test queries incl. 東京):

| | PyTorch worker | ONNX fp32 | ONNX, word table int8 (used) |
|---|---|---|---|
| model on disk | 1.4 GB | 1.1 GB | 540 MB |
| ready (load + first query) | ~5 s+ | ~1.3 s | **~1.0 s** |
| a query | | 75 ms (python) | **~19 ms** |
| resident | 1.7 GB VRAM + python | 880 MB | **550 MB** (server total 600 MB) |
| cosine to the PyTorch embedding | 1 | 1.0000 | 0.998-0.9999 |

Decisions and traps:
- 3/4 of the text tower is the 256k x 768 word table, so int8 on the *Gather
  only* is almost free (cos 0.998+). int8 on the MatMuls too is not: cos
  0.93-0.98, which moves scores by more than the 0.09 floor's margin. fp32
  matmuls stay.
- The weights are saved as external data (`model.onnx.data`): ORT maps them
  instead of reading the file in and copying (880 MB -> 550 MB resident).
- SigLIP pools the **last position** of a text padded to 64. hugot pads to the
  longest text, so the model pads itself (`F.pad` in the exported graph).
- hugot sizes a batch by `attention_mask`; a model without that input gets every
  text cut to one token (cos 0.95 for "a photo", 0.59 for longer queries). The
  export has the input, and uses it (`ids * mask`) so it is not pruned.
- The query is cleaned as open_clip does (`canonicalize`: lower case, ASCII
  punctuation out, `_` to space), `cleanText` in `embed_clean.go`. ftfy and html
  unescape, which open_clip also runs first, are not done.
- hugot's tokenizer is pure Go (gomlx/go-huggingface); the ids matched
  open_clip's on the test queries. Not checked: every script, long queries.
- hugot's pure-Go (GoMLX) backend failed to compile the graph ("Node is nil");
  ORT it is. That needs `libonnxruntime.so` at run time (not at build time).
- go.mod went to `go 1.27.0` (hugot's requirement) and gained gomlx and friends.
- On a small server the model is loaded on demand: `Classify.IdleMinutes` (0 =
  keep it) lets go of the embedder that long after its last use, and a look at
  any gallery list page (`/gallery/`, its pages, a search) starts it, so that it
  is in by the time the next search is made (~1 s to load). Measured: server
  32 MB at start, 570 MB after a gallery page, 238 MB after the unload (not all
  of it comes back: ~200 MB stays, probably the Go side's tokenizer and the
  allocator; not investigated), 596 MB again after a search.
- Re-export when changing model: the tokenizer, `context_length` and the pooling
  are model specific (this was only checked for ViT-B-16-SigLIP2).

## 13. Decision log (what to re-examine, and when)

| question | now | re-examine when |
|---|---|---|
| vector floor | 0.09, by eye, SigLIP 2 B/16 | a different embedding model (cosines differ: new floor), or a judge file exists: use `classify compare` |
| which VLM for words | undecided; qwen3-vl:4b-instruct likely | new VLMs; write ~15 real queries and a judge TSV, `compare` |
| embedding model | SigLIP 2 B/16 (PyTorch worker for photos; ONNX for queries, 12a) | SO400M or Jina CLIP v2 / Nomic Embed Vision to compare; the server's 1 GiB means an int8 ONNX text tower or a text-only worker |
| fusion | RRF k=60 + place boost 0.05 | if results look wrong: weights between words and vectors, the cosine as a score, a date tie-break |
| GeoNames lists | cities1000 serves all three sizes | GeoNames improves (Refresh), or street level detail is wanted (Nominatim) |
| local names | first country language with a name | the data gaps above, or a UX that shows them (the user wants them as a hover or option) |
| prefix matching on places | on, whole word counts the same | complaints about Chinatown-like cases |
| classification in image metadata (XMP) and workflow B (classify before upload) | not done, deferred by the user | when classifying new uploads on the server matters |
| Rescan and Refresh | by hand in the admin | when it is worth a periodic job |

### Commands

```
# places: compare the three lists on a gallery's photos, with local names
make -C pkg/geo data                        # the GeoNames files (not the 205 MB one)
go run ./pkg/geo/cmd/geo -db prod.db        # needs data/alternateNamesV2.zip for local names
                                            # (the server fetches and deletes it itself)

# classification, on a running gallery
go build -o classify ./pkg/classify/cmd/classify
./classify pull -url http://localhost:7001/gallery -dir pulled
./classify run -dir pulled -out results -b "siglip2=worker:pkg/classify/.venv/bin/python pkg/classify/scripts/worker_openclip.py --model ViT-B-16-SigLIP2 --pretrained webli"
./classify run -dir pulled -out results -b qwen3vl=ollama:qwen3-vl:4b-instruct    # one ollama model at a time; ollama stop MODEL after
./classify export -dir pulled -out results -b <the same -b specs> -o classifications.json
# then drop the file on Gallery, Classify in the admin
```

## Open items

- [x] ollama installed and models pulled (section 7)
- [x] rerun the 5-photo trial with the fixes of section 8 (done on machine 2, section 9)
- [x] first real run over the 296 photos (siglip2 12 s; gemma4:e4b about 25 min)
- [x] SigLIP 2 worker tested against a model (1.4 GB download; section 10a)
- [x] a vector floor (0.09, by eye)
- [ ] write ~15 real queries and a judge TSV; `compare` the backends and settle the VLM
- [ ] run qwen3-vl:4b-instruct (and gemma3:4b) over all 296 photos
- [ ] the floor tunable in the admin (per-search field first)
- [x] a smaller query embedder for the server: int8 ONNX, section 12a. Left: the *photo* side (classify runs) is still PyTorch; try the image tower as ONNX too, and compare search results of the two embedders on the real photos
- [ ] try a bigger SigLIP 2, Jina CLIP v2, Nomic Embed Vision (new floor each)
- [ ] a place line on the photo pages (and the local names, as a hover); a date tie-break for place matches
- [ ] a periodic or on demand Rescan and Refresh beyond the admin buttons
- [ ] show the local tuple (`渋谷, 東京都, 日本`) anywhere but the geo tool; it is stored and searched
- [ ] the admin Classify page's buttons and status are only tested by Go tests, not in a browser
- [ ] the first-start fetch of the data by the server, and its memory on the 1 GiB server, unmeasured
- [ ] special search terms (`year:2022`, `camera:Canon`) as UI links
- [ ] workflow B and XMP metadata
