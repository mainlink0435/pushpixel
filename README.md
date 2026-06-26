# PushPixel

<p align="center">
  <img src="pushpixel.svg" alt="PushPixel" width="400">
</p>

Auto-uploads photos and videos from local folders to Google Photos.

## Features

- Monitors folders for new and modified media
- One-way sync to your main Google Photos library
- Resumable uploads for files over 50 MB
- Exponential backoff with jitter on rate limits
- Local SQLite state tracking across restarts
- Web dashboard with live stats and retry controls
- Runs as a background daemon
- Windows and Linux binaries

## Quick Start

1. [Set up OAuth credentials](docs/oauth-setup.md) in Google Cloud Console
2. Copy `config.example.yaml` to `config.yaml` and fill in your client ID and secret
3. Build: `go build -o pushpixel ./cmd/pushpixel/`
4. Run: `./pushpixel`
5. Open `http://localhost:1978` → Connect to Google Photos

## Configuration

See `config.example.yaml` for all options.

```yaml
directories:
  - /path/to/photos
auth:
  client_id: "your-client-id"
  client_secret: "your-client-secret"
```

## Documentation

- [Product Brief](docs/product-brief.md)
- [OAuth Setup](docs/oauth-setup.md)
- [API Investigation](docs/api-investigation.md)

## License

GPL-3.0 — see [LICENSE](LICENSE)
