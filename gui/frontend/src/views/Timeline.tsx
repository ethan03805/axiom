// Timeline view: chronological event visualization.
// Displays task starts, completions, reviews, commits, errors, ECOs.
// See Architecture Section 26.2.
import React from "react";
import type { AxiomEvent } from "../types";

const EVENT_COLORS: Record<string, string> = {
  task_created: "#6b7280",
  task_started: "#3b82f6",
  task_completed: "#10b981",
  task_failed: "#ef4444",
  task_blocked: "#f59e0b",
  review_started: "#8b5cf6",
  review_completed: "#8b5cf6",
  merge_started: "#06b6d4",
  merge_completed: "#06b6d4",
  eco_proposed: "#f59e0b",
  eco_approved: "#10b981",
  eco_rejected: "#ef4444",
  budget_warning: "#f97316",
  budget_exhausted: "#ef4444",
  container_spawned: "#3b82f6",
  container_destroyed: "#6b7280",
  srs_submitted: "#3b82f6",
  srs_approved: "#10b981",
  provider_unavailable: "#f97316",
};

const EVENT_LABELS: Record<string, string> = {
  task_created: "Task Created",
  task_started: "Task Started",
  task_completed: "Task Completed",
  task_failed: "Task Failed",
  task_blocked: "Task Blocked",
  review_started: "Review Started",
  review_completed: "Review Completed",
  merge_started: "Merge Started",
  merge_completed: "Merge Completed",
  eco_proposed: "ECO Proposed",
  eco_approved: "ECO Approved",
  eco_rejected: "ECO Rejected",
  budget_warning: "Budget Warning",
  budget_exhausted: "Budget Exhausted",
  container_spawned: "Container Spawned",
  container_destroyed: "Container Destroyed",
};

interface Props {
  events: AxiomEvent[];
}

export const Timeline: React.FC<Props> = ({ events }) => {
  return (
    <div>
      <h2 style={{ marginBottom: 16, fontSize: 20 }}>Timeline</h2>

      {events.length === 0 ? (
        <p style={{ color: "#94a3b8" }}>No events recorded yet.</p>
      ) : (
        <div style={{ position: "relative", paddingLeft: 24 }}>
          {/* Vertical line */}
          <div
            style={{
              position: "absolute",
              left: 7,
              top: 0,
              bottom: 0,
              width: 2,
              backgroundColor: "#334155",
            }}
          />

          {events.map((event, i) => {
            const color = EVENT_COLORS[event.type] || "#6b7280";
            return (
              <div
                key={i}
                style={{
                  position: "relative",
                  marginBottom: 12,
                  paddingLeft: 16,
                }}
              >
                {/* Dot on the timeline */}
                <div
                  style={{
                    position: "absolute",
                    left: -20,
                    top: 6,
                    width: 12,
                    height: 12,
                    borderRadius: "50%",
                    backgroundColor: color,
                    border: "2px solid #0f172a",
                  }}
                />

                <div
                  style={{
                    padding: "8px 12px",
                    backgroundColor: "#1e293b",
                    borderRadius: 6,
                    border: `1px solid ${color}33`,
                    borderLeft: `3px solid ${color}`,
                  }}
                >
                  <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                    <span style={{ color, fontWeight: 600, fontSize: 13 }}>
                      {EVENT_LABELS[event.type] || event.type}
                    </span>
                    <span style={{ color: "#64748b", fontSize: 11 }}>
                      {new Date(event.timestamp).toLocaleString()}
                    </span>
                  </div>
                  {event.task_id && (
                    <div style={{ fontSize: 12, color: "#94a3b8", marginTop: 4 }}>
                      Task: {event.task_id}
                    </div>
                  )}
                  {event.agent_type && (
                    <div style={{ fontSize: 12, color: "#64748b", marginTop: 2 }}>
                      Agent: {event.agent_type}
                      {event.agent_id ? ` (${event.agent_id})` : ""}
                    </div>
                  )}
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
};
