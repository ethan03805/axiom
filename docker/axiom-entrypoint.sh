#!/usr/bin/env bash
# Axiom Entrypoint Script for Meeseeks/Reviewer containers.
#
# Monitors /workspace/ipc/input/ for new JSON files using inotifywait
# (preferred, event-driven) or falls back to 1-second polling if
# inotifywait is not available.
#
# Responses are written to /workspace/ipc/output/ by the agent runtime.
# This script handles file detection and dispatching only.
#
# See Architecture Section 20.3.

set -euo pipefail

INPUT_DIR="/workspace/ipc/input"
OUTPUT_DIR="/workspace/ipc/output"
PROCESSED_DIR="/workspace/ipc/processed"

mkdir -p "$INPUT_DIR" "$OUTPUT_DIR" "$PROCESSED_DIR"

log() {
    echo "[axiom-entrypoint] $(date -u +%Y-%m-%dT%H:%M:%SZ) $*"
}

process_file() {
    local file="$1"
    local basename
    basename=$(basename "$file")

    # Skip temp files (atomic write pattern uses .tmp suffix).
    case "$basename" in
        *.tmp) return ;;
    esac

    # Only process JSON files.
    case "$basename" in
        *.json) ;;
        *) return ;;
    esac

    # Validate the file is non-empty and readable.
    if [ ! -s "$file" ]; then
        log "WARNING: Skipping empty file: $basename"
        return
    fi

    log "Processing: $basename"

    # The container's main agent process reads from this directory.
    # This script detects files and moves them to the processed directory
    # to avoid re-processing. The actual agent runtime handles the content.

    # Move processed file to avoid re-processing.
    mv "$file" "$PROCESSED_DIR/$basename" 2>/dev/null || true
}

# Process any files that were delivered before the watcher started.
process_existing() {
    for file in "$INPUT_DIR"/*.json; do
        [ -f "$file" ] || continue
        process_file "$file"
    done
}

log "Starting Axiom IPC watcher"
log "Input directory:  $INPUT_DIR"
log "Output directory: $OUTPUT_DIR"

# Process files that arrived before this script started.
process_existing

# Try inotifywait first (preferred: event-driven, no CPU overhead).
if command -v inotifywait >/dev/null 2>&1; then
    log "Using inotifywait for event-driven file detection"
    inotifywait -m -e close_write --format '%w%f' "$INPUT_DIR" | while read -r file; do
        process_file "$file"
    done
else
    # Fallback: poll at 1-second intervals.
    # See Architecture Section 20.3 (Fallback).
    log "inotifywait not available, falling back to 1-second polling"
    declare -A SEEN
    while true; do
        for file in "$INPUT_DIR"/*.json; do
            [ -f "$file" ] || continue
            basename=$(basename "$file")
            if [ -z "${SEEN[$basename]+x}" ]; then
                SEEN[$basename]=1
                process_file "$file"
            fi
        done
        sleep 1
    done
fi
