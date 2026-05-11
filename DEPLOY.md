# Centralized Licensing System — Full Deployment Guide

## Architecture & Flow

```
┌──────────────────────────────────────────────────────────────────────┐
│  YOU (admin)                                                         │
│  license-manager TUI  ──POST /admin/licenses──►  Cloudflare Worker  │
│                                                  (D1 database)       │
└──────────────────────────────────────────────────────────────────────┘

                          CUSTOMER INSTALL
┌──────────────────────┐  POST /v1/activate   ┌─────────────────────┐
│  install.sh          │ ───────────────────► │  Cloudflare Worker  │
│  (first boot)        │ ◄─────────────────── │  validates key,     │
│                      │   signed 24 h token  │  checks seats/expiry│
│  writes token to     │                      └─────────────────────┘
│  /app/data/          │
│  license_cache.json  │
└──────────────────────┘

                          EVERY ENGINE BOOT
┌──────────────────────┐
│  engine              │  1. Read cache  →  verify HMAC signature + exp
│  VerifyLicense()     │  2. If valid    →  start immediately (offline OK)
│                      │  3. If invalid  →  POST /v1/activate (need network)
└──────────────────────┘

                          EVERY 12 HOURS (background)
┌──────────────────────┐  POST /v1/check      ┌─────────────────────┐
│  engine goroutine    │ ───────────────────► │  Cloudflare Worker  │
│                      │ ◄─────────────────── │  re-validates,      │
│  refreshes cache     │   new 24 h token     │  checks revocation  │
│  (resets 24 h clock) │                      └─────────────────────┘
└──────────────────────┘

  If server unreachable:   cached token valid until token_exp (24 h from last check)
  If 403 (revoked):        engine calls GracefulStop() on next check
  If 402 (expired):        engine calls GracefulStop() on next check
```

---

## Part 1 — Cloudflare Worker

### 1.1 Create secrets

```bash
# One-time: generate two secrets and save them securely
openssl rand -hex 32   # → ADMIN_API_KEY  (for your license-manager TUI)
openssl rand -hex 32   # → TOKEN_SECRET   (baked into engine .env AND worker secret)
```

### 1.2 Deploy worker

```bash
cd licensing/worker
npm install

# Create D1 database
wrangler d1 create logistics-licenses
# → copy database_id into wrangler.toml

# Create KV namespace for rate limiting
wrangler kv:namespace create RATE_LIMIT
# → copy id into wrangler.toml

# Set your domain in wrangler.toml
#   pattern   = "license.yourcompany.com/*"
#   zone_name = "yourcompany.com"
# Also add custom domain in Cloudflare dashboard → Workers → Triggers

# Apply schema
wrangler d1 execute logistics-licenses --remote --file=src/schema.sql

# Set secrets (never in wrangler.toml)
wrangler secret put ADMIN_API_KEY   # paste your ADMIN_API_KEY
wrangler secret put TOKEN_SECRET    # paste your TOKEN_SECRET

# Deploy
npm run deploy
```

### 1.3 Verify

```bash
curl https://license.yourcompany.com/health
# → {"ok":true,"ts":...,"version":"2.0"}
```

---

## Part 2 — License Manager TUI

### 2.1 Build

```bash
cd licensing/manager
go build -ldflags="-w -s" -o license-manager .
```

### 2.2 Configure

First run creates `~/.config/logistics-license-manager/config.toml`. Edit it:

```toml
api_base        = "https://license.yourcompany.com"
admin_key       = "<your ADMIN_API_KEY>"
issued_by       = "Your Name"
product_version = "4.0.0"
```

Or use env vars: `LM_API_BASE`, `LM_ADMIN_KEY`.

### 2.3 Issue a license

```
./license-manager
  n  →  Issue new license
         Step 1: customer name, email, company, country
         Step 2: plan (1=Standard 2=Professional 3=Enterprise), seats, days (0=perpetual)
         Step 3: confirm with y
  →  License key displayed + PDF saved to ~/Downloads/
```

TUI key bindings:

| Key     | Action                    |
|---------|---------------------------|
| `n`     | Issue new license         |
| `l`     | List all licenses         |
| `Enter` | View license detail       |
| `a`     | Activate (un-suspend)     |
| `s`     | Suspend                   |
| `r`     | Revoke permanently        |
| `e`     | Extend by 365 days        |

---

## Part 3 — Engine Deployment

### 3.1 Files to update in ECP

Replace these two files in the `engine/` directory:
- `engine/license_verifier.go`  →  use the new version (HMAC token, 24 h offline cache)
- `engine/main.go`              →  use the new version (wires background checker)

### 3.2 Configure .env

```bash
cd deploy
cp .env.example .env
nano .env
```

Required fields:

```env
TELEGRAM_BOT_TOKEN=<from @BotFather>
INTERNAL_TOKEN=<openssl rand -hex 32>
LICENSE_KEY=ENG-XXXX-XXXX-XXXX-XXXX    # key from license-manager TUI
LICENSE_API_URL=https://license.yourcompany.com
TOKEN_SECRET=<same value as Cloudflare Worker TOKEN_SECRET secret>
```

### 3.3 Build and start

```bash
docker compose up -d --build
```

Engine startup logs to watch for:

```
[LICENSE] Contacting license server for activation...
[LICENSE] Activated  customer=Acme Logistics  plan=standard  expires=2026-05-10
engine ready  socket=/app/sockets/logistics.sock  version=4.0.0
```

On subsequent boots (cache valid):

```
[LICENSE] Offline validation OK  customer=Acme Logistics  plan=standard  token_exp=2026-05-11 14:32
engine ready  ...
```

### 3.4 Background checker

Every 12 h the engine logs one of:

```
[LICENSE] Token refreshed  plan=standard  new_exp=2026-05-12 02:15
```

or (if revoked):

```
[LICENSE] License revoked/suspended — shutting down  reason=revoked
```

---

## Part 4 — Offline Behaviour

| Scenario                                  | Result                                      |
|-------------------------------------------|---------------------------------------------|
| Server online, license valid              | Token refreshed every 12 h                  |
| Server unreachable, token < 24 h old      | Engine starts normally, logs a warning      |
| Server unreachable, token ≥ 24 h old      | Engine refuses to start, clear error logged |
| Server returns 403 (revoked/suspended)    | Engine shuts down gracefully on next check  |
| Server returns 402 (license expired)      | Engine shuts down gracefully on next check  |
| `LICENSE_KEY` changed (reactivation)      | Old cache ignored, fresh activation forced  |

---

## Part 5 — Rotating TOKEN_SECRET

If you rotate `TOKEN_SECRET` (e.g. suspected compromise):

1. Update the Cloudflare Worker secret: `wrangler secret put TOKEN_SECRET`
2. Update `TOKEN_SECRET` in every engine's `.env` and restart
3. All cached tokens become invalid → engines re-activate on next boot (need network once)

---

## Security Notes

- `TOKEN_SECRET` and `ADMIN_API_KEY` never appear in wrangler.toml — always Wrangler secrets
- The raw license key is never stored on the server; only its SHA-256 hash is in D1
- The signed token contains no private data beyond license ID, hostname, plan, and customer name
- Rate limiting: 15 requests per IP per 60 s on public endpoints
