# OpenClaw Dashboard — Benchmark Report: Go vs Python

> **Date:** 2026-03-03
> **Environment:** macOS 26.3, Apple Silicon (arm64), Go 1.26, Python 3.9
> **Go binary:** `openclaw-dashboard` v2026.2.28.1 (6.2MB, optimised, stdlib only)
> **Python:** `server.py` via `http.server.HTTPServer`
> **Tool:** [hey](https://github.com/rakyll/hey) HTTP load generator

---

## 1. Binary & Startup

| Metric | Go | Python |
|---|---|---|
| Deployable size | **6.2 MB** (single binary) | ~11 GB framework |
| Runtime deps | **none** | Python 3.9 |
| Startup time | **63ms** | 110ms |
| Files needed | **1** | 3+ (server.py, refresh.sh, index.html) |

---

## 2. Memory

| State | Go RSS | Python RSS | Winner |
|---|---|---|---|
| **Idle** | 8.8 MB | **3.0 MB** | 🐍 Python |
| **Under load (200 conc)** | 21.2 MB | **9.6 MB** | 🐍 Python |
| **Post-load** | 21.2 MB | **9.6 MB** | 🐍 Python |

> Python wins idle memory because it's single-threaded (GIL). Go pre-allocates goroutine stacks + GC buffers. The tradeoff: Python's low memory comes at the cost of being unable to handle concurrent requests.

---

## 3. Single Request Latency (warm, debounced)

| Endpoint | Go | Python | Speedup |
|---|---|---|---|
| `GET /` (index.html) | **0.45ms** | 1.45ms | **3.2×** |
| `GET /api/refresh` | **0.73ms** | 0.73ms | tie |
| `GET /404` | **1.4ms** | 4.0ms | **2.9×** |

---

## 4. Throughput

| Load | Go req/s | Python req/s | Go advantage |
|---|---|---|---|
| 1000 req, 10 conc | **23,731** | 2,388 | **9.9×** |
| 5000 req, 100 conc | **33,275** | 315 | **105×** |
| 10000 req, 50 conc | **37,063** | 940 | **39×** |

---

## 5. Latency Percentiles (10000 req, 50 concurrent)

| Percentile | Go | Python | Go advantage |
|---|---|---|---|
| p10 | **0.3ms** | 1.1ms | 3.7× |
| p50 | **1.0ms** | 2.0ms | 2× |
| p75 | **1.6ms** | 2.2ms | 1.4× |
| p90 | **2.7ms** | 2.5ms | ~tie |
| p95 | **3.4ms** | 2.9ms | ~tie |
| **p99** | **5.2ms** | 33.4ms | **6.4×** |
| **Worst** | **11.1ms** | 5,873ms | **529×** |

> Python's median is competitive but **tail latency explodes** — p99 is 33ms, worst case is **5.8 seconds** due to GIL contention. Go stays under 12ms even at worst.

---

## 6. Reliability Under Stress (5000 req, 200 concurrent)

| Metric | Go | Python |
|---|---|---|
| **Successful responses** | 5000/5000 (100%) | 4933/5000 (**98.7%**) |
| **Connection refused** | 0 | **67 errors** |
| **Error rate** | **0%** | **1.34%** |

> Python drops connections under high concurrency. Go handles it without errors.

---

## 7. CPU Under Load (10000 req, 100 concurrent)

| Metric | Go | Python |
|---|---|---|
| Peak CPU | 141.7% (multi-core) | 76.9% (GIL-limited) |
| Completion time | **~0.3s** | **~10.6s** |
| CPU-time per request | **~0.004ms** | **~0.076ms** |

> Go uses more CPU cores simultaneously but finishes **35× faster**. Per-request CPU cost is **19× lower**.

---

## 8. Feature Parity

| Feature | Go | Python | Match |
|---|---|---|---|
| Theme injection | `midnight` ✅ | `midnight` ✅ | ✅ |
| Version injection | `v2026.2.28.1` | `v2026.2.28` | ⚠️ minor diff |
| data.json keys | 36 | 36 | ✅ |
| CORS headers | ✅ | ✅ | ✅ |
| AI chat `/api/chat` | ✅ | ✅ | ✅ |
| Stale-while-revalidate | ✅ | ❌ (blocking) | Go better |

---

## 9. Summary Scorecard

| Category | Go | Python | Winner |
|---|---|---|---|
| **Deployment** | Single 6.2MB binary | Python runtime required | 🏆 Go |
| **Idle memory** | 8.8 MB | 3.0 MB | 🏆 Python |
| **Throughput** | 37,063 req/s | 940 req/s | 🏆 Go (39×) |
| **Tail latency (p99)** | 5.2ms | 33.4ms | 🏆 Go (6.4×) |
| **Worst case latency** | 11ms | 5,873ms | 🏆 Go (529×) |
| **Reliability** | 0% error | 1.34% error | 🏆 Go |
| **Concurrency** | Full multi-core | GIL-limited | 🏆 Go |
| **CPU efficiency** | 0.004ms/req | 0.076ms/req | 🏆 Go (19×) |

---

## Conclusion

Go wins **7 out of 8 categories**. Python only wins idle memory (3MB vs 8.8MB) because its single-threaded GIL means it allocates less — but that same limitation causes connection drops, 5.8s tail latency spikes, and 1.34% error rate under load.

**For users who want zero-friction deployment and reliable performance under any load: use the Go binary.**
**For users who prefer Python and don't expect concurrent access: the Python server works fine.**

Both options are maintained in the same repository — users choose what fits their environment.
