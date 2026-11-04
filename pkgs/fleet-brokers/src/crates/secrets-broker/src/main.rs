//! secrets-broker — resident Bitwarden Secrets Manager cache (code#5362).
//!
//! Fetches the vault's secrets ONCE per TTL via the `bws` CLI on the
//! laptop and serves them to token-authed local consumers, replacing the
//! per-call `bws secret list` + `get` fetch that made every
//! repo-queue.py invocation pay 10-40 s. Binds the laptop tailnet IP
//! only; consumers reach it through the sandbox session proxy.
//!
//! Endpoints (header `X-Agent-Token` required except /health):
//!   GET /health        -> {ok, secrets, last_refresh_unix, refreshing}
//!   GET /secrets       -> {names: [...]}
//!   GET /secret/<name> -> {key, value} | 404 | 503 (not loaded yet)
//!
//! Env: SECRETS_BROKER_BIND (default 100.99.113.103:4800),
//!      SECRETS_BROKER_TOKEN_FILE (default ~/.config/secrets-broker/token),
//!      SECRETS_BROKER_TTL_S (default 900).

use std::collections::HashMap;
use std::io::Read;
use std::process::Command;
use std::sync::{Arc, RwLock};
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use serde_json::{json, Value};
use tiny_http::{Header, Method, Response, Server, StatusCode};

struct State {
    secrets: RwLock<HashMap<String, String>>,
    last_refresh: RwLock<Option<u64>>,
    refreshing: RwLock<bool>,
}

fn now_unix() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs())
        .unwrap_or(0)
}

fn bws_token() -> Option<String> {
    if let Ok(t) = std::env::var("BWS_ACCESS_TOKEN") {
        if !t.trim().is_empty() {
            return Some(t.trim().to_string());
        }
    }
    let home = std::env::var("HOME").ok()?;
    for rel in [".config/bitwarden/access-token", ".config/bws/access-token"] {
        if let Ok(t) = std::fs::read_to_string(format!("{home}/{rel}")) {
            if !t.trim().is_empty() {
                return Some(t.trim().to_string());
            }
        }
    }
    None
}

fn bws_run(args: &[&str], token: &str) -> Result<String, String> {
    let out = Command::new("bws")
        .args(args)
        .env("BWS_ACCESS_TOKEN", token)
        .output()
        .map_err(|e| format!("bws spawn failed: {e}"))?;
    if !out.status.success() {
        return Err(format!(
            "bws {} failed: {}",
            args.join(" "),
            String::from_utf8_lossy(&out.stderr).trim().chars().take(200).collect::<String>()
        ));
    }
    String::from_utf8(out.stdout).map_err(|e| format!("bws output not utf8: {e}"))
}

/// One full vault refresh: list, then get each secret (8-way parallel).
fn refresh(state: &Arc<State>) -> Result<usize, String> {
    {
        let mut r = state.refreshing.write().unwrap();
        if *r {
            return Err("refresh already in progress".into());
        }
        *r = true;
    }
    let result = do_refresh(state);
    *state.refreshing.write().unwrap() = false;
    result
}

fn do_refresh(state: &Arc<State>) -> Result<usize, String> {
    let token = bws_token().ok_or("no Bitwarden access token available")?;
    let list_raw = bws_run(&["secret", "list", "-o", "json"], &token)?;
    let list: Value =
        serde_json::from_str(&list_raw).map_err(|e| format!("bws list not json: {e}"))?;
    let items: Vec<(String, String)> = list
        .as_array()
        .ok_or("bws list is not an array")?
        .iter()
        .filter_map(|s| {
            Some((
                s.get("id")?.as_str()?.to_string(),
                s.get("key")?.as_str()?.to_string(),
            ))
        })
        .collect();
    // Fetch values with a small worker pool over a shared index.
    let items = Arc::new(items);
    let idx = Arc::new(std::sync::atomic::AtomicUsize::new(0));
    let got = Arc::new(RwLock::new(HashMap::new()));
    let mut handles = Vec::new();
    for _ in 0..8 {
        let items = Arc::clone(&items);
        let idx = Arc::clone(&idx);
        let got = Arc::clone(&got);
        let token = token.clone();
        handles.push(std::thread::spawn(move || {
            loop {
                let i = idx.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
                if i >= items.len() {
                    return;
                }
                let (id, key) = &items[i];
                if let Ok(raw) = bws_run(&["secret", "get", id, "-o", "json"], &token) {
                    if let Ok(v) = serde_json::from_str::<Value>(&raw) {
                        if let Some(val) = v.get("value").and_then(|x| x.as_str()) {
                            got.write().unwrap().insert(key.clone(), val.to_string());
                        }
                    }
                }
            }
        }));
    }
    for h in handles {
        let _ = h.join();
    }
    let map = got.read().unwrap().clone();
    let n = map.len();
    *state.secrets.write().unwrap() = map;
    *state.last_refresh.write().unwrap() = Some(now_unix());
    Ok(n)
}

fn json_response(status: u16, body: Value) -> Response<std::io::Cursor<Vec<u8>>> {
    let data = serde_json::to_vec(&body).unwrap_or_else(|_| b"{}".to_vec());
    Response::from_data(data)
        .with_status_code(StatusCode(status))
        .with_header(
            Header::from_bytes(&b"Content-Type"[..], &b"application/json"[..]).unwrap(),
        )
}

fn token_ok(req: &tiny_http::Request, expected: &str) -> bool {
    req.headers()
        .iter()
        .find(|h| h.field.as_str().to_ascii_lowercase() == "x-agent-token")
        .map(|h| h.value.as_str() == expected)
        .unwrap_or(false)
}

fn main() {
    let bind = std::env::var("SECRETS_BROKER_BIND")
        .unwrap_or_else(|_| "100.99.113.103:4800".to_string());
    let home = std::env::var("HOME").expect("HOME unset");
    let token_file = std::env::var("SECRETS_BROKER_TOKEN_FILE")
        .unwrap_or_else(|_| format!("{home}/.config/secrets-broker/token"));
    let token = std::fs::read_to_string(&token_file)
        .map(|t| t.trim().to_string())
        .unwrap_or_else(|_| {
            eprintln!("secrets-broker: cannot read token file {token_file}");
            std::process::exit(3);
        });
    let ttl_s: u64 = std::env::var("SECRETS_BROKER_TTL_S")
        .ok()
        .and_then(|v| v.parse().ok())
        .unwrap_or(900);

    let state = Arc::new(State {
        secrets: RwLock::new(HashMap::new()),
        last_refresh: RwLock::new(None),
        refreshing: RwLock::new(false),
    });

    // Background refresh loop: initial load + TTL refresh,
    // stale-while-revalidate (the cache keeps serving during a refresh,
    // and a failed refresh keeps the old values).
    {
        let state = Arc::clone(&state);
        std::thread::spawn(move || loop {
            match refresh(&state) {
                Ok(n) => eprintln!("secrets-broker: refreshed {n} secrets"),
                Err(e) => eprintln!("secrets-broker: refresh failed: {e}"),
            }
            std::thread::sleep(Duration::from_secs(ttl_s));
        });
    }

    let server = Server::http(&bind).unwrap_or_else(|e| {
        eprintln!("secrets-broker: cannot bind {bind}: {e:?}");
        std::process::exit(3);
    });
    eprintln!("secrets-broker listening on {bind}");
    for mut req in server.incoming_requests() {
        let state = Arc::clone(&state);
        let token = token.clone();
        std::thread::spawn(move || {
            let url = req.url().to_string();
            let path = url.split('?').next().unwrap_or("").to_string();
            let resp = if req.method() == &Method::Get && path == "/health" {
                json_response(
                    200,
                    json!({
                        "ok": true,
                        "secrets": state.secrets.read().unwrap().len(),
                        "last_refresh_unix": state.last_refresh.read().unwrap().clone(),
                        "refreshing": *state.refreshing.read().unwrap(),
                    }),
                )
            } else if !token_ok(&req, &token) {
                json_response(401, json!({"error": "unauthorized"}))
            } else if req.method() == &Method::Get && path == "/secrets" {
                let mut names: Vec<String> =
                    state.secrets.read().unwrap().keys().cloned().collect();
                names.sort();
                json_response(200, json!({"names": names}))
            } else if req.method() == &Method::Post && path == "/refresh" {
                let mut buf = String::new();
                let _ = req.as_reader().read_to_string(&mut buf);
                let st = Arc::clone(&state);
                std::thread::spawn(move || {
                    let _ = refresh(&st);
                });
                json_response(202, json!({"ok": true, "refresh": "started"}))
            } else if req.method() == &Method::Get && path.starts_with("/secret/") {
                let name = path.trim_start_matches("/secret/").to_string();
                let name = url_decode(&name);
                let cache = state.secrets.read().unwrap();
                match cache.get(&name) {
                    Some(v) => {
                        json_response(200, json!({"key": name, "value": v}))
                    }
                    None => {
                        if state.last_refresh.read().unwrap().is_none() {
                            json_response(
                                503,
                                json!({"error": "secrets not loaded yet"}),
                            )
                        } else {
                            json_response(404, json!({"error": "no such secret"}))
                        }
                    }
                }
            } else {
                json_response(404, json!({"error": "not found"}))
            };
            let _ = req.respond(resp);
        });
    }
}

fn url_decode(s: &str) -> String {
    let bytes = s.as_bytes();
    let mut out = Vec::with_capacity(bytes.len());
    let mut i = 0;
    while i < bytes.len() {
        if bytes[i] == b'%' && i + 2 < bytes.len() {
            if let Ok(v) = u8::from_str_radix(
                std::str::from_utf8(&bytes[i + 1..i + 3]).unwrap_or(""),
                16,
            ) {
                out.push(v);
                i += 3;
                continue;
            }
        }
        out.push(if bytes[i] == b'+' { b' ' } else { bytes[i] });
        i += 1;
    }
    String::from_utf8_lossy(&out).to_string()
}
