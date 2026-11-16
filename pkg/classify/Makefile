# The python side of classify: a virtualenv with PyTorch (the right build for
# this machine) and what the workers in scripts/ need.
.PHONY: venv venv-plan sample

venv:
	python3 scripts/setup_venv.py

# what `make venv` would do, without doing it
venv-plan:
	python3 scripts/setup_venv.py --dry-run

# the photos of an instagram export, to try the classifiers on: make sample ZIP=path/to.zip
sample:
	mkdir -p sample/instagram
	unzip -q -o $(ZIP) 'media/*' -x '*.mp4' -d sample/instagram

# Embedding search queries without PyTorch: the model's text tower as ONNX, run
# by hugot and ONNX Runtime (build with --tags=ORT; see the onnx: spec).
#   make onnx            the model, in onnx/siglip2 (needs `make venv`)
#   make onnxruntime     libonnxruntime.so, in onnx/lib
# then Classify.Embedder = "siglip2=onnx:pkg/classify/onnx/siglip2@pkg/classify/onnx/lib"
ORT_VERSION ?= 1.31.1
.PHONY: onnx onnxruntime

onnx:
	.venv/bin/pip install -q onnx onnxscript onnxruntime
	.venv/bin/python scripts/export_text_onnx.py --model ViT-B-16-SigLIP2 --pretrained webli --out onnx/siglip2

# the library is per platform (a .so on linux, a .dylib on macOS; x64 or arm64),
# and the model beside it is not. Only linux-x64 has been run; the others are
# the release files that exist for 1.31.1 (no Intel mac).
ORT_OS_ARCH := $(shell uname -s | sed 's/Linux/linux/; s/Darwin/osx/')-$(shell uname -m | sed 's/x86_64/x64/; s/aarch64/aarch64/')
ifeq ($(ORT_OS_ARCH),osx-x64)
  $(warning onnxruntime has no Intel mac release; use the worker: embedder there)
endif
ifeq ($(ORT_OS_ARCH),osx-arm64)
  ORT_EXT := dylib
else
  ORT_EXT := so
endif

onnxruntime:
	mkdir -p onnx/lib
	curl -fsSL https://github.com/microsoft/onnxruntime/releases/download/v$(ORT_VERSION)/onnxruntime-$(ORT_OS_ARCH)-$(ORT_VERSION).tgz \
		| tar -xz -C onnx/lib --strip-components=2 --wildcards '*/lib/libonnxruntime*'
	ln -sf $$(cd onnx/lib && ls libonnxruntime*.$(ORT_EXT)* | grep -v "^libonnxruntime.$(ORT_EXT)$$" | head -1) onnx/lib/libonnxruntime.$(ORT_EXT)
