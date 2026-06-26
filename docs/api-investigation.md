# Google Photos API Investigation — PushPixel

Date: 2026-06-25

## 1. Overview

This document captures the findings from the pre-development investigation phase required by the product brief. It covers the Google Photos Library API endpoints, quotas, authentication flow, and the initial state matching/deduplication strategy that will drive the v1 sync behaviour.

---

## 2. OAuth 2.0 Authentication

### 2.1 Device Authorization Grant (Device Flow)

PushPixel will authenticate via the OAuth 2.0 Device Flow (RFC 8628). This is the correct choice because:

- No embedded browser is available in a daemon/background service or Docker container
- The device flow works by displaying a user code and verification URL on stdout (or the WebUI), which the user visits on any device to authorise
- Google supports the device flow for "limited input devices" and "installed applications"

### 2.2 Required OAuth Scopes

Two scopes are needed to cover the full read-write lifecycle:

| Scope | Purpose |
|-------|---------|
| `photoslibrary.appendonly` | Upload bytes, create media items, create albums, add enrichments |
| `photoslibrary.readonly.appcreateddata` | List/search media items and albums created by our app |

The `readonly.appcreateddata` scope is needed to reconcile our local SQLite state against what Google already has (e.g., after a token reset or DB loss). **Important:** this scope only returns media items created by PushPixel, not the user's entire library.

### 2.3 Scope Constraint (March 31, 2025)

As of March 31, 2025, Google removed the broader `photoslibrary.readonly` scope for new applications. The following scopes are no longer available for new development:

- `photoslibrary.readonly` — full read access to the user's entire library
- `photoslibrary.sharing` — sharing functionality
- `photoslibrary` — full read/write access

The only way to access the user's full library is now the **Picker API**, which is an interactive UI component — not suitable for a headless daemon. This is a hard constraint for v1.

### 2.4 Token Persistence

- Access tokens are short-lived (typically 1 hour)
- Refresh tokens are long-lived but can be revoked by the user
- Tokens will be persisted in the local SQLite database, encrypted at rest using a machine-derived key
- No service account support — Google Photos API does not support service accounts

### 2.5 App Verification

PushPixel must pass Google's OAuth verification review before it can be used outside of testing. During development, the OAuth consent screen will show "unverified app."

---

## 3. API Endpoints

### 3.1 Upload Raw Bytes — `POST /v1/uploads`

Uploads the raw binary content of a media file to Google's servers and returns an upload token.

**Headers:**

| Header | Value |
|--------|-------|
| `Authorization` | `Bearer {access_token}` |
| `Content-type` | `application/octet-stream` |
| `X-Goog-Upload-Content-Type` | MIME type of the file (e.g., `image/jpeg`) |
| `X-Goog-Upload-Protocol` | `raw` for simple uploads, `resumable` for large files |

**Response:** Plain text upload token (valid for 1 day).

**Resumable Upload Protocol** (`X-Goog-Upload-Protocol: resumable`):

- Required for files that may be interrupted by network failures
- Splits the file into chunks and uploads each chunk with offset tracking
- Supports resuming from the last successfully transmitted byte via `X-Goog-Upload-Offset`
- Recommended by Google for large files and mobile/daemon environments

### 3.2 Create Media Items — `POST /v1/mediaItems:batchCreate`

Creates media items in the user's library from previously obtained upload tokens.

**Constraints:**

| Constraint | Value |
|------------|-------|
| Max items per call | 50 |
| Max photos | 200 MB each |
| Max videos | 20 GB each |
| Upload token TTL | 24 hours |
| Album capacity | 20,000 items |

**Key behaviours:**

- Each call for the same user must be serial (no parallel `batchCreate` calls for the same user)
- Calls for *different* users can be parallel
- If the exact same bytes are uploaded twice, Google returns the same `mediaItem.id` (server-side byte dedup)
- Stored at **original quality** (counts against user storage quota)
- On success: returns a `NewMediaItemResult` with the full `MediaItem` object
- On partial failure: returns HTTP 207 Multi-Status
- Videos return with `PROCESSING` status initially, then `READY` once processed

### 3.3 List Media Items — `GET /v1/mediaItems`

Pagination-based listing of all media items created by our app.

| Parameter | Type | Description |
|-----------|------|-------------|
| `pageSize` | int | Max 100, default 25 |
| `pageToken` | string | Pagination token from previous response |

**Response:**

```
{
  "mediaItems": [ MediaItem, ... ],
  "nextPageToken": "string"
}
```

**Important:** Only returns items created by PushPixel (app-created data). Cannot see items uploaded by the user via other means.

### 3.4 Search Media Items — `POST /v1/mediaItems:search`

POST-based search with filters (date, content type, media type, features).

**Filters available:**

| Filter | Purpose |
|--------|---------|
| `dateFilter` | Filter by creation date (dates or ranges, max 5) |
| `contentFilter` | Include/exclude content categories (landscapes, selfies, receipts, etc.) |
| `mediaTypeFilter` | Filter by `ALL_MEDIA`, `PHOTO`, or `VIDEO` |
| `featureFilter` | Filter by `NONE` or `FAVORITES` |
| `includeArchivedMedia` | Include archived items (default: false) |

**Restriction:** Only returns app-created data. The `excludeNonAppCreatedData` parameter is available but defaults to true when using the `readonly.appcreateddata` scope.

### 3.5 MediaItem Fields Available

| Field | Type | Notes |
|-------|------|-------|
| `id` | string | Persistent identifier |
| `filename` | string | Original filename shown to user |
| `mimeType` | string | e.g., `image/jpeg` |
| `productUrl` | string | Google Photos URL |
| `baseUrl` | string | URL to download media bytes (with dimension params appended) |
| `mediaMetadata.creationTime` | Timestamp | When the photo/video was taken (not uploaded) |
| `mediaMetadata.width` | int64 | Pixel width |
| `mediaMetadata.height` | int64 | Pixel height |
| `mediaMetadata.photo` | Photo | Camera make/model, focal length, aperture, ISO, exposure time |
| `mediaMetadata.video` | Video | Camera make/model, fps, processing status |

**Notably absent:** file hash/checksum, byte size, upload timestamp, EXIF GPS coordinates.

---

## 4. Quotas and Rate Limits

### 4.1 Per-Project Daily Quotas

| Resource | Limit | Scope |
|----------|-------|-------|
| Library API requests | 10,000/day | All API calls (upload, list, search, batchCreate) |
| Media byte access (base URLs) | 75,000/day | Downloading media from `baseUrl` |

### 4.2 Rate Limit Handling

| HTTP Status | Meaning | Action |
|-------------|---------|--------|
| 429 | Quota exceeded or rate limited | Minimum **30 second** delay, then exponential backoff with jitter |
| 500 | Server error | Typically caused by parallel writes for same user; ensure serial batchCreate |
| 207 | Multi-Status | Partial success in batchCreate; inspect per-item statuses |

Note: The 10,000/day quota is shared across all Library API calls. At 50 items per `batchCreate`, you can upload a maximum of 500,000 media items per day before hitting the quota ceiling.

### 4.3 Quota Monitoring

Available via the Google Cloud Console Quotas page for the `photoslibrary.googleapis.com` service. Increased quotas require joining the Google Photos Partner Program.

---

## 5. Upload Constraints and Best Practices

### 5.1 Accepted File Types

PushPixel will support the following subset as required by the product brief:

| Type | Formats |
|------|---------|
| Photos | JPG, PNG, WEBP |
| Videos | MP4, MOV |

The Google Photos API actually supports many more formats (AVIF, BMP, GIF, HEIC, ICO, TIFF, RAW for photos; 3GP, AVI, MKV, etc. for videos) but the product brief limits us to the above for v1. This can be expanded later.

### 5.2 Files to Exclude

Per the product brief:

- Hidden files (names starting with `.`)
- Hidden directories (names starting with `.`)
- System files: `Thumbs.db`, `.DS_Store`, etc.
- Documents and unsupported sidecar files: `.xmp`, `.json`, etc.

### 5.3 Upload Strategy

- Upload raw bytes in parallel (multiple files concurrently)
- Collect upload tokens per user
- Call `batchCreate` serially per user with batches of up to 50 tokens
- Use resumable upload protocol for files > ~50 MB
- Retry failed tokens individually in subsequent `batchCreate` calls

### 5.4 Storage Impact

Media uploaded via the API is stored at **original quality** and counts against the user's Google Account storage quota. Google recommends reminding the user if uploads exceed 25 MB.

---

## 6. Initial Deduplication Strategy

### 6.1 Context

This is the most critical investigation finding. The March 2025 scope change means PushPixel **cannot query the user's pre-existing Google Photos library** to check what's already been uploaded by other means (Drive sync, mobile app, web uploads).

### 6.2 Why Filename/Hash Matching Won't Work

| Approach | Problem |
|----------|---------|
| Query remote API | Impossible — only app-created data is visible |
| Filename matching | `IMG_0001.JPG` from two different cameras are different files |
| Local hash + remote comparison | No hash or size field on MediaItem, and can't see non-app items anyway |
| Hash + local-only dedup | Same as using SQLite — hashing adds CPU cost with no extra benefit |

### 6.3 The "Blind Upload" Fallback

Google's upload endpoint performs **server-side byte-level deduplication**. When identical bytes are uploaded, the same `mediaItem.id` is returned — the duplicate is silently discarded, doesn't count against storage, and doesn't create a visible duplicate in the library.

This means the worst case for a blind upload is:
- **Bandwidth waste**: files must be re-uploaded even if they're already in Google Photos
- **Quota consumption**: each upload burns a `batchCreate` call (max 50 items/call, 10,000 calls/day)
- **Videos**: upload + processing time, even if already present

### 6.4 Recommended v1 Strategy: Local SQLite State Only

**Decision: Track uploads by absolute file path in local SQLite, and skip only what we know we've uploaded.**

```
For each file in monitored directory:
    if file is in local DB with status="success"
       and file_size matches DB
       and mod_time matches DB:
        skip
    else:
        upload
```

**First run behaviour:** Every file in the target directories is uploaded. If the user's library already contains these same files (from Drive sync), Google's server-side dedup handles it. The user incurs bandwidth and quota consumption, but no duplicates appear in the library.

**Rationale:**

1. There is no API-supported way to do better for v1
2. The legacy Google Drive sync had no dedup against other sources either
3. Server-side byte dedup guarantees no storage waste or visual duplicates
4. Tracking by absolute path lets us correctly handle renames (new path = new upload)
5. If the SQLite DB is lost, the worst case is a re-upload (same as first-run)

**Mitigations to consider for a future v2:**

- Use a hash of (filename + file size + creation date) as a best-effort dedup key across runs
- Implement a "dry-run" mode that shows how many files would be uploaded
- Add an optional user-specified "media age" filter to skip files older than X months (assuming they were already synced)

---

## 7. Summary of Key Constraints for Architecture

| Constraint | Implication |
|------------|-------------|
| 10,000 requests/day | Must batch aggressively (50 items per batchCreate) |
| App-created data only | Cannot read pre-existing library — local DB is the sole dedup authority |
| No file hash/size in API | Cannot verify upload integrity remotely; must trust local metadata |
| Serial batchCreate per user | Upload pipeline must serialize creation per user (uploads can be parallel) |
| Upload token 24h TTL | Cannot queue tokens for extended periods |
| Original quality only | All uploads count against user's storage quota |
| Device Flow only | User must complete OAuth via browser — first-run flow is critical to get right |
