#!/usr/bin/env python3
"""make-e2e-live-model.py — browser-E2E LIVE-VISIBILITY model fixture (v1.8.2).

A wide-context tiny llama GGUF whose generation runs to the token budget
instead of stopping at EOS: the eos_token_id metadata points BEYOND the
vocabulary, so the engine's EOS gate (`eos_of`: has_eos requires
eos < vocab size) stays closed and every turn generates exactly
`max_tokens` tokens of real streamed output.

Why: the standard e2e fixture samples EOS within a few tokens, so the
whole generation completes in ~5 ms — the run settles before any browser
test can observe the live surface. THIS fixture makes the live streaming
window real and deterministic (~160 tokens at slow-dims ≈ 1-3 s), which is
what the live-visibility E2E (`e2e/live-stream.spec.ts`) must prove:

    send → run live → streamed text visible WHILE the run is live →
    more text arrives without user action → settle → one final message.

Generation is still REAL (real logits, real sampler, real streaming, the
validated llama graph executed by the real C++ engine); the token content
is synthetic-weights noise and the EOS suppression is a FIXTURE property,
never a product behavior.

Usage: python3 e2e/make-e2e-live-model.py <output.gguf>
"""

import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
REFERENCE = os.path.join(
    HERE, "..", "native", "engine", "tests", "reference", "make_fixture.py"
)

sys.path.insert(0, os.path.dirname(REFERENCE))
import make_fixture  # noqa: E402  (the canonical writer)


def main() -> int:
    if len(sys.argv) != 2:
        print(__doc__)
        return 2

    out = os.path.abspath(sys.argv[1])
    os.makedirs(os.path.dirname(out) or ".", exist_ok=True)

    tokens = (
        ["<unk>", "<s>", "</s>", "▁"]
        + [chr(ord("a") + i) for i in range(26)]
        + ["he", "ll", "o ", "th", "re", " w", "or", "ld", "sa", "y",
           "so", "me", "in", "g", "hi", "er", "an", "es", "at", "en"]
    )
    token_types = [2, 3, 3, 1] + [1] * (len(tokens) - 4)
    merges = ["h e", "l l", "o o", "t h", "r e"]

    # eos_token_id far beyond the vocabulary: the engine resolves
    # has_eos = special.eos < vocab_size → false → generation runs to the
    # request's max_tokens (LENGTH finish), never stops early on EOS.
    size = make_fixture.write_gguf(
        out,
        tokens=tokens,
        token_types=token_types,
        merges=merges,
        emb=96,
        layers=4,
        heads=4,
        kv_heads=2,
        head_dim=16,
        ffn=256,
        ctx=4096,
        eos_token_id=0x7FFFFFF0,
    )
    print(f"wrote {out} ({size} bytes, llama graph, ctx=4096, EOS unreachable)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
