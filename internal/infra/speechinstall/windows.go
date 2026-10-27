package speechinstall

// Archive versions and digests are pinned so a changed upstream payload fails closed.
var WhisperWindows = Archive{
	URL:       "https://github.com/ggml-org/whisper.cpp/releases/download/b5454/whisper-bin-x64.zip",
	SHA256:    "6ba69e3482d7826214f90a6a9c84ca07782aec1e1d0c6a7c30c994fd5d816ccb",
	Directory: "whisper-b5454", Prefix: "Release/", Executable: "whisper-cli.exe",
}

var FFmpegWindows = Archive{
	URL:       "https://www.gyan.dev/ffmpeg/builds/packages/ffmpeg-9.0.2-essentials_build.zip",
	SHA256:    "60f467265b1e312373dbcd92200c2618a74850f98d3d078e94296bb3fa2047ba",
	Directory: "ffmpeg-9.0.2", Prefix: "ffmpeg-9.0.2-essentials_build/bin/", Executable: "ffmpeg.exe",
}

// The upstream decoder is used only when a source build has no bundled decoder.
const SilkWindowsURL = "https://raw.githubusercontent.com/kn007/silk-v3-decoder/d216599502662db01c07cc0dfd95ff1f1eaaea02/windows/silk_v3_decoder.exe"
const SilkWindowsSHA256 = "afe908fdf8bb5ddc3566caef224a365159a6216e517d8a915db50ce5ecf86d1b"
