class YtChonk < Formula
  desc "Transcribe YouTube audio with Mistral or whisper.cpp"
  homepage "https://github.com/juleshrd/yt-chonk"
  url "https://github.com/juleshrd/yt-chonk.git",
      revision: "c75fad1273309bdbf46c82f66f4128388bc52568"
  version "0.1.1"
  license "MIT"

  depends_on "go" => :build
  depends_on "deno"
  depends_on "ffmpeg"
  depends_on "whisper.cpp"
  depends_on "yt-dlp"

  def install
    ENV["CGO_ENABLED"] = "0"
    system "go", "build", *std_go_args(ldflags: "-X yt-chonk/internal/app.Version=#{version}"), "./cmd/yt-chonk"
  end

  def caveats
    <<~EOS
      Run yt-chonk setup to choose a backend, then yt-chonk doctor.
      Local transcription also needs a Whisper model; setup explains where to get it.
    EOS
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/yt-chonk --version")
    assert_match "transcribe", shell_output("#{bin}/yt-chonk --help")
  end
end
