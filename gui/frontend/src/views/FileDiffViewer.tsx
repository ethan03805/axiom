// File Diff Viewer: side-by-side diff of Meeseeks output with pipeline status.
// Placeholder for real diff rendering -- will integrate a diff library later.
// See Architecture Section 26.2.
import React from "react";
import { StatusBadge } from "../components/StatusBadge";

interface DiffFile {
  path: string;
  operation: "add" | "modify" | "delete" | "rename";
  oldContent?: string;
  newContent?: string;
}

interface Props {
  taskId: string;
  files: DiffFile[];
  pipelineStatus: string;
}

const OP_COLORS: Record<string, string> = {
  add: "#10b981",
  modify: "#3b82f6",
  delete: "#ef4444",
  rename: "#f59e0b",
};

export const FileDiffViewer: React.FC<Props> = ({
  taskId,
  files,
  pipelineStatus,
}) => {
  return (
    <div>
      <h2 style={{ marginBottom: 16, fontSize: 20 }}>File Diff</h2>

      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 12,
          marginBottom: 16,
          padding: "8px 12px",
          backgroundColor: "#1e293b",
          borderRadius: 4,
          border: "1px solid #334155",
        }}
      >
        <span style={{ color: "#94a3b8", fontSize: 12 }}>Task:</span>
        <span style={{ color: "#e2e8f0" }}>{taskId || "(none selected)"}</span>
        <span style={{ marginLeft: "auto", color: "#94a3b8", fontSize: 12 }}>
          Pipeline:
        </span>
        <StatusBadge status={pipelineStatus} size="sm" />
      </div>

      {files.length === 0 ? (
        <div
          style={{
            padding: 40,
            textAlign: "center",
            color: "#64748b",
            backgroundColor: "#1e293b",
            borderRadius: 6,
            border: "1px solid #334155",
          }}
        >
          <p>No files to display.</p>
          <p style={{ fontSize: 12, marginTop: 8 }}>
            Select a task from the Task Tree to view its file diff.
          </p>
        </div>
      ) : (
        files.map((file) => (
          <div
            key={file.path}
            style={{
              marginBottom: 12,
              borderRadius: 6,
              border: "1px solid #334155",
              overflow: "hidden",
            }}
          >
            <div
              style={{
                display: "flex",
                alignItems: "center",
                gap: 8,
                padding: "8px 12px",
                backgroundColor: "#1e293b",
                borderBottom: "1px solid #334155",
              }}
            >
              <span
                style={{
                  color: OP_COLORS[file.operation] || "#94a3b8",
                  fontWeight: 600,
                  fontSize: 11,
                  textTransform: "uppercase",
                }}
              >
                {file.operation}
              </span>
              <span style={{ color: "#e2e8f0", fontFamily: "monospace", fontSize: 13 }}>
                {file.path}
              </span>
            </div>
            <div style={{ display: "flex" }}>
              {/* Left pane: old content */}
              <pre
                style={{
                  flex: 1,
                  margin: 0,
                  padding: 12,
                  backgroundColor: "#0f172a",
                  color: "#94a3b8",
                  fontSize: 12,
                  fontFamily: "monospace",
                  overflow: "auto",
                  borderRight: "1px solid #334155",
                  minHeight: 60,
                }}
              >
                {file.oldContent || "(no previous content)"}
              </pre>
              {/* Right pane: new content */}
              <pre
                style={{
                  flex: 1,
                  margin: 0,
                  padding: 12,
                  backgroundColor: "#0f172a",
                  color: "#e2e8f0",
                  fontSize: 12,
                  fontFamily: "monospace",
                  overflow: "auto",
                  minHeight: 60,
                }}
              >
                {file.newContent || "(no new content)"}
              </pre>
            </div>
          </div>
        ))
      )}
    </div>
  );
};
