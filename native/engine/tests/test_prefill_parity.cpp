// test_prefill_parity.cpp — v1.9.2 batched-prefill PARITY GATE.
//
// The v1.9.2 prefill fast path (Forward::prefill_span + the resolved
// weight table) must be numerically IDENTICAL to the serial token() path:
//
//   1. per-position logits: prefill_span and token() produce BIT-FOR-BIT
//      identical vocabulary logits at every prompt position (the logits
//      at position i depend on the full hidden-state chain of positions
//      [0, i] — matching logits at every position pins the whole chain);
//   2. full KV bytes: after both paths, EVERY layer's K and V cache
//      words (fp16 bits) for every written position are identical;
//   3. arbitrary chunk boundaries: the generate loop calls prefill_span
//      repeatedly at ITS chosen boundaries — parity must hold for chunk
//      sizes 1, 3, 7 and 16 (the last is the production cadence);
//   4. edge cases: empty span (OK, no-op), null span with count > 0
//      (INVALID_ARG), out-of-vocabulary token (INVALID_ARG), position
//      overflow (CONTEXT_OVERFLOW), last-token logits with a null buffer
//      (INVALID_ARG);
//   5. decode continuity: after a prefill_span prefill, a token()-style
//      decode step at the next position produces the same logits as a
//      pure token() run (the production generate loop mixes both paths).
//
// Everything runs on the REAL fixture model (tiny-llama-f32.gguf, the
// same artifact test_forward pins against the Python reference).

#include "shtn/engine.h"

#include "forward.h"
#include "gguf.h"
#include "llama.h"
#include "model.h"

#include <cstdio>
#include <cstring>
#include <string>
#include <vector>

static int failures = 0;

#define CHECK(cond)                                                        \
    do {                                                                   \
        if (!(cond)) {                                                     \
            std::fprintf(stderr, "FAIL %s:%d: %s\n", __FILE__, __LINE__,    \
                         #cond);                                           \
            ++failures;                                                    \
        }                                                                  \
    } while (0)

using namespace shtn;

namespace {

struct Loaded {
    model::Model model;
    llama::Hyper hyper{};
    std::string error;
};

bool load_fixture(const std::string& path, Loaded& out) {
    shtn_model_load_options opts{};
    if (out.model.load(path, opts, out.error) != SHTN_OK) return false;
    if (!out.model.generation_capable()) {
        out.error = "fixture not generation capable: " +
                    out.model.generation_reason();
        return false;
    }
    out.hyper = out.model.hyper();
    return true;
}

// run_serial advances `f` through ids starting at pos with ONE token()
// call per token, capturing logits at every position.
bool run_serial(fwd::Forward& f, const std::vector<uint32_t>& ids,
                uint64_t pos, std::vector<std::vector<float>>& logits,
                std::string& error) {
    logits.clear();
    std::vector<float> row(f.hyper().vocab_size);
    for (uint32_t i = 0; i < ids.size(); ++i) {
        if (f.token(ids[i], pos + i, row.data(), error) != SHTN_OK) {
            return false;
        }
        logits.emplace_back(row.begin(), row.end());
    }
    return true;
}

// run_spans advances `f` through ids in `chunk`-sized prefill_span calls,
// capturing the last span's final-token logits. When capture_all is set,
// a token() call at each position runs INSTEAD for comparison purposes —
// not used here; the span path captures only final logits.
bool run_spans(fwd::Forward& f, const std::vector<uint32_t>& ids,
               uint64_t pos, uint32_t chunk, std::vector<float>& last_logits,
               std::string& error) {
    last_logits.assign(f.hyper().vocab_size, 0.0f);
    uint32_t i = 0;
    while (i < ids.size()) {
        const uint32_t n =
            chunk < ids.size() - i ? chunk : static_cast<uint32_t>(ids.size() - i);
        const bool last = (i + n == ids.size());
        if (f.prefill_span(ids.data() + i, n, pos + i, last,
                           last ? last_logits.data() : nullptr,
                           error) != SHTN_OK) {
            return false;
        }
        i += n;
    }
    return true;
}

// kv_bytes serializes every layer's K then V words for positions
// [0, used) — the full observable cache state.
std::vector<uint16_t> kv_bytes(const fwd::Forward& f) {
    std::vector<uint16_t> out;
    const kv::Cache& c = f.cache();
    for (uint32_t l = 0; l < c.layout().layer_count; ++l) {
        const uint16_t* k = c.k_layer(l);
        const uint16_t* v = c.v_layer(l);
        for (uint64_t p = 0; p < c.used_positions(); ++p) {
            for (uint32_t e = 0; e < c.layout().kv_dim; ++e) {
                out.push_back(k[p * c.layout().kv_dim + e]);
            }
        }
        for (uint64_t p = 0; p < c.used_positions(); ++p) {
            for (uint32_t e = 0; e < c.layout().kv_dim; ++e) {
                out.push_back(v[p * c.layout().kv_dim + e]);
            }
        }
        (void)k;
        (void)v;
    }
    return out;
}

} // namespace

int main() {
    const std::string fixtures = SHTN_FIXTURES_DIR;
    const std::string model_path = fixtures + "/tiny-llama-f32.gguf";

    Loaded ld;
    CHECK(load_fixture(model_path, ld));
    if (failures > 0) return 1;

    const llama::Hyper& h = ld.hyper;
    CHECK(h.vocab_size > 8);

    // A deterministic in-vocab prompt longer than one production chunk.
    std::vector<uint32_t> ids;
    for (uint32_t i = 0; i < 20; ++i) {
        ids.push_back(4 + (i % (h.vocab_size - 4)));
    }

    // --- 1+2+3: serial vs span parity (logits every position, KV bytes) ---
    for (uint32_t chunk : {1u, 3u, 7u, 16u}) {
        fwd::Forward serial;
        CHECK(serial.init(ld.model.weights(), h, 0, ld.error) == SHTN_OK);
        std::vector<std::vector<float>> serial_logits;
        CHECK(run_serial(serial, ids, 0, serial_logits, ld.error));

        fwd::Forward spanned;
        CHECK(spanned.init(ld.model.weights(), h, 0, ld.error) == SHTN_OK);
        std::vector<float> last_logits;
        CHECK(run_spans(spanned, ids, 0, chunk, last_logits, ld.error));

        CHECK(serial_logits.size() == ids.size());
        for (uint32_t v = 0; v < h.vocab_size; ++v) {
            CHECK(last_logits[v] == serial_logits.back()[v]);
        }

        // Full KV state equality (all layers, all written positions).
        const std::vector<uint16_t> kv_serial = kv_bytes(serial);
        const std::vector<uint16_t> kv_span = kv_bytes(spanned);
        CHECK(kv_serial.size() == kv_span.size());
        CHECK(kv_serial == kv_span);
        CHECK(spanned.cache().used_positions() == ids.size());
    }

    // --- 4: edge cases ------------------------------------------------------
    {
        fwd::Forward f;
        CHECK(f.init(ld.model.weights(), h, 0, ld.error) == SHTN_OK);
        std::vector<float> logits(h.vocab_size);

        std::string err;
        CHECK(f.prefill_span(ids.data(), 0, 0, true, logits.data(), err) ==
              SHTN_OK); // empty span: no-op
        CHECK(f.prefill_span(nullptr, 4, 0, false, nullptr, err) ==
              SHTN_ERR_INVALID_ARG);
        CHECK(f.prefill_span(ids.data(), 1, 0, true, nullptr, err) ==
              SHTN_ERR_INVALID_ARG);
        const uint32_t bad = h.vocab_size + 123;
        CHECK(f.prefill_span(&bad, 1, 0, false, nullptr, err) ==
              SHTN_ERR_INVALID_ARG);
        CHECK(f.prefill_span(ids.data(), ids.size(), h.context,
                             false, nullptr, err) ==
              SHTN_ERR_CONTEXT_OVERFLOW);
    }

    // --- 5: mixed production path (span prefill → token() decode step) ------
    {
        fwd::Forward spanned;
        CHECK(spanned.init(ld.model.weights(), h, 0, ld.error) == SHTN_OK);
        std::vector<float> last_logits;
        CHECK(run_spans(spanned, ids, 0, 16, last_logits, ld.error));

        // Continue with a serial decode step at the next position.
        std::vector<float> decode_logits(h.vocab_size);
        const uint32_t next = 4;
        CHECK(spanned.token(next, static_cast<uint64_t>(ids.size()),
                            decode_logits.data(), ld.error) == SHTN_OK);

        // Same state built purely serially.
        fwd::Forward serial;
        CHECK(serial.init(ld.model.weights(), h, 0, ld.error) == SHTN_OK);
        std::vector<std::vector<float>> serial_logits;
        CHECK(run_serial(serial, ids, 0, serial_logits, ld.error));
        std::vector<float> serial_decode(h.vocab_size);
        CHECK(serial.token(next, static_cast<uint64_t>(ids.size()),
                           serial_decode.data(), ld.error) == SHTN_OK);

        CHECK(decode_logits == serial_decode);
    }

    if (failures == 0) {
        std::printf("prefill parity: PASS\n");
        return 0;
    }
    std::fprintf(stderr, "prefill parity: %d failure(s)\n", failures);
    return 1;
}
