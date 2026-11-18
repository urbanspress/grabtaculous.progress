# GRAB Ecosystem Master VPS Starter Kit & Scale-Up Playbook (`vps-starter-kit.md`)

**Target Architecture:** Official `nginx.org` NGINX `1.30.6` (Stable, HTTP/3 QUIC, `aio threads;`, `X-Accel-Redirect`) + Hardened `--read-only` Multi-Worker **GRAB `v1.1.0` (`Grisp`) + GrabLab CMS + Grex Theme Builder `v1.0.0` + Groxy AI `v1.8.0` + PostgreSQL 18 + Redis 8.2** + Optional Isolated **WordPress (`php-fpm` + `MariaDB 11.8`)** + **Backblaze B2** Offload & **>= 15 GB NVMe Safety Reserve**

> **Primary Production Target:** **Bluehost Ashburn (`vps-8c-16g`)** — `8 vCPU / 16 GB DDR5 RAM / 200 GB NVMe / 1 Gbit/s` on Ubuntu 24.04 LTS, replacing expiring **Contabo Core (`vps-6c-12g`)** (`6 vCPU / 12 GB RAM / 100 GB NVMe`, expiring **December 8, 2026**).  
> **Zero-Secrets Policy:** Never commit real passwords, API keys, `GRABLAB_MASTER_KEY`, `GRAB_B2_APP_KEY`, or production private IPs to Git. Supply all secrets via untracked `.env` (`0600`) or secret managers.

---

## 1. 10-Minute Fresh Ubuntu 24.04 LTS Bootstrap Checklist

### 1.1 Non-Root SSH Hardening & `known_hosts` SHA-256 Pinning
1. Create a dedicated non-root sudo operator (`deploy`) and install your Ed25519 SSH public key:
   ```bash
   adduser --disabled-password --gecos "" deploy
   usermod -aG sudo,docker deploy
   install -d -m 0700 -o deploy -g deploy /home/deploy/.ssh
   install -m 0600 -o deploy -g deploy /root/.ssh/authorized_keys /home/deploy/.ssh/authorized_keys
   ```
2. Record the new host's SSH Ed25519 SHA-256 fingerprint on the server console and pin it in your local `~/.ssh/known_hosts` before connecting from **Gashboard** or **Groxy Desk**:
   ```bash
   ssh-keygen -l -E sha256 -f /etc/ssh/ssh_host_ed25519_key.pub
   ```
3. Lock down `/etc/ssh/sshd_config.d/99-grabtaculous-hardening.conf` (`PasswordAuthentication no`, `PermitRootLogin no`, `PubkeyAuthentication yes`), validate with `sshd -t`, and reload `ssh`.

### 1.2 UFW Firewall (Public 80/443 TCP+UDP Only — Internal Ports Never Public)
Keep `8765–8772` (Grisp), `8090` (Groxy AI), `9000` (PHP-FPM), `5432` (PostgreSQL 18), `3306` (MariaDB 11.8), `6379` (Redis 8.2), and `9440` (`grabtaculousd`) **strictly on `127.0.0.1` or internal Docker bridges**:
```bash
sudo ufw default deny incoming
sudo ufw default allow outgoing
sudo ufw allow OpenSSH
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
sudo ufw allow 443/udp   # HTTP/3 (QUIC)
sudo ufw --force enable
sudo ufw status numbered
```

### 1.3 Kernel HTTP/3 (QUIC) & Socket Queue Tuning (`/etc/sysctl.d/99-nginx-quic.conf`)
```ini
net.core.rmem_max = 7500000
net.core.wmem_max = 7500000
net.core.somaxconn = 8192
```
Apply immediately: `sudo sysctl --system`

### 1.4 Remove Stock Ubuntu `nginx 1.24.x` -> Install Official `nginx.org` NGINX `1.30.6` (Stable)
> Or run `grabtaculous edge bootstrap-plan --json` to inspect the exact zero-shell `exec.CommandContext` steps.

```bash
# 1. Snapshot existing /etc/nginx before package changes
sudo mkdir -p /var/backups/nginx-pre-130
sudo cp -a /etc/nginx /var/backups/nginx-pre-130/ 2>/dev/null || true

# 2. Stop & purge stock Ubuntu nginx 1.24.x packages
sudo systemctl stop nginx || true
sudo apt-get remove --purge -y nginx nginx-common nginx-core nginx-full nginx-light 'libnginx-mod-*'
sudo apt-get autoremove -y

# 3. Strip legacy Ubuntu directives that break official nginx.org packages:
#    - Remove `include /etc/nginx/modules-enabled/*.conf;`
#    - Replace `include snippets/fastcgi-php.conf;` with `include /etc/nginx/fastcgi_params;`
#    - Replace `listen 443 ssl http2;` with `listen 443 ssl; http2 on;`

# 4. Install official nginx.org Ubuntu 24.04 (noble) stable repo with pinned PGP verification
sudo apt-get update
sudo apt-get install -y curl gnupg2 ca-certificates lsb-release ubuntu-keyring
curl -fsSL https://nginx.org/keys/nginx_signing.key | sudo tee /tmp/nginx_signing.key >/dev/null
gpg --show-keys --with-colons /tmp/nginx_signing.key | grep -q '573BFD6B3D8FBC641079A6ABABF5BD827BD9BF62'
sudo gpg --batch --yes --dearmor -o /usr/share/keyrings/nginx-archive-keyring.gpg /tmp/nginx_signing.key
rm -f /tmp/nginx_signing.key

echo "deb [signed-by=/usr/share/keyrings/nginx-archive-keyring.gpg] https://nginx.org/packages/ubuntu noble nginx" \
  | sudo tee /etc/apt/sources.list.d/nginx.list >/dev/null

printf "Package: *\nPin: origin nginx.org\nPin: release o=nginx\nPin-Priority: 900\n" \
  | sudo tee /etc/apt/preferences.d/99nginx >/dev/null

sudo apt-get update
sudo apt-get install -y nginx
nginx -v   # Expect: nginx/1.30.6 (or >= 1.30.1 official nginx.org stable)
sudo mkdir -p /etc/nginx/sites-available /etc/nginx/sites-enabled /etc/nginx/conf.d /var/www/certbot
```

---

## 2. Hardware Profile Reference Matrix & 15 GB NVMe Reserve Guard

| Profile ID | Hardware Target | NGINX `worker_processes` | Grisp Workers & Ports | Grisp Limit (`GOMEMLIMIT`) | PostgreSQL 18 Limit (`shared_buffers` / `effective_cache`) | Groxy AI | Redis 8.2 (`noeviction`) |
|---|---|---|---|---|---|---|---|
| **`vps-6c-12g`** | Legacy Contabo Core (`6 vCPU / 12 GB / 100 GB NVMe`) | `6` | `4` (`8765–8768`) | `640M` (`540MiB`) | `3072M` (`2GB` / `6GB`) | `512M` | `768M` (`512mb`) |
| **`vps-8c-16g`** | **Bluehost Ashburn (`8 vCPU / 16 GB DDR5 / 200 GB NVMe / 1 Gbit/s`)** | **`8`** | **`6` (`8765–8770`)** | **`768M` (`650MiB`)** | **`4096M` (`2560MB` / `8GB`)** | **`512M`** | **`1024M` (`768mb`)** |
| **`vps-8c-24g`** | Scale-Up Tier 1 (`8 vCPU / 24 GB / 300 GB NVMe`) | `8` | `8` (`8765–8772`) | `1024M` (`860MiB`) | `6144M` (`4GB` / `12GB`) | `1024M` | `1536M` (`1024mb`) |
| **`vps-8c-32g`** | Scale-Up Tier 2 (`8 vCPU / 32 GB / 400 GB NVMe`) | `8` | `8` (`8765–8772`) | `1536M` (`1280MiB`) | `8192M` (`6GB` / `18GB`) | `1024M` | `2048M` (`1536mb`) |
| **`vps-16c-64g`** | Flagship Scale-Up (`16 vCPU / 64 GB / 800 GB NVMe`) | `16` | `8` (`8765–8772`) | `2048M` (`1700MiB`) | `16384M` (`12GB` / `36GB`) | `2048M` | `4096M` (`3072mb`) |

- **Mandatory NVMe Reserve Guard:** Every snapshot, rollout preflight, backup, and migration import enforces **`>= 15 GB` free NVMe space** AND **`<= 92%` disk usage** so PostgreSQL 18 WAL checkpoints and Docker pulls never stall.
- **Instant Template Generation CLI:**
  ```bash
  grabtaculous profile render vps-8c-16g --domain bookstore.example.com --out-dir /opt/grisp-stack
  grabtaculous edge render --profile vps-8c-16g --domain bookstore.example.com --wp-domain blog.bookstore.example.com --out-dir /
  ```

---

## 3. Complete Drop-In Production Configuration Templates

### 3.1 `/etc/nginx/nginx.conf` (Official `nginx.org` `1.30.6` Stable)
```nginx
user nginx;
worker_processes 8; # 8 on Bluehost 8-vCPU (use 6 on Contabo vps-6c-12g)
worker_cpu_affinity auto;
worker_rlimit_nofile 65535;
error_log /var/log/nginx/error.log warn;
pid /run/nginx.pid;

events {
    worker_connections 8192;
    multi_accept on;
    use epoll;
}

http {
    include /etc/nginx/mime.types;
    default_type application/octet-stream;

    log_format main '$remote_addr - $remote_user [$time_local] "$request" '
                    '$status $body_bytes_sent "$http_referer" '
                    '"$http_user_agent" "$http_x_forwarded_for" rt=$request_time';
    access_log /var/log/nginx/access.log main buffer=64k flush=5s;

    sendfile on;
    sendfile_max_chunk 2m;
    aio threads;
    tcp_nopush on;
    tcp_nodelay on;
    keepalive_timeout 65;
    types_hash_max_size 2048;
    server_tokens off;
    client_max_body_size 1m;

    open_file_cache max=10000 inactive=60s;
    open_file_cache_valid 30s;
    open_file_cache_min_uses 2;
    open_file_cache_errors on;

    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_prefer_server_ciphers off;
    ssl_session_cache shared:SSL:20m;
    ssl_session_timeout 1d;
    ssl_session_tickets off;

    # Selective edge Gzip (strictly excludes binary .epub, .pdf, .m4b, .mp3, .webp, .png, .woff2)
    gzip on;
    gzip_vary on;
    gzip_proxied any;
    gzip_comp_level 5;
    gzip_min_length 256;
    gzip_buffers 16 8k;
    gzip_types text/plain text/css application/json application/ld+json application/javascript text/javascript application/xml text/xml image/svg+xml;

    # Outer socket flood shields (HTTP 429)
    limit_req_zone $binary_remote_addr zone=auth_flood:10m rate=10r/s;
    limit_req_zone $binary_remote_addr zone=media_flood:10m rate=5r/s;
    limit_req_zone $binary_remote_addr zone=wp_login_flood:10m rate=2r/s;
    limit_conn_zone $binary_remote_addr zone=media_conn:10m;
    limit_req_status 429;
    limit_conn_status 429;

    map $http_upgrade $connection_upgrade {
        default upgrade;
        ''      close;
    }

    include /etc/nginx/conf.d/*.conf;
    include /etc/nginx/sites-enabled/*.conf;
}
```

### 3.2 `/etc/nginx/sites-available/grisp.conf` (`HTTP/3` + `X-Accel-Redirect` Local NVMe & Backblaze B2)
```nginx
upstream grisp_backend {
    least_conn;
    server 127.0.0.1:8765 max_fails=2 fail_timeout=5s;
    server 127.0.0.1:8766 max_fails=2 fail_timeout=5s;
    server 127.0.0.1:8767 max_fails=2 fail_timeout=5s;
    server 127.0.0.1:8768 max_fails=2 fail_timeout=5s;
    server 127.0.0.1:8769 max_fails=2 fail_timeout=5s;
    server 127.0.0.1:8770 max_fails=2 fail_timeout=5s;
    keepalive 192;
    keepalive_requests 10000;
    keepalive_timeout 60s;
}

server {
    listen 80;
    listen [::]:80;
    server_name bookstore.example.com;
    location /.well-known/acme-challenge/ {
        root /var/www/certbot;
    }
    location / {
        return 301 https://$host$request_uri;
    }
}

server {
    listen 443 quic reuseport;
    listen [::]:443 quic reuseport;
    listen 443 ssl;
    listen [::]:443 ssl;
    http2 on;
    server_name bookstore.example.com;

    ssl_certificate /etc/letsencrypt/live/bookstore.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/bookstore.example.com/privkey.pem;
    quic_retry on;
    quic_gso on;
    ssl_early_data off;

    add_header Alt-Svc 'h3=":443"; ma=86400' always;
    add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header Referrer-Policy "strict-origin-when-cross-origin" always;

    location / {
        proxy_pass http://grisp_backend;
        proxy_http_version 1.1;
        proxy_set_header Connection "";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-Host $host;
        proxy_buffer_size 16k;
        proxy_buffers 16 32k;
        proxy_busy_buffers_size 64k;
        proxy_max_temp_file_size 0;
        proxy_next_upstream error timeout http_502 http_503 http_504;
    }

    location ^~ /api/auth/ {
        limit_req zone=auth_flood burst=20 nodelay;
        proxy_pass http://grisp_backend;
        proxy_http_version 1.1;
        proxy_set_header Connection "";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-Host $host;
        proxy_buffer_size 16k;
        proxy_buffers 16 32k;
        proxy_max_temp_file_size 0;
    }

    location = /api/admin/media/upload {
        client_max_body_size 11m;
        proxy_request_buffering off;
        proxy_pass http://grisp_backend;
        proxy_http_version 1.1;
        proxy_set_header Connection "";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-Host $host;
        proxy_max_temp_file_size 0;
    }

    location ^~ /api/webhooks/ {
        client_max_body_size 2m;
        proxy_next_upstream off;
        proxy_request_buffering off;
        proxy_pass http://grisp_backend;
        proxy_http_version 1.1;
        proxy_set_header Connection "";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-Host $host;
    }

    location ^~ /api/commerce/webhooks/ {
        client_max_body_size 2m;
        proxy_next_upstream off;
        proxy_request_buffering off;
        proxy_pass http://grisp_backend;
        proxy_http_version 1.1;
        proxy_set_header Connection "";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-Host $host;
    }

    location ^~ /media/ {
        limit_req zone=media_flood burst=15 nodelay;
        limit_conn media_conn 10;
        gzip off;
        proxy_buffering off;
        proxy_request_buffering off;
        proxy_max_temp_file_size 0;
        limit_rate_after 20m;
        limit_rate 12m;
        proxy_connect_timeout 10s;
        proxy_send_timeout 600s;
        proxy_read_timeout 600s;
        proxy_pass http://grisp_backend;
        proxy_http_version 1.1;
        proxy_set_header Connection "";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-Host $host;
    }

    location ^~ /_protected_media/ {
        internal;
        alias /var/lib/grisp/uploads/;
        sendfile on;
        aio threads;
        directio 8m;
        output_buffers 2 1m;
        gzip off;
        limit_conn media_conn 10;
        limit_rate_after 20m;
        limit_rate 12m;
        add_header X-Content-Type-Options "nosniff" always;
    }

    location ~ ^/_protected_b2/([^/]+)/(.+)$ {
        internal;
        resolver 1.1.1.1 8.8.8.8 valid=300s ipv6=off;
        set $b2_host $1;
        set $b2_path $2;
        proxy_http_version 1.1;
        proxy_set_header Host $b2_host;
        proxy_set_header Connection "";
        proxy_hide_header x-amz-id-2;
        proxy_hide_header x-amz-request-id;
        proxy_hide_header x-bz-file-id;
        proxy_hide_header x-bz-file-name;
        proxy_hide_header x-bz-content-sha1;
        proxy_ssl_server_name on;
        proxy_buffering off;
        proxy_max_temp_file_size 0;
        gzip off;
        limit_conn media_conn 10;
        limit_rate_after 20m;
        limit_rate 12m;
        proxy_pass https://$b2_host/$b2_path$is_args$args;
    }

    location ^~ /ws/ {
        proxy_pass http://grisp_backend;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
        proxy_set_header Origin $http_origin;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-Host $host;
        proxy_next_upstream off;
    }

    location ~ ^/_(health|ready)$ {
        allow 127.0.0.1;
        allow ::1;
        deny all;
        proxy_pass http://grisp_backend;
        proxy_http_version 1.1;
        proxy_set_header Connection "";
    }
}
```

### 3.3 `/etc/nginx/sites-available/wordpress-fpm.conf` (Isolated Companion Vhost)
```nginx
server {
    listen 80;
    listen [::]:80;
    server_name blog.bookstore.example.com;
    location /.well-known/acme-challenge/ {
        root /var/www/certbot;
    }
    location / {
        return 301 https://$host$request_uri;
    }
}

server {
    listen 443 quic;
    listen [::]:443 quic;
    listen 443 ssl;
    listen [::]:443 ssl;
    http2 on;
    server_name blog.bookstore.example.com;

    root /var/www/wordpress;
    index index.php index.html;
    client_max_body_size 64m;

    ssl_certificate /etc/letsencrypt/live/blog.bookstore.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/blog.bookstore.example.com/privkey.pem;
    quic_retry on;
    quic_gso on;
    ssl_early_data off;

    add_header Alt-Svc 'h3=":443"; ma=86400' always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header Referrer-Policy "strict-origin-when-cross-origin" always;

    location = /xmlrpc.php { deny all; return 403; }
    location ~* /wp-config\.php { deny all; return 403; }
    location ~* ^/wp-content/uploads/.*\.php$ { deny all; return 403; }

    location = /wp-login.php {
        limit_req zone=wp_login_flood burst=5 nodelay;
        include /etc/nginx/fastcgi_params;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
        fastcgi_pass 127.0.0.1:9000;
    }

    location / {
        try_files $uri $uri/ /index.php?$args;
    }

    location ~ \.php$ {
        try_files $uri =404;
        include /etc/nginx/fastcgi_params;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
        fastcgi_index index.php;
        fastcgi_pass 127.0.0.1:9000;
    }
}
```

### 3.4 `deploy/docker-compose.yml` (`vps-8c-16g` Hardened `--read-only` 6-Worker Stack)
Generate the exact profile-tuned file anytime with:
```bash
grabtaculous profile render vps-8c-16g --domain bookstore.example.com --out-dir /opt/grisp-stack
```
Every service runs with `read_only: true`, `cap_drop: ["ALL"]`, `security_opt: ["no-new-privileges:true"]`, `user: "65532:65532"`, `tmpfs` with `noexec,nosuid`, pinned `@sha256:...` digests, `--trusted-proxy ${GRABLAB_TRUSTED_PROXY:-127.0.0.1}`, `GOMEMLIMIT="650MiB"`, Redis `8.2` (`--maxmemory 768mb --maxmemory-policy noeviction`), and `127.0.0.1` loopback port bindings only.

### 3.5 `deploy/postgres/postgresql.conf` (`vps-8c-16g`)
```ini
listen_addresses = '*'
max_connections = 160
shared_buffers = 2560MB
effective_cache_size = 8GB
work_mem = 24MB
maintenance_work_mem = 256MB
wal_level = replica
max_wal_size = 2GB
min_wal_size = 512MB
checkpoint_completion_target = 0.9
random_page_cost = 1.1
effective_io_concurrency = 200
default_toast_compression = lz4
log_min_duration_statement = 250
```

### 3.6 `grab.data.json` & `grab.platform.json`
**`grab.data.json`:**
```json
{
  "version": 1,
  "databases": {
    "main": {
      "driver": "postgres",
      "host": "postgres-18",
      "port": 5432,
      "database": "grablab",
      "user": "grablab",
      "password_env": "POSTGRES_PASSWORD",
      "sslmode": "disable",
      "max_open_conns": 16,
      "max_idle_conns": 8,
      "conn_max_lifetime_seconds": 900
    }
  }
}
```

**`grab.platform.json`:**
```json
{
  "version": 1,
  "caches": {
    "cms": {
      "driver": "redis",
      "host": "redis-8-2",
      "port": 6379,
      "password_env": "REDIS_PASSWORD",
      "namespace": "grablab-prod",
      "max_keys": 50000,
      "max_bytes": 134217728
    }
  }
}
```

### 3.7 `deploy/.env.example` (Placeholder-Only — Copy to Untracked `.env` `0600`)
```env
GRABLAB_IMAGE=ghcr.io/urbanspress/grablab-cms:v1.0.1@sha256:6b212cf89c4d19129f6d4a3c749e1611bb279f7de0e0d4aa0b0b91f0b17f1001
GREX_BUILDER_IMAGE=ghcr.io/urbanspress/grex-builder:v1.0.0@sha256:9f4b23c421a4b1f079a96f8cc03ab31bc10d409e413bcbb52d0a9e77101f1000
GROXY_IMAGE=ghcr.io/urbanspress/groxy-ai:v1.8.0@sha256:3c94bc8306fb9125a41c4a9c46022f7136f8d3ab65d03b7a05226a9b3c417800

GRABLAB_MASTER_KEY=<set-32-plus-random-bytes-via-secret-manager>
GROXY_MASTER_KEY=<set-32-plus-random-bytes-via-secret-manager>
GROXY_PROVIDER_API_KEY=<optional-byok-secret>
POSTGRES_DB=grablab
POSTGRES_USER=grablab
POSTGRES_PASSWORD=<set-strong-postgres-password>
REDIS_PASSWORD=<set-strong-redis-password>
GRABLAB_MAX_UPLOAD_BYTES=10485760
GRABLAB_TRUSTED_PROXY=172.16.0.0/12,127.0.0.1
GROXY_TRUSTED_PROXY=172.16.0.0/12,127.0.0.1

# Optional Backblaze B2 S3 Offload & Off-Box Nightly Backup Env Vars
GRAB_B2_ENDPOINT=s3.us-east-005.backblazeb2.com
GRAB_B2_BUCKET=<your-private-bookstore-bucket>
GRAB_B2_KEY_ID=<set-backblaze-b2-key-id>
GRAB_B2_APP_KEY=<set-backblaze-b2-application-key>
```

### 3.8 `deploy/reload-workers.sh`, `grisp-stack.service`, & `grisp-backup.sh`
Rendered automatically by `grabtaculous profile render vps-8c-16g`:
- `deploy/reload-workers.sh --sighup` / `--recreate`: Sequentially reloads or recreates `grisp-worker-1..6` (`127.0.0.1:8765..8770`), waiting on `GET /_ready === 200` and `"status":"ready"` before advancing to the next worker.
- `deploy/grisp-stack.service`: Systemd oneshot unit calling `docker compose up -d` and `ExecReload=/opt/grisp-stack/deploy/reload-workers.sh --sighup`.
- `deploy/grisp-backup.sh`: Enforces `>= 15 GB` free NVMe and `< 92%` disk utilization before running `pg_dumpall`, `mariadb-dump`, tar archiving `/var/lib/grisp/uploads` + `/var/lib/grisp/public/grex`, generating `SHA256SUMS.txt`, and syncing off-box to Backblaze B2 (`rclone`).

---

## 4. Zero-Downtime Contabo (`vps-6c-12g`) -> Bluehost (`vps-8c-16g`) Cutover Runbook

Execute **before December 8, 2026** so checkouts and email never drop:

1. **Pre-Cutover Preparation on Bluehost Ashburn (`vps-8c-16g`):**
   - Complete Section 1 bootstrap (`nginx.org` `1.30.6`, QUIC sysctl, UFW `443/tcp` + `443/udp`).
   - Render `vps-8c-16g` templates (`grabtaculous profile render vps-8c-16g` & `grabtaculous edge render`).
   - Lower DNS `A`/`AAAA` TTL for the store domain to `300s` inside **Gashboard** or **Groxy Desk** (preserving `MX`/`TXT`/`SPF`/`DKIM` email records `100%` byte-identical).
2. **Export SHA-256 Verified Bundle from Contabo (`vps-6c-12g`):**
   ```bash
   docker exec grabtac-postgres-18 pg_dumpall -U grablab > /tmp/pg18-cutover.sql
   grabtaculous migrate-export \
     --dir /opt/grisp-stack \
     --source-profile vps-6c-12g \
     --target-profile vps-8c-16g \
     --uploads-dir /var/lib/grisp/uploads \
     --grex-dir /var/lib/grisp/public/grex \
     --pg-dump /tmp/pg18-cutover.sql \
     --out /var/backups/contabo-to-bluehost.tar.gz \
     --json
   ```
3. **Transfer & Import onto Bluehost (`vps-8c-16g`):**
   ```bash
   scp -o StrictHostKeyChecking=yes /var/backups/contabo-to-bluehost.tar.gz deploy@<BLUEHOST_PUBLIC_IP>:/tmp/
   grabtaculous migrate-import \
     --bundle /tmp/contabo-to-bluehost.tar.gz \
     --dir /opt/grisp-stack \
     --target-profile vps-8c-16g \
     --uploads-dir /var/lib/grisp/uploads \
     --grex-dir /var/lib/grisp/public/grex \
     --json
   ```
   Restore PostgreSQL 18 dump (`psql`), start stack (`systemctl enable --now grisp-stack`), and issue Let's Encrypt certificate (`certbot certonly --webroot -w /var/www/certbot -d bookstore.example.com`).
4. **Pre-DNS-Switch Smoke Tests on Bluehost (`--resolve` against Bluehost IP):**
   ```bash
   # 1. Verify /_ready is loopback-only externally (403) and 200 locally
   curl -s -o /dev/null -w "%{http_code}\n" https://bookstore.example.com/_ready --resolve bookstore.example.com:443:<BLUEHOST_IP>   # Expect 403
   curl -fsS http://127.0.0.1:8765/_ready   # Expect {"status":"ready",...}

   # 2. Verify HTTP/3 Alt-Svc & Gzip on HTML vs Gzip OFF on .epub/.m4b
   curl -sI --http2 https://bookstore.example.com/ --resolve bookstore.example.com:443:<BLUEHOST_IP> | grep -i alt-svc
   curl -sI -H "Accept-Encoding: gzip" https://bookstore.example.com/ --resolve bookstore.example.com:443:<BLUEHOST_IP> | grep -i content-encoding

   # 3. Verify 429 flood shield on /api/auth/ burst > 20
   for i in $(seq 1 30); do curl -s -o /dev/null -w "%{http_code} " https://bookstore.example.com/api/auth/login --resolve bookstore.example.com:443:<BLUEHOST_IP>; done
   ```
5. **Switch DNS via Gashboard / Groxy Desk:**
   - Apply `A`/`AAAA` cutover in **Gashboard** (`POST /api/https/apply`) or **Groxy Desk** (both verify DNS propagation `422` guard and keep `MX`/`TXT`/`SPF`/`DKIM` email records untouched).
   - Keep Contabo NGINX running in proxy/read-only mode for 24h while DNS TTL converges, then decommission Contabo cleanly before December 8, 2026.

---

## 5. Backblaze B2 ($6.95/TB/mo) Audiobook Offload & Disaster Recovery

1. **Create Private Backblaze B2 Bucket:** Create a **Private** bucket (e.g., `my-bookstore-audiobooks`) in `us-east-005` (`s3.us-east-005.backblazeb2.com`) and generate a scoped Application Key restricted to that bucket.
2. **Configure Runtime Environment Variables (Never in Git):**
   - `GRAB_B2_ENDPOINT=s3.us-east-005.backblazeb2.com`
   - `GRAB_B2_BUCKET=<your-private-bookstore-bucket>`
   - `GRAB_B2_KEY_ID=<key-id>`
   - `GRAB_B2_APP_KEY=<app-key>`
3. **Zero-Temp-Disk Audiobook Streaming (`/_protected_b2/`):**
   - Your Grisp download handler validates the customer's signed download token, mints a short-lived S3 presigned URL for `s3.us-east-005.backblazeb2.com/<bucket>/<audiobook.m4b>?X-Amz-...`, and returns:
     ```http
     X-Accel-Redirect: /_protected_b2/s3.us-east-005.backblazeb2.com/<bucket>/<key>?<presigned_query>
     Content-Disposition: attachment; filename="audiobook.m4b"
     ```
   - Public download URLs stay identical (`/api/commerce/download/<token>`), Grisp releases its Go worker slot in ~1 ms, and NGINX `1.30.6` streams 300–800 MB `.m4b`/`.mp3` files straight from Backblaze B2 with `proxy_buffering off;` and `proxy_max_temp_file_size 0;` — writing **0 bytes** to your local NVMe!
4. **Nightly Off-Box Disaster Recovery Cron:**
   ```cron
   15 3 * * * root /bin/bash -c 'set -a && source /etc/grisp/backup.env && set +a && /opt/grisp-stack/deploy/grisp-backup.sh >> /var/log/grisp-backup.log 2>&1'
   ```
