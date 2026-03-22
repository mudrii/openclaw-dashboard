# OpenClaw Dashboard — Benchmark Report: Go vs Python (Historical)

> **Note:** As of v2026.3.22, the Python server has been removed. The Go binary is the only implementation. This document is retained as a historical reference for the benchmarks that informed that decision.

## 2026-03-09 Re-benchmark (v2026.3.8)

> **Date:** 2026-03-09
> **Environment:** macOS 26.3, Apple Silicon (arm64), Go 1.26.1, Python 3.14.3
> **Go binary:** `openclaw-dashboard` (v2026.3.8, arm64, 9.5 MB)
> **Python:** `server.py` via stdlib `threading/requests` harness

### Benchmark command used

```bash
python3 /tmp/benchmark_go_vs_py.py --requests 20000 --conc 200
python3 /tmp/benchmark_go_vs_py.py --requests 10000 --conc 100 --go http://127.0.0.1:8080/ --py http://127.0.0.1:9090/
```

The Go server was running on `127.0.0.1:8080` and Python on `127.0.0.1:9090` on the same host.

### Summary (`/api/system`)

| Metric | Go | Python | Delta |
|---|---:|---:|---:|
| Requests | 20,000 | 20,000 | — |
| Concurrency | 200 | 200 | — |
| Total time | 9.907s | 11.605s | +1.70s |
| Throughput | 2,018.74 req/s | 1,723.44 req/s | **1.17× faster** |
| Avg latency | 82.638 ms | 102.463 ms | -19.3 ms |
| p95 | 166.306 ms | 193.564 ms | -27.3 ms |
| p99 | 214.515 ms | 253.554 ms | -39.0 ms |
| Max | 364.619 ms | 472.451 ms | -107.8 ms |
| Errors | 0 | 0 | — |

### Supplemental (`/`)

| Metric | Go | Python | Delta |
|---|---:|---:|---:|
| Requests | 10,000 | 10,000 | — |
| Concurrency | 100 | 100 | — |
| Total time | 5.263s | 5.729s | +0.47s |
| Throughput | 1,900.21 req/s | 1,745.47 req/s | **1.09× faster** |
| Avg latency | 50.097 ms | 54.506 ms | -4.409 ms |
| p95 | 90.184 ms | 93.187 ms | -3.003 ms |
| p99 | 112.646 ms | 116.488 ms | -3.842 ms |
| Max | 162.183 ms | 185.005 ms | -22.822 ms |

### Conclusion

Go remains slightly ahead under sustained concurrent reads in this environment, with lower average and tail latency and ~17% higher throughput on `/api/system` at the tested load.

---

## Historical benchmark (2026-03-03)

> **Date:** 2026-03-03
> **Environment:** macOS 26.3, Apple Silicon (arm64), Go 1.26, Python 3.9
> **Go binary:** `openclaw-dashboard` v2026.2.28.1 (6.2MB, optimised, stdlib only)
> **Python:** `server.py` via `http.server.HTTPServer`
> **Tool:** [hey](https://github.com/rakyll/hey) HTTP load generator

---

## 1. Binary & Startup

| Metric | Go | Python |
|---|---|---|
| Deployable size | **6.2 MB** (single binary) | ~81 MB (interpreter + stdlib) |
| Modules used | 0 (embedded) | 509 KB of stdlib imports |
| Runtime deps | **none** | Python 3.x (51KB bin + 67MB stdlib + libpython) |
| Startup time | **63ms** | 110ms |
| Files needed | **1** | 3+ (server.py, refresh.sh, index.html) |

> Note: Python's 81MB is the minimum framework (interpreter + stdlib + C extensions).
> The `server.py` only imports ~509KB of stdlib modules, but the full framework must
> be installed. Go embeds everything into a single 6.2MB binary — 13× smaller.

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
