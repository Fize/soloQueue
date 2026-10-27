use laya::{LayaEngine, Question, QuestionSet};
use serde::Deserialize;
use serde_json::{json, Map, Value};
use std::{
    io::{self, BufRead, Write},
    path::{Path, PathBuf},
};

const MAX_FRAME: usize = 1 << 20;
const MAX_TEXT: usize = 64 << 10;

type Fallible<T> = Result<T, Box<dyn std::error::Error>>;

// 选项描述与 Go 侧 llmClassifierSystemPrompt 同义；改这里必须同时改 internal/router/laya。
const INSTRUCTIONS: &str = "What is the primary nature of the user's request?";
const OPTIONS: [(&str, &str); 3] = [
    (
        "general",
        "conversation, explanation, writing, translation, summarizing supplied content, ordinary planning",
    ),
    (
        "engineering",
        "code, repositories, debugging, tests, databases, APIs, automation, deployment, or technical implementation",
    ),
    (
        "research",
        "external search, current information, source verification, comparisons, or citations",
    ),
];

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Request {
    v: u32,
    id: u64,
    op: String,
    text: Option<String>,
}

struct Worker {
    engine: LayaEngine,
    questions: QuestionSet,
}

impl Worker {
    fn decide(&self, text: &str) -> Fallible<(String, Map<String, Value>)> {
        let result = self.engine.predict(text, &self.questions)?;
        let answer = result["task_type"]
            .as_choice()
            .ok_or("engine returned no choice answer")?;
        let mut probs = Map::new();
        for (label, p) in answer.probabilities.iter() {
            probs.insert(label.clone(), json!(p));
        }
        Ok((answer.choice.clone(), probs))
    }
}

fn send(id: u64, data: Option<Value>, error: Option<(&str, String)>) -> Fallible<()> {
    let frame = match error {
        Some((code, message)) => json!({
            "v": 1, "id": id, "data": Value::Null,
            "error": {"code": code, "message": message},
        }),
        None => json!({"v": 1, "id": id, "data": data, "error": Value::Null}),
    };
    let mut out = io::stdout().lock();
    serde_json::to_writer(&mut out, &frame)?;
    out.write_all(b"\n")?;
    out.flush()?;
    Ok(())
}

fn main() -> Fallible<()> {
    let mut model_dir: Option<PathBuf> = None;
    let mut runtime: Option<PathBuf> = None;
    let mut device = String::from("cpu");
    let mut variant = String::from("laya-multilingual-fp32");

    let mut args = std::env::args().skip(1);
    while let Some(flag) = args.next() {
        let mut value = || args.next();
        match flag.as_str() {
            "--model-dir" => model_dir = value().map(PathBuf::from),
            "--runtime" => runtime = value().map(PathBuf::from),
            "--device" => device = value().unwrap_or(device),
            "--variant" => variant = value().unwrap_or(variant),
            other => return Err(format!("unknown argument {other}").into()),
        }
    }

    let model_dir = model_dir.ok_or("--model-dir required")?;
    let runtime = runtime.ok_or("--runtime required")?;
    if !runtime.is_absolute() || !runtime.is_file() {
        return Err("runtime must be an existing absolute file".into());
    }
    if !model_dir.is_absolute() || !model_dir.is_dir() {
        return Err("model-dir must be an existing absolute directory".into());
    }
    if device != "cpu" {
        return Err(format!("unsupported device {device}; only cpu is supported").into());
    }

    init_runtime(&runtime)?;

    let (max_len, head_max_len) = budgets(&model_dir);
    let engine = LayaEngine::from_directory(&model_dir)?;
    let questions = QuestionSet::new().with("task_type", build_question()?)?;
    let worker = Worker { engine, questions };

    send(
        0,
        Some(json!({
            "variant": variant,
            "maxLen": max_len,
            "headMaxLen": head_max_len,
            "backend": "cpu",
        })),
        None,
    )?;

    let stdin = io::stdin();
    let mut line = String::new();
    loop {
        line.clear();
        let read = stdin.lock().read_line(&mut line)?;
        if read == 0 {
            return Ok(());
        }
        if line.len() > MAX_FRAME {
            send(
                0,
                None,
                Some(("too_large", format!("frame exceeds {MAX_FRAME} bytes"))),
            )?;
            continue;
        }
        let request: Request = match serde_json::from_str(line.trim_end()) {
            Ok(request) => request,
            Err(err) => {
                send(0, None, Some(("bad_json", err.to_string())))?;
                continue;
            }
        };
        if request.v != 1 {
            send(
                request.id,
                None,
                Some(("bad_version", format!("unsupported v {}", request.v))),
            )?;
            continue;
        }
        match request.op.as_str() {
            "shutdown" => {
                send(request.id, Some(json!({"stopped": true})), None)?;
                return Ok(());
            }
            "decide" => {
                let text = request.text.unwrap_or_default();
                if text.trim().is_empty() {
                    send(
                        request.id,
                        None,
                        Some(("bad_request", "text is empty".into())),
                    )?;
                    continue;
                }
                if text.len() > MAX_TEXT {
                    send(
                        request.id,
                        None,
                        Some(("too_large", format!("text exceeds {MAX_TEXT} bytes"))),
                    )?;
                    continue;
                }
                match worker.decide(&text) {
                    Ok((choice, probabilities)) => send(
                        request.id,
                        Some(json!({"choice": choice, "probabilities": probabilities})),
                        None,
                    )?,
                    Err(err) => send(
                        request.id,
                        None,
                        Some((error_code(err.as_ref()), err.to_string())),
                    )?,
                }
            }
            other => {
                send(
                    request.id,
                    None,
                    Some(("bad_request", format!("unknown op {other}"))),
                )?;
            }
        }
    }
}

fn build_question() -> Fallible<Question> {
    Ok(Question::choice(INSTRUCTIONS, OPTIONS)?)
}

fn error_code(err: &dyn std::error::Error) -> &'static str {
    let text = err.to_string();
    if text.contains("no option marker") || text.contains("NoMarkers") {
        "truncated"
    } else {
        "model_error"
    }
}

// 读 checkpoint 的 token 预算只用于 ready 帧上报，与引擎实际行为无关。
fn budgets(dir: &Path) -> (u64, u64) {
    let raw = std::fs::read_to_string(dir.join("rl_agent_config.json")).unwrap_or_default();
    let cfg: Value = serde_json::from_str(&raw).unwrap_or(Value::Null);
    let get = |key: &str, default: u64| cfg.get(key).and_then(Value::as_u64).unwrap_or(default);
    (get("max_len", 512), get("head_max_len", 192))
}

#[cfg(target_os = "windows")]
fn init_runtime(runtime: &Path) -> Fallible<()> {
    use std::os::windows::ffi::OsStrExt;
    let directory: Vec<u16> = runtime
        .parent()
        .ok_or("runtime has no directory")?
        .as_os_str()
        .encode_wide()
        .chain(Some(0))
        .collect();
    unsafe {
        windows::Win32::System::LibraryLoader::SetDllDirectoryW(windows::core::PCWSTR(
            directory.as_ptr(),
        ))?;
    }
    ort::init_from(runtime.to_str().ok_or("runtime path encoding")?)?.commit();
    Ok(())
}

#[cfg(not(target_os = "windows"))]
fn init_runtime(runtime: &Path) -> Fallible<()> {
    ort::init_from(runtime.to_str().ok_or("runtime path encoding")?)?.commit();
    Ok(())
}
