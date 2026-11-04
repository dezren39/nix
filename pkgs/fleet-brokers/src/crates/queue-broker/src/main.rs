//! queue-broker — the repo-health queue as a resident laptop service
//! (code#5362). Server-side port of repo-queue.py's HTTP layer: the
//! Supabase service-role key lives only here, fetched from the
//! secrets broker; all queue semantics stay in the Postgres RPCs.
//! Sandbox clients reach it only through the session proxy.
//!
//! Protocol: POST /<command> with a JSON args object; response is a
//! JSON envelope {ok:true, ...} or {ok:false, exit, error} where exit
//! mirrors repo-queue.py's exit codes (2 validation, 3 config, 4 HTTP,
//! 5 no row). Auth: X-Agent-Token header.
//!
//! Env: QUEUE_BROKER_BIND (default 100.99.113.103:4801),
//!      QUEUE_BROKER_TOKEN_FILE (default ~/.config/queue-broker/token),
//!      QUEUE_BROKER_SECRETS_URL (default http://100.99.113.103:4800),
//!      QUEUE_BROKER_SECRETS_TOKEN_FILE (default ~/.config/secrets-broker/token).

mod validate;

use std::io::Read;
use std::sync::{Arc, RwLock};
use std::time::Duration;

use serde_json::{json, Value};
use tiny_http::{Header, Method, Response, Server, StatusCode};

const PILOT_REPO: &str = "developing-today/code";

#[derive(Clone)]
struct Creds {
    key: String,
    base: String, // project URL, no trailing slash
}

struct State {
    secrets_url: String,
    secrets_token: String,
    creds: RwLock<Option<Creds>>,
    agent: ureq::Agent,
}

struct BErr {
    exit: i32,
    msg: String,
}

fn err4(msg: String) -> BErr {
    BErr { exit: 4, msg }
}

impl State {
    fn fetch_creds(&self) -> Result<Creds, BErr> {
        let get_secret = |name: &str| -> Result<String, BErr> {
            let url = format!("{}/secret/{}", self.secrets_url, name);
            let mut resp = self
                .agent
                .get(&url)
                .header("X-Agent-Token", &self.secrets_token)
                .call()
                .map_err(|e| BErr {
                    exit: 3,
                    msg: format!("secrets broker unreachable: {e}"),
                })?;
            let status = resp.status().as_u16();
            let text = resp.body_mut().read_to_string().unwrap_or_default();
            if status >= 300 {
                return Err(BErr {
                    exit: 3,
                    msg: format!("secrets broker HTTP {status} for {name}: {}", trim(&text, 200)),
                });
            }
            let v: Value = serde_json::from_str(&text).map_err(|e| BErr {
                exit: 3,
                msg: format!("secrets broker bad JSON for {name}: {e}"),
            })?;
            v.get("value")
                .and_then(|x| x.as_str())
                .map(|s| s.to_string())
                .ok_or(BErr {
                    exit: 3,
                    msg: format!("secrets broker response for {name} has no value"),
                })
        };
        // Startup race: the secrets broker may still be doing its first
        // vault load; retry 503s for up to ~60 s.
        let mut last_err = None;
        for _ in 0..20 {
            match (get_secret("supabase.muse-dev2.service-role-key"), get_secret("supabase.muse-dev2.project-url")) {
                (Ok(key), Ok(url)) => {
                    let creds = Creds {
                        key,
                        base: url.trim_end_matches('/').to_string(),
                    };
                    *self.creds.write().unwrap() = Some(creds.clone());
                    return Ok(creds);
                }
                (Err(e), _) | (_, Err(e)) => {
                    let retryable = e.msg.contains("503");
                    last_err = Some(e);
                    if !retryable {
                        break;
                    }
                    std::thread::sleep(Duration::from_secs(3));
                }
            }
        }
        Err(last_err.unwrap_or(BErr {
            exit: 3,
            msg: "credential fetch failed".into(),
        }))
    }

    fn creds(&self) -> Result<Creds, BErr> {
        if let Some(c) = self.creds.read().unwrap().clone() {
            return Ok(c);
        }
        self.fetch_creds()
    }

    /// One PostgREST request. Returns (parsed body, content-range).
    fn postgrest(
        &self,
        method: &str,
        path: &str,
        body: Option<&Value>,
        count_exact: bool,
    ) -> Result<(Value, Option<String>), BErr> {
        for attempt in 0..2 {
            let creds = self.creds()?;
            let url = format!("{}/rest/v1/{}", creds.base, path);
            let bearer = format!("Bearer {}", creds.key);
            let mut headers: Vec<(&str, &str)> = vec![
                ("apikey", &creds.key),
                ("Authorization", &bearer),
                ("Accept-Profile", "agent"),
            ];
            if method != "GET" {
                headers.push(("Content-Profile", "agent"));
                headers.push(("Content-Type", "application/json"));
            }
            if count_exact {
                headers.push(("Prefer", "count=exact"));
            }
            // ureq's builders are typestated (WithBody vs WithoutBody),
            // so each arm applies the same header set itself.
            let sent = match (method, body) {
                ("GET", _) => {
                    let mut r = self.agent.get(&url);
                    for (k, v) in &headers {
                        r = r.header(*k, *v);
                    }
                    r.call()
                }
                (_, Some(b)) => {
                    let mut r = self.agent.post(&url);
                    for (k, v) in &headers {
                        r = r.header(*k, *v);
                    }
                    r.send_json(b.clone())
                }
                (_, None) => {
                    let mut r = self.agent.post(&url);
                    for (k, v) in &headers {
                        r = r.header(*k, *v);
                    }
                    r.send("")
                }
            };
            let mut resp = sent.map_err(|e| err4(format!("PostgREST request failed: {e}")))?;
            let status = resp.status().as_u16();
            let content_range = resp
                .headers()
                .get("content-range")
                .and_then(|v| v.to_str().ok())
                .map(|s| s.to_string());
            let text = resp.body_mut().read_to_string().unwrap_or_default();
            if status == 401 && attempt == 0 {
                // Key may have rotated: refetch from the secrets broker.
                *self.creds.write().unwrap() = None;
                continue;
            }
            if status >= 300 {
                return Err(err4(format!(
                    "HTTP {status} from PostgREST: {}",
                    trim(text.trim(), 500)
                )));
            }
            let parsed = if text.trim().is_empty() {
                Value::Null
            } else {
                serde_json::from_str(&text)
                    .map_err(|e| err4(format!("PostgREST returned non-JSON: {e}")))?
            };
            return Ok((parsed, content_range));
        }
        Err(err4("PostgREST request failed after credential refresh".into()))
    }

    fn rpc(&self, fn_name: &str, args: Value) -> Result<Value, BErr> {
        let (v, _) = self.postgrest("POST", &format!("rpc/{fn_name}"), Some(&args), false)?;
        Ok(v)
    }
}

fn trim(s: &str, n: usize) -> String {
    s.chars().take(n).collect()
}

/// Percent-encode like Python urllib quote(s, safe).
fn quote(s: &str, safe: &str) -> String {
    let mut out = String::new();
    for b in s.bytes() {
        let c = b as char;
        if c.is_ascii_alphanumeric() || "-._~".contains(c) || safe.contains(c) {
            out.push(c);
        } else {
            out.push_str(&format!("%{b:02X}"));
        }
    }
    out
}

fn ok(fields: Value) -> Value {
    let mut m = json!({"ok": true});
    if let (Some(dst), Some(src)) = (m.as_object_mut(), fields.as_object()) {
        for (k, v) in src {
            dst.insert(k.clone(), v.clone());
        }
    }
    m
}

fn fail(exit: i32, msg: impl Into<String>) -> Value {
    json!({"ok": false, "exit": exit, "error": msg.into()})
}

fn record_execution(st: &State, req: &Value, row: &Value) -> Result<Option<Value>, BErr> {
    let Some(ex) = req.get("execution").filter(|e| e.is_object()) else {
        return Ok(None);
    };
    let ledger = st.rpc(
        "record_repo_task_lane_execution",
        json!({
            "p_task_id": req.get("task_id"),
            "p_worker": req.get("worker"),
            "p_executed_lane": ex.get("lane"),
            "p_fallback_reason": ex.get("fallback_reason"),
            "p_session_id": ex.get("session_id"),
        }),
    )?;
    let _ = row;
    Ok(Some(ledger))
}

/// Verbs whose RPC takes the args object nearly verbatim.
fn simple_rpc(st: &State, fn_name: &str, args: Value) -> Value {
    match st.rpc(fn_name, args) {
        Ok(row) => ok(json!({"row": row})),
        Err(e) => fail(e.exit, e.msg),
    }
}

fn verb_with_execution(st: &State, fn_name: &str, args: Value, req: &Value) -> Value {
    let row = match st.rpc(fn_name, args) {
        Ok(r) => r,
        Err(e) => return fail(e.exit, e.msg),
    };
    if row.is_null() {
        return ok(json!({"row": Value::Null}));
    }
    match record_execution(st, req, &row) {
        Ok(Some(ledger)) => ok(json!({"row": row, "lane_ledger": ledger})),
        Ok(None) => ok(json!({"row": row})),
        Err(e) => fail(e.exit, e.msg),
    }
}

fn dispatch(st: &State, path: &str, req: &Value) -> Value {
    match path {
        "/validate" => {
            let packet = req.get("packet").cloned().unwrap_or(Value::Null);
            let (errors, warnings) = validate::validate_packet(&packet);
            ok(json!({"errors": errors, "warnings": warnings}))
        }
        "/enqueue" => {
            let packet = req.get("packet").cloned().unwrap_or(Value::Null);
            let (errors, warnings) = validate::validate_packet(&packet);
            if !errors.is_empty() {
                return json!({
                    "ok": false, "exit": 2,
                    "error": format!("packet validation failed ({} error(s))", errors.len()),
                    "errors": errors, "warnings": warnings,
                });
            }
            let queue = req
                .get("queue")
                .and_then(|q| q.as_str())
                .unwrap_or("")
                .to_string();
            let task = validate::task_from_packet(&packet, &queue);
            if req.get("dry_run").and_then(|d| d.as_bool()).unwrap_or(false) {
                return ok(json!({"dry_run": true, "task": task, "warnings": warnings}));
            }
            let repo = packet
                .get("payload")
                .and_then(|p| p.get("repo"))
                .and_then(|r| r.as_str())
                .map(|s| s.to_string());
            let rpc_name = match repo.as_deref() {
                None | Some(PILOT_REPO) => "enqueue_repo_task",
                _ => "enqueue_repo_task_fleet",
            };
            let args = json!({
                "p_queue": task.get("queue"),
                "p_title": task.get("title"),
                "p_body": task.get("body"),
                "p_payload": task.get("payload"),
                "p_dedupe_key": task.get("dedupe_key"),
                "p_priority": task.get("priority"),
                "p_not_before": task.get("not_before"),
                "p_max_attempts": task.get("max_attempts"),
            });
            match st.rpc(rpc_name, args) {
                Ok(row) if !row.is_null() => {
                    ok(json!({"row": row, "rpc": rpc_name, "warnings": warnings}))
                }
                Ok(_) => fail(5, format!("{rpc_name} returned no row")),
                Err(e) => fail(e.exit, e.msg),
            }
        }
        "/claim" => {
            let mut args = json!({
                "p_worker": req.get("worker"),
                "p_queue": req.get("queue"),
                "p_lease_seconds": req.get("lease_seconds"),
            });
            if let Some(lane) = req.get("lane").filter(|l| !l.is_null()) {
                args["p_lane_id"] = lane.clone();
            }
            simple_rpc(st, "claim_repo_task", args)
        }
        "/lane" => simple_rpc(
            st,
            "assign_repo_task_lane",
            json!({
                "p_task_id": req.get("task_id"),
                "p_worker": req.get("worker"),
                "p_exclusion_reason": req.get("exclusion_reason"),
            }),
        ),
        "/bind" => simple_rpc(
            st,
            "bind_repo_task_lane_session",
            json!({
                "p_task_id": req.get("task_id"),
                "p_worker": req.get("worker"),
                "p_session_id": req.get("session_id"),
                "p_launched_by": req.get("launched_by"),
            }),
        ),
        "/heartbeat" => simple_rpc(
            st,
            "heartbeat_task",
            json!({
                "p_task_id": req.get("task_id"),
                "p_worker": req.get("worker"),
                "p_extend_seconds": req.get("extend_seconds"),
                "p_progress": req.get("progress"),
            }),
        ),
        "/checkpoint" => simple_rpc(
            st,
            "checkpoint_task",
            json!({
                "p_task_id": req.get("task_id"),
                "p_worker": req.get("worker"),
                "p_stage": req.get("stage"),
                "p_progress": req.get("progress").cloned().unwrap_or(json!({})),
            }),
        ),
        "/park" => {
            let mut args = json!({
                "p_task_id": req.get("task_id"),
                "p_worker": req.get("worker"),
                "p_reason": req.get("reason"),
                "p_progress": req.get("progress").cloned().unwrap_or(json!({})),
            });
            if let Some(nb) = req.get("not_before").filter(|v| !v.is_null()) {
                args["p_not_before"] = nb.clone();
            } else if let Some(d) = req.get("delay_seconds").filter(|v| !v.is_null()) {
                args["p_delay_seconds"] = d.clone();
            }
            if let Some(rc) = req.get("ready_condition").filter(|v| !v.is_null()) {
                args["p_ready_condition"] = rc.clone();
            }
            verb_with_execution(st, "park_task", args, req)
        }
        "/block" => simple_rpc(
            st,
            "block_task",
            json!({
                "p_task_id": req.get("task_id"),
                "p_worker": req.get("worker"),
                "p_reason_class": req.get("reason_class"),
                "p_blocker": req.get("blocker"),
                "p_progress": req.get("progress").cloned().unwrap_or(json!({})),
            }),
        ),
        "/cancel" => simple_rpc(
            st,
            "cancel_repo_task",
            json!({
                "p_task_id": req.get("task_id"),
                "p_worker": req.get("worker"),
                "p_reason": req.get("reason"),
            }),
        ),
        "/fail" => verb_with_execution(
            st,
            "fail_repo_task",
            json!({
                "p_task_id": req.get("task_id"),
                "p_worker": req.get("worker"),
                "p_error": req.get("error"),
                "p_reason_class": req.get("reason_class"),
            }),
            req,
        ),
        "/complete" => verb_with_execution(
            st,
            "complete_task",
            json!({
                "p_task_id": req.get("task_id"),
                "p_worker": req.get("worker"),
                "p_result": req.get("result").cloned().unwrap_or(json!({})),
            }),
            req,
        ),
        "/list" => {
            let queue = req.get("queue").and_then(|q| q.as_str()).unwrap_or("");
            let limit = req.get("limit").and_then(|l| l.as_i64()).unwrap_or(20);
            let mut path = format!(
                "tasks?select=id,queue,title,priority,status,dedupe_key,created_at,not_before&queue=eq.{}&order=priority.asc,created_at.asc&limit={limit}",
                quote(queue, "")
            );
            if let Some(s) = req.get("status").and_then(|s| s.as_str()) {
                path += &format!("&status=eq.{}", quote(s, ""));
            }
            if let Some(nb) = req.get("not_before").and_then(|s| s.as_str()) {
                path += &format!(
                    "&or=(not_before.is.null,not_before.lte.{})",
                    quote(nb, ":")
                );
            }
            match st.postgrest("GET", &path, None, false) {
                Ok((rows, _)) => ok(json!({"rows": rows})),
                Err(e) => fail(e.exit, e.msg),
            }
        }
        "/health" => {
            let creds = match st.creds() {
                Ok(c) => c,
                Err(e) => return fail(e.exit, e.msg),
            };
            let netloc = creds
                .base
                .split("://")
                .nth(1)
                .unwrap_or(&creds.base)
                .split('/')
                .next()
                .unwrap_or("")
                .to_string();
            let mut path = "repo_queue_health?select=*".to_string();
            if let Some(q) = req.get("queue").and_then(|q| q.as_str()) {
                path += &format!("&queue=eq.{}", quote(q, ""));
            }
            match st.postgrest("GET", &path, None, false) {
                Ok((rows, _)) => {
                    ok(json!({"project_netloc": netloc, "project_source": "bitwarden", "rows": rows}))
                }
                Err(e) => fail(e.exit, e.msg),
            }
        }
        "/signal-count" => {
            let queue = req.get("queue").and_then(|q| q.as_str()).unwrap_or("");
            let path = format!(
                "tasks?select=id,created_at&queue=eq.{}&status=eq.queued&order=created_at.asc&limit=1",
                quote(queue, "")
            );
            match st.postgrest("GET", &path, None, true) {
                Ok((rows, content_range)) => {
                    let arr = rows.as_array().cloned().unwrap_or_default();
                    let mut count = arr.len() as i64;
                    if let Some(cr) = content_range {
                        if let Some(total) = cr.rsplit('/').next() {
                            if let Ok(n) = total.parse::<i64>() {
                                count = n;
                            }
                        }
                    }
                    let oldest = arr
                        .first()
                        .and_then(|r| r.get("created_at"))
                        .cloned()
                        .unwrap_or(Value::Null);
                    ok(json!({"queued_count": count, "oldest_queued_at": oldest}))
                }
                Err(e) => fail(e.exit, e.msg),
            }
        }
        "/healthz" => ok(json!({"service": "queue-broker"})),
        _ => json!({"ok": false, "exit": 2, "error": format!("unknown command {path}")}),
    }
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
    let bind =
        std::env::var("QUEUE_BROKER_BIND").unwrap_or_else(|_| "100.99.113.103:4801".to_string());
    let home = std::env::var("HOME").expect("HOME unset");
    let token_file = std::env::var("QUEUE_BROKER_TOKEN_FILE")
        .unwrap_or_else(|_| format!("{home}/.config/queue-broker/token"));
    let token = std::fs::read_to_string(&token_file)
        .map(|t| t.trim().to_string())
        .unwrap_or_else(|_| {
            eprintln!("queue-broker: cannot read token file {token_file}");
            std::process::exit(3);
        });
    let secrets_url = std::env::var("QUEUE_BROKER_SECRETS_URL")
        .unwrap_or_else(|_| "http://100.99.113.103:4800".to_string());
    let secrets_token_file = std::env::var("QUEUE_BROKER_SECRETS_TOKEN_FILE")
        .unwrap_or_else(|_| format!("{home}/.config/secrets-broker/token"));
    let secrets_token = std::fs::read_to_string(&secrets_token_file)
        .map(|t| t.trim().to_string())
        .unwrap_or_else(|_| {
            eprintln!("queue-broker: cannot read secrets token file {secrets_token_file}");
            std::process::exit(3);
        });

    let agent: ureq::Agent = ureq::Agent::config_builder()
        .http_status_as_error(false)
        .timeout_global(Some(Duration::from_secs(90)))
        .build()
        .into();
    let state = Arc::new(State {
        secrets_url,
        secrets_token,
        creds: RwLock::new(None),
        agent,
    });

    let server = Server::http(&bind).unwrap_or_else(|e| {
        eprintln!("queue-broker: cannot bind {bind}: {e:?}");
        std::process::exit(3);
    });
    eprintln!("queue-broker listening on {bind}");
    for mut req in server.incoming_requests() {
        let state = Arc::clone(&state);
        let token = token.clone();
        std::thread::spawn(move || {
            let path = req.url().split('?').next().unwrap_or("").to_string();
            let resp = if req.method() == &Method::Get && path == "/healthz" {
                json_response(200, ok(json!({"service": "queue-broker"})))
            } else if !token_ok(&req, &token) {
                json_response(401, json!({"error": "unauthorized"}))
            } else if req.method() != &Method::Post {
                json_response(405, json!({"error": "POST only"}))
            } else {
                let mut buf = String::new();
                match req.as_reader().read_to_string(&mut buf) {
                    Ok(_) => {
                        let parsed: Value =
                            serde_json::from_str(&buf).unwrap_or(Value::Null);
                        json_response(200, dispatch(&state, &path, &parsed))
                    }
                    Err(e) => json_response(
                        400,
                        json!({"ok": false, "exit": 2, "error": format!("bad body: {e}")}),
                    ),
                }
            };
            let _ = req.respond(resp);
        });
    }
}
