// Log Stream view: real-time scrolling event log.
// Events arrive via the Wails event system (axiom:event channel).
// See Architecture Section 26.2.
import React, { useRef, useEffect } from "react";
import type { AxiomEvent } from "../types";

interface Props {
  events: AxiomEvent[];
  onClear: () => void;
}

const EVENT_COLORS: Record<string, string> = {
  task_created: "#6b7280",
  task_started: "#3b82f6",
  task_completed: "#10b981",
  task_failed: "#ef4444",
  task_blocked: "#f59e0b",
  container_spawned: "#3b82f6",
  container_destroyed: "#6b7280",
  review_started: "#8b5cf6",
  review_completed: "#8b5cf6",
  merge_started: "#06b6d4",
  merge_completed: "#06b6d4",
  budget_warning: "#f97316",
  budget_exhausted: "#ef4444",
  eco_proposed: "#f59e0b",
  eco_approved: "#10b981",
  eco_rejected: "#ef4444",
  srs_submitted: "#3b82f6",
  srs_approved: "#10b981",
  provider_unavailable: "#f97316",
};

export const LogStream: React.FC<Props> = ({ events, onClear }) => {
  const scrollRef = useRef<HTMLDivElement>(null);

  // Auto-scroll to bottom when new events arrive.
  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = 0;
    }
  }, [events.length]);

  return (
    <div>
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          marginBottom: 12,
        }}
      >
        <h2 style={{ fontSize: 20 }}>Event Log ({events.length})</h2>
        <button
          onClick={onClear}
          style={{
            padding: "4px 12px",
            border: "1px solid #475569",
            borderRadius: 4,
            backgroundColor: "#334155",
            color: "#e2e8f0",
            cursor: "pointer",
            fontSize: 12,
          }}
        >
          Clear
        </button>
      </div>

      <div
        ref={scrollRef}
        style={{
          maxHeight: "calc(100vh - 140px)",
          overflow: "auto",
          backgroundColor: "#0f172a",
          border: "1px solid #334155",
          borderRadius: 6,
          fontFamily: "monospace",
          fontSize: 12,
        }}
      >
        {events.length === 0 ? (
          <div style={{ padding: 20, color: "#64748b", textAlign: "center" }}>
            No events yet. Waiting for engine activity.
          </div>
        ) : (
          events.map((event, i) => (
            <div
              key={i}
              style={{
                display: "flex",
                gap: 8,
                padding: "4px 12px",
                borderBottom: "1px solid #1e293b",
                alignItems: "baseline",
              }}
            >
              <span style={{ color: "#64748b", flexShrink: 0, width: 80 }}>
                {new Date(event.timestamp).toLocaleTimeString()}
              </span>
              <span
                style={{
                  color: EVENT_COLORS[event.type] || "#94a3b8",
                  flexShrink: 0,
                  width: 180,
                  fontWeight: 500,
                }}
              >
                [{event.type}]
              </span>
              {event.task_id && (
                <span style={{ color: "#94a3b8", flexShrink: 0 }}>
                  {event.task_id}
                </span>
              )}
              {event.agent_type && (
                <span style={{ color: "#64748b", flexShrink: 0 }}>
                  ({event.agent_type})
                </span>
              )}
              <span style={{ color: "#475569", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                {JSON.stringify(event.details)}
              </span>
            </div>
          ))
        )}
      </div>
    </div>
  );
};
