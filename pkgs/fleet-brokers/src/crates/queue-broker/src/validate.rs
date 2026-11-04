//! Packet validation — faithful port of repo-queue.py's built-in
//! (manual) validator: envelope checks, manual schema checks, and the
//! dedupe-key family rules. (Legacy Python prefers the jsonschema
//! library when importable; messages for exotic packets can differ in
//! wording, but accept/reject semantics match the manual contract and
//! the database RPCs remain the backstop.)

use serde_json::{json, Value};

pub const PRIORITY_SCALE: [i64; 7] = [10, 20, 30, 40, 50, 70, 90];
pub const KINDS: [&str; 4] = ["pr_shepherd", "triage", "small_fix", "ci_investigation"];
pub const FLEET_REPOS: [&str; 6] = [
    "developing-today/code",
    "developing-today/opencode",
    "developing-today/openchamber",
    "dezren39/nix",
    "developing-today/zettel",
    "dezren39/dezren39",
];
const PAYLOAD_WARN_BYTES: usize = 4096;
const PAYLOAD_MAX_BYTES: usize = 16384;
const BODY_WARN_WORDS: usize = 250;

pub const DEDUPE_PATTERNS_DOC: &str = "fix:         repo-health:v1:fix:<repo>:issue:<n>\n\
triage:      repo-health:v1:triage:<repo>:issue:<n>\n\
pr:          repo-health:v1:pr:<repo>:pr:<n>\n\
pr-set:      repo-health:v1:pr-set:<repo>:<n>-<n>[-...] (sorted)\n\
set:         repo-health:v1:set:<origin issue>:owed:<repo>\n\
maintenance: repo-health:v1:maintenance:<repo>:<kind>:<YYYY-Www>";

fn is_empty(v: Option<&Value>) -> bool {
    match v {
        None | Some(Value::Null) => true,
        Some(Value::String(s)) => s.is_empty(),
        Some(Value::Array(a)) => a.is_empty(),
        Some(Value::Object(o)) => o.is_empty(),
        _ => false,
    }
}

fn manual_schema_errors(p: &Value) -> Vec<String> {
    let mut errs = Vec::new();
    let obj = match p.as_object() {
        Some(o) => o,
        None => return vec!["payload: must be a JSON object".to_string()],
    };
    let get = |f: &str| obj.get(f);
    if get("schema") != Some(&json!("repo-task/v1")) {
        errs.push("payload.schema: must be 'repo-task/v1'".to_string());
    }
    let kind_ok = matches!(get("kind"), Some(Value::String(k)) if KINDS.contains(&k.as_str()));
    if !kind_ok {
        let mut sorted = KINDS;
        sorted.sort();
        errs.push(format!("payload.kind: must be one of {sorted:?}"));
    }
    let repo_ok = matches!(get("repo"), Some(Value::String(r)) if FLEET_REPOS.contains(&r.as_str()));
    if !repo_ok {
        errs.push(format!(
            "payload.repo: {} is not a fleet repo",
            python_repr(get("repo"))
        ));
    }
    for f in ["problem", "priority_reason"] {
        if is_empty(get(f)) {
            errs.push(format!("payload.{f}: required field missing or empty"));
        }
    }
    for f in ["evidence", "acceptance", "tests", "context_refs"] {
        if !matches!(get(f), Some(Value::Array(_))) {
            errs.push(format!("payload.{f}: must be a list"));
        }
    }
    for f in ["evidence", "acceptance", "tests"] {
        if let Some(Value::Array(a)) = get(f) {
            if a.is_empty() {
                errs.push(format!("payload.{f}: must not be empty"));
            }
        }
    }
    match get("scope") {
        Some(Value::Object(scope)) => {
            if !matches!(scope.get("include"), Some(Value::Array(_))) {
                errs.push("payload.scope.include: must be a list".to_string());
            }
            if is_empty(scope.get("exclude")) {
                errs.push("payload.scope.exclude: required (scope exclusions)".to_string());
            }
        }
        _ => errs.push("payload.scope: must be an object with include/exclude".to_string()),
    }
    match get("risk") {
        Some(Value::Object(risk))
            if matches!(risk.get("level"), Some(Value::String(l)) if ["low","medium","high"].contains(&l.as_str())) =>
        {
            if !matches!(risk.get("flags"), Some(Value::Array(_))) {
                errs.push("payload.risk.flags: must be a list".to_string());
            }
        }
        _ => errs.push("payload.risk.level: must be low|medium|high".to_string()),
    }
    for f in ["timebox_minutes", "max_attempts"] {
        let ok = matches!(get(f), Some(Value::Number(n)) if n.as_i64().map(|v| v >= 1).unwrap_or(false));
        if !ok {
            errs.push(format!("payload.{f}: must be a positive integer"));
        }
    }
    let ded_ok = matches!(get("dedupe"), Some(Value::Object(d))
        if !is_empty(d.get("checked_at")) && !is_empty(d.get("basis")));
    if !ded_ok {
        errs.push("payload.dedupe: must have checked_at and basis".to_string());
    }
    let has_issue = matches!(get("issue"), Some(Value::Object(i))
        if matches!(i.get("number"), Some(Value::Number(n)) if n.is_i64()));
    let has_prs = matches!(get("prs"), Some(Value::Array(a)) if !a.is_empty());
    let has_set = matches!(get("set"), Some(Value::Object(_)));
    let has_source = matches!(get("source"), Some(Value::Object(s))
        if matches!(s.get("pr"), Some(Value::Number(n)) if n.is_i64())
            && matches!(s.get("finding"), Some(Value::String(f)) if !f.is_empty()));
    if !(has_issue || has_prs || has_set || has_source) {
        errs.push(
            "payload identity: need issue.number, a non-empty prs list, a set object, \
             or a pre-filing source {pr, finding}"
                .to_string(),
        );
    }
    errs
}

/// Python-style repr for the one message that embeds a value.
fn python_repr(v: Option<&Value>) -> String {
    match v {
        None | Some(Value::Null) => "None".to_string(),
        Some(Value::String(s)) => format!("'{s}'"),
        Some(other) => other.to_string(),
    }
}

pub struct DedupeMatch {
    pub family: String,
    pub repo: String,
    pub num: Option<i64>,
    pub nums: Option<Vec<i64>>,
    pub origin: Option<i64>,
}

fn all_digits(s: &str) -> bool {
    !s.is_empty() && s.bytes().all(|b| b.is_ascii_digit())
}

/// Hand-rolled equivalent of the DEDUPE_REGEXES family patterns.
fn match_dedupe(key: &str) -> Option<DedupeMatch> {
    let rest = key.strip_prefix("repo-health:v1:")?;
    // set family has a different shape: set:<num>:owed:<repo>
    if let Some(tail) = rest.strip_prefix("set:") {
        let (num_s, repo) = tail.split_once(":owed:")?;
        if all_digits(num_s) && FLEET_REPOS.contains(&repo) {
            return Some(DedupeMatch {
                family: "set".into(),
                repo: repo.to_string(),
                num: None,
                nums: None,
                origin: num_s.parse().ok(),
            });
        }
        return None;
    }
    for fam in ["fix", "triage", "pr-set", "pr", "maintenance"] {
        let Some(after) = rest.strip_prefix(&format!("{fam}:")) else {
            continue;
        };
        for repo in FLEET_REPOS {
            let Some(tail) = after.strip_prefix(&format!("{repo}:")) else {
                continue;
            };
            match fam {
                "fix" | "triage" => {
                    if let Some(n) = tail.strip_prefix("issue:") {
                        if all_digits(n) {
                            return Some(DedupeMatch {
                                family: fam.into(),
                                repo: repo.to_string(),
                                num: n.parse().ok(),
                                nums: None,
                                origin: None,
                            });
                        }
                    }
                }
                "pr" => {
                    if let Some(n) = tail.strip_prefix("pr:") {
                        if all_digits(n) {
                            return Some(DedupeMatch {
                                family: fam.into(),
                                repo: repo.to_string(),
                                num: n.parse().ok(),
                                nums: None,
                                origin: None,
                            });
                        }
                    }
                }
                "pr-set" => {
                    let parts: Vec<&str> = tail.split('-').collect();
                    if parts.iter().all(|p| all_digits(p)) {
                        return Some(DedupeMatch {
                            family: fam.into(),
                            repo: repo.to_string(),
                            num: None,
                            nums: Some(
                                parts.iter().filter_map(|p| p.parse().ok()).collect(),
                            ),
                            origin: None,
                        });
                    }
                }
                "maintenance" => {
                    // <slug>:<YYYY-Www>; slug [a-z0-9][a-z0-9_-]*
                    if let Some((slug, week)) = tail.rsplit_once(':') {
                        let slug_ok = !slug.is_empty()
                            && slug
                                .bytes()
                                .next()
                                .map(|b| b.is_ascii_lowercase() || b.is_ascii_digit())
                                .unwrap_or(false)
                            && slug.bytes().all(|b| {
                                b.is_ascii_lowercase()
                                    || b.is_ascii_digit()
                                    || b == b'_'
                                    || b == b'-'
                            });
                        let wb = week.as_bytes();
                        let week_ok = week.len() == 8
                            && all_digits(&week[..4])
                            && week.as_bytes()[4] == b'-'
                            && wb[5] == b'W'
                            && all_digits(&week[6..]);
                        if slug_ok && week_ok {
                            return Some(DedupeMatch {
                                family: fam.into(),
                                repo: repo.to_string(),
                                num: None,
                                nums: None,
                                origin: None,
                            });
                        }
                    }
                }
                _ => {}
            }
        }
        // Family prefix matched but no repo/tail did: Python would fall
        // through to trying other families too (regexes are tried in
        // order over the whole key). Keep scanning other families.
    }
    None
}

fn kind_families(kind: &str) -> Option<&'static [&'static str]> {
    match kind {
        "small_fix" => Some(&["fix"]),
        "triage" => Some(&["triage", "maintenance"]),
        "pr_shepherd" => Some(&["pr", "pr-set", "set"]),
        "ci_investigation" => Some(&["fix", "triage", "pr"]),
        _ => None,
    }
}

fn dedupe_errors(packet: &Value, payload: &Value) -> Vec<String> {
    let mut errors = Vec::new();
    let key = match packet.get("dedupe_key") {
        Some(Value::String(k)) if !k.is_empty() => k.clone(),
        _ => {
            return vec![format!(
                "packet.dedupe_key: required deterministic key:\n{DEDUPE_PATTERNS_DOC}"
            )]
        }
    };
    let m = match match_dedupe(&key) {
        Some(m) => m,
        None => {
            return vec![format!(
                "packet.dedupe_key: '{key}' matches no design pattern:\n{DEDUPE_PATTERNS_DOC}"
            )]
        }
    };
    if let Some(repo) = payload.get("repo") {
        if repo != &json!(m.repo) {
            errors.push(format!(
                "packet.dedupe_key: repo '{}' in key does not match payload.repo {}",
                m.repo,
                python_repr(Some(repo))
            ));
        }
    }
    let kind = payload.get("kind").and_then(|k| k.as_str()).unwrap_or("");
    if let Some(allowed) = kind_families(kind) {
        if !allowed.contains(&m.family.as_str()) {
            let mut sorted = allowed.to_vec();
            sorted.sort();
            errors.push(format!(
                "packet.dedupe_key: family '{}' is not valid for kind '{}' (allowed: {:?})",
                m.family, kind, sorted
            ));
        }
    }
    let issue_no = payload
        .get("issue")
        .and_then(|i| i.get("number"))
        .and_then(|n| n.as_i64());
    let pr_numbers: Vec<i64> = payload
        .get("prs")
        .and_then(|p| p.as_array())
        .map(|a| {
            a.iter()
                .filter_map(|p| p.get("number").and_then(|n| n.as_i64()))
                .collect()
        })
        .unwrap_or_default();
    match m.family.as_str() {
        "fix" | "triage" => {
            let n = m.num.unwrap_or(0);
            if let Some(ino) = issue_no {
                if n != ino {
                    errors.push(format!(
                        "packet.dedupe_key: issue {n} in key does not match payload.issue.number {ino}"
                    ));
                }
            }
        }
        "pr" => {
            let n = m.num.unwrap_or(0);
            if !pr_numbers.is_empty() && !pr_numbers.contains(&n) {
                errors.push(format!(
                    "packet.dedupe_key: PR {n} in key is not in payload.prs {pr_numbers:?}"
                ));
            }
        }
        "pr-set" => {
            let nums = m.nums.clone().unwrap_or_default();
            let mut sorted = nums.clone();
            sorted.sort();
            if nums != sorted {
                errors.push(
                    "packet.dedupe_key: pr-set numbers must be sorted ascending".to_string(),
                );
            }
            if !pr_numbers.is_empty() {
                let mut sp = pr_numbers.clone();
                sp.sort();
                if nums != sp {
                    errors.push(format!(
                        "packet.dedupe_key: pr-set {nums:?} does not match payload.prs {sp:?}"
                    ));
                }
            }
        }
        "set" => {
            let origin = m.origin.unwrap_or(0);
            if let Some(st) = payload.get("set").and_then(|s| s.as_object()) {
                if let Some(oi) = st.get("origin_issue").and_then(|v| v.as_i64()) {
                    if oi != origin {
                        errors.push(format!(
                            "packet.dedupe_key: origin issue {origin} does not match payload.set.origin_issue {oi}"
                        ));
                    }
                }
            }
        }
        _ => {}
    }
    errors
}

/// validate_packet: returns (errors, warnings).
pub fn validate_packet(packet: &Value) -> (Vec<String>, Vec<String>) {
    let mut errors: Vec<String> = Vec::new();
    let mut warnings: Vec<String> = Vec::new();
    let Some(obj) = packet.as_object() else {
        return (vec!["packet: must be a JSON object".to_string()], warnings);
    };
    let Some(payload) = obj.get("payload").filter(|p| p.is_object()) else {
        return (
            vec!["packet.payload: required object (repo-task/v1 payload)".to_string()],
            warnings,
        );
    };
    errors.extend(manual_schema_errors(payload));

    match obj.get("title") {
        Some(Value::String(t)) if !t.trim().is_empty() => {}
        _ => errors.push("packet.title: required non-empty string".to_string()),
    }
    match obj.get("priority") {
        None => errors.push(
            "packet.priority: required (numeric priority from the design scale: 10, 20, 30, 40, 50, 70, 90)"
                .to_string(),
        ),
        Some(Value::Number(n)) if n.is_i64() => {
            let v = n.as_i64().unwrap();
            if !PRIORITY_SCALE.contains(&v) {
                errors.push(format!(
                    "packet.priority: {v} is not on the design scale {:?}",
                    PRIORITY_SCALE
                ));
            }
        }
        Some(_) => errors.push("packet.priority: must be an integer".to_string()),
    }
    if let Some(Value::String(body)) = obj.get("body") {
        let words = body.split_whitespace().count();
        if words > BODY_WARN_WORDS {
            warnings.push(format!(
                "packet.body: {words} words exceeds the ~{BODY_WARN_WORDS}-word design target"
            ));
        }
    }
    let size = serde_json::to_string(payload).map(|s| s.len()).unwrap_or(0);
    if size > PAYLOAD_MAX_BYTES {
        errors.push(format!(
            "payload size: {size} bytes exceeds the {PAYLOAD_MAX_BYTES}-byte hard ceiling (evidence and pointers, not full diffs/logs)"
        ));
    } else if size > PAYLOAD_WARN_BYTES {
        warnings.push(format!(
            "payload size: {size} bytes is above the {PAYLOAD_WARN_BYTES}-byte design target"
        ));
    }
    if let Some(tb) = payload.get("timebox_minutes").and_then(|v| v.as_i64()) {
        if tb != 20 && tb != 45 {
            warnings.push(format!(
                "payload.timebox_minutes: {tb} is not a standard design timebox (20 triage / 45 standard)"
            ));
        }
    }
    if let Some(ma) = payload.get("max_attempts").and_then(|v| v.as_i64()) {
        let kind = payload.get("kind").and_then(|k| k.as_str()).unwrap_or("");
        let want = if kind == "triage" || kind == "ci_investigation" { 6 } else { 4 };
        if ma != want {
            warnings.push(format!(
                "payload.max_attempts: design default for {kind} is {want}, packet says {ma}"
            ));
        }
    }
    errors.extend(dedupe_errors(packet, payload));
    (errors, warnings)
}

/// task_from_packet: the agent.tasks row shape the enqueue RPC takes.
pub fn task_from_packet(packet: &Value, queue: &str) -> Value {
    let payload = packet.get("payload").cloned().unwrap_or(json!({}));
    let max_attempts = packet
        .get("max_attempts")
        .filter(|v| !v.is_null())
        .or_else(|| payload.get("max_attempts"))
        .cloned()
        .unwrap_or(json!(4));
    json!({
        "queue": queue,
        "title": packet.get("title").cloned().unwrap_or(Value::Null),
        "body": packet.get("body").cloned().unwrap_or(Value::Null),
        "payload": payload,
        "dedupe_key": packet.get("dedupe_key").cloned().unwrap_or(Value::Null),
        "priority": packet.get("priority").cloned().unwrap_or(Value::Null),
        "not_before": packet.get("not_before").cloned().unwrap_or(Value::Null),
        "max_attempts": max_attempts,
    })
}
