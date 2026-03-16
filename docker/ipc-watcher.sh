#!/bin/sh
# IPC Watcher Script for Axiom Meeseeks/Reviewer containers.
# Monitors /workspace/ipc/input/ for new JSON files using inotifywait,
# reads each message, and processes it. Responses are written to
# /workspace/ipc/output/.
#
# Falls back to 1-second polling if inotifywait is not available.
# See Architecture Section 20.3.

set -e

INPUT_DIR="/workspace/ipc/input"
OUTPUT_DIR="/workspace/ipc/output"
PROCESSED_DIR="/workspace/ipc/processed"

mkdir -p "$INPUT_DIR" "$OUTPUT_DIR" "$PROCESSED_DIR"

process_file() {
    local file="$1"
    local basename
    basename=$(basename "$file")

    # Skip temp files (atomic write pattern).
    case "$basename" in
        *.tmp) return ;;
    esac

    # Skip non-JSON files.
    case "$basename" in
        *.json) ;;
        *) return ;;
    esac

    echo "[ipc-watcher] Processing: $basename"

    # The container's main agent process reads from this directory.
    # This script just ensures files are detected and logged.
    # The actual processing is done by the agent runtime.

    # Move processed file to avoid re-processing.
    mv "$file" "$PROCESSED_DIR/$basename" 2>/dev/null || true
}

# Try inotifywait first (preferred: event-driven, no CPU overhead).
if command -v inotifywait >/dev/null 2>&1; then
    echo "[ipc-watcher] Using inotifywait for event-driven file detection"
    inotifywait -m -e close_write --format '%w%f' "$INPUT_DIR" | while read -r file; do
        process_file "$file"
    done
else
    # Fallback: poll at 1-second intervals.
    # See Architecture Section 20.3 (Fallback).
    echo "[ipc-watcher] inotifywait not available, falling back to polling"
    SEEN=""
    while true; do
        for file in "$INPUT_DIR"/*.json; do
            [ -f "$file" ] || continue
            basename=$(basename "$file")
            case "$SEEN" in
                *"$basename"*) continue ;;
            esac
            SEEN="$SEEN $basename"
            process_file "$file"
        done
        sleep 1
    done
fi
