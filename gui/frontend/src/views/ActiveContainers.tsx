// Active Containers view: live list of running Meeseeks/reviewers.
// Displays model, task, duration, CPU/memory for each container.
// See Architecture Section 26.2.
import React from "react";
import type { Container } from "../types";
import { StatusBadge } from "../components/StatusBadge";

interface Props {
  containers: Container[];
}

const tableStyle: React.CSSProperties = {
  width: "100%",
  borderCollapse: "collapse",
  fontSize: 13,
};

const thStyle: React.CSSProperties = {
  textAlign: "left",
  padding: "8px 12px",
  borderBottom: "1px solid #334155",
  color: "#94a3b8",
  fontSize: 11,
  textTransform: "uppercase",
  letterSpacing: 1,
};

const tdStyle: React.CSSProperties = {
  padding: "8px 12px",
  borderBottom: "1px solid #1e293b",
  color: "#e2e8f0",
};

function elapsed(started: string): string {
  const ms = Date.now() - new Date(started).getTime();
  const sec = Math.floor(ms / 1000);
  if (sec < 60) return `${sec}s`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min}m ${sec % 60}s`;
  const hr = Math.floor(min / 60);
  return `${hr}h ${min % 60}m`;
}

export const ActiveContainers: React.FC<Props> = ({ containers }) => {
  return (
    <div>
      <h2 style={{ marginBottom: 16, fontSize: 20 }}>
        Active Containers ({containers.length})
      </h2>
      {containers.length === 0 ? (
        <p style={{ color: "#94a3b8" }}>No active containers.</p>
      ) : (
        <table style={tableStyle}>
          <thead>
            <tr>
              <th style={thStyle}>Container</th>
              <th style={thStyle}>Task</th>
              <th style={thStyle}>Type</th>
              <th style={thStyle}>Model</th>
              <th style={thStyle}>Image</th>
              <th style={thStyle}>CPU</th>
              <th style={thStyle}>Memory</th>
              <th style={thStyle}>Duration</th>
              <th style={thStyle}>Status</th>
            </tr>
          </thead>
          <tbody>
            {containers.map((c) => (
              <tr key={c.id}>
                <td style={tdStyle}>
                  <code style={{ fontSize: 11 }}>{c.id.substring(0, 24)}</code>
                </td>
                <td style={tdStyle}>{c.task_id}</td>
                <td style={tdStyle}>{c.container_type}</td>
                <td style={tdStyle}>{c.model_id || "-"}</td>
                <td style={{ ...tdStyle, fontSize: 11, color: "#94a3b8" }}>
                  {c.image}
                </td>
                <td style={tdStyle}>{c.cpu_limit}</td>
                <td style={tdStyle}>{c.mem_limit}</td>
                <td style={tdStyle}>{elapsed(c.started_at)}</td>
                <td style={tdStyle}>
                  <StatusBadge status="running" size="sm" />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
};
