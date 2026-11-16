#!/usr/bin/env python3
"""A classify worker (see worker.go) for open_clip models: CLIP, SigLIP, SigLIP 2.

    make -C pkg/classify venv        # once: pkg/classify/.venv, with the right PyTorch
    pkg/classify/.venv/bin/python worker_openclip.py --model ViT-B-16-SigLIP2 --pretrained webli \
        [--vocab words.txt] [--top 10]

It gives, for every photo, an embedding to search with text (the query is
embedded with the same model: {"op":"text"}), and, when --vocab is given (one
word or phrase per line), the vocabulary's best matches as labels, so that a
word can be found without embeddings, and so that the labels can be read.

Only syntax checked so far: expect to fix something small.
"""
import argparse
import json
import sys

import torch
import open_clip
from PIL import Image, ImageOps

ap = argparse.ArgumentParser()
ap.add_argument("--model", default="ViT-B-16-SigLIP2")
ap.add_argument("--pretrained", default="webli")
ap.add_argument("--vocab")
ap.add_argument("--top", type=int, default=10)
ap.add_argument("--min-score", type=float, default=0.0)
ap.add_argument("--template", default="a photo of {}")
args = ap.parse_args()

device = "mps" if torch.backends.mps.is_available() else ("cuda" if torch.cuda.is_available() else "cpu")
model, _, preprocess = open_clip.create_model_and_transforms(args.model, pretrained=args.pretrained, device=device)
model.eval()
tokenizer = open_clip.get_tokenizer(args.model)


def enc_text(texts):
    with torch.no_grad():
        t = model.encode_text(tokenizer(texts).to(device))
        return t / t.norm(dim=-1, keepdim=True)


vocab, vocab_emb = [], None
if args.vocab:
    vocab = [w.strip() for w in open(args.vocab) if w.strip()]
    vocab_emb = enc_text([args.template.format(w) for w in vocab])

print("worker ready on %s: %s" % (device, args.model), file=sys.stderr, flush=True)


def image(path):
    img = ImageOps.exif_transpose(Image.open(path)).convert("RGB")
    with torch.no_grad():
        e = model.encode_image(preprocess(img).unsqueeze(0).to(device))
        e = e / e.norm(dim=-1, keepdim=True)
    out = {"embedding": [round(x, 5) for x in e[0].tolist()]}
    if vocab:
        logits = e @ vocab_emb.T
        if hasattr(model, "logit_bias"):  # SigLIP: each word is its own yes/no
            scores = torch.sigmoid(logits * model.logit_scale.exp() + model.logit_bias)[0]
        else:  # CLIP: the words compete
            scores = (logits * model.logit_scale.exp()).softmax(dim=-1)[0]
        top = scores.topk(min(args.top, len(vocab)))
        out["labels"] = [
            {"name": vocab[i], "score": round(s, 4)}
            for s, i in zip(top.values.tolist(), top.indices.tolist())
            if s >= args.min_score
        ]
    return out


for line in sys.stdin:
    try:
        req = json.loads(line)
        if req["op"] == "image":
            res = image(req["path"])
        elif req["op"] == "text":
            res = {"embedding": [round(x, 5) for x in enc_text([req["text"]])[0].tolist()]}
        else:
            res = {"error": "unknown op %r" % req["op"]}
    except Exception as e:  # one bad photo is not the end of the run
        res = {"error": "%s: %s" % (type(e).__name__, e)}
    print(json.dumps(res), flush=True)
