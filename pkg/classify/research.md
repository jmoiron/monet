# Searching photos by what is in them, locally

The goal: type "pizza", get the photos with pizza in them, with the photos never
leaving this machine. This is the research, the tradeoffs, and how to use
`pkg/classify` to find out which approach works best on *your* photos.

**What this is and isn't.** The research below is from reading, not from running
models on your corpus: I have not downloaded any model or benchmarked anything
here. Where a number comes from a source it is linked; where it is from my
memory it says so. Treat the numbers as orders of magnitude, and use the tool to
get real ones. The tool and its tests are real (`go test ./pkg/classify/...`);
the Python worker has only been syntax checked (torch is now installed, but it
has not been run against a model).

## Short version

There are really two different things people mean by "classification" for
search, and they behave very differently:

| | embeddings (CLIP / SigLIP 2) | words (captions + tags from a vision-language model) |
|---|---|---|
| output | one vector per photo (512-1152 floats) | a sentence and 10-20 tags |
| search | embed the query with the same model, nearest photos | ordinary text search (SQLite FTS5, which monet already builds with) |
| vocabulary | open: any query | whatever the model happened to say |
| cost per photo | tens of ms | seconds |
| you can read why it matched | no | yes |
| has a "no results" | no (there is always a nearest photo) | yes |
| good at | "pizza", "beach at sunset", "dog" | the above, plus text in photos, counts, details, and facets you can show |

**Recommendation:** try embeddings first. They are the approach Immich uses for
its search, they are cheap, and "pizza" is exactly what they are best at. If the
results feel mushy (no threshold, can't see why, misses small things), add a
vision-language model pass for captions and tags and search those with FTS5 as
well. They complement each other, and nothing stops you running both. The
package is built so that this is an experiment and not a commitment.

## What the output looks like, and what it costs

### 1. Embeddings: CLIP, SigLIP, SigLIP 2

A "dual encoder" maps images and text into one vector space. Index time: run each
photo through the image encoder once, store the vector. Query time: run the query
text through the text encoder (a small model, tens of ms), then rank photos by
cosine similarity. No labels, no vocabulary, so "red door" and "pizza" and "Japan
temple" all work the same way.

- **Model families.** OpenAI CLIP (2021) is the old baseline. SigLIP replaced its
  softmax loss with a pairwise sigmoid loss, and SigLIP 2 (Feb 2025) improved on
  it again in zero-shot classification and retrieval across sizes; the paper
  releases four sizes, ViT-B (86M parameters), L (303M), So400m (400M) and g (1B).
  One review quotes ImageNet zero-shot for B/16 at 256px rising from 76.7% to
  79.1% over SigLIP ([SigLIP 2 paper](https://arxiv.org/abs/2502.14786);
  [summary](https://www.emergentmind.com/topics/siglip-siglip2)). I did not find a
  clean SigLIP 2 vs CLIP head to head, only comparisons against SigLIP.
- **What Immich does and measured.** Immich (self-hosted photo library) searches
  with exactly this. Its docs have a table of models by retrieval recall,
  latency and memory. As I read it: `ViT-B-16-SigLIP2__webli` is a good small
  default (about 84.9% recall, a few ms, about 3 GB), and
  `ViT-SO400M-16-SigLIP2-384__webli` is the best English model (about 86.0%, about
  57 ms, about 3.9 GB). Both are marked Pareto optimal. SigLIP 2 models also handle
  queries in other languages. Check the table for the current numbers
  ([Immich smart search docs](https://docs.immich.app/features/smart-search)).
  Note that the gap between the small and large model is a point or two of recall:
  the small model is probably enough.
- **Size of the output** (dimensions from memory): 512 floats for CLIP B/32, 768
  for SigLIP 2 B/16, 1152 for So400m. At 4 bytes each, 3 KB per photo for the
  middle one: 10,000 photos is 30 MB. The tool's results file stores them as JSON
  text, about 8 KB per photo; the real store would be a BLOB.
- **Searching them.** At personal-gallery scale, don't use a vector database.
  Load the vectors into memory and compute every dot product: for 10k x 768 that
  is on the order of a few milliseconds. Published numbers for brute force:
  sqlite-vec on an M1 Pro, 100k x 384, a full scan took 68 ms
  ([Bambini](https://marcobambini.substack.com/p/the-state-of-vector-search-in-sqlite));
  a Go benchmark write-up says exact scan is fine below ~50k vectors and gets slow
  by 1M x 512 (~400 ms)
  ([forum post](https://forum.golangbridge.org/t/benchmarking-a-pure-go-vector-index-against-hnswlib-and-sqlite-vec-every-number-i-got-wrong-first/42173)).
  `pkg/classify`'s `Index.Vector` is exactly this loop.
- **Weak spots** (from memory and general experience, which is why the tool has a
  judging mode): counting ("three dogs"), negation ("no people"), spatial
  relations, reading text in the photo, and fine details of a large scene.
  There is **no natural cutoff**: every photo has a nearest, so a query for
  something you have no photos of still returns ten photos. You need a score
  threshold (tune it on your corpus; the sigmoid models like SigLIP give a more
  meaningful absolute score than CLIP's softmax) or a "top N only" UI.
- **Runtime.** Python (`open_clip`, `transformers`) is the path of least
  resistance, with MPS acceleration on Apple Silicon. Immich ships ONNX exports
  of these models (for example on Hugging Face under `immich-app/`) which is the
  way to run them without torch.

### 2. Vision-language models: captions and tags

A multimodal LLM looks at the photo and writes about it. Asked for a caption plus
tags as JSON, the output is a few hundred bytes of text that goes straight into
FTS5, and you can show it, edit it, and facet on it.

- **Models that run in ollama** (per ollama's library, via the sources below):
  Qwen2.5-VL and the newer Qwen3-VL, Gemma 3, MiniCPM-V, Moondream, LLaVA,
  Llama 3.2 Vision, Granite Vision. PhotoPrism's docs, which have thought about
  this for a photo library specifically, suggest Gemma 3 for captions and
  Qwen3-VL for labels, and note that Qwen3-VL may do better on Apple Silicon
  ([PhotoPrism model comparison](https://docs.photoprism.app/developer-guide/vision/model-comparison/),
  [Ollama models](https://docs.photoprism.app/user-guide/ai/ollama-models/)).
- **Speed is the cost.** The only datapoint I found: Gemma 3 4B through ollama
  at about 4 seconds per photo on a Mac mini M4 Pro
  ([a user's write-up](https://p.simianer.de/blog/ollama-generated-captions-with-photoprism-vision)),
  and Moondream (about 1.6B, about 1.5 GB) is the small fast option that will
  miss detail ([guide](https://insiderllm.com/guides/vision-models-locally/)).
  This machine is an M3 with 24 GB, so expect the same order or slower. At 4 s per
  photo, 10,000 photos is about 11 hours: fine as a one-time background job and
  for new uploads, not fine as something you re-run casually. **That is the main
  reason to compare models on a 200-photo sample first.** The vision encoder, not
  the text generation, is usually the bottleneck for short outputs, so image
  size (`MaxEdge`, default 896 px) is the speed knob.
- **Quality caveats.** They hallucinate details; they are not deterministic across
  models or prompts (temperature is pinned to 0 here); "thinking" models can spend
  their whole budget thinking and return a truncated answer, which is why the
  backend sends `think: false`
  ([PhotoPrism troubleshooting](https://docs.photoprism.app/developer-guide/vision/caption-generation/)).
  The prompt is the biggest lever on label quality, and it is a field you can
  change (`OllamaVLM.Prompt`).
- **Strengths.** Text in photos (signs, menus), scene and setting, specific
  objects, and an answer you can read when a search is wrong.

### 3. Fixed-vocabulary classifiers

ImageNet-1k models (ResNet, EfficientNet, ConvNeXt...) and Google's Open Images
classifiers give you labels from a fixed list, for a few ms a photo, with scores
and a natural cutoff. "pizza" is an ImageNet class. But 1,000 classes is mostly
dog breeds and objects, and "Japan", "street", "sunset" or "abstract" are not
among them, which is the kind of thing you said you want to browse by. I would
only use one as a cheap first pass. (From memory.)

### 4. Open-set taggers: RAM / RAM++

"Recognize Anything" models tag a photo from a large vocabulary (the original
covers 6,400+ tags; RAM++ adds open-set recognition of tags it wasn't trained on)
and beat CLIP at tagging on OpenImages and ImageNet by several mAP points
([RAM++ paper](https://arxiv.org/abs/2310.15200)). They sit between 1 and 2: cheap
like a classifier, readable like captions, a bigger vocabulary than ImageNet.
They are PyTorch research code (`xinyu1205/recognize-anything`), so this would
be run through the worker protocol. I found no inference speed numbers. Worth a
try if you want readable labels without paying VLM prices.

### 5. OCR, as a complement

Text in photos (signs, menus, boards) is poorly served by every approach above
except the larger VLMs. `tesseract` is installed on this machine
(`/opt/homebrew/bin/tesseract`). It is cheap, and its output goes into the same
FTS5 index. It isn't wired into `pkg/classify`; a `worker:` that shells out to it
would be ten lines. Suggested after the first experiment, not before.

## Getting a model to run from Go

The honest summary is that the Go ML ecosystem is thin, which is why the package
talks to other things.

| way | notes |
|---|---|
| **ollama over HTTP** (`ollama:` backend) | no Go ML dependency at all; any ollama vision model is a flag; GPU (Metal) acceleration is ollama's problem. Needs ollama running. Good for VLMs; it can't give you CLIP-style embeddings. |
| **a worker process** (`worker:` backend) | any Python (or other) program that speaks line-delimited JSON on stdin/stdout. Loads the model once. How to try SigLIP 2, RAM++, an ONNX model, tesseract. Shipping it means shipping Python, which for a personal site is fine as a sidecar. |
| **[hugot](https://pkg.go.dev/github.com/knights-analytics/hugot)** (Go library over ONNX) | a pure Go backend (no cgo; for small models, small batches), an ONNX Runtime backend (`ORT` build tag, fastest on CPU, needs `libonnxruntime`) and a GoMLX/XLA backend. Pipelines include embeddings and image classification. I did **not** confirm it has a CLIP/SigLIP dual-encoder image+text pipeline, which is what search needs; check before committing. |
| **onnxruntime via cgo** (`onnxruntime_go`, from memory) | what you'd use to embed CLIP in-process: you do preprocessing (resize, normalise) and tokenisation yourself. The most work, the best end state: one binary plus a shared library. Not verified here. |
| **reuse Immich's ML container** | its `/predict` endpoint takes an image or text and returns a CLIP embedding, with model names like `ViT-B-16-SigLIP2__webli`. The request format isn't documented anywhere I could find and has changed between versions, so it would mean reading `machine-learning/app/main.py` for your version ([mirror](https://ark.sudovanilla.org/Infrastructure/immich/src/branch/renovate/flutter/machine-learning/app/main.py)). Tempting, fragile. |

Python here is 3.14, and PyTorch supports 3.10-3.14. `make -C pkg/classify venv`
makes `pkg/classify/.venv` with the right PyTorch for the machine
(`scripts/setup_venv.py`, which reads the builds PyTorch has published at
download.pytorch.org/whl rather than keeping a table: Apple GPU on a Mac; on
Linux or Windows the newest CUDA build the driver allows, ROCm, or CPU).

## Using `pkg/classify`

Build once: `go build -o /tmp/classify ./pkg/classify/cmd/classify`. A backend is
a spec string; each run's results are one JSONL file in `-out` named after it.

```sh
# a sample of photos: the ~300 in the instagram export (make -C pkg/classify
# sample ZIP=...). Instagram's files are already the right way up; for others,
# use the gallery's own thumbnails, because the tool does not apply EXIF
# orientation.
SAMPLE=pkg/classify/sample/instagram

# words, from a VLM (ollama pull qwen2.5vl:7b gemma3:4b moondream first;
# check the exact tags in ollama's library)
classify run -dir $SAMPLE -limit 200 \
    -b ollama:qwen2.5vl:7b -b ollama:gemma3:4b -b ollama:moondream

# embeddings, from SigLIP 2, via the worker (`make -C pkg/classify venv` first)
classify run -dir $SAMPLE -limit 200 \
    -b "siglip2=worker:pkg/classify/.venv/bin/python pkg/classify/scripts/worker_openclip.py --model ViT-B-16-SigLIP2 --vocab words.txt"

classify stats   -b ... -b ...          # time per photo, labels per photo, bytes
classify search  -b ollama:gemma3:4b -q pizza
classify compare -b A -b B -q pizza,beach,"red door"
classify compare -b A -b B -judge judged.tsv -n 10    # precision / recall
```

- `run` skips photos already done, so it can be interrupted and resumed, and run
  again over more photos. Errors are recorded and retried next time.
- `search` and `compare` use the **vector** search when a backend produced
  embeddings (starting the worker again to embed the query) and the **lexical**
  search otherwise: every word of the query has to be in the photo's labels or
  caption; a label counts more than the caption; plurals match.
- To judge: write a TSV of `query<TAB>path of a photo that should match`, a few
  queries and a few photos each is enough to see differences. `compare` marks
  each hit `+` or `-` and prints mean precision and recall at N. The best use of
  an afternoon is probably to write down 15 queries you would really type.
- Adding a backend is implementing `Classifier` (and `TextEmbedder` if it has a
  text side). Adding a model to try is, in most cases, no code at all.

What to look at in the results, beyond precision: how often the *top* hit is
right; whether queries you have no photos for return junk (for embeddings) or
nothing; whether the VLM's tags are things you would actually type; the time per
photo times your library size.

## If this goes into monet

Not built; this is the shape I'd start with once an experiment has picked.

- `photo_label (photo_id, source, name, score)`, a `caption` on the photo, and an
  FTS5 table over caption and labels, which fits how blog search already works.
  `photo_embedding (photo_id, model, dim, vec BLOB)`, keyed by model so that
  changing models means re-running and not migrating.
- Classification runs as a background job after upload (the upload handler
  already serialises the heavy work, and `importjob.go` is a pattern for a long
  job with progress), so an upload never waits on a model.
- Search: embed the query, brute-force the vectors in memory (loaded at start,
  updated on upload), union with FTS5 hits, with a score floor. Public search
  should only see published photos.
- Content-addressed photo IDs help here: the same bytes are the same photo, so
  results for a photo can be cached by the ID or the `sha256`.

## Unverified, and open questions

- Nothing here has been timed on this machine or scored on your photos. That is
  what the tool is for.
- Whether hugot can do CLIP/SigLIP search end to end. Whether `onnxruntime_go`
  is a good fit (unchecked).
- Immich's table numbers are as I read a secondary summary of it; its memory
  column in particular I can't interpret from the summary. Read the page.
- The exact current ollama tags for the vision models (`qwen2.5vl`,
  `qwen3-vl`, `gemma3`, `moondream`, `minicpm-v`) and their sizes.
- Licences: model weights have their own (some SigLIP, Qwen and Gemma
  variants have terms); fine for a personal site, check before anything else.
- Privacy is why this is local, but note the *tool* also never calls out: the
  only network is to your own ollama.
- Open question for you: are the geographic collections (Japan, Portugal) best
  served by this at all? They are already given by GPS and by you putting photos
  in them. Search matters most for the topic ones (abstract, buildings, food),
  which is where an embedding model is strongest.

## Sources

- [SigLIP 2 paper (arXiv 2502.14786)](https://arxiv.org/abs/2502.14786) and a
  [summary](https://www.emergentmind.com/topics/siglip-siglip2)
- [Immich smart search docs](https://docs.immich.app/features/smart-search)
- [RAM++ paper (arXiv 2310.15200)](https://arxiv.org/abs/2310.15200)
- [PhotoPrism vision model comparison](https://docs.photoprism.app/developer-guide/vision/model-comparison/),
  [Ollama models](https://docs.photoprism.app/user-guide/ai/ollama-models/),
  [caption generation](https://docs.photoprism.app/developer-guide/vision/caption-generation/)
- [Gemma 3 4B captions with PhotoPrism on an M4 Pro](https://p.simianer.de/blog/ollama-generated-captions-with-photoprism-vision)
- [Local vision models guide](https://insiderllm.com/guides/vision-models-locally/)
- [hugot](https://pkg.go.dev/github.com/knights-analytics/hugot)
- [The state of vector search in SQLite](https://marcobambini.substack.com/p/the-state-of-vector-search-in-sqlite)
- [Go vector index benchmark](https://forum.golangbridge.org/t/benchmarking-a-pure-go-vector-index-against-hnswlib-and-sqlite-vec-every-number-i-got-wrong-first/42173)
- [Immich machine-learning `main.py` (mirror)](https://ark.sudovanilla.org/Infrastructure/immich/src/branch/renovate/flutter/machine-learning/app/main.py)
