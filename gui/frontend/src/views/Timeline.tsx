// Timeline view: chronological event visualization.
// See Architecture Section 26.2.
import React from "react";
import type { AxiomEvent } from "../types";

const EVENT_COLORS: Record<string, string> = {
  task_started: "#3b82f6",
  task_completed: "#10b981",
  task_failed: "#ef4444",
  review_completed: "#8b5cf6",
  merge_completed: "#06b6d4",
  eco_proposed: "#f59e0b",
  budget_warning: "#f97316",
};

interface Props {
  events: AxiomEvent[];
}

export const Timeline: React.FC<Props> = ({ events }) => {
  return (
    <div className="view timeline">
      <h2>Timeline</h2>
      <div className="timeline-track">
        {events.map((event, i) => (
          <div key={i} className="timeline-event" style={{ borderLeftColor: EVENT_COLORS[event.type] || "#6b7280" }}>
            <div className="timeline-time">{new Date(event.timestamp).toLocaleString()}</div>
            <div className="timeline-type">{event.type}</div>
            {event.task_id && <div className="timeline-task">Task: {event.task_id}</div>}
          </div>
        ))}
        {events.length === 0 && <p>No events recorded yet.</p>}
      </div>
    </div>
  );
};
