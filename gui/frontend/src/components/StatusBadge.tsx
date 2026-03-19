// StatusBadge: reusable status indicator component.
// Renders a colored badge for task status, container state, or pipeline stage.
// Colors match Architecture Section 26.2 task tree status indicators.
import React from "react";
import type { TaskStatus } from "../types";

const STATUS_COLORS: Record<string, string> = {
  queued: "#6b7280",
  in_progress: "#3b82f6",
  in_review: "#8b5cf6",
  done: "#10b981",
  failed: "#ef4444",
  blocked: "#f59e0b",
  waiting_on_lock: "#f97316",
  cancelled_eco: "#9ca3af",
  // Container states
  running: "#3b82f6",
  stopped: "#6b7280",
  timeout: "#ef4444",
  // Pipeline stages
  extraction: "#8b5cf6",
  validation: "#3b82f6",
  review: "#06b6d4",
  merge: "#10b981",
  idle: "#374151",
};

const STATUS_LABELS: Record<string, string> = {
  queued: "Queued",
  in_progress: "In Progress",
  in_review: "In Review",
  done: "Done",
  failed: "Failed",
  blocked: "Blocked",
  waiting_on_lock: "Waiting on Lock",
  cancelled_eco: "Cancelled (ECO)",
};

interface Props {
  status: TaskStatus | string;
  size?: "sm" | "md" | "lg";
  showLabel?: boolean;
}

const SIZES = {
  sm: { dot: 8, fontSize: 11, padding: "1px 6px" },
  md: { dot: 10, fontSize: 12, padding: "2px 8px" },
  lg: { dot: 12, fontSize: 13, padding: "3px 10px" },
};

export const StatusBadge: React.FC<Props> = ({
  status,
  size = "md",
  showLabel = true,
}) => {
  const color = STATUS_COLORS[status] || "#6b7280";
  const label = STATUS_LABELS[status] || status;
  const dims = SIZES[size];

  const containerStyle: React.CSSProperties = {
    display: "inline-flex",
    alignItems: "center",
    gap: 4,
    padding: dims.padding,
    borderRadius: 4,
    backgroundColor: `${color}22`,
    border: `1px solid ${color}44`,
    fontSize: dims.fontSize,
    lineHeight: 1,
    whiteSpace: "nowrap",
  };

  const dotStyle: React.CSSProperties = {
    width: dims.dot,
    height: dims.dot,
    borderRadius: "50%",
    backgroundColor: color,
    flexShrink: 0,
  };

  const labelStyle: React.CSSProperties = {
    color,
    fontWeight: 500,
  };

  return (
    <span style={containerStyle}>
      <span style={dotStyle} />
      {showLabel && <span style={labelStyle}>{label}</span>}
    </span>
  );
};
