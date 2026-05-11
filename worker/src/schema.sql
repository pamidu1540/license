-- ═══════════════════════════════════════════════════════════════════════════
--  Logistics Engine License System — Cloudflare D1 Schema v2
-- ═══════════════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS customers (
  id         TEXT    PRIMARY KEY,
  name       TEXT    NOT NULL,
  email      TEXT    NOT NULL UNIQUE,
  company    TEXT    NOT NULL DEFAULT '',
  country    TEXT    NOT NULL DEFAULT '',
  notes      TEXT    NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS licenses (
  id              TEXT    PRIMARY KEY,
  customer_id     TEXT    NOT NULL REFERENCES customers(id),
  key_hash        TEXT    NOT NULL UNIQUE,   -- SHA-256(raw_key)
  plan            TEXT    NOT NULL DEFAULT 'standard',
  max_seats       INTEGER NOT NULL DEFAULT 1,
  status          TEXT    NOT NULL DEFAULT 'active',  -- active|suspended|expired|revoked
  issued_at       INTEGER NOT NULL,
  expires_at      INTEGER,                             -- NULL = perpetual
  issued_by       TEXT    NOT NULL DEFAULT '',
  product_version TEXT    NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_licenses_customer ON licenses(customer_id);
CREATE INDEX IF NOT EXISTS idx_licenses_status   ON licenses(status);
CREATE INDEX IF NOT EXISTS idx_licenses_expires  ON licenses(expires_at);

-- One row per machine. Unique on (license_id, hostname).
CREATE TABLE IF NOT EXISTS activations (
  id                TEXT    PRIMARY KEY,
  license_id        TEXT    NOT NULL REFERENCES licenses(id),
  hostname          TEXT    NOT NULL,
  ip_address        TEXT    NOT NULL DEFAULT '',
  installer_version TEXT    NOT NULL DEFAULT '',
  activated_at      INTEGER NOT NULL,
  last_seen_at      INTEGER NOT NULL,   -- updated on every /v1/check
  revoked_at        INTEGER,            -- NULL = active seat
  UNIQUE(license_id, hostname)
);

CREATE INDEX IF NOT EXISTS idx_activations_license ON activations(license_id);

-- Audit/event log. Pruned after 90 days by the scheduled worker.
CREATE TABLE IF NOT EXISTS events (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  license_id TEXT    NOT NULL,
  hostname   TEXT    NOT NULL DEFAULT '',
  ip_address TEXT    NOT NULL DEFAULT '',
  event      TEXT    NOT NULL,   -- activate_ok|activate_invalid|check_ok|issued|admin_patch|…
  detail     TEXT    NOT NULL DEFAULT '',
  ts         INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_events_license ON events(license_id, ts DESC);
CREATE INDEX IF NOT EXISTS idx_events_ts      ON events(ts DESC);
