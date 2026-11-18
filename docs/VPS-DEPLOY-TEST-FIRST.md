# VPS-DEPLOY-TEST-FIRST.md — Mandatory 5-Gate Hardware & Pre-Deploy Verification Protocol

**Applies to:** Every live VPS deployment, migration, edge proxy change, and scale-up across **all GRAB Ecosystem hardware tiers** (`vps-6c-12g`, `vps-8c-16g`, `vps-8c-24g`, `vps-8c-32g`, and `vps-16c-64g`).

> **Non-Negotiable Rule:** Never guess worker counts, NGINX `keepalive` pools, PostgreSQL `shared_buffers`, or Redis `maxmemory` budgets from memory, and never deploy or reload live traffic without passing all **5 Gates** below in order.

---

## Hardware Profile Auto-Selection Matrix (`--profile auto`)

`grabtaculous profile detect` and `--profile auto` probe the live machine natively (zero `sh -c` shell calls per `ADR-017`) and map hardware deterministically:

| Detected Host Specs | Profile ID | NGINX `worker_processes` / `keepalive` | Grisp Workers & Ports | PostgreSQL 18 Limit / `shared_buffers` / `effective_cache` / `io_workers` | Redis 8.2 Limit / `--maxmemory` (`noeviction`) | Groxy AI / Grex Builder |
|---|---|---|---|---|---|---|
| `<= 6 vCPUs` or `< 14 GB RAM` | **`vps-6c-12g`** | `6` / `keepalive 128;` | `4` (`127.0.0.1:8765–8768`) | `3072M` / `2GB` / `6GB` / `3` | `768M` / `512mb` | `512M` / `512M` (`:8771`) |
| `7–8 vCPUs` & `14–20 GB RAM` | **`vps-8c-16g`** | `8` / `keepalive 192;` | `6` (`127.0.0.1:8765–8770`) | `4096M` / `2560MB` / `8GB` / `4` | `1024M` / `768mb` | `512M` / `512M` (`:8771`) |
| `7–8 vCPUs` & `21–27 GB RAM` | **`vps-8c-24g`** | `8` / `keepalive 192;` | `6` (`127.0.0.1:8765–8770`) | `6144M` / `4GB` / `12GB` / `4` | `1536M` / `1280mb` | `1024M` / `512M` (`:8771`) |
| `7–8 vCPUs` & `28–45 GB RAM` | **`vps-8c-32g`** | `8` / `keepalive 192;` | `6` (`127.0.0.1:8765–8770`) | `8192M` / `6GB` / `18GB` / `4` | `2560M` / `2048mb` | `1024M` / `512M` (`:8771`) |
| `> 8 vCPUs` or `> 45 GB RAM` | **`vps-16c-64g`** | `16` / `keepalive 384;` | `12` (`127.0.0.1:8765–8776`) | `16384M` / `12GB` / `36GB` / `6` | `4096M` / `3072mb` | `2048M` / `512M` (`:8777`) |

---

## Gate 1 — Live Hardware & NVMe Reserve Discovery

Run on the target VPS before generating configs or touching containers:

```bash
grabtaculous profile detect --json
```

**Pass Criteria:**
1. Logical vCPUs (`runtime.NumCPU()` / `/proc/cpuinfo`), total & available physical RAM (`/proc/meminfo`), and root filesystem capacity (`syscall.Statfs("/")`) are reported cleanly.
2. `"nvme_reserve_ok": true` — at least **15 GB free disk** AND **<= 92% disk usage** (`GRABTAC-E701` fails closed if violated).
3. Selected `"profile_id"` and `"worker_count"` match the table above for the target VPS.

*(Optional dry-run simulation on any machine: `grabtaculous profile detect --vcpus 8 --ram-mb 16384 --disk-gb 200 --free-disk-gb 140 --json`.)*

---

## Gate 2 — Profile-Matched Config Rendering (`--profile auto`)

Render both the hardened `--read-only` container stack and the NGINX `1.30.x` edge config directly from detected hardware (or stage into a temporary directory first when verifying an already-live server):

```bash
grabtaculous profile render auto --domain bookstore.example.com --out-dir /opt/grisp-stack --json
grabtaculous edge render --profile auto --out-dir / --json
```

**Pass Criteria:**
1. `etc/nginx/nginx.conf` sets profile-matched `worker_processes`, `keepalive_requests 10000;`, `reset_timedout_connection on;`, `gzip_http_version 1.1;`, and profile-aware `open_file_cache`.
2. `etc/nginx/sites-available/grisp.conf`, `etc/nginx/conf.d/grisp-locations.inc`, and `etc/nginx/sites-available/grisp-domain.conf.template` declare exact worker ports and `keepalive` (`4` workers / `keepalive 128` on `vps-6c-12g`; `6` workers / `keepalive 192` on `vps-8c-16g..32g`; `12` workers / `keepalive 384` on `vps-16c-64g`), all 9 production routes (`/_protected_media/`, `/_protected_b2/`, `/api/webhooks/`, `/api/commerce/webhooks/`, `/media/` `limit_rate_after 20m; limit_rate 12m;`), and fail-closed catch-all `444` / `ssl_reject_handshake on;`.
3. `deploy/docker-compose.yml`, `deploy/postgres/postgresql.conf`, `deploy/reload-workers.sh` (`WORKER_COUNT`), `deploy/grab.data.json`, and `deploy/grab.platform.json` match the selected profile.
4. **StrictJSON Guard (`GRAB-E210`):** `deploy/grab.platform.json` contains **zero unknown fields** (never emits an unknown `"count"` field inside `"workers"`) and uses loopback-only `"allowed_cidrs": ["127.0.0.1/32", "::1/128"]` so `grabman doctor --strict` and `grab check` pass with zero RFC-1918 IP warnings.

---

## Gate 3 — Static Admission & Syntax Gate

Validate edge syntax, lockfile integrity, and workspace prerequisites **before** restarting or draining any worker:

```bash
sudo nginx -t
grabtaculous audit --dir /opt/grisp-stack --file deploy/docker-compose.yml --require-digest --json
grabman doctor --dir /opt/grisp-stack --strict --json
grabman solve --dir /opt/grisp-stack --verify --json
```

*(If running in a pre-staged directory without `grabman.json`, verify `grab.data.json` / `grab.platform.json` and `sudo nginx -t` cleanly before cutover.)*

**Pass Criteria:**
- `nginx -t` prints `syntax is ok` and `test is successful`.
- `grabtaculous audit`, `grabman doctor --strict`, and `grabman solve --verify` exit `0`.

---

## Gate 4 — Zero-Downtime Rollout & `/_ready` Probe Gate

Verify every active worker port and companion service responds `200 OK` with `"status":"ready"` (or healthy status) and test a rolling `SIGHUP` reload:

```bash
# 1. Verify every active Grisp worker port (127.0.0.1:8765..P+N-1)
for p in 8765 8766 8767 8768; do   # extend to 8770 for 6-worker or 8776 for 12-worker profiles
  curl -fsS "http://127.0.0.1:${p}/_ready"
done

# 2. Verify companion services on loopback
curl -fsS http://127.0.0.1:8771/_ready   # grex-builder (:8777 on vps-16c-64g)
curl -fsS http://127.0.0.1:8090/_ready   # groxy-ai
curl -fsS http://127.0.0.1:9390/_health  # gashboard (when active)

# 3. Verify zero-downtime rolling worker reload
cd /opt/grisp-stack && ./deploy/reload-workers.sh --sighup
```

**Pass Criteria:**
- Every worker port (`/_ready`), `grex-builder`, `groxy-ai`, and active `gashboard` (`/_health`) returns `HTTP 200` (`"status":"ready"` / `"status":"ok"`).
- `./deploy/reload-workers.sh --sighup` reloads workers sequentially with zero failed `/_ready` gates.

---

## Gate 5 — Edge Security & Memory Headroom Audit

Verify public fail-closed edge behavior, loopback-only welcome/readiness isolation, and Linux NVMe page-cache headroom:

```bash
# 1. Unconfigured public HTTP host/IP drops with 444 (empty reply / connection reset)
curl -sS -o /dev/null -w "http_public=%{http_code}\n" http://127.0.0.1/ -H "Host: unconfigured.example.com"

# 2. Unconfigured public HTTPS TLS handshake is rejected (ssl_reject_handshake on;)
curl -sk -o /dev/null -w "https_public=%{http_code}\n" https://127.0.0.1/ -H "Host: unconfigured.example.com" || echo "TLS handshake rejected (PASS)"

# 3. Loopback localhost serves the Grex-animated Grisp Welcome Page (200 OK)
curl -fsS -o /dev/null -w "localhost_welcome=%{http_code}\n" http://localhost/

# 4. External /_ready is blocked (403 or 444 when tested against public IP/unknown Host)
curl -sS -o /dev/null -w "external_ready=%{http_code}\n" http://127.0.0.1/_ready -H "Host: unconfigured.example.com"

# 5. Memory headroom & container resource footprint (>= 30-40% free host RAM for Linux NVMe page cache)
free -h
docker stats --no-stream --format "table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.MemPerc}}"
```

**Pass Criteria:**
- Unconfigured public HTTP returns `000`/`444` (dropped); unconfigured HTTPS rejects TLS handshake.
- `http://localhost/` returns `200` with the Grex JS v1.0.0 welcome page.
- `/_ready` is never reachable from public hosts (`403` or `444` drop).
- `free -h` confirms `available` host RAM maintains `>= 30–40%` headroom for Linux NVMe page caching.
