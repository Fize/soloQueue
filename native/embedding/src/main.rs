use ort::{ep, session::Session, value::Tensor, value::TensorElementType};
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use std::{
    io::{self, BufRead, Write},
    path::{Path, PathBuf},
};
use tokenizers::{PaddingParams, PaddingStrategy, Tokenizer, TruncationParams};

type Fallible<T> = Result<T, Box<dyn std::error::Error>>;
const MODEL: &str = "intfloat/multilingual-e5-small";
const DIM: usize = 384;
const MAX_FRAME: usize = 1024 * 1024;
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Request {
    version: u32,
    id: u64,
    op: String,
    purpose: Option<String>,
    texts: Option<Vec<String>>,
}
#[derive(Serialize)]
struct Embedding {
    embedding: Vec<f32>,
    tokens: usize,
    truncated: bool,
}
struct Worker {
    tokenizer: Tokenizer,
    session: Session,
    model: PathBuf,
    backend: String,
    fallback: Option<String>,
}

fn prefixed(text: &str, purpose: &str) -> String {
    let prefix = if purpose == "query" {
        "query: "
    } else {
        "passage: "
    };
    format!("{prefix}{text}")
}
fn validate(req: &Request) -> Result<(), &'static str> {
    if req.version != 1 || req.id == 0 {
        return Err("unsupported version or zero request ID");
    }
    if req.op == "shutdown" && req.texts.is_none() && req.purpose.is_none() {
        return Ok(());
    }
    if req.op != "embed" || !matches!(req.purpose.as_deref(), Some("query" | "document")) {
        return Err("invalid operation or purpose");
    }
    let texts = req.texts.as_ref().ok_or("texts required")?;
    if texts.is_empty() || texts.len() > 32 || texts.iter().any(|t| t.len() > 65536) {
        return Err("text count or size exceeds bounds");
    }
    Ok(())
}
#[cfg(target_os = "windows")]
fn directml_device() -> Fallible<i32> {
    use windows::Win32::Graphics::Dxgi::{
        CreateDXGIFactory1, DXGI_ADAPTER_FLAG_SOFTWARE, IDXGIFactory1,
    };
    let factory: IDXGIFactory1 = unsafe { CreateDXGIFactory1()? };
    for index in 0..32u32 {
        let adapter = match unsafe { factory.EnumAdapters1(index) } {
            Ok(adapter) => adapter,
            Err(_) => break,
        };
        let desc = unsafe { adapter.GetDesc1()? };
        if desc.Flags & DXGI_ADAPTER_FLAG_SOFTWARE.0 as u32 == 0 {
            return Ok(index as i32);
        }
    }
    Err("no hardware DirectML adapter available".into())
}
#[cfg(not(target_os = "windows"))]
fn directml_device() -> Fallible<i32> {
    Err("DirectML requires Windows".into())
}
fn build(model: &Path, backend: &str, profile: Option<&Path>) -> Fallible<Session> {
    let mut builder = Session::builder()?
        .with_no_environment_execution_providers()?
        .with_intra_threads(2)?;
    builder = match backend {
        "coreml" => builder.with_execution_providers([ep::CoreML::default()
            .with_compute_units(ep::coreml::ComputeUnits::CPUAndGPU)
            .build()
            .error_on_failure()])?,
        "cuda" => {
            builder.with_execution_providers([ep::CUDA::default().build().error_on_failure()])?
        }
        "directml" => builder
            .with_parallel_execution(false)?
            .with_memory_pattern(false)?
            .with_execution_providers([ep::DirectML::default()
                .with_device_id(directml_device()?)
                .build()
                .error_on_failure()])?,
        _ => builder.with_execution_providers([ep::CPU::default().build().error_on_failure()])?,
    };
    if let Some(path) = profile {
        builder = builder.with_profiling(path)?;
    }
    let session = builder.commit_from_file(model)?;
    let names: Vec<_> = session.inputs().iter().map(|x| x.name()).collect();
    if !names.contains(&"input_ids")
        || !names.contains(&"attention_mask")
        || names
            .iter()
            .any(|n| !["input_ids", "attention_mask", "token_type_ids"].contains(n))
    {
        return Err("unexpected model input signature".into());
    }
    for input in session.inputs() {
        if input.dtype().tensor_type() != Some(TensorElementType::Int64)
            || input.dtype().tensor_shape().is_none_or(|s| s.len() != 2)
        {
            return Err("expected rank-2 int64 input".into());
        }
    }
    let output = session
        .outputs()
        .iter()
        .find(|o| o.name() == "last_hidden_state")
        .ok_or("missing last_hidden_state")?;
    if output.dtype().tensor_type() != Some(TensorElementType::Float32)
        || output
            .dtype()
            .tensor_shape()
            .is_none_or(|s| s.len() != 3 || s[2] != DIM as i64)
    {
        return Err("unexpected output signature".into());
    }
    Ok(session)
}
impl Worker {
    fn new(dir: &Path, device: &str) -> Fallible<Self> {
        let model = dir.join("onnx/model.onnx");
        let mut tokenizer =
            Tokenizer::from_file(dir.join("onnx/tokenizer.json")).map_err(|e| e.to_string())?;
        tokenizer.with_padding(None);
        tokenizer.with_truncation(None).map_err(|e| e.to_string())?;
        let mut worker = Self {
            tokenizer,
            session: build(&model, "cpu", None)?,
            model,
            backend: "cpu".into(),
            fallback: None,
        };
        if device == "auto" {
            let candidates: &[&str] = if cfg!(target_os = "macos") {
                &["coreml"]
            } else if cfg!(target_os = "windows") {
                &["cuda", "directml"]
            } else {
                &["cuda"]
            };
            let mut failures = Vec::new();
            for candidate in candidates {
                // Profiling demonstrates execution by the provider, not merely registration.
                let prefix = std::env::temp_dir()
                    .join(format!("soloqueue-onnx-{}-{candidate}", std::process::id()));
                match build(&worker.model, candidate, Some(&prefix)) {
                    Ok(session) => {
                        worker.session = session;
                        let warmup = worker.infer(&["warmup".into()], "query");
                        let profile = worker.session.end_profiling();
                        let verified = profile
                            .ok()
                            .and_then(|path| {
                                let result = std::fs::read(&path).ok().and_then(|bytes| {
                                    serde_json::from_slice::<Vec<Value>>(&bytes).ok()
                                });
                                let _ = std::fs::remove_file(path);
                                result
                            })
                            .is_some_and(|events| {
                                events.iter().any(|e| {
                                    e["args"]["provider"].as_str()
                                        == Some(match *candidate {
                                            "coreml" => "CoreMLExecutionProvider",
                                            "cuda" => "CUDAExecutionProvider",
                                            _ => "DmlExecutionProvider",
                                        })
                                })
                            });
                        if warmup.is_ok() && verified {
                            worker.backend = candidate.to_string();
                            return Ok(worker);
                        }
                        failures.push(format!(
                            "{candidate}: warmup failed or no profiled provider nodes"
                        ));
                    }
                    Err(_) => failures.push(format!(
                        "{candidate}: unavailable or session creation failed"
                    )),
                }
            }
            worker.session = build(&worker.model, "cpu", None)?;
            worker.fallback = Some(failures.join("; "));
        }
        worker.infer(&["warmup".into()], "query")?;
        Ok(worker)
    }
    fn infer(&mut self, texts: &[String], purpose: &str) -> Fallible<Vec<Embedding>> {
        let mut results = Vec::with_capacity(texts.len());
        // Single-item inference bounds peak memory for long batches; protocol still accepts batches.
        for text in texts {
            let (encoding, truncated) = tokenize(&self.tokenizer, text, purpose)?;
            let mask: Vec<i64> = encoding
                .get_attention_mask()
                .iter()
                .map(|v| *v as i64)
                .collect();
            let ids: Vec<i64> = encoding.get_ids().iter().map(|v| *v as i64).collect();
            let types: Vec<i64> = encoding.get_type_ids().iter().map(|v| *v as i64).collect();
            let mut inputs = vec![
                (
                    "input_ids",
                    Tensor::from_array(([1usize, 512], ids))?.into_dyn(),
                ),
                (
                    "attention_mask",
                    Tensor::from_array(([1usize, 512], mask.clone()))?.into_dyn(),
                ),
            ];
            if self
                .session
                .inputs()
                .iter()
                .any(|i| i.name() == "token_type_ids")
            {
                inputs.push((
                    "token_type_ids",
                    Tensor::from_array(([1usize, 512], types))?.into_dyn(),
                ));
            }
            let outputs = self.session.run(inputs)?;
            let (shape, hidden) = outputs["last_hidden_state"].try_extract_tensor::<f32>()?;
            if shape.as_ref() != [1, 512, DIM as i64] {
                return Err("unexpected inference output shape".into());
            }
            let tokens: usize = mask.iter().map(|x| *x as usize).sum();
            let mut vector = vec![0f32; DIM];
            for (row, included) in hidden.chunks_exact(DIM).zip(mask) {
                if included != 0 {
                    for (sum, v) in vector.iter_mut().zip(row) {
                        *sum += v / tokens as f32;
                    }
                }
            }
            normalize(&mut vector)?;
            results.push(Embedding {
                embedding: vector,
                tokens,
                truncated,
            });
        }
        Ok(results)
    }
    fn embed(&mut self, texts: &[String], purpose: &str) -> Fallible<Vec<Embedding>> {
        match self.infer(texts, purpose) {
            Ok(values) => Ok(values),
            Err(error) if self.backend != "cpu" => {
                switch_to_cpu(
                    &mut self.session,
                    &mut self.backend,
                    &mut self.fallback,
                    || build(&self.model, "cpu", None),
                )?;
                let _ = error;
                self.infer(texts, purpose)
            }
            Err(error) => Err(error),
        }
    }
}
fn switch_to_cpu<S>(
    session: &mut S,
    backend: &mut String,
    fallback: &mut Option<String>,
    build_cpu: impl FnOnce() -> Fallible<S>,
) -> Fallible<()> {
    // Preserve the accelerated session and its metadata if reconstruction fails.
    let cpu_session = build_cpu()?;
    let reason = format!("{backend}: inference failed; entire request retried on CPU");
    *session = cpu_session;
    *backend = "cpu".into();
    *fallback = Some(reason);
    Ok(())
}
fn tokenize(
    tokenizer: &Tokenizer,
    text: &str,
    purpose: &str,
) -> Fallible<(tokenizers::Encoding, bool)> {
    let input = prefixed(text, purpose);
    let raw = tokenizer
        .encode(input.clone(), true)
        .map_err(|e| e.to_string())?;
    let truncated = raw.len() > 512;
    let mut tokenizer = tokenizer.clone();
    tokenizer
        .with_truncation(Some(TruncationParams {
            max_length: 512,
            ..Default::default()
        }))
        .map_err(|e| e.to_string())?;
    let pad_id = tokenizer
        .token_to_id("<pad>")
        .ok_or("missing model pad token")?;
    tokenizer.with_padding(Some(PaddingParams {
        strategy: PaddingStrategy::Fixed(512),
        pad_id,
        pad_token: "<pad>".into(),
        ..Default::default()
    }));
    let encoding = tokenizer.encode(input, true).map_err(|e| e.to_string())?;
    Ok((encoding, truncated))
}
fn normalize(values: &mut [f32]) -> Fallible<()> {
    let norm = values
        .iter()
        .map(|x| (*x as f64).powi(2))
        .sum::<f64>()
        .sqrt();
    if !norm.is_finite() || norm <= 0. {
        return Err("invalid output norm".into());
    }
    for value in values {
        *value = (*value as f64 / norm) as f32;
    }
    Ok(())
}
fn send(id: u64, data: Value, error: Value) -> Fallible<()> {
    let mut out = io::stdout().lock();
    serde_json::to_writer(
        &mut out,
        &json!({"version":1,"id":id,"data":data,"error":error}),
    )?;
    out.write_all(b"\n")?;
    out.flush()?;
    Ok(())
}
fn read_frame(reader: &mut impl BufRead) -> io::Result<Option<Vec<u8>>> {
    let mut frame = Vec::new();
    let mut oversized = false;
    loop {
        let chunk = reader.fill_buf()?;
        if chunk.is_empty() {
            return if frame.is_empty() && !oversized {
                Ok(None)
            } else {
                Err(io::Error::new(
                    io::ErrorKind::UnexpectedEof,
                    "partial frame",
                ))
            };
        }
        let end = chunk.iter().position(|b| *b == b'\n');
        let len = end.map_or(chunk.len(), |i| i + 1);
        if frame.len() + len > MAX_FRAME {
            oversized = true;
        } else if !oversized {
            frame.extend_from_slice(&chunk[..len]);
        }
        reader.consume(len);
        if end.is_some() {
            return if oversized {
                Err(io::Error::new(
                    io::ErrorKind::InvalidData,
                    "frame too large",
                ))
            } else {
                Ok(Some(frame))
            };
        }
    }
}
fn run() -> Fallible<()> {
    let mut dir = None;
    let mut runtime = None;
    let mut device = "auto".to_string();
    let mut args = std::env::args().skip(1);
    while let Some(arg) = args.next() {
        let value = args.next().ok_or("missing option value")?;
        match arg.as_str() {
            "--model-dir" => dir = Some(PathBuf::from(value)),
            "--runtime" => runtime = Some(PathBuf::from(value)),
            "--device" => device = value,
            _ => return Err("unknown option".into()),
        }
    }
    if !["cpu", "auto"].contains(&device.as_str()) {
        return Err("invalid device".into());
    }
    let runtime = runtime.ok_or("--runtime required")?;
    if !runtime.is_absolute() || !runtime.is_file() {
        return Err("runtime must be an existing absolute file".into());
    }
    #[cfg(target_os = "windows")]
    {
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
    }
    ort::init_from(runtime.to_str().ok_or("runtime path encoding")?)?.commit();
    let mut worker = Worker::new(&dir.ok_or("--model-dir required")?, &device)?;
    send(
        0,
        json!({"model":MODEL,"dimension":DIM,"backend":worker.backend,"fallbackReason":worker.fallback}),
        Value::Null,
    )?;
    let mut reader = io::stdin().lock();
    loop {
        let frame = match read_frame(&mut reader) {
            Ok(Some(frame)) => frame,
            Ok(None) => break,
            Err(e) if e.kind() == io::ErrorKind::InvalidData => {
                send(
                    0,
                    Value::Null,
                    json!({"code":"frame_too_large","message":"frame exceeds 1MiB"}),
                )?;
                continue;
            }
            Err(e) => return Err(e.into()),
        };
        let request = match serde_json::from_slice::<Request>(&frame) {
            Ok(request) => request,
            Err(_) => {
                send(
                    0,
                    Value::Null,
                    json!({"code":"invalid_request","message":"invalid request JSON or schema"}),
                )?;
                continue;
            }
        };
        if let Err(message) = validate(&request) {
            send(
                request.id,
                Value::Null,
                json!({"code":"invalid_request","message":message}),
            )?;
            continue;
        }
        if request.op == "shutdown" {
            send(request.id, json!({}), Value::Null)?;
            break;
        }
        match worker.embed(
            request.texts.as_ref().unwrap(),
            request.purpose.as_deref().unwrap(),
        ) {
            Ok(results) => send(
                request.id,
                json!({"backend":worker.backend,"fallbackReason":worker.fallback,"results":results}),
                Value::Null,
            )?,
            Err(_) => send(
                request.id,
                Value::Null,
                json!({"code":"inference_failed","message":"local inference failed"}),
            )?,
        }
    }
    Ok(())
}
fn main() {
    if run().is_err() {
        eprintln!("local embedding worker initialization or transport failed");
        std::process::exit(1);
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn cpu_rebuild_failure_preserves_accelerated_state() {
        let mut session = "accelerated session";
        let mut backend = "cuda".to_string();
        let mut fallback = Some("previous provider failure".to_string());
        let accelerated_inference: Fallible<()> = Err("accelerated inference failed".into());
        let result = accelerated_inference.or_else(|_| {
            switch_to_cpu(&mut session, &mut backend, &mut fallback, || {
                Err("CPU session reconstruction failed".into())
            })
        });
        assert_eq!(
            result.unwrap_err().to_string(),
            "CPU session reconstruction failed"
        );
        assert_eq!(session, "accelerated session");
        assert_eq!(backend, "cuda");
        assert_eq!(fallback.as_deref(), Some("previous provider failure"));

        // A later request can still transition from the actual accelerated state.
        switch_to_cpu(&mut session, &mut backend, &mut fallback, || {
            Ok("CPU session")
        })
        .unwrap();
        assert_eq!(session, "CPU session");
        assert_eq!(backend, "cpu");
        assert_eq!(
            fallback.as_deref(),
            Some("cuda: inference failed; entire request retried on CPU")
        );
    }
    #[test]
    #[ignore = "requires verified official model and generated reference fixture; run explicitly in native CI"]
    fn tokenizer_parity() {
        let model = std::env::var("SOLOQUEUE_ONNX_MODEL_DIR").expect("model directory required");
        let fixture =
            std::env::var("SOLOQUEUE_ONNX_TOKEN_FIXTURE").expect("reference fixture required");
        let mut tokenizer =
            Tokenizer::from_file(Path::new(&model).join("onnx/tokenizer.json")).unwrap();
        tokenizer.with_padding(None);
        tokenizer.with_truncation(None).unwrap();
        let cases: Vec<Value> = serde_json::from_slice(&std::fs::read(fixture).unwrap()).unwrap();
        assert!(!cases.is_empty());
        for case in cases {
            let (encoding, truncated) = tokenize(
                &tokenizer,
                case["text"].as_str().unwrap(),
                case["purpose"].as_str().unwrap(),
            )
            .unwrap();
            assert_eq!(json!(encoding.get_ids()), case["input_ids"]);
            assert_eq!(json!(encoding.get_attention_mask()), case["attention_mask"]);
            assert_eq!(json!(encoding.get_type_ids()), case["token_type_ids"]);
            assert_eq!(json!(truncated), case["truncated"]);
        }
    }
    #[test]
    fn validates_requests() {
        for value in [
            json!({"version":2,"id":1,"op":"shutdown"}),
            json!({"version":1,"id":0,"op":"shutdown"}),
            json!({"version":1,"id":1,"op":"embed","purpose":"query","texts":[]}),
        ] {
            assert!(validate(&serde_json::from_value(value).unwrap()).is_err());
        }
    }
    #[test]
    fn purpose_prefix() {
        assert_eq!(prefixed("中文", "document"), "passage: 中文");
        assert_eq!(prefixed("query: source", "query"), "query: query: source");
    }
    #[test]
    fn bounded_frames_recover() {
        let mut bytes = vec![b'x'; MAX_FRAME + 4];
        bytes.extend_from_slice(b"\n{}\n");
        let mut reader = io::Cursor::new(bytes);
        assert_eq!(
            read_frame(&mut reader).unwrap_err().kind(),
            io::ErrorKind::InvalidData
        );
        assert_eq!(read_frame(&mut reader).unwrap().unwrap(), b"{}\n");
    }
    #[test]
    fn normalization_rejects_invalid() {
        assert!(normalize(&mut [f32::NAN]).is_err());
        assert!(normalize(&mut [0.]).is_err());
        let mut v = [3., 4.];
        normalize(&mut v).unwrap();
        assert!((v[0] - 0.6).abs() < 1e-6);
    }
}
