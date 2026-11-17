#!/usr/bin/env python3
"""Export the text tower of an open_clip model to ONNX, to embed search queries
without PyTorch (hugot / onnxruntime):

    pkg/classify/.venv/bin/python scripts/export_text_onnx.py \
        --model ViT-B-16-SigLIP2 --pretrained webli --out pkg/classify/onnx/siglip2

It writes, in OUT, what hugot's feature extraction pipeline loads:
  model.onnx      input_ids, attention_mask int64 [batch, length<=context] ->
                  embedding float32
                  [batch, dim], normalized. SigLIP pools the last position of a
                  padded text, so the model pads to the context itself.
  model.onnx.data its weights
  tokenizer.json  the tokenizer's (it adds the eos token)
  config.json     max_position_embeddings: the context
The queries have to be cleaned like open_clip's tokenizer does before that: see
cleanText in embed_onnx.go.

The text tower is mostly the 256k word table (3/4 of 1.1 GB), which is stored as
int8; the transformer's weights stay as trained (--bits 32, the default: 540 MB,
~550 MB to run). --bits 8 stores them as 8-bit integers with the arithmetic still
in float: 280 MB, ~400 MB to run, embeddings within 0.002 cosine of the above.
--bits 4 is 240 MB and 0.01 away.
Quantizing the matrix multiplies' *activations* as well (onnxruntime's
quantize_dynamic) moves them 0.02-0.07, and is not used.
"""
import argparse
import json
import os
import shutil

import open_clip
import torch

ap = argparse.ArgumentParser()
ap.add_argument("--model", default="ViT-B-16-SigLIP2")
ap.add_argument("--pretrained", default="webli")
ap.add_argument("--out", required=True)
ap.add_argument("--bits", type=int, choices=(4, 8, 32), default=32, help="bits of the transformer weights (8 or 4: weight-only, smaller); 32 leaves them as trained")
args = ap.parse_args()

model, _, _ = open_clip.create_model_and_transforms(args.model, pretrained=args.pretrained, device="cpu")
model.eval()
tokenizer = open_clip.get_tokenizer(args.model)


class Text(torch.nn.Module):
    def __init__(self, m):
        super().__init__()
        self.m = m
        self.context = m.text.context_length

    def forward(self, ids, mask):
        # the mask is all ones, but hugot sizes its batches by it: without one the
        # texts are cut to a token
        ids = torch.nn.functional.pad(ids * mask, (0, self.context - ids.shape[1]))
        t = self.m.encode_text(ids)
        return t / t.norm(dim=-1, keepdim=True)


os.makedirs(args.out, exist_ok=True)
ids = tokenizer(["a photo"])
short = ids[:, :8]  # the model has to take texts that are not padded
with torch.no_grad():
    torch.onnx.export(
        Text(model), (short, torch.ones_like(short)), os.path.join(args.out, "model.onnx"),
        input_names=["input_ids", "attention_mask"], output_names=["embedding"],
        dynamic_axes={"input_ids": {0: "batch", 1: "length"}, "attention_mask": {0: "batch", 1: "length"}, "embedding": {0: "batch"}},
        opset_version=17, dynamo=False,
    )
    ref = Text(model)(ids, torch.ones_like(ids))[0]
print("exported; context", ids.shape[1], "dim", ref.shape[0])
with open(os.path.join(args.out, "config.json"), "w") as f:
    json.dump({"max_position_embeddings": ids.shape[1]}, f)

import onnx

from onnxruntime.quantization import QuantType, quantize_dynamic

# the word table is always int8 (cos 0.998+); the transformer's weights only if --bits says
path = os.path.join(args.out, "model.onnx")
mid = os.path.join(args.out, "mid.onnx")
if args.bits != 32:
    from onnxruntime.quantization.matmul_nbits_quantizer import MatMulNBitsQuantizer

    q = MatMulNBitsQuantizer(onnx.load(path), bits=args.bits, block_size=32, is_symmetric=True, accuracy_level=4)
    q.process()
    onnx.save_model(q.model.model, mid, save_as_external_data=True, all_tensors_to_one_file=True,
                    location="mid.data", size_threshold=1024)
else:
    os.replace(path, mid)
quantize_dynamic(mid, path, weight_type=QuantType.QInt8, op_types_to_quantize=["Gather"],
                 use_external_data_format=True)
for f in ("mid.onnx", "mid.data"):
    if os.path.exists(os.path.join(args.out, f)):
        os.remove(os.path.join(args.out, f))

# the weights beside the graph, not in it: ONNX Runtime maps them from the file
# instead of reading them in twice
m = onnx.load(os.path.join(args.out, "model.onnx"))
data = "model.onnx.data"
if os.path.exists(os.path.join(args.out, data)):
    os.remove(os.path.join(args.out, data))
onnx.save_model(m, os.path.join(args.out, "model.onnx"), save_as_external_data=True,
                all_tensors_to_one_file=True, location=data, size_threshold=1024)

# the tokenizer, from the same place open_clip got it
import huggingface_hub
tok = tokenizer.tokenizer
src = getattr(tok, "name_or_path", None)
try:
    path = huggingface_hub.hf_hub_download(src, "tokenizer.json")
    shutil.copy(path, os.path.join(args.out, "tokenizer.json"))
    print("tokenizer from", src)
except Exception as e:
    print("could not copy tokenizer.json:", e)
