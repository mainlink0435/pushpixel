

\# PushPixel - Product Documentation



\## 1. Problem Statement



Google Drive is deprecating its automated folder-to-Google Photos synchronisation feature in one month. Users relying on this native integration will lose the ability to automatically back up local directories directly to Google Photos, creating an immediate need for an automated, API-driven replacement.



\## 2. Product Scope



A software application designed to run as a background service or executable. It will monitor designated local folders containing photos and videos and automatically upload them to a user's Google Photos account using the Google Photos API. The overarching principle is to mirror the legacy Google Drive functionality without introducing novel features.



\## 3. Repository \& Development Guidelines (AGENTS.MD Reference)



These directives govern the CI/CD pipeline, repository structure, and development lifecycle.



\### 3.1 Branching Strategy



\-   \*\*`main`\*\*: The primary, production-ready branch.

&#x20;   

\-   \*\*Feature Branches\*\*: All development occurs here. Commits must be squashed and merged into `main` to maintain a clean, linear commit history.

&#x20;   

\-   \*\*`internal`\*\*: A dedicated branch containing private CI/CD infrastructure details and configurations specific to the private Gitea instance.

&#x20;   



\### 3.2 CI/CD \& Build Targets



\-   \*\*Pipeline\*\*: Automated builds via CI/CD (initially hosted on a private Gitea instance, migrating to GitHub).

&#x20;   

\-   \*\*Deliverables\*\*: The pipeline must compile and distribute:

&#x20;   

&#x20;   -   Docker container images.

&#x20;       

&#x20;   -   Executable binaries for Windows (amd64).

&#x20;       

&#x20;   -   Executable binaries for Linux (amd64).

&#x20;       

\-   \*\*Packaging\*\*: Raw, standalone binaries are sufficient for the initial release; formal installers (e.g., `.msi`, `.deb`) are not required.

&#x20;   

\-   \*\*Execution\*\*: Binaries must be capable of running as a daemon or background service.

&#x20;   



\### 3.3 Documentation \& Standard Files



\-   \*\*`docs/` Directory\*\*: Must remain strictly in sync with this product definition. It will house operational mechanisms, architecture, and user usage guidelines.

&#x20;   

\-   \*\*Root Directory\*\*: Must include standard repository files such as `LICENCE`, `README.md`, and `AGENTS.MD`.

&#x20;   



\## 4. Authentication \& Security



\-   \*\*OAuth 2.0 Device Authorisation Grant\*\*: The application will authenticate via the Device Flow. This allows headless environments (like Docker containers and background daemons) to display a user code. The user can then authorise the application from a standard web browser on any device, ensuring secure, token-based access to the Google Photos API without needing an embedded browser in the application.

&#x20;   

\-   \*\*Token Persistence\*\*: The application must persist the authentication state across restarts. The specific mechanism for securely storing these tokens is left to the discretion of the engineering team.

&#x20;   



\## 5. Investigation Phase (Pre-Development)



As part of the initial development cycle, the engineering team must execute and document an investigation phase covering:



\-   \*\*API Documentation\*\*: A comprehensive technical breakdown of the Google Photos API endpoints, limits, and authentication flows to be utilised.

&#x20;   

\-   \*\*Initial State Matching \& Deduplication\*\*: The Google Photos API does not return file hashes or byte sizes, making precise remote file querying difficult. The team must investigate the best approach for the initial sync to prevent a massive bandwidth drain (e.g., blindly re-uploading everything to let Google's byte-level deduplication handle it) versus data loss (e.g., skipping files locally because a file with the same, non-unique filename already exists remotely). The findings will dictate the v1 sync behaviour.

&#x20;   



\## 6. Sync Behaviour \& Media Handling



\-   \*\*File Detection (Polling)\*\*: The application will utilise a polling mechanism to scan the target directories for new or modified files.

&#x20;   

\-   \*\*Destination (Main Library)\*\*: All supported media will be uploaded directly to the user's main Google Photos library (timeline). The application will not attempt to map local folder structures to Google Photos albums.

&#x20;   

\-   \*\*One-Way Backup\*\*: The application will perform a strict one-way upload from the local folder to Google Photos.

&#x20;   

\-   \*\*Local Deletions\*\*: Deleting or moving a file locally after it has been successfully uploaded will \_not\_ trigger a deletion in Google Photos. The cloud copy remains intact.

&#x20;   

\-   \*\*Media Filtering \& Hidden Files\*\*:

&#x20;   

&#x20;   -   The application will \_only\_ attempt to upload explicitly accepted file types (e.g., JPG, PNG, WEBP, MP4, MOV).

&#x20;       

&#x20;   -   It will strictly ignore hidden files, hidden folders (e.g., directories starting with a dot), system files (e.g., `Thumbs.db`, `.DS\_Store`), documents, and unsupported sidecar files (e.g., `.xmp`) to mimic native Google Drive behaviour.

&#x20;       

\-   \*\*Metadata Preservation\*\*: The application will not modify, strip, or parse internal file metadata (such as EXIF data or GPS coordinates). Files will be uploaded exactly as they exist on disk.

&#x20;   



\## 7. State Management \& Reliability



\-   \*\*Local State Tracking\*\*: The application will utilise a local SQLite database to maintain a persistent record of all uploaded assets and their current statuses (e.g., 'success', 'failed').

&#x20;   

\-   \*\*File Identity\*\*: The application tracks upload state based on absolute file paths. If a previously uploaded file is renamed or moved within a monitored directory, it will be treated as a new asset and uploaded again.

&#x20;   

\-   \*\*Resumable Uploads\*\*: For large files (e.g., videos), the application must utilise the API's resumable upload protocol (`X-Goog-Upload-Protocol: resumable`). If an upload is interrupted by a network failure, the application will attempt to resume the transfer from the last successfully transmitted byte.

&#x20;   

\-   \*\*Transient Errors (Rate Limits/Network)\*\*: If the Google Photos API is unreachable or rate limits are hit (e.g., HTTP 429), the file will \_not\_ be marked as failed. The application will implement exponential backoff with jitter, keeping the file in the active queue for automatic retry.

&#x20;   

\-   \*\*Storage Quota Exceeded\*\*: If the API returns a 'Storage Full' or 'Quota Exceeded' error, the application will pause the upload queue globally. It will log a 'Storage Full' state and wait for a prolonged backoff period (configurable in YAML) or manual intervention via the WebUI before attempting further uploads.

&#x20;   

\-   \*\*Permanent Failures\*\*: Files rejected by the API for permanent validation reasons (e.g., file too small, file too large) will be logged in the SQLite database with a 'failed' status to prevent endless retry loops.

&#x20;   

\-   \*\*File Modification Trigger\*\*: The application will monitor file metadata. If a file marked as 'failed' is modified locally (e.g., its file size or modified date changes), its status will be reset and it will be re-queued for a fresh upload attempt.

&#x20;   



\## 8. Configuration



\-   \*\*YAML Configuration\*\*: The application will be configured via a static `.yaml` file.

&#x20;   

\-   \*\*Target Directories\*\*: This file will define the list of local directories the application must monitor for media files.

&#x20;   

\-   \*\*Missing Directories\*\*: If a configured folder becomes unavailable or is deleted while the service is running, the application will log a warning and continue monitoring the remaining available directories without halting.

&#x20;   

\-   \*\*Configurable Logic\*\*: Every modifiable operational parameter (e.g., polling interval in minutes, exponential backoff thresholds, long-polling intervals for quota limits, concurrent upload limits, log rotation max sizes and backup counts) must be exposed as a configurable variable within this YAML file. No magic numbers are permitted in the codebase.

&#x20;   



\## 9. Logging



\-   \*\*Standard Logging Platform\*\*: All system logging must be implemented using a standard, established logging framework rather than custom-written logging logic.

&#x20;   

\-   \*\*Format\*\*: Logs must be output in structured JSON format to ensure compatibility with modern log ingestion and analysis tools.

&#x20;   

\-   \*\*Rotation\*\*: Log rotation must be implemented to prevent disk exhaustion, with constraints (e.g., maximum file size, retention count) driven by the YAML configuration.

&#x20;   



\## 10. Monitoring \& Interface



\-   \*\*Local WebUI\*\*: The application will serve a simple, local web interface.

&#x20;   

\-   \*\*Statistics \& System State\*\*: The WebUI will display upload statistics, explicitly highlighting a 'failed files' count. It will also prominently display global system states, such as a 'Storage Full' warning.

&#x20;   

\-   \*\*Actions\*\*: The WebUI will provide manual intervention options, including buttons to 'retry failed files' and 'resume uploads' after clearing storage.

