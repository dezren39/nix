//! repo-queue — client for the laptop queue-broker (code#5362).
//!
//! Same CLI surface, exit codes, and stdout formats as the retired
//! repo-queue.py. Two transports reach the same queue-broker: from
//! the sandbox, the session proxy (the laptop-agent broker's Unix
//! socket); on the laptop itself, a direct HTTP connection selected
//! by REPO_QUEUE_BROKER_URL or ~/.config/repo-queue/broker-url (the
//! opencode claimant and the worker sessions it launches use this).
//! No Bitwarden fetch, no direct Supabase access. Exit 3 means the
//! broker path itself is unavailable.

use std::io::{Read, Write};
use std::net::TcpStream;
use std::os::unix::net::UnixStream;
use std::process::exit;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use serde_json::{json, Map, Value};

const QUEUE_BROKER_PORT: u64 = 4801;
const DEFAULT_QUEUE: &str = "repo-health-code-shadow";
const DEFAULT_WORKER: &str = "repo-worker-code";
const TELEMETRY_FIELDS: [&str; 4] = ["pool", "model", "effort", "cost_usd"];
const TERMINAL_STATUSES: [&str; 3] = ["done", "cancelled", "failed"];

fn home() -> String {
    std::env::var("HOME").unwrap_or_default()
}

fn eprint(s: &str) {
    eprintln!("{s}");
}

fn die(code: i32, msg: &str) -> ! {
    eprintln!("repo-queue: {msg}");
    exit(code);
}

fn print_json(v: &Value) {
    println!(
        "{}",
        serde_json::to_string_pretty(v).unwrap_or_else(|_| "null".to_string())
    );
}

// ---------------------------------------------------------------------------
// Session-proxy transport
// ---------------------------------------------------------------------------

fn broker_sock() -> String {
    format!("{}/.cache/laptop-agent/broker.sock", home())
}

// ---------------------------------------------------------------------------
// Direct-broker transport (on-laptop use: the opencode claimant and
// the worker sessions it launches run on the same host as the
// queue-broker, where no session proxy exists). Selected by
// REPO_QUEUE_BROKER_URL or ~/.config/repo-queue/broker-url; the token
// comes from REPO_QUEUE_TOKEN_FILE or ~/.config/queue-broker/token.
// With neither configured, the session-proxy transport below is used,
// exactly as before.
// ---------------------------------------------------------------------------

fn direct_broker_url() -> Option<String> {
    if let Ok(u) = std::env::var("REPO_QUEUE_BROKER_URL") {
        let u = u.trim().to_string();
        if !u.is_empty() {
            return Some(u);
        }
    }
    let p = format!("{}/.config/repo-queue/broker-url", home());
    if let Ok(s) = std::fs::read_to_string(&p) {
        let u = s.trim().to_string();
        if !u.is_empty() {
            return Some(u);
        }
    }
    None
}

fn direct_roundtrip(url: &str, path: &str, body: &Value) -> Result<Value, String> {
    let rest = url
        .strip_prefix("http://")
        .ok_or("broker URL must be http://host:port")?;
    let hostport = rest.trim_end_matches('/');
    if hostport.is_empty() || hostport.contains('/') {
        return Err(format!("bad broker URL {url:?}"));
    }
    let token_file = std::env::var("REPO_QUEUE_TOKEN_FILE")
        .unwrap_or_else(|_| format!("{}/.config/queue-broker/token", home()));
    let token = std::fs::read_to_string(&token_file)
        .map_err(|e| format!("cannot read queue-broker token file {token_file}: {e}"))?;
    let token = token.trim();
    let payload = serde_json::to_vec(body).unwrap();
    let mut s = TcpStream::connect(hostport)
        .map_err(|e| format!("cannot connect to queue-broker at {hostport}: {e}"))?;
    s.set_read_timeout(Some(Duration::from_secs(300))).ok();
    s.set_write_timeout(Some(Duration::from_secs(60))).ok();
    let head = format!(
        "POST {path} HTTP/1.1\r\nHost: {hostport}\r\nX-Agent-Token: {token}\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n",
        payload.len()
    );
    s.write_all(head.as_bytes())
        .and_then(|_| s.write_all(&payload))
        .map_err(|e| format!("queue-broker write failed: {e}"))?;
    let mut raw = Vec::new();
    s.read_to_end(&mut raw)
        .map_err(|e| format!("queue-broker read failed: {e}"))?;
    let split = raw
        .windows(4)
        .position(|w| w == b"\r\n\r\n")
        .ok_or("queue-broker bad reply")?;
    let head_txt = String::from_utf8_lossy(&raw[..split]).to_string();
    let status: u64 = head_txt
        .split_whitespace()
        .nth(1)
        .and_then(|c| c.parse().ok())
        .unwrap_or(0);
    let resp_body: Value = serde_json::from_slice(&raw[split + 4..]).unwrap_or(Value::Null);
    if status == 401 {
        die(3, "queue-broker rejected the client token (HTTP 401)");
    }
    if status >= 400 {
        let msg = resp_body
            .get("error")
            .and_then(|e| e.as_str())
            .unwrap_or("queue-broker error")
            .to_string();
        die(3, &msg);
    }
    Ok(resp_body)
}

fn broker_roundtrip(path: &str, body: &Value) -> Result<Value, String> {
    if let Some(url) = direct_broker_url() {
        return direct_roundtrip(&url, path, body);
    }
    let payload = json!({
        "method": "POST",
        "path": path,
        "body": body,
        "port": QUEUE_BROKER_PORT,
        "token_file": format!("{}/tools/queue-broker-token", home()),
    });
    let data = serde_json::to_vec(&payload).unwrap();
    for attempt in 0..2 {
        match unix_roundtrip(&data) {
            Ok(resp) => {
                let status = resp.get("status").and_then(|s| s.as_u64()).unwrap_or(0);
                let body = resp.get("body").cloned().unwrap_or(Value::Null);
                if status == 401 {
                    die(3, "queue-broker rejected the client token (HTTP 401)");
                }
                if status >= 400 {
                    let msg = body
                        .get("error")
                        .and_then(|e| e.as_str())
                        .unwrap_or("session proxy error")
                        .to_string();
                    die(3, &msg);
                }
                return Ok(body);
            }
            Err(e) => {
                if attempt == 0 {
                    spawn_broker();
                    continue;
                }
                return Err(e);
            }
        }
    }
    Err("unreachable".to_string())
}

fn unix_roundtrip(data: &[u8]) -> Result<Value, String> {
    let mut s = UnixStream::connect(broker_sock())
        .map_err(|e| format!("cannot connect to session proxy: {e}"))?;
    s.set_read_timeout(Some(Duration::from_secs(660))).ok();
    s.set_write_timeout(Some(Duration::from_secs(60))).ok();
    s.write_all(&(data.len() as u64).to_be_bytes())
        .and_then(|_| s.write_all(data))
        .map_err(|e| format!("session proxy write failed: {e}"))?;
    let mut hdr = [0u8; 8];
    s.read_exact(&mut hdr)
        .map_err(|e| format!("session proxy read failed: {e}"))?;
    let n = u64::from_be_bytes(hdr) as usize;
    let mut buf = vec![0u8; n];
    s.read_exact(&mut buf)
        .map_err(|e| format!("session proxy read failed: {e}"))?;
    serde_json::from_slice(&buf).map_err(|e| format!("session proxy bad reply: {e}"))
}

fn spawn_broker() {
    let dir = format!("{}/.cache/laptop-agent", home());
    let _ = std::fs::create_dir_all(&dir);
    let log = std::fs::OpenOptions::new()
        .create(true)
        .append(true)
        .open(format!("{dir}/broker.log"));
    let (out, err) = match log {
        Ok(f) => (
            std::process::Stdio::from(f.try_clone().unwrap()),
            std::process::Stdio::from(f),
        ),
        Err(_) => (std::process::Stdio::null(), std::process::Stdio::null()),
    };
    let _ = std::process::Command::new("python3")
        .arg(format!("{}/tools/laptop-agent-broker.py", home()))
        .stdout(out)
        .stderr(err)
        .stdin(std::process::Stdio::null())
        .spawn();
    for _ in 0..50 {
        if std::path::Path::new(&broker_sock()).exists() {
            // Give the listener a moment to accept.
            std::thread::sleep(Duration::from_millis(100));
            return;
        }
        std::thread::sleep(Duration::from_millis(100));
    }
}

/// POST one queue-broker command; dies (exit 3) when the broker path
/// is unavailable, otherwise returns the response envelope (which may
/// itself be {ok:false, exit, error} — the caller decides).
fn call(path: &str, body: &Value) -> Value {
    match broker_roundtrip(path, body) {
        Ok(v) => v,
        Err(e) => die(3, &e),
    }
}

/// Unwrap an envelope: on {ok:false} print the error and exit with its
/// code; otherwise return the envelope for field extraction.
fn unwrap_ok(resp: Value) -> Value {
    if resp.get("ok").and_then(|o| o.as_bool()) == Some(false) {
        let code = resp.get("exit").and_then(|e| e.as_i64()).unwrap_or(4) as i32;
        let msg = resp
            .get("error")
            .and_then(|e| e.as_str())
            .unwrap_or("queue-broker error")
            .to_string();
        die(code, &msg);
    }
    resp
}

fn require_row(resp: &Value, fn_name: &str) -> Value {
    let row = resp.get("row").cloned().unwrap_or(Value::Null);
    if row.is_null() {
        die(
            5,
            &format!(
                "{fn_name}: no matching claimed row (task not claimed by this worker, or already terminal)"
            ),
        );
    }
    row
}

fn row_or_ledger(resp: &Value, fn_name: &str) {
    let row = require_row(resp, fn_name);
    if let Some(ledger) = resp.get("lane_ledger") {
        print_json(&json!({"task": row, "lane_ledger": ledger}));
    } else {
        print_json(&row);
    }
}

// ---------------------------------------------------------------------------
// Time helpers (ISO 8601 <-> epoch, civil-date math)
// ---------------------------------------------------------------------------

fn days_from_civil(y: i64, m: i64, d: i64) -> i64 {
    let y = if m <= 2 { y - 1 } else { y };
    let era = if y >= 0 { y } else { y - 399 } / 400;
    let yoe = y - era * 400;
    let mp = (m + 9) % 12;
    let doy = (153 * mp + 2) / 5 + d - 1;
    let doe = yoe * 365 + yoe / 4 - yoe / 100 + doy;
    era * 146097 + doe - 719468
}

fn civil_from_days(z: i64) -> (i64, i64, i64) {
    let z = z + 719468;
    let era = if z >= 0 { z } else { z - 146096 } / 146097;
    let doe = z - era * 146097;
    let yoe = (doe - doe / 1460 + doe / 36524 - doe / 146096) / 365;
    let y = yoe + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = doy - (153 * mp + 2) / 5 + 1;
    let m = if mp < 10 { mp + 3 } else { mp - 9 };
    (if m <= 2 { y + 1 } else { y }, m, d)
}

/// Parse an ISO 8601 timestamp to epoch microseconds (None if unusable).
fn parse_ts(v: &Value) -> Option<i64> {
    let s = v.as_str()?;
    parse_ts_str(s)
}

fn parse_ts_str(s: &str) -> Option<i64> {
    let (date, rest) = s.split_once('T')?;
    let mut dp = date.split('-');
    let y: i64 = dp.next()?.parse().ok()?;
    let mo: i64 = dp.next()?.parse().ok()?;
    let d: i64 = dp.next()?.parse().ok()?;
    let (time, offset_min) = if let Some(t) = rest.strip_suffix('Z') {
        (t, 0i64)
    } else if let Some(idx) = rest.rfind(['+', '-']) {
        if idx == 0 {
            return None;
        }
        let (t, off) = rest.split_at(idx);
        let sign = if rest.as_bytes()[idx] == b'-' { -1 } else { 1 };
        let mut op = off[1..].split(':');
        let oh: i64 = op.next()?.parse().ok()?;
        let om: i64 = op.next().unwrap_or("0").parse().ok()?;
        (t, sign * (oh * 60 + om))
    } else {
        (rest, 0)
    };
    let mut tp = time.split(':');
    let h: i64 = tp.next()?.parse().ok()?;
    let mi: i64 = tp.next()?.parse().ok()?;
    let sec_part = tp.next().unwrap_or("0");
    let (sec_s, frac) = match sec_part.split_once('.') {
        Some((a, b)) => (a, b),
        None => (sec_part, ""),
    };
    let sec: i64 = sec_s.parse().ok()?;
    let mut micros: i64 = 0;
    let mut scale = 100_000i64;
    for c in frac.chars().take(6) {
        micros += c.to_digit(10)? as i64 * scale;
        scale /= 10;
    }
    let days = days_from_civil(y, mo, d);
    let epoch_s = days * 86400 + h * 3600 + mi * 60 + sec - offset_min * 60;
    Some(epoch_s * 1_000_000 + micros)
}

fn now_micros() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_micros() as i64)
        .unwrap_or(0)
}

fn utcnow_iso() -> String {
    let secs = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0);
    let days = secs.div_euclid(86400);
    let rem = secs.rem_euclid(86400);
    let (y, m, d) = civil_from_days(days);
    format!(
        "{y:04}-{m:02}-{d:02}T{:02}:{:02}:{:02}Z",
        rem / 3600,
        (rem % 3600) / 60,
        rem % 60
    )
}

fn age_str(created: &Value) -> String {
    let Some(c) = parse_ts(created) else {
        return String::new();
    };
    let secs = ((now_micros() - c) / 1_000_000).max(0);
    let (d, rem) = (secs / 86400, secs % 86400);
    let (h, rem) = (rem / 3600, rem % 3600);
    let (m, s) = (rem / 60, rem % 60);
    if d > 0 {
        format!("{d}d {h:02}:{m:02}")
    } else {
        format!("{h}:{m:02}:{s:02}")
    }
}

// ---------------------------------------------------------------------------
// Formatting (mirrors repo-queue.py tables)
// ---------------------------------------------------------------------------

fn fmt_cell(v: Option<&Value>) -> String {
    match v {
        None | Some(Value::Null) => String::new(),
        Some(Value::Object(_)) | Some(Value::Array(_)) => {
            serde_json::to_string(v.unwrap()).unwrap_or_default()
        }
        Some(Value::String(s)) => s.clone(),
        Some(other) => other.to_string(),
    }
}

fn truncate(s: &str, n: usize) -> String {
    if s.chars().count() <= n {
        s.to_string()
    } else {
        let head: String = s.chars().take(n - 1).collect();
        format!("{head}…")
    }
}

fn print_table(cols: &[String], rows: &[Vec<String>]) {
    let mut widths: Vec<usize> = cols.iter().map(|c| c.chars().count()).collect();
    for r in rows {
        for (i, c) in r.iter().enumerate() {
            widths[i] = widths[i].max(c.chars().count());
        }
    }
    let fmt_row = |cells: &[String]| -> String {
        cells
            .iter()
            .enumerate()
            .map(|(i, c)| {
                let pad = widths[i] - c.chars().count();
                format!("{}{}", c, " ".repeat(pad))
            })
            .collect::<Vec<_>>()
            .join("  ")
    };
    println!("{}", fmt_row(cols));
    let dashes: Vec<String> = cols.iter().map(|c| "-".repeat(c.chars().count())).collect();
    println!("{}", fmt_row(&dashes));
    for r in rows {
        println!("{}", fmt_row(r));
    }
}

// ---------------------------------------------------------------------------
// Argument parsing (argparse-shaped subset the fleet uses)
// ---------------------------------------------------------------------------

struct Args {
    pos: Vec<String>,
    opts: Map<String, Value>,
}

fn parse_args(rest: &[String], flags: &[&str], bools: &[&str]) -> Args {
    let mut pos = Vec::new();
    let mut opts = Map::new();
    let mut i = 0;
    while i < rest.len() {
        let a = &rest[i];
        if let Some(long) = a.strip_prefix("--") {
            let (name, inline) = match long.split_once('=') {
                Some((n, v)) => (n.to_string(), Some(v.to_string())),
                None => (long.to_string(), None),
            };
            if bools.contains(&name.as_str()) {
                opts.insert(name, Value::Bool(true));
                i += 1;
            } else if flags.contains(&name.as_str()) {
                if let Some(v) = inline {
                    opts.insert(name, Value::String(v));
                    i += 1;
                } else if i + 1 < rest.len() {
                    opts.insert(name, Value::String(rest[i + 1].clone()));
                    i += 2;
                } else {
                    die(2, &format!("argument --{name}: expected one argument"));
                }
            } else {
                die(2, &format!("unrecognized argument: --{name}"));
            }
        } else {
            pos.push(a.clone());
            i += 1;
        }
    }
    Args { pos, opts }
}

impl Args {
    fn get(&self, name: &str) -> Option<String> {
        self.opts.get(name).and_then(|v| v.as_str()).map(|s| s.to_string())
    }
    fn has(&self, name: &str) -> bool {
        self.opts.contains_key(name)
    }
    fn get_i64(&self, name: &str) -> Option<i64> {
        self.get(name).and_then(|s| s.parse().ok())
    }
    fn pos_i64(&self, idx: usize, what: &str) -> i64 {
        match self.pos.get(idx).and_then(|s| s.parse::<i64>().ok()) {
            Some(n) => n,
            None => die(2, &format!("argument {what}: invalid int value")),
        }
    }
}

fn default_queue() -> String {
    std::env::var("REPO_QUEUE_DEFAULT_QUEUE").unwrap_or_else(|_| DEFAULT_QUEUE.to_string())
}

fn opt_queue(args: &Args) -> String {
    args.get("queue").unwrap_or_else(default_queue)
}

fn opt_worker(args: &Args) -> String {
    args.get("worker").unwrap_or_else(|| DEFAULT_WORKER.to_string())
}

fn load_packet(path: &str) -> Value {
    let text = match std::fs::read_to_string(path) {
        Ok(t) => t,
        Err(e) => die(2, &format!("cannot read packet file: {e}")),
    };
    match serde_json::from_str(&text) {
        Ok(v) => v,
        Err(e) => die(2, &format!("packet file is not valid JSON: {e}")),
    }
}

fn progress_arg(args: &Args) -> Option<Value> {
    let raw = args.get("progress");
    let pfile = args.get("progress-file");
    if raw.is_some() && pfile.is_some() {
        die(2, "pass only one of --progress / --progress-file");
    }
    let raw = if let Some(f) = pfile {
        match std::fs::read_to_string(&f) {
            Ok(t) => Some(t),
            Err(e) => die(2, &format!("cannot read progress file: {e}")),
        }
    } else {
        raw
    };
    let raw = raw?;
    let val: Value = match serde_json::from_str(&raw) {
        Ok(v) => v,
        Err(e) => die(2, &format!("--progress is not valid JSON: {e}")),
    };
    if !val.is_object() {
        die(2, "--progress must be a JSON object");
    }
    Some(val)
}

fn execution_arg(args: &Args) -> Option<Value> {
    let raw = args.get("execution")?;
    let val: Value = match serde_json::from_str(&raw) {
        Ok(v) => v,
        Err(e) => die(2, &format!("--execution is not valid JSON: {e}")),
    };
    if !val.is_object() {
        die(2, "--execution must be a JSON object");
    }
    if let Some(lane) = val.get("lane") {
        if lane != &json!("opencode") && lane != &json!("direct") {
            die(2, "--execution.lane must be 'opencode' or 'direct'");
        }
    }
    Some(val)
}

fn py_str_list(items: &[String]) -> String {
    let inner: Vec<String> = items.iter().map(|s| format!("'{s}'")).collect();
    format!("[{}]", inner.join(", "))
}

fn check_telemetry_fields(obj: &Value, source: &str) {
    for field in TELEMETRY_FIELDS {
        let Some(v) = obj.get(field) else { continue };
        if v.is_null() {
            continue;
        }
        if field == "cost_usd" {
            let ok = matches!(v, Value::Number(n) if n.as_f64().map(|x| x >= 0.0).unwrap_or(false));
            if !ok {
                die(
                    2,
                    &format!("{source}: telemetry field 'cost_usd' must be a non-negative number or null"),
                );
            }
        } else if !matches!(v, Value::String(s) if !s.is_empty()) {
            die(
                2,
                &format!("{source}: telemetry field '{field}' must be a non-empty string or null"),
            );
        }
    }
}

/// Shared validate flow for `validate` and `enqueue`: returns warnings
/// after printing them; dies (exit 2) on errors exactly like
/// repo-queue.py's check_packet.
fn validate_via_broker(packet: &Value, quiet: bool) -> Vec<String> {
    let resp = call("/validate", &json!({"packet": packet}));
    let resp = unwrap_ok(resp);
    let warnings: Vec<String> = resp
        .get("warnings")
        .and_then(|w| w.as_array())
        .map(|a| a.iter().filter_map(|w| w.as_str().map(|s| s.to_string())).collect())
        .unwrap_or_default();
    let errors: Vec<String> = resp
        .get("errors")
        .and_then(|w| w.as_array())
        .map(|a| a.iter().filter_map(|w| w.as_str().map(|s| s.to_string())).collect())
        .unwrap_or_default();
    for w in &warnings {
        eprint(&format!("warning: {w}"));
    }
    if !errors.is_empty() {
        for e in &errors {
            eprint(&format!("error: {e}"));
        }
        die(
            2,
            &format!("packet validation failed ({} error(s))", errors.len()),
        );
    }
    if !quiet {
        eprint("packet valid (repo-task/v1)");
    }
    warnings
}

// ---------------------------------------------------------------------------
// Subcommands
// ---------------------------------------------------------------------------

fn cmd_validate(rest: &[String]) -> i32 {
    let args = parse_args(rest, &[], &[]);
    let path = args.pos.first().cloned().unwrap_or_else(|| die(2, "validate: packet required"));
    let packet = load_packet(&path);
    validate_via_broker(&packet, false);
    print_json(&json!({
        "valid": true,
        "dedupe_key": packet.get("dedupe_key").cloned().unwrap_or(Value::Null),
        "kind": packet.get("payload").and_then(|p| p.get("kind")).cloned().unwrap_or(Value::Null),
        "title": packet.get("title").cloned().unwrap_or(Value::Null),
    }));
    0
}

fn cmd_enqueue(rest: &[String]) -> i32 {
    let args = parse_args(rest, &["queue"], &["dry-run"]);
    let path = args.pos.first().cloned().unwrap_or_else(|| die(2, "enqueue: packet required"));
    let packet = load_packet(&path);
    let dry_run = args.has("dry-run");
    validate_via_broker(&packet, dry_run);
    let queue = opt_queue(&args);
    let call_started = now_micros();
    let resp = call(
        "/enqueue",
        &json!({"packet": packet, "queue": queue, "dry_run": dry_run}),
    );
    let resp = unwrap_ok(resp);
    if dry_run {
        let mut task = resp.get("task").cloned().unwrap_or(json!({}));
        if let Some(o) = task.as_object_mut() {
            o.insert("_dry_run".to_string(), Value::Bool(true));
            o.insert("_rpc".to_string(), json!("agent.enqueue_repo_task"));
        }
        print_json(&task);
        return 0;
    }
    let row = resp.get("row").cloned().unwrap_or(Value::Null);
    // Outcome classification (mirrors _enqueue_outcome in repo-queue.py).
    let tid = row.get("id").cloned().unwrap_or(Value::Null);
    let q = row.get("queue").and_then(|v| v.as_str()).unwrap_or("").to_string();
    let s = row.get("status").and_then(|v| v.as_str()).unwrap_or("").to_string();
    let created_raw = row.get("created_at").cloned().unwrap_or(Value::Null);
    let created = parse_ts(&created_raw);
    let updated = parse_ts(&row.get("updated_at").cloned().unwrap_or(Value::Null));
    let skew = 10_000_000i64; // 10 s in micros
    let mut warnings: Vec<String> = Vec::new();
    let line;
    if created.map(|c| c >= call_started - skew).unwrap_or(false) {
        line = format!("enqueued new task {} (queue {q}, status {s})", fmt_cell(Some(&tid)));
    } else {
        if q != queue {
            warnings.push(format!(
                "dedupe hit is in a different queue ({q} != requested {queue})"
            ));
        }
        if s == "queued" && updated.map(|u| u >= call_started - skew).unwrap_or(false) {
            line = format!(
                "revived: existing task {} (queue {q}, status {s}, created {})",
                fmt_cell(Some(&tid)),
                fmt_cell(Some(&created_raw))
            );
        } else {
            if TERMINAL_STATUSES.contains(&s.as_str()) {
                warnings.push(format!("dedupe hit is in a terminal state (status {s})"));
            }
            line = format!(
                "dedupe hit: existing task {} (queue {q}, status {s}, created {})",
                fmt_cell(Some(&tid)),
                fmt_cell(Some(&created_raw))
            );
        }
    }
    println!("{line}");
    for w in &warnings {
        println!("warning: {w}");
    }
    print_json(&row);
    0
}

fn cmd_list(rest: &[String]) -> i32 {
    let args = parse_args(rest, &["queue", "status", "limit", "not-before"], &[]);
    let resp = call(
        "/list",
        &json!({
            "queue": opt_queue(&args),
            "status": args.get("status"),
            "limit": args.get_i64("limit").unwrap_or(20),
            "not_before": args.get("not-before"),
        }),
    );
    let resp = unwrap_ok(resp);
    let rows = resp.get("rows").and_then(|r| r.as_array()).cloned().unwrap_or_default();
    if rows.is_empty() {
        println!("(no tasks)");
        return 0;
    }
    let cols: Vec<String> = ["task", "priority", "status", "age", "dedupe_key", "title"]
        .iter()
        .map(|s| s.to_string())
        .collect();
    let str_rows: Vec<Vec<String>> = rows
        .iter()
        .map(|r| {
            vec![
                format!("task {}", fmt_cell(r.get("id"))),
                fmt_cell(r.get("priority")),
                fmt_cell(r.get("status")),
                age_str(&r.get("created_at").cloned().unwrap_or(Value::Null)),
                fmt_cell(r.get("dedupe_key")),
                truncate(&fmt_cell(r.get("title")), 72),
            ]
        })
        .collect();
    print_table(&cols, &str_rows);
    0
}

fn cmd_claim(rest: &[String]) -> i32 {
    let args = parse_args(rest, &["queue", "worker", "lease-seconds", "lane"], &[]);
    let mut body = json!({
        "queue": opt_queue(&args),
        "worker": opt_worker(&args),
        "lease_seconds": args.get_i64("lease-seconds").unwrap_or(2700),
    });
    if let Some(lane) = args.get("lane") {
        body["lane"] = json!(lane);
    }
    let resp = unwrap_ok(call("/claim", &body));
    let row = resp.get("row").cloned().unwrap_or(Value::Null);
    if row.is_null() {
        eprint("repo-queue: no claimable task");
        print_json(&json!({"task": Value::Null}));
        return 5;
    }
    print_json(&row);
    0
}

fn cmd_lane(rest: &[String]) -> i32 {
    let args = parse_args(rest, &["worker", "exclusion-reason"], &[]);
    let resp = unwrap_ok(call(
        "/lane",
        &json!({
            "task_id": args.pos_i64(0, "task_id"),
            "worker": opt_worker(&args),
            "exclusion_reason": args.get("exclusion-reason"),
        }),
    ));
    print_json(&require_row(&resp, "assign_repo_task_lane"));
    0
}

fn cmd_bind(rest: &[String]) -> i32 {
    let args = parse_args(rest, &["worker", "session-id", "launched-by"], &[]);
    let session_id = args
        .get("session-id")
        .unwrap_or_else(|| die(2, "bind: --session-id is required"));
    let resp = unwrap_ok(call(
        "/bind",
        &json!({
            "task_id": args.pos_i64(0, "task_id"),
            "worker": opt_worker(&args),
            "session_id": session_id,
            "launched_by": args.get("launched-by").unwrap_or_else(|| "opencode-claimant".to_string()),
        }),
    ));
    print_json(&require_row(&resp, "bind_repo_task_lane_session"));
    0
}

fn cmd_heartbeat(rest: &[String]) -> i32 {
    let args = parse_args(
        rest,
        &["worker", "extend-seconds", "progress", "progress-file"],
        &[],
    );
    let resp = unwrap_ok(call(
        "/heartbeat",
        &json!({
            "task_id": args.pos_i64(0, "task_id"),
            "worker": opt_worker(&args),
            "extend_seconds": args.get_i64("extend-seconds").unwrap_or(900),
            "progress": progress_arg(&args),
        }),
    ));
    print_json(&require_row(&resp, "heartbeat_task"));
    0
}

fn cmd_checkpoint(rest: &[String]) -> i32 {
    let args = parse_args(rest, &["worker", "stage", "progress", "progress-file"], &[]);
    let stage = args.get("stage").unwrap_or_else(|| die(2, "checkpoint: --stage is required"));
    let resp = unwrap_ok(call(
        "/checkpoint",
        &json!({
            "task_id": args.pos_i64(0, "task_id"),
            "worker": opt_worker(&args),
            "stage": stage,
            "progress": progress_arg(&args).unwrap_or(json!({})),
        }),
    ));
    print_json(&require_row(&resp, "checkpoint_task"));
    0
}

fn cmd_park(rest: &[String]) -> i32 {
    let args = parse_args(
        rest,
        &[
            "worker",
            "reason",
            "delay-seconds",
            "not-before",
            "progress",
            "progress-file",
            "ready-condition",
            "execution",
        ],
        &[],
    );
    if args.has("delay-seconds") && args.has("not-before") {
        die(2, "argument --not-before: not allowed with argument --delay-seconds");
    }
    let reason = args.get("reason").unwrap_or_else(|| die(2, "park: --reason is required"));
    let mut body = json!({
        "task_id": args.pos_i64(0, "task_id"),
        "worker": opt_worker(&args),
        "reason": reason,
        "progress": progress_arg(&args).unwrap_or(json!({})),
    });
    if let Some(nb) = args.get("not-before") {
        body["not_before"] = json!(nb);
    } else if let Some(d) = args.get_i64("delay-seconds") {
        body["delay_seconds"] = json!(d);
    }
    if let Some(rc_raw) = args.get("ready-condition") {
        let rc: Value = match serde_json::from_str(&rc_raw) {
            Ok(v) => v,
            Err(e) => die(2, &format!("--ready-condition is not valid JSON: {e}")),
        };
        if !rc.is_object() {
            die(2, "--ready-condition must be a JSON object");
        }
        body["ready_condition"] = rc;
    }
    if let Some(ex) = execution_arg(&args) {
        body["execution"] = ex;
    }
    let resp = unwrap_ok(call("/park", &body));
    row_or_ledger(&resp, "park_task");
    0
}

fn cmd_block(rest: &[String]) -> i32 {
    let args = parse_args(
        rest,
        &["worker", "reason-class", "blocker", "progress", "progress-file"],
        &[],
    );
    let rc = args.get("reason-class").unwrap_or_else(|| die(2, "block: --reason-class is required"));
    let blocker = args.get("blocker").unwrap_or_else(|| die(2, "block: --blocker is required"));
    let resp = unwrap_ok(call(
        "/block",
        &json!({
            "task_id": args.pos_i64(0, "task_id"),
            "worker": opt_worker(&args),
            "reason_class": rc,
            "blocker": blocker,
            "progress": progress_arg(&args).unwrap_or(json!({})),
        }),
    ));
    print_json(&require_row(&resp, "block_task"));
    0
}

fn cmd_cancel(rest: &[String]) -> i32 {
    let args = parse_args(rest, &["worker", "reason"], &[]);
    let reason = args.get("reason").unwrap_or_else(|| die(2, "cancel: --reason is required"));
    let worker = args.get("worker").unwrap_or_else(|| "fleet-monitor".to_string());
    let resp = unwrap_ok(call(
        "/cancel",
        &json!({
            "task_id": args.pos_i64(0, "task_id"),
            "worker": worker,
            "reason": reason,
        }),
    ));
    print_json(&require_row(&resp, "cancel_repo_task"));
    0
}

fn cmd_fail(rest: &[String]) -> i32 {
    let args = parse_args(rest, &["worker", "error", "reason-class", "execution"], &[]);
    let error = args.get("error").unwrap_or_else(|| die(2, "fail: --error is required"));
    let reason_class = args.get("reason-class").unwrap_or_else(|| "transient".to_string());
    if !["transient", "deterministic", "blocked"].contains(&reason_class.as_str()) {
        die(2, &format!("argument --reason-class: invalid choice: '{reason_class}'"));
    }
    let mut body = json!({
        "task_id": args.pos_i64(0, "task_id"),
        "worker": opt_worker(&args),
        "error": error,
        "reason_class": reason_class,
    });
    if let Some(ex) = execution_arg(&args) {
        body["execution"] = ex;
    }
    let resp = unwrap_ok(call("/fail", &body));
    row_or_ledger(&resp, "fail_repo_task");
    0
}

fn cmd_complete(rest: &[String]) -> i32 {
    let args = parse_args(
        rest,
        &["worker", "result", "result-file", "telemetry", "execution"],
        &[],
    );
    let mut result_raw = args.get("result");
    if let Some(f) = args.get("result-file") {
        result_raw = Some(match std::fs::read_to_string(&f) {
            Ok(t) => t,
            Err(e) => die(2, &format!("cannot read result file: {e}")),
        });
    }
    let mut result_obj = json!({});
    if let Some(raw) = result_raw {
        if !raw.is_empty() {
            result_obj = match serde_json::from_str(&raw) {
                Ok(v) => v,
                Err(e) => die(2, &format!("--result is not valid JSON: {e}")),
            };
            if !result_obj.is_object() {
                die(2, "--result must be a JSON object");
            }
        }
    }
    check_telemetry_fields(&result_obj, "--result");
    if let Some(tel_raw) = args.get("telemetry") {
        let tel: Value = match serde_json::from_str(&tel_raw) {
            Ok(v) => v,
            Err(e) => die(2, &format!("--telemetry is not valid JSON: {e}")),
        };
        if !tel.is_object() {
            die(2, "--telemetry must be a JSON object");
        }
        let unknown: Vec<String> = tel
            .as_object()
            .unwrap()
            .keys()
            .filter(|k| !TELEMETRY_FIELDS.contains(&k.as_str()))
            .cloned()
            .collect();
        if !unknown.is_empty() {
            let mut sorted = unknown.clone();
            sorted.sort();
            die(
                2,
                &format!(
                    "--telemetry: unknown field(s) {}; allowed: {}",
                    py_str_list(&sorted),
                    py_str_list(&TELEMETRY_FIELDS.iter().map(|s| s.to_string()).collect::<Vec<_>>())
                ),
            );
        }
        check_telemetry_fields(&tel, "--telemetry");
        for field in TELEMETRY_FIELDS {
            if tel.get(field).is_some() && result_obj.get(field).is_none() {
                result_obj[field] = tel[field].clone();
            }
        }
    }
    let ex = execution_arg(&args);
    if let Some(ex) = &ex {
        if result_obj.get("execution").is_none() {
            let mut block = Map::new();
            block.insert("lane".to_string(), ex.get("lane").cloned().unwrap_or(Value::Null));
            block.insert(
                "session_id".to_string(),
                ex.get("session_id").cloned().unwrap_or(Value::Null),
            );
            let has_fb = ex.get("fallback_reason").map(|v| !v.is_null()).unwrap_or(false);
            block.insert("fallback".to_string(), Value::Bool(has_fb));
            if has_fb {
                block.insert("fallback_reason".to_string(), ex["fallback_reason"].clone());
            }
            if let Some(a) = ex.get("assigned") {
                block.insert("assigned".to_string(), a.clone());
            }
            result_obj["execution"] = Value::Object(block);
        }
    }
    let mut body = json!({
        "task_id": args.pos_i64(0, "task_id"),
        "worker": opt_worker(&args),
        "result": result_obj,
    });
    if let Some(ex) = ex {
        body["execution"] = ex;
    }
    let resp = unwrap_ok(call("/complete", &body));
    row_or_ledger(&resp, "complete_task");
    0
}

fn cmd_health(rest: &[String]) -> i32 {
    let args = parse_args(rest, &["queue"], &[]);
    let mut body = json!({});
    if let Some(q) = args.get("queue") {
        body["queue"] = json!(q);
    }
    let resp = unwrap_ok(call("/health", &body));
    let netloc = resp.get("project_netloc").and_then(|v| v.as_str()).unwrap_or("");
    let source = resp.get("project_source").and_then(|v| v.as_str()).unwrap_or("bitwarden");
    println!("project: {netloc} (resolved via {source})");
    let rows = resp.get("rows").and_then(|r| r.as_array()).cloned().unwrap_or_default();
    if rows.is_empty() {
        println!("(no queues have tasks)");
        return 0;
    }
    let cols: Vec<String> = rows[0]
        .as_object()
        .map(|o| o.keys().cloned().collect())
        .unwrap_or_default();
    let str_rows: Vec<Vec<String>> = rows
        .iter()
        .map(|r| cols.iter().map(|c| fmt_cell(r.get(c))).collect())
        .collect();
    print_table(&cols, &str_rows);
    0
}

fn cmd_signal(rest: &[String]) -> i32 {
    let args = parse_args(rest, &["queue", "reason"], &[]);
    let queue = opt_queue(&args);
    let reason = args.get("reason").unwrap_or_else(|| "manual_signal".to_string());
    let resp = unwrap_ok(call("/signal-count", &json!({"queue": queue})));
    let queued_count = resp.get("queued_count").cloned().unwrap_or(json!(0));
    let oldest = resp.get("oldest_queued_at").cloned().unwrap_or(Value::Null);

    let signal_path = std::env::var("REPO_QUEUE_SIGNAL_PATH").unwrap_or_else(|_| {
        format!(
            "{}/workspace/goals/keep-the-four-repos-healthy/hidden_files/repo-queue-signal.json",
            home()
        )
    });
    let dir = std::path::Path::new(&signal_path)
        .parent()
        .map(|p| p.to_string_lossy().to_string())
        .unwrap_or_default();
    let state_path = format!("{dir}/repo-queue-signal-state.json");
    let mut generation: i64 = 0;
    for candidate in [&signal_path, &state_path] {
        if let Ok(text) = std::fs::read_to_string(candidate) {
            if let Ok(prev) = serde_json::from_str::<Value>(&text) {
                if prev.get("queue").and_then(|q| q.as_str()) == Some(queue.as_str()) {
                    if let Some(g) = prev.get("generation").and_then(|g| g.as_i64()) {
                        generation = generation.max(g);
                    }
                }
            }
        }
    }
    generation += 1;
    let updated = utcnow_iso();
    let signal = json!({
        "queue": queue,
        "generation": generation,
        "queued_count": queued_count,
        "oldest_queued_at": oldest,
        "updated_at": updated,
        "reason": reason,
    });
    let state = json!({
        "queue": queue,
        "generation": generation,
        "updated_at": updated,
    });
    let _ = std::fs::create_dir_all(&dir);
    atomic_write_json(&signal_path, &signal);
    atomic_write_json(&state_path, &state);
    print_json(&signal);
    0
}

fn atomic_write_json(path: &str, obj: &Value) {
    let tmp = format!("{path}.tmp{}", std::process::id());
    let mut text = serde_json::to_string_pretty(obj).unwrap_or_default();
    text.push('\n');
    match std::fs::write(&tmp, &text).and_then(|_| std::fs::rename(&tmp, path)) {
        Ok(_) => {}
        Err(e) => {
            let _ = std::fs::remove_file(&tmp);
            die(3, &format!("cannot write {path}: {e}"));
        }
    }
}

fn main() {
    let argv: Vec<String> = std::env::args().skip(1).collect();
    if argv.is_empty() {
        eprint("usage: repo-queue <validate|list|enqueue|claim|lane|bind|heartbeat|checkpoint|park|block|cancel|fail|complete|health|signal> ...");
        exit(2);
    }
    let sub = argv[0].as_str();
    let rest = &argv[1..];
    let code = match sub {
        "validate" => cmd_validate(rest),
        "enqueue" => cmd_enqueue(rest),
        "list" => cmd_list(rest),
        "claim" => cmd_claim(rest),
        "lane" => cmd_lane(rest),
        "bind" => cmd_bind(rest),
        "heartbeat" => cmd_heartbeat(rest),
        "checkpoint" => cmd_checkpoint(rest),
        "park" => cmd_park(rest),
        "block" => cmd_block(rest),
        "cancel" => cmd_cancel(rest),
        "fail" => cmd_fail(rest),
        "complete" => cmd_complete(rest),
        "health" => cmd_health(rest),
        "signal" => cmd_signal(rest),
        _ => {
            eprint(&format!("repo-queue: unknown command {sub:?}"));
            2
        }
    };
    exit(code);
}
