/**
 * Logistics Engine — Centralized Licensing Worker
 *
 * Flow:
 *   1. You (admin) issue a license via license-manager TUI → stored in D1
 *   2. Customer runs install.sh → POST /v1/activate → returns a signed 24 h token
 *   3. Engine caches token on disk, checks expiry on every boot
 *   4. Background goroutine re-validates every 12 h (while online)
 *   5. If server unreachable, cached token is valid for up to 24 h from last check
 *   6. If license revoked/suspended, next online check returns 403 → engine shuts down
 *
 * Admin routes (X-Admin-Key required):
 *   POST   /admin/customers          create customer
 *   GET    /admin/customers          list customers
 *   POST   /admin/licenses           create license
 *   GET    /admin/licenses           list licenses  (?status=active|suspended|revoked)
 *   GET    /admin/licenses/:id       get one + activations
 *   PATCH  /admin/licenses/:id       suspend | revoke | extend | change seats
 *
 * Public routes:
 *   POST   /v1/activate              first-time activation → returns signed token
 *   POST   /v1/check                 periodic online check → returns fresh signed token
 *   GET    /health                   unauthenticated ping
 */

export interface Env {
  DB:             D1Database;
  RATE_LIMIT_KV:  KVNamespace;
  ADMIN_API_KEY:  string;   // wrangler secret put ADMIN_API_KEY
  TOKEN_SECRET:   string;   // wrangler secret put TOKEN_SECRET  (openssl rand -hex 32)
}

// ── Constants ─────────────────────────────────────────────────────────────────

const TOKEN_TTL_SECONDS = 24 * 60 * 60;   // 24 hours — clients must re-check within this window
const RATE_LIMIT_WINDOW = 60;             // seconds
const RATE_LIMIT_MAX    = 15;             // requests per window per IP

// ── Helpers ───────────────────────────────────────────────────────────────────

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: {
      "Content-Type":                "application/json",
      "Access-Control-Allow-Origin": "*",
    },
  });
}

function err(message: string, status = 400): Response {
  return json({ success: false, error: message }, status);
}

function now(): number {
  return Math.floor(Date.now() / 1000);
}

async function sha256hex(text: string): Promise<string> {
  const buf = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(text));
  return Array.from(new Uint8Array(buf)).map(b => b.toString(16).padStart(2, "0")).join("");
}

/**
 * HMAC-SHA256 signed token — compact, verifiable without an RSA key pair.
 *
 * Token payload (JSON, base64url encoded):
 *   { sub: licenseId, host: hostname, plan: string,
 *     customer: string, iat: unix, exp: unix, seats: number }
 *
 * Signature: HMAC-SHA256(base64url(header) + "." + base64url(payload), TOKEN_SECRET)
 *
 * The engine verifies the signature locally using the same secret baked into
 * the binary at build time — so validation is instant and offline-capable.
 */
function b64url(data: Uint8Array): string {
  return btoa(String.fromCharCode(...data))
    .replace(/\+/g, "-").replace(/\//g, "_").replace(/=/g, "");
}

async function signToken(payload: object, secret: string): Promise<string> {
  const header  = b64url(new TextEncoder().encode(JSON.stringify({ alg: "HS256", typ: "JWT" })));
  const body    = b64url(new TextEncoder().encode(JSON.stringify(payload)));
  const sigInput = `${header}.${body}`;

  const key = await crypto.subtle.importKey(
    "raw", new TextEncoder().encode(secret),
    { name: "HMAC", hash: "SHA-256" }, false, ["sign"]
  );
  const sigBuf = await crypto.subtle.sign("HMAC", key, new TextEncoder().encode(sigInput));
  const sig = b64url(new Uint8Array(sigBuf));
  return `${sigInput}.${sig}`;
}

async function checkRateLimit(ip: string, env: Env): Promise<boolean> {
  const key   = `rl:${ip}`;
  const raw   = await env.RATE_LIMIT_KV.get(key);
  const count = raw ? parseInt(raw, 10) : 0;
  if (count >= RATE_LIMIT_MAX) return false;
  await env.RATE_LIMIT_KV.put(key, String(count + 1), { expirationTtl: RATE_LIMIT_WINDOW });
  return true;
}

async function requireAdmin(req: Request, env: Env): Promise<boolean> {
  return req.headers.get("X-Admin-Key") === env.ADMIN_API_KEY;
}

async function logEvent(
  db: D1Database,
  licenseId: string, hostname: string, ip: string, event: string, detail = ""
) {
  await db.prepare(
    "INSERT INTO events(license_id,hostname,ip_address,event,detail,ts) VALUES(?,?,?,?,?,?)"
  ).bind(licenseId, hostname, ip, event, detail, now()).run().catch(() => {});
}

// ── Shared license lookup ─────────────────────────────────────────────────────

async function lookupLicense(db: D1Database, keyHash: string) {
  return db.prepare(`
    SELECT l.id, l.status, l.expires_at, l.plan, l.max_seats,
           c.name AS customer_name, c.email AS customer_email, c.company
    FROM   licenses l
    JOIN   customers c ON c.id = l.customer_id
    WHERE  l.key_hash = ?
  `).bind(keyHash).first<{
    id: string; status: string; expires_at: number | null;
    plan: string; max_seats: number;
    customer_name: string; customer_email: string; company: string;
  }>();
}

// ── Route: POST /v1/activate ──────────────────────────────────────────────────
//
// Called by install.sh on first install.
// Validates key, checks seats, upserts activation row, returns a signed token.

async function handleActivate(req: Request, env: Env, ip: string): Promise<Response> {
  if (!(await checkRateLimit(ip, env))) {
    return err("Rate limit exceeded. Wait 60 seconds.", 429);
  }

  let body: { license_key: string; hostname: string; installer_version?: string };
  try { body = await req.json(); }
  catch { return err("Invalid JSON"); }

  const { license_key, hostname, installer_version = "" } = body;
  if (!license_key?.trim() || !hostname?.trim()) {
    return err("license_key and hostname are required");
  }

  const keyHash = await sha256hex(license_key.trim());
  const license = await lookupLicense(env.DB, keyHash);

  if (!license) {
    await logEvent(env.DB, "unknown", hostname, ip, "activate_invalid");
    return err("Invalid license key.", 401);
  }

  if (license.status === "revoked") {
    await logEvent(env.DB, license.id, hostname, ip, "activate_revoked");
    return err("License revoked. Contact support.", 403);
  }
  if (license.status === "suspended") {
    await logEvent(env.DB, license.id, hostname, ip, "activate_suspended");
    return err("License suspended. Contact support.", 403);
  }
  if (license.expires_at && now() > license.expires_at) {
    await env.DB.prepare("UPDATE licenses SET status='expired' WHERE id=?").bind(license.id).run();
    await logEvent(env.DB, license.id, hostname, ip, "activate_expired");
    const d = new Date(license.expires_at * 1000).toISOString().slice(0, 10);
    return err(`License expired on ${d}. Please renew.`, 402);
  }

  // Seat check
  const seatRow = await env.DB
    .prepare("SELECT COUNT(*) AS n FROM activations WHERE license_id=? AND revoked_at IS NULL")
    .bind(license.id).first<{ n: number }>();
  const usedSeats = seatRow?.n ?? 0;

  const existing = await env.DB
    .prepare("SELECT id FROM activations WHERE license_id=? AND hostname=? AND revoked_at IS NULL")
    .bind(license.id, hostname).first<{ id: string }>();

  if (!existing && usedSeats >= license.max_seats) {
    await logEvent(env.DB, license.id, hostname, ip, "activate_seat_limit",
      `used=${usedSeats} max=${license.max_seats}`);
    return err(
      `Seat limit reached (${license.max_seats} licensed, all in use). Contact support.`, 403
    );
  }

  const ts = now();
  if (existing) {
    await env.DB
      .prepare("UPDATE activations SET last_seen_at=?,ip_address=?,installer_version=? WHERE id=?")
      .bind(ts, ip, installer_version, existing.id).run();
  } else {
    await env.DB.prepare(`
      INSERT INTO activations(id,license_id,hostname,ip_address,installer_version,activated_at,last_seen_at)
      VALUES(?,?,?,?,?,?,?)
    `).bind(crypto.randomUUID(), license.id, hostname, ip, installer_version, ts, ts).run();
  }

  await logEvent(env.DB, license.id, hostname, ip, "activate_ok");

  // Issue signed 24 h token
  const exp = ts + TOKEN_TTL_SECONDS;
  const token = await signToken({
    sub:      license.id,
    host:     hostname,
    plan:     license.plan,
    customer: license.customer_name,
    iat:      ts,
    exp,
  }, env.TOKEN_SECRET);

  return json({
    success:     true,
    token,
    token_exp:   exp,
    customer:    license.customer_name,
    company:     license.company,
    plan:        license.plan,
    seats_used:  existing ? usedSeats : usedSeats + 1,
    seats_max:   license.max_seats,
    expires:     license.expires_at
      ? new Date(license.expires_at * 1000).toISOString().slice(0, 10)
      : "never",
  });
}

// ── Route: POST /v1/check ─────────────────────────────────────────────────────
//
// Called by the engine's background goroutine every 12 h.
// Refreshes the signed token; if license is revoked returns 403 → engine shuts down.

async function handleCheck(req: Request, env: Env, ip: string): Promise<Response> {
  if (!(await checkRateLimit(ip, env))) {
    return err("Rate limit exceeded.", 429);
  }

  let body: { license_key: string; hostname: string };
  try { body = await req.json(); }
  catch { return err("Invalid JSON"); }

  const { license_key, hostname } = body;
  if (!license_key?.trim() || !hostname?.trim()) {
    return err("license_key and hostname are required");
  }

  const keyHash = await sha256hex(license_key.trim());
  const license = await lookupLicense(env.DB, keyHash);

  if (!license)                   return json({ valid: false, reason: "invalid_key"   }, 401);
  if (license.status === "revoked")   return json({ valid: false, reason: "revoked"       }, 403);
  if (license.status === "suspended") return json({ valid: false, reason: "suspended"     }, 403);
  if (license.expires_at && now() > license.expires_at) {
    await env.DB.prepare("UPDATE licenses SET status='expired' WHERE id=?").bind(license.id).run();
    return json({ valid: false, reason: "expired" }, 402);
  }

  // Update last_seen_at
  const ts = now();
  await env.DB
    .prepare("UPDATE activations SET last_seen_at=?,ip_address=? WHERE license_id=? AND hostname=? AND revoked_at IS NULL")
    .bind(ts, ip, license.id, hostname).run();

  await logEvent(env.DB, license.id, hostname, ip, "check_ok");

  const exp = ts + TOKEN_TTL_SECONDS;
  const token = await signToken({
    sub:      license.id,
    host:     hostname,
    plan:     license.plan,
    customer: license.customer_name,
    iat:      ts,
    exp,
  }, env.TOKEN_SECRET);

  return json({
    valid:     true,
    token,
    token_exp: exp,
    plan:      license.plan,
    expires:   license.expires_at
      ? new Date(license.expires_at * 1000).toISOString().slice(0, 10)
      : "never",
  });
}

// ── Admin: Customers ──────────────────────────────────────────────────────────

async function handleAdminCustomers(req: Request, env: Env): Promise<Response> {
  if (req.method === "GET") {
    const { results } = await env.DB
      .prepare("SELECT * FROM customers ORDER BY created_at DESC").all();
    return json({ success: true, customers: results });
  }
  if (req.method === "POST") {
    const body: any = await req.json();
    const { name, email, company = "", country = "", notes = "" } = body;
    if (!name || !email) return err("name and email required");
    const id = crypto.randomUUID();
    await env.DB
      .prepare("INSERT INTO customers(id,name,email,company,country,notes,created_at) VALUES(?,?,?,?,?,?,?)")
      .bind(id, name, email, company, country, notes, now()).run();
    return json({ success: true, id }, 201);
  }
  return err("Method not allowed", 405);
}

// ── Admin: Licenses ───────────────────────────────────────────────────────────

async function handleAdminLicenses(req: Request, env: Env): Promise<Response> {
  const url      = new URL(req.url);
  const segments = url.pathname.split("/").filter(Boolean);
  const licId    = segments[2]; // /admin/licenses/:id

  // GET /admin/licenses/:id — detail + activations
  if (req.method === "GET" && licId) {
    const lic = await env.DB.prepare(`
      SELECT l.*, c.name AS customer_name, c.email AS customer_email, c.company,
        (SELECT COUNT(*) FROM activations a WHERE a.license_id=l.id AND a.revoked_at IS NULL) AS seats_used
      FROM licenses l JOIN customers c ON c.id=l.customer_id WHERE l.id=?
    `).bind(licId).first();
    if (!lic) return err("Not found", 404);
    const { results: acts } = await env.DB
      .prepare("SELECT * FROM activations WHERE license_id=? ORDER BY activated_at DESC")
      .bind(licId).all();
    const { results: evts } = await env.DB
      .prepare("SELECT * FROM events WHERE license_id=? ORDER BY ts DESC LIMIT 50")
      .bind(licId).all();
    return json({ success: true, license: lic, activations: acts, events: evts });
  }

  // GET /admin/licenses — list
  if (req.method === "GET") {
    const status = url.searchParams.get("status") || "";
    const q = status
      ? "SELECT l.*,c.name AS customer_name,c.company FROM licenses l JOIN customers c ON c.id=l.customer_id WHERE l.status=? ORDER BY l.issued_at DESC"
      : "SELECT l.*,c.name AS customer_name,c.company FROM licenses l JOIN customers c ON c.id=l.customer_id ORDER BY l.issued_at DESC";
    const { results } = await env.DB.prepare(q).bind(...(status ? [status] : [])).all();
    return json({ success: true, licenses: results });
  }

  // POST /admin/licenses — create
  if (req.method === "POST") {
    const body: any = await req.json();
    const {
      customer_id, raw_key, plan = "standard", max_seats = 1,
      days, issued_by = "", product_version = "",
    } = body;
    if (!customer_id || !raw_key) return err("customer_id and raw_key required");
    const keyHash  = await sha256hex(raw_key.trim());
    const expAt    = days ? now() + Number(days) * 86400 : null;
    const id       = crypto.randomUUID();
    await env.DB.prepare(`
      INSERT INTO licenses(id,customer_id,key_hash,plan,max_seats,status,issued_at,expires_at,issued_by,product_version)
      VALUES(?,?,?,?,?,'active',?,?,?,?)
    `).bind(id, customer_id, keyHash, plan, max_seats, now(), expAt, issued_by, product_version).run();
    await logEvent(env.DB, id, "-", "-", "issued", `plan=${plan} seats=${max_seats}`);
    return json({ success: true, id }, 201);
  }

  // PATCH /admin/licenses/:id — update
  if (req.method === "PATCH" && licId) {
    const body: any = await req.json();
    const patches: string[] = [];
    const vals: unknown[]   = [];

    if (body.status !== undefined) {
      const allowed = ["active", "suspended", "revoked"];
      if (!allowed.includes(body.status)) return err(`status must be one of: ${allowed.join(", ")}`);
      patches.push("status=?"); vals.push(body.status);
    }
    if (body.days !== undefined) {
      patches.push("expires_at=?"); vals.push(now() + Number(body.days) * 86400);
    }
    if (body.max_seats !== undefined) {
      patches.push("max_seats=?"); vals.push(Number(body.max_seats));
    }
    if (patches.length === 0) return err("No valid fields to update");

    vals.push(licId);
    await env.DB.prepare(`UPDATE licenses SET ${patches.join(",")} WHERE id=?`).bind(...vals).run();
    await logEvent(env.DB, licId, "-", "-", "admin_patch", JSON.stringify(body));
    return json({ success: true });
  }

  return err("Method not allowed", 405);
}

// ── Admin: Stats ──────────────────────────────────────────────────────────────

async function handleAdminStats(env: Env): Promise<Response> {
  const [total, active, suspended, revoked, expired, customers, activations] = await Promise.all([
    env.DB.prepare("SELECT COUNT(*) AS n FROM licenses").first<{n:number}>(),
    env.DB.prepare("SELECT COUNT(*) AS n FROM licenses WHERE status='active'").first<{n:number}>(),
    env.DB.prepare("SELECT COUNT(*) AS n FROM licenses WHERE status='suspended'").first<{n:number}>(),
    env.DB.prepare("SELECT COUNT(*) AS n FROM licenses WHERE status='revoked'").first<{n:number}>(),
    env.DB.prepare("SELECT COUNT(*) AS n FROM licenses WHERE status='expired'").first<{n:number}>(),
    env.DB.prepare("SELECT COUNT(*) AS n FROM customers").first<{n:number}>(),
    env.DB.prepare("SELECT COUNT(*) AS n FROM activations WHERE revoked_at IS NULL").first<{n:number}>(),
  ]);
  return json({
    licenses:    { total: total?.n, active: active?.n, suspended: suspended?.n, revoked: revoked?.n, expired: expired?.n },
    customers:   customers?.n,
    activations: activations?.n,
    ts:          now(),
  });
}

// ── Scheduled: prune old events ───────────────────────────────────────────────

async function handleScheduled(env: Env) {
  const cutoff = now() - 90 * 86400;
  await env.DB.prepare("DELETE FROM events WHERE ts < ?").bind(cutoff).run();
}

// ── Router ────────────────────────────────────────────────────────────────────

export default {
  async fetch(req: Request, env: Env): Promise<Response> {
    const url  = new URL(req.url);
    const path = url.pathname;
    const ip   = req.headers.get("CF-Connecting-IP") || "unknown";

    if (req.method === "OPTIONS") {
      return new Response(null, { headers: {
        "Access-Control-Allow-Origin":  "*",
        "Access-Control-Allow-Methods": "GET, POST, PATCH, OPTIONS",
        "Access-Control-Allow-Headers": "Content-Type, X-Admin-Key",
      }});
    }

    if (path === "/health") {
      return json({ ok: true, ts: now(), version: "2.0" });
    }

    if (path === "/v1/activate" && req.method === "POST") return handleActivate(req, env, ip);
    if (path === "/v1/check"    && req.method === "POST") return handleCheck(req, env, ip);

    if (path.startsWith("/admin/")) {
      if (!(await requireAdmin(req, env))) return err("Unauthorized", 401);
      if (path === "/admin/stats")                   return handleAdminStats(env);
      if (path.startsWith("/admin/licenses"))        return handleAdminLicenses(req, env);
      if (path === "/admin/customers")               return handleAdminCustomers(req, env);
      return err("Not found", 404);
    }

    return err("Not found", 404);
  },

  async scheduled(_event: ScheduledEvent, env: Env): Promise<void> {
    await handleScheduled(env);
  },
};
