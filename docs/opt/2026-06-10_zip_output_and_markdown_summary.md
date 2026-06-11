# Zip Output Pull + Markdown Summary Rendering

**Created:** 2026-06-10
**Context:** The device now zips a job's `/work/output` tree into a single `{job_id}_outputs.zip` and pulls it to the cloud (bandwidth-efficient). The cloud must unzip it and expose the individual members; the web UI must render a markdown summary instead of showing raw JSON.

## Decisions (locked)

1. **Keep the zip** as a downloadable "download all" artifact (bandwidth-friendly — single transfer device→cloud). Also extract members on the cloud for display.
2. **Summary file selection order:** `summary.md` → else `README.md` (case-insensitive) → else the **largest** `*.md` file. Render this as the job summary.
3. **Markdown source:** render an extracted `.md` output file (not a JSON field). The `job.result` JSON is kept but displayed **collapsed/folded** (unreadable for most users when expanded).

## Device-side review (already implemented by user)

- `device/iagent_device/files/puller.py` — zip logic is correct. Members preserve relative subpaths. Default `IAGENT_PULL_MODE=zip`.
- **Note for cloud:** zip members may contain subpaths; the cloud extractor MUST guard against zip-slip (reject `..` / absolute paths).

## Scripts to change

### Gateway (cloud)

| Script | Change | Purpose |
|---|---|---|
| `gateway/internal/relay/relay.go` | In `OnFilePullEnd`: after writing + SHA-verifying the file, if `pt.name` ends with `.zip`, extract members into the same `jobs/{jobID}/output/` dir. Register each member as a `files` row + `LinkToJob(role="output")`. Keep the zip itself registered as an output too. Add helper `extractZip(zipPath, destDir, jobID)` using `archive/zip`, guarding every member with `isSafeOutputName` (zip-slip defence). | Cloud unzips after poll; individual files (incl. the `.md`) become listable/downloadable while the zip remains for "download all". |
| `gateway/internal/httpapi/jobs_handler.go` | In `handleDownloadJobOutput`, when `?inline=1` is present, set an inline `Content-Type` by extension (`.md`→`text/markdown`, `.json`→`application/json`, `.txt`→`text/plain`) and `Content-Disposition: inline`. Default (no param) keeps `attachment`/`octet-stream`. | Lets the web fetch markdown text for in-page rendering without forcing a download. |

### Web (frontend)

| Script | Change | Purpose |
|---|---|---|
| `web/package.json` | Add `react-markdown` + `remark-gfm`. | No markdown renderer currently exists. |
| `web/src/api/client.ts` | Add `getText(path)` helper (fetch + `.text()`). | Fetch markdown content inline. |
| `web/src/components/JobOutputs.tsx` | After listing outputs: pick the summary file (`summary.md` → `README.md` → largest `*.md`), fetch it via the inline endpoint, render with `react-markdown`+`remark-gfm` above the file list. Keep zip + other files as download buttons (zip labelled "Download all"). | Human-readable markdown summary. |
| `web/src/pages/JobsPage.tsx` | The result card (currently `<pre>{formatResultContent(job.result)}</pre>`) becomes **collapsed by default** (e.g. a collapsible "Raw result (JSON)" section). The primary surface is the markdown summary from `JobOutputs`. | Stop dumping unreadable JSON; fold it. |
| `web/src/api/schemas.ts` | Add types only if the inline endpoint needs a new response shape (likely none; `JobOutputFile` unchanged). | Type safety. |

## Validation

- Submit a job that writes `summary.md` + a `.json` + other files to `/work/output`.
- Confirm cloud lists: the zip (download-all) + each extracted member including `summary.md`.
- Confirm web renders `summary.md` as formatted markdown, shows the JSON folded, and the zip downloads all files.
- Zip-slip test: craft a zip member named `../evil.txt`; confirm the cloud rejects it and writes nothing outside the output dir.
