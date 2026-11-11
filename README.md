# yt-chonk

Turn a YouTube video into text, using Mistral Voxtral or whisper.cpp locally.
The transcript stays in the original language. Public and unlisted videos up to
four hours are supported.

## Install

With [Homebrew](https://brew.sh) on macOS or Linux:

```sh
brew tap juleshrd/yt-chonk https://github.com/juleshrd/yt-chonk
brew install juleshrd/yt-chonk/yt-chonk
```

Homebrew installs the audio tools and puts `yt-chonk` in your PATH. It also
handles the Go build, so you don't need to install Go yourself or copy files.
A Whisper model is a separate download if you choose local transcription.

To update later: `brew update && brew upgrade juleshrd/yt-chonk/yt-chonk`.

## Use

```sh
yt-chonk setup
yt-chonk doctor
yt-chonk transcribe 'https://www.youtube.com/watch?v=VIDEO_ID'
```

Choose a backend during setup:

- `mistral` sends audio to Mistral's paid API. Set `MISTRAL_API_KEY` or save
  your key in the system keychain through setup.
- `local` needs [whisper-cli and a model](https://github.com/ggml-org/whisper.cpp).
  Start with the multilingual `ggml-small.bin` and give setup its file path.

The default output is a TXT file in the current folder. A few options:

```sh
yt-chonk transcribe 'URL' --format md --output notes.md
yt-chonk transcribe 'URL' --format json --output transcript.json
yt-chonk transcribe 'URL' --backend local --model '/path/to/ggml-small.bin' --language en
```

Use `--force` to replace an existing output, or `--quiet` to hide progress.
If interrupted, run the same command again to resume from the last completed chunk.
Changing the export format reuses the cached transcript without another API call.
`yt-chonk cache clean` removes inactive cached jobs, keeping your exports and models.

Local transcription stays on your machine; fetching YouTube audio still needs
the internet. Playlists, live streams, private videos, translation and summaries
aren't supported.

## Development

Requires Go 1.23 or newer. To build from source instead of using Homebrew:

```sh
go build -o yt-chonk ./cmd/yt-chonk
go test ./...
go vet ./...
sh scripts/release.sh v0.1.0
```

Release archives and checksums go into `dist/`. Integration tests use fake tools
and local HTTP servers; real transcription still needs checking with your setup.
Details: [spec](docs/SPEC.md), [integrations](docs/INTEGRATIONS.md),
[JSON schema](docs/JSON-SCHEMA.json). [MIT license](LICENSE).
