// Package relay handles file staging (local disk or S3) and
// chunked push over the tunnel (FILE_PUSH_BEGIN/CHUNK/END)
// with backpressure (≤8 chunks in flight) and sha256 integrity verification.
package relay
