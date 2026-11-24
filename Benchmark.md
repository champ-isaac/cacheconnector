# Cache backend benchmarks

This report compares the basic read and write operations of the `cache.Cache` interface (`cache.go`) on four backends: in-memory, Memcached, Redis and Valkey. A fifth series runs Memcached with a larger connection pool. It measures the client library plus one loopback round trip to a container. It is not a capacity test of the servers or of a whole service.

Numbers are in **ns/op** (nanoseconds per operation; 1,000,000 ns = 1 ms), **B/op** (bytes allocated per call) and **allocs/op** (allocations per call). Each figure is the **median of five rounds**, with the min–max across the rounds beside it.

## Summary

- **In-memory is far faster on every operation.** Its median is about 250× lower than the fastest network backend's at the smallest gap (`GetHit/64KiB`) and about 450,000× lower at the largest (`Ping`), and its range does not overlap any other backend's on any operation. This is not a like-for-like comparison: the network backends include a round trip of about 1.5–1.9 ms (the raw p50) through the local port forwarder (see Environment), and in-memory is per process, so it cannot be shared between replicas.
- **The three network backends (Memcached, Redis, Valkey) are not distinguishable on latency.** On every operation their min–max ranges across the five rounds overlap one another. The coefficient of variation across rounds is 7–59% for the network series, and the largest difference between the best and worst median on one operation is 55% (`Set/1KiB`), where the ranges still overlap. This run does not show that any of the three is faster.
- **Memcached differs in allocations per call.** Its median B/op and allocs/op are lower than both Redis's and Valkey's on all ten operations. For example, `GetMiss` is 1 allocation and 32 B on Memcached against 26 allocations and 448 B on Redis and Valkey, and `Set/64B` is 4 allocations and 115 B against 9 and 288 B. Redis and Valkey have the same allocs/op on every operation. For the sequential operations these counts are far steadier between rounds than the timings (see Results).
- **Treat the absolute network timings with care.** The network `Ping` medians are 1.8–2.3 ms, against a raw p50 of 1.5–1.9 ms through the same port forwarder with no Go client, and the VM process used more CPU at the start of the first two rounds than of the later ones (see Conditions).

## Environment

| Item | Value |
|---|---|
| Date | 2026-11-23, 22:14:29–22:26:48 local time (UTC−6). The date comes from the result files' modification times; `meta.txt` records clock times only. |
| Machine | Intel Core i9-9880H @ 2.30 GHz, 8 cores / 16 threads, 32 GiB RAM, x86_64 |
| OS | macOS 15.8.3 |
| Go | go1.26.0 darwin/amd64, GOMAXPROCS 16 |
| Servers | Redis 8.12.0, Valkey 9.2.0 and Memcached 1.6.45, started from `docker-compose.yaml` (`redis:latest`, `valkey/valkey:latest`, `memcached:latest`) |
| Where the servers run | Colima (macOS Virtualization.framework), Docker 29.3.0 in an Ubuntu 24.04.4 VM with 2 CPUs and 4 GiB of memory, shared with six other containers that were running when checked at 21:58, before the run (three from a `pg-redis-sync` stack, a RedisInsight UI, a frontend and a Mongo UI); none of them was part of the benchmark |
| How the client reaches them | Host ports 6380 (Valkey), 6381 (Redis) and 11211 (Memcached) are all listened on by one `ssh` process, Colima's port forwarder. Every network call goes through it. |

## Method

- **Operations.** One shared benchmark suite, `cachetest.Bench` in `cachetest/bench.go`, runs ten cases against every backend through the `cache.Cache` interface: `Ping`, `Get` of a missing key, `Get` of an existing key at 64 B, 1 KiB and 64 KiB, `Set` at the same three sizes, and a parallel `Get` and `Set` at 1 KiB (16 callers, `b.RunParallel`).
- **Workload.** Each case rotates over 128 keys, so it does not hammer one key. Existing-key reads are preloaded. `Set` uses a ttl of one hour. The keys a case wrote are deleted afterwards. Every case opens its own client. Redis and Valkey connect when the client is created, and `Ping` and the cases that preload keys make untimed calls before timing starts. `GetMiss` and the `Set` cases make none, so on Memcached, which connects on the first call, the first connection's setup is inside the timed loop.
- **Correctness.** Every `Get` hit checks the length and the first byte of the value, and the sequential cases also check the last byte. `GetMiss` checks for `ErrNotFound`, and the sequential `Set` cases read a value back after the timed loop. A test in `cachetest/bench_test.go` confirms that the `Get`-hit checks and the sequential `Set` checks fail on a cache that truncates values.
- **Backends.** In-memory runs in the benchmark process. Redis is `127.0.0.1:6381`, database 0, and Valkey is `valkey://127.0.0.1:6380/1`; both use the client's default pool (10 connections, 3 kept idle) and the same client code. Memcached is `127.0.0.1:11211` with the client's default of 2 idle connections per server and a 2 s timeout. The fifth series, `memcached (16 idle)`, is the same with 16 idle connections, as many as the parallel cases have callers.
- **Rounds.** Five rounds ran one after another. Each round was a separate `go test -tags integration -p 1 -run '^$' -bench . -benchmem -count=1 ./...` over all backends, in the order in-memory, Memcached, Redis, Valkey, with Go's default benchtime of 1 s per case. `-p 1` stops Go from running packages in parallel, and `-tags integration` includes the real-server benchmarks. Running every backend in each round, instead of five runs of one backend in a row, spreads any drift in the machine across backends.
- **Server state.** Memcached held no items when checked just before the run started. The Valkey and Memcached test setups flush Valkey database 1 and the whole Memcached server before and after the run. Redis database 0 holds other keys; the benchmark only deletes its own.
- **Statistics.** For each backend and operation, the median, the min–max, and the coefficient of variation (CV: the population standard deviation divided by the mean, in percent) across the five rounds. The numbers were aggregated by a script, `aggregate.py`, that is not part of the repository. In this report, two backends count as different only if their min–max ranges do not overlap.

## Conditions during the run

The runner waited up to 300 s for the one-minute load average to fall below 3 before starting, and it did not: `waited 300s for load1<3; load1 at start=5.00`. The load averages at the start of the rounds were 4.4–6.5 on a machine with 16 hardware threads. The busiest process at the start of every round was the macOS Virtualization.framework VM process, which is where Colima's Docker VM runs, so the servers were competing for CPU with whatever else the VM was doing.

| Round | Start | End | Load average at start (1 / 5 / 15 min) | Load average at end | Busiest process at start (CPU) | Speed index |
|---:|---|---|---|---|---|---:|
| 1 | 22:14:29 | 22:17:19 | 4.42 / 7.58 / 8.80 | 6.47 / 7.15 / 8.40 | Virtualization VM, 198.6% | 1.25 |
| 2 | 22:17:19 | 22:20:02 | 6.47 / 7.15 / 8.40 | 4.85 / 6.50 / 7.94 | Virtualization VM, 212.2% | 1.14 |
| 3 | 22:20:02 | 22:22:22 | 4.85 / 6.50 / 7.94 | 5.67 / 6.18 / 7.58 | Virtualization VM, 158.6% | 1.00 |
| 4 | 22:22:22 | 22:24:32 | 5.67 / 6.18 / 7.58 | 5.81 / 5.78 / 7.21 | Virtualization VM, 118.9% | 0.90 |
| 5 | 22:24:32 | 22:26:48 | 5.81 / 5.78 / 7.21 | 4.49 / 5.16 / 6.73 | Virtualization VM, 125.6% | 0.90 |

The speed index is, for each round, the median over the 40 network series of that round's ns/op divided by the series' own median across the rounds. Above 1.00 means slower than usual. In-memory series are left out.

Rounds 1 and 2 were slower than the rest (index 1.25 and 1.14, against 0.90 for rounds 4 and 5). They also had the highest CPU use by the VM process at the start (198.6% and 212.2%, against 158.6%, 118.9% and 125.6% in rounds 3 to 5). That fits load having slowed them, but the data cannot separate load from time order: the rounds ran in sequence and the load fell over the run, so a warm-up effect would look the same.

**Raw TCP round-trip floor.** Before the first round and after the last, a small script sent 400 requests on one TCP connection to each server (`version` for Memcached, `PING` for Redis and Valkey) with no Go client involved. It went through the same port forwarder.

| Server | p50 before | p50 after | Change in p50 | p99 before | p99 after | min before | min after |
|---|---:|---:|---:|---:|---:|---:|---:|
| memcached | 1,572 µs | 1,542 µs | -2% | 5,672 µs | 4,337 µs | 1,186 µs | 1,038 µs |
| valkey | 1,732 µs | 1,893 µs | +9% | 3,221 µs | 9,182 µs | 1,373 µs | 1,141 µs |
| redis | 1,815 µs | 1,647 µs | -9% | 10,855 µs | 2,778 µs | 1,227 µs | 1,237 µs |

The floor was not measured in each round, only at the two ends. It moved by up to 9% at the median and by up to 3.9× at p99 between the two measurements, so the floor itself is not steady. For comparison, on a single operation the best and worst of the three backends' medians differ by 5–55% in the results below.

## Results

Each table has one row per backend. The five per-round values are in round order (round 1 to round 5), so the variation is visible. The rows are in the order in-memory, Memcached, Redis, Valkey, Memcached with 16 idle connections.

### Ping

One round trip to the server; the cheapest call there is.

| Backend | Median ns/op | Min–max | CV % | Per round, 1–5 (ns/op) | B/op | allocs/op | Iterations per run (median) |
|---|---:|---:|---:|---|---:|---:|---:|
| inmemory | 4.6 | 4.2–4.7 | 5 | 4.6 / 4.7 / 4.2 / 4.6 / 4.2 | 0 | 0 | 277,229,760 |
| memcached | 2,276,906 | 1,932,629–2,509,945 | 10 | 1,936,978 / 2,509,945 / 2,276,906 / 2,330,649 / 1,932,629 | 16 | 1 | 651 |
| redis | 2,083,640 | 1,706,244–4,556,232 | 39 | 4,556,232 / 3,269,748 / 2,083,640 / 1,706,244 / 2,050,705 | 172 | 6 | 576 |
| valkey | 2,326,132 | 1,792,825–2,419,540 | 13 | 2,326,132 / 2,419,540 / 1,829,081 / 2,363,809 / 1,792,825 | 172 | 6 | 584 |
| memcached (16 idle) | 1,816,996 | 1,496,896–4,295,575 | 44 | 4,295,575 / 3,217,838 / 1,816,996 / 1,496,896 / 1,660,049 | 16 | 1 | 636 |

### Get of a missing key (`GetMiss`)

The key does not exist, so the call returns `ErrNotFound`.

| Backend | Median ns/op | Min–max | CV % | Per round, 1–5 (ns/op) | B/op | allocs/op | Iterations per run (median) |
|---|---:|---:|---:|---|---:|---:|---:|
| inmemory | 44.8 | 44.5–51.9 | 6 | 44.8 / 51.9 / 44.7 / 47.0 / 44.5 | 0 | 0 | 27,028,003 |
| memcached | 2,040,516 | 1,806,421–2,813,412 | 17 | 2,813,412 / 2,040,516 / 1,880,612 / 1,806,421 / 2,094,056 | 32 (30–33) | 1 | 644 |
| redis | 2,190,944 | 1,977,055–4,126,264 | 31 | 3,033,458 / 4,126,264 / 2,028,055 / 1,977,055 / 2,190,944 | 448 | 26 | 549 |
| valkey | 2,102,681 | 1,897,733–3,519,477 | 24 | 3,519,477 / 2,102,681 / 2,074,929 / 2,327,796 / 1,897,733 | 448 | 26 | 615 |
| memcached (16 idle) | 1,935,481 | 1,564,511–2,183,223 | 12 | 2,180,773 / 2,183,223 / 1,935,481 / 1,564,511 / 1,901,933 | 34 (29–38) | 1 | 559 |

### Get hit, 64 B value (`GetHit/64B`)

| Backend | Median ns/op | Min–max | CV % | Per round, 1–5 (ns/op) | B/op | allocs/op | Iterations per run (median) |
|---|---:|---:|---:|---|---:|---:|---:|
| inmemory | 218 | 209–274 | 10 | 218 / 274 / 218 / 225 / 209 | 64 | 1 | 5,408,697 |
| memcached | 2,699,499 | 1,631,889–2,892,904 | 24 | 2,699,499 / 2,892,904 / 2,866,229 / 1,631,889 / 1,716,450 | 208 | 4 | 584 |
| redis | 2,280,360 | 1,773,911–3,398,667 | 23 | 2,932,887 / 3,398,667 / 2,280,360 / 1,773,911 / 2,182,868 | 280 | 7 | 600 |
| valkey | 2,158,744 | 2,013,205–4,251,918 | 32 | 4,251,918 / 2,525,496 / 2,013,205 / 2,158,744 / 2,146,563 | 280 | 7 | 534 |
| memcached (16 idle) | 2,060,284 | 1,518,103–6,703,279 | 59 | 6,703,279 / 3,944,500 / 2,060,284 / 1,518,103 / 2,000,106 | 224 | 4 | 663 |

### Get hit, 1 KiB value (`GetHit/1KiB`)

| Backend | Median ns/op | Min–max | CV % | Per round, 1–5 (ns/op) | B/op | allocs/op | Iterations per run (median) |
|---|---:|---:|---:|---|---:|---:|---:|
| inmemory | 475 | 434–518 | 7 | 475 / 518 / 490 / 444 / 434 | 1,024 | 1 | 2,593,736 |
| memcached | 2,196,940 | 1,668,471–3,182,741 | 25 | 2,196,940 / 2,563,006 / 3,182,741 / 1,704,711 / 1,668,471 | 1,283 (1,282–1,283) | 4 | 614 |
| redis | 2,234,380 | 1,955,915–3,799,117 | 27 | 2,546,542 / 3,799,117 / 1,955,915 / 2,234,380 / 1,963,679 | 1,352 | 7 | 612 |
| valkey | 2,118,394 | 1,865,073–2,454,324 | 10 | 2,210,074 / 1,970,345 / 2,118,394 / 2,454,324 / 1,865,073 | 1,352 | 7 | 607 |
| memcached (16 idle) | 2,118,371 | 1,788,825–4,738,512 | 41 | 2,646,722 / 4,738,512 / 1,948,449 / 1,788,825 / 2,118,371 | 1,296 | 4 | 648 |

### Get hit, 64 KiB value (`GetHit/64KiB`)

| Backend | Median ns/op | Min–max | CV % | Per round, 1–5 (ns/op) | B/op | allocs/op | Iterations per run (median) |
|---|---:|---:|---:|---|---:|---:|---:|
| inmemory | 12,827 | 10,253–13,915 | 11 | 13,915 / 12,977 / 12,827 / 11,230 / 10,253 | 65,536 | 1 | 98,496 |
| memcached | 3,259,189 | 2,543,476–3,889,485 | 13 | 3,889,485 / 3,273,027 / 3,083,122 / 2,543,476 / 3,259,189 | 73,924 (73,920–73,925) | 4 | 386 |
| redis | 3,794,913 | 3,100,925–5,345,333 | 20 | 5,345,333 / 4,443,340 / 3,496,960 / 3,794,913 / 3,100,925 | 73,928 | 7 | 324 |
| valkey | 3,204,597 | 2,862,943–3,770,738 | 10 | 3,770,738 / 3,421,814 / 3,204,597 / 2,968,216 / 2,862,943 | 73,928 (73,928–73,942) | 7 | 412 |
| memcached (16 idle) | 3,548,783 | 2,289,798–4,239,423 | 22 | 4,239,423 / 3,548,783 / 4,075,787 / 2,289,798 / 2,730,250 | 73,927 (73,923–73,930) | 4 | 368 |

### Set, 64 B value (`Set/64B`)

The ttl is one hour.

| Backend | Median ns/op | Min–max | CV % | Per round, 1–5 (ns/op) | B/op | allocs/op | Iterations per run (median) |
|---|---:|---:|---:|---|---:|---:|---:|
| inmemory | 226 | 213–259 | 7 | 239 / 259 / 226 / 222 / 213 | 64 | 1 | 5,325,340 |
| memcached | 2,079,868 | 1,580,183–2,415,329 | 17 | 2,249,824 / 2,415,329 / 2,079,868 / 1,580,183 / 1,646,714 | 115 (113–122) | 4 | 650 |
| redis | 2,606,423 | 2,173,030–3,441,675 | 16 | 2,606,423 / 3,441,675 / 2,865,760 / 2,434,668 / 2,173,030 | 288 | 9 | 585 |
| valkey | 2,267,373 | 2,100,810–2,572,465 | 7 | 2,572,465 / 2,100,810 / 2,267,373 / 2,189,513 / 2,271,163 | 288 | 9 | 531 |
| memcached (16 idle) | 2,015,387 | 1,518,131–3,055,320 | 23 | 3,055,320 / 2,269,766 / 2,015,387 / 1,518,131 / 1,963,236 | 119 (113–123) | 4 | 540 |

### Set, 1 KiB value (`Set/1KiB`)

The ttl is one hour.

| Backend | Median ns/op | Min–max | CV % | Per round, 1–5 (ns/op) | B/op | allocs/op | Iterations per run (median) |
|---|---:|---:|---:|---|---:|---:|---:|
| inmemory | 465 | 421–533 | 8 | 466 / 533 / 465 / 445 / 421 | 1,024 | 1 | 2,592,108 |
| memcached | 1,961,782 | 1,625,254–2,283,652 | 14 | 2,283,652 / 2,198,200 / 1,961,782 / 1,669,708 / 1,625,254 | 129 (125–132) | 5 | 586 |
| redis | 3,042,886 | 1,934,777–3,480,342 | 21 | 3,042,886 / 3,480,342 / 2,309,295 / 3,350,260 / 1,934,777 | 288 | 9 | 390 |
| valkey | 2,253,288 | 1,882,000–2,776,135 | 13 | 2,253,288 / 2,411,299 / 1,882,000 / 2,776,135 / 2,213,765 | 288 | 9 | 620 |
| memcached (16 idle) | 1,985,377 | 1,764,079–4,037,330 | 35 | 2,564,411 / 4,037,330 / 1,985,377 / 1,791,637 / 1,764,079 | 129 (124–136) | 5 | 595 |

### Set, 64 KiB value (`Set/64KiB`)

The ttl is one hour.

| Backend | Median ns/op | Min–max | CV % | Per round, 1–5 (ns/op) | B/op | allocs/op | Iterations per run (median) |
|---|---:|---:|---:|---|---:|---:|---:|
| inmemory | 10,010 | 8,487–10,904 | 10 | 10,018 / 10,904 / 10,010 / 8,637 / 8,487 | 65,536 | 1 | 127,219 |
| memcached | 2,829,649 | 2,235,089–4,028,460 | 20 | 3,513,808 / 4,028,460 / 2,829,649 / 2,235,089 / 2,741,762 | 137 (130–145) | 5 | 402 |
| redis | 3,755,597 | 2,702,809–4,062,687 | 17 | 4,062,687 / 3,755,597 / 2,782,194 / 3,982,627 / 2,702,809 | 288 | 9 | 360 |
| valkey | 3,345,831 | 2,725,971–5,423,223 | 28 | 5,423,223 / 2,725,971 / 3,345,831 / 3,554,917 / 2,742,624 | 288 | 9 | 396 |
| memcached (16 idle) | 2,805,354 | 2,074,612–6,837,154 | 46 | 5,465,173 / 6,837,154 / 2,805,354 / 2,074,612 / 2,701,579 | 136 (130–145) | 5 | 428 |

### Get hit, 1 KiB value, 16 parallel callers (`GetHitParallel/1KiB`)

Here ns/op is the time per operation across all 16 callers (the inverse of throughput), not the latency of one call.

| Backend | Median ns/op | Min–max | CV % | Per round, 1–5 (ns/op) | B/op | allocs/op | Iterations per run (median) |
|---|---:|---:|---:|---|---:|---:|---:|
| inmemory | 475 | 374–522 | 13 | 475 / 522 / 487 / 394 / 374 | 1,024 | 1 | 2,673,278 |
| memcached | 525,269 | 328,757–965,086 | 38 | 965,086 / 525,269 / 568,914 / 328,757 / 439,673 | 1,479 (1,446–1,569) | 4 | 2,328 |
| redis | 540,741 | 469,649–1,034,178 | 33 | 1,034,178 / 753,277 / 469,649 / 540,741 / 470,985 | 1,589 (1,530–1,768) | 7 (7–8) | 1,975 |
| valkey | 575,375 | 440,285–849,713 | 23 | 849,713 / 583,960 / 440,285 / 543,567 / 575,375 | 1,561 (1,526–1,624) | 7 | 2,176 |
| memcached (16 idle) | 572,382 | 393,515–1,030,572 | 35 | 668,703 / 1,030,572 / 496,922 / 393,515 / 572,382 | 1,362 (1,362–1,389) | 4 | 3,056 |

### Set, 1 KiB value, 16 parallel callers (`SetParallel/1KiB`)

The ttl is one hour. Here ns/op is the time per operation across all 16 callers, not the latency of one call.

| Backend | Median ns/op | Min–max | CV % | Per round, 1–5 (ns/op) | B/op | allocs/op | Iterations per run (median) |
|---|---:|---:|---:|---|---:|---:|---:|
| inmemory | 765 | 688–846 | 8 | 813 / 846 / 765 / 705 / 688 | 1,024 | 1 | 1,630,948 |
| memcached | 517,926 | 382,980–792,029 | 25 | 792,029 / 662,484 / 517,926 / 480,160 / 382,980 | 435 (331–534) | 6 (5–6) | 2,318 |
| redis | 644,598 | 527,679–1,219,940 | 34 | 771,358 / 644,598 / 527,679 / 1,219,940 / 566,856 | 554 (488–593) | 9 (9–10) | 1,729 |
| valkey | 509,320 | 461,300–938,716 | 30 | 938,716 / 509,320 / 561,718 / 461,300 / 469,748 | 472 (458–510) | 9 | 2,414 |
| memcached (16 idle) | 466,762 | 331,234–632,575 | 22 | 632,575 / 608,030 / 452,883 / 331,234 / 466,762 | 172 (159–209) | 5 | 2,680 |

## Reading the results

**In-memory against the fastest network backend** (median of the fastest of Memcached, Redis and Valkey, divided by in-memory's median, rounded to two significant figures):

| Operation | Fastest network median (ns/op) | In-memory median (ns/op) | Ratio |
|---|---:|---:|---:|
| Ping | 2,083,640 (redis) | 4.6 | about 450,000× |
| Get of a missing key | 2,040,516 (memcached) | 44.8 | about 46,000× |
| Get hit, 64 B value | 2,158,744 (valkey) | 218 | about 9,900× |
| Get hit, 1 KiB value | 2,118,394 (valkey) | 475 | about 4,500× |
| Get hit, 64 KiB value | 3,204,597 (valkey) | 12,827 | about 250× |
| Set, 64 B value | 2,079,868 (memcached) | 226 | about 9,200× |
| Set, 1 KiB value | 1,961,782 (memcached) | 465 | about 4,200× |
| Set, 64 KiB value | 2,829,649 (memcached) | 10,010 | about 280× |
| Get hit, 1 KiB value, 16 parallel callers | 525,269 (memcached) | 475 | about 1,100× |
| Set, 1 KiB value, 16 parallel callers | 509,320 (valkey) | 765 | about 670× |

**Allocations per call** (median B/op and allocs/op, written `B / allocs`). A range in brackets means the value varied across rounds:

| Operation | inmemory | memcached | redis | valkey | memcached (16 idle) |
|---|---:|---:|---:|---:|---:|
| Ping | 0 / 0 | 16 / 1 | 172 / 6 | 172 / 6 | 16 / 1 |
| Get of a missing key | 0 / 0 | 32 (30–33) / 1 | 448 / 26 | 448 / 26 | 34 (29–38) / 1 |
| Get hit, 64 B value | 64 / 1 | 208 / 4 | 280 / 7 | 280 / 7 | 224 / 4 |
| Get hit, 1 KiB value | 1,024 / 1 | 1,283 (1,282–1,283) / 4 | 1,352 / 7 | 1,352 / 7 | 1,296 / 4 |
| Get hit, 64 KiB value | 65,536 / 1 | 73,924 (73,920–73,925) / 4 | 73,928 / 7 | 73,928 (73,928–73,942) / 7 | 73,927 (73,923–73,930) / 4 |
| Set, 64 B value | 64 / 1 | 115 (113–122) / 4 | 288 / 9 | 288 / 9 | 119 (113–123) / 4 |
| Set, 1 KiB value | 1,024 / 1 | 129 (125–132) / 5 | 288 / 9 | 288 / 9 | 129 (124–136) / 5 |
| Set, 64 KiB value | 65,536 / 1 | 137 (130–145) / 5 | 288 / 9 | 288 / 9 | 136 (130–145) / 5 |
| Get hit, 1 KiB value, 16 parallel callers | 1,024 / 1 | 1,479 (1,446–1,569) / 4 | 1,589 (1,530–1,768) / 7 (7–8) | 1,561 (1,526–1,624) / 7 | 1,362 (1,362–1,389) / 4 |
| Set, 1 KiB value, 16 parallel callers | 1,024 / 1 | 435 (331–534) / 6 (5–6) | 554 (488–593) / 9 (9–10) | 472 (458–510) / 9 | 172 (159–209) / 5 |

For the sequential operations, B/op varied by at most 15 B across the rounds and allocs/op did not vary. The parallel operations varied more, by up to 238 B in B/op for the network backends (47% of that series' median). For the sequential operations, latency varied much more than allocations: the CV of the network timings is 7–59%.

**Memcached with 2 against 16 idle connections.** On every operation the two series' ranges overlap, so the run shows no latency difference between them. In the parallel `Set` case the larger pool allocated fewer bytes per call (172 B, against 435 B for the default) and one fewer allocation (5 against 6). This report did not look into why.

## Limitations

- **Five rounds only.** The ranges come from five values per series, and a different set of five could give different medians.
- **The network path dominates the network timings.** Every call goes through an `ssh` port forwarder. The raw floor through it had a p50 of 1.5–1.9 ms and a minimum of 1.0–1.4 ms, and a real network would be different. The results do not show the speed of the servers themselves.
- **The machine was not quiet.** The load average was 4.4–6.5 at the start of each round, and the Virtualization.framework VM process was the busiest process at the start of every round. The servers share a 2-CPU VM with six other containers that were running before the run.
- **Mean time per operation only.** Go benchmarks report an average, not p50, p99 or p99.9, and tail latency matters for a cache. None was measured.
- **The parallel cases are not a capacity test.** With 16 callers the three network backends ran 3.7–4.7 times as many operations per second as one caller on the same operation (the ratio of the sequential to the parallel median at 1 KiB), not 16 times. Whether the forwarder, the 2-CPU VM or the server set that limit was not tested.
- **In-memory is not a like-for-like comparison.** It has no network, and it is per process. Its numbers show the library's own cost, which the network backends add to.
- **One configuration.** One value pattern, three value sizes, one key-set size, one ttl, default pool sizes (apart from the Memcached variant) and one hit ratio per case (all hits or all misses).

## Reproducing

```sh
docker compose up -d redis valkey memcached
go test -tags integration -p 1 -run '^$' -bench . -benchmem -count=1 ./...   # one round; repeat for more rounds
```

The Memcached test setup refuses to run if the server already holds items, unless `MEMCACHED_TEST_FLUSH=1` is set, and it flushes the whole server. Run it only against the compose container. The `inmemory` benchmark needs no server and runs without `-tags integration`.
