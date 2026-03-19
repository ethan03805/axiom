// Project Overview view: SRS summary, budget gauge, progress, elapsed time, ECO history.
// See Architecture Section 26.2.
import React from "react";
import type { ProjectStatus, ECO } from "../types";
import { BudgetGauge } from "../components/BudgetGauge";
import { StatusBadge } from "../components/StatusBadge";

interface Props {
  status: ProjectStatus | null;
  ecos: ECO[];
  onPause: () => void;
  onResume: () => void;
  onCancel: () => void;
}

const gridStyle: React.CSSProperties = {
  display: "grid",
  gridTemplateColumns: "repeat(auto-fill, minmax(180px, 1fr))",
  gap: 12,
  marginBottom: 20,
};

const statStyle: React.CSSProperties = {
  backgroundColor: "#1e293b",
  borderRadius: 6,
  padding: 14,
  border: "1px solid #334155",
};

const labelStyle: React.CSSProperties = {
  fontSize: 11,
  color: "#94a3b8",
  textTransform: "uppercase",
  letterSpacing: 1,
  marginBottom: 4,
};

const valueStyle: React.CSSProperties = {
  fontSize: 18,
  fontWeight: 600,
  color: "#e2e8f0",
};

const btnStyle: React.CSSProperties = {
  padding: "6px 16px",
  border: "1px solid #475569",
  borderRadius: 4,
  backgroundColor: "#334155",
  color: "#e2e8f0",
  cursor: "pointer",
  marginRight: 8,
  fontSize: 13,
};

export const ProjectOverview: React.FC<Props> = ({
  status,
  ecos,
  onPause,
  onResume,
  onCancel,
}) => {
  if (!status) {
    return <div style={{ color: "#94a3b8" }}>Loading project status...</div>;
  }

  return (
    <div>
      <h2 style={{ marginBottom: 16, fontSize: 20 }}>Project: {status.name}</h2>

      <div style={gridStyle}>
        <div style={statStyle}>
          <div style={labelStyle}>Phase</div>
          <div style={valueStyle}>{status.phase}</div>
        </div>
        <div style={statStyle}>
          <div style={labelStyle}>Progress</div>
          <div style={valueStyle}>
            {status.progress_pct.toFixed(0)}%
            <span style={{ fontSize: 12, color: "#94a3b8", marginLeft: 4 }}>
              ({status.done_tasks}/{status.total_tasks})
            </span>
          </div>
        </div>
        <div style={statStyle}>
          <div style={labelStyle}>Elapsed</div>
          <div style={valueStyle}>{status.elapsed_time}</div>
        </div>
        <div style={statStyle}>
          <div style={labelStyle}>Active Meeseeks</div>
          <div style={valueStyle}>{status.active_meeseeks}</div>
        </div>
      </div>

      <h3 style={{ marginBottom: 8 }}>Budget</h3>
      <BudgetGauge spent={status.budget_used} budget={status.budget_max} />

      <h3 style={{ marginTop: 20, marginBottom: 8 }}>Controls</h3>
      <div>
        <button style={btnStyle} onClick={onPause}>Pause</button>
        <button style={btnStyle} onClick={onResume}>Resume</button>
        <button style={{ ...btnStyle, borderColor: "#ef4444", color: "#ef4444" }} onClick={onCancel}>
          Cancel
        </button>
      </div>

      {ecos.length > 0 && (
        <>
          <h3 style={{ marginTop: 20, marginBottom: 8 }}>ECO History</h3>
          <ul style={{ listStyle: "none", padding: 0 }}>
            {ecos.map((eco) => (
              <li
                key={eco.id}
                style={{
                  padding: "8px 12px",
                  backgroundColor: "#1e293b",
                  borderRadius: 4,
                  marginBottom: 4,
                  border: "1px solid #334155",
                  display: "flex",
                  alignItems: "center",
                  gap: 8,
                }}
              >
                <StatusBadge status={eco.status} size="sm" />
                <span>[{eco.eco_code}]</span>
                <span style={{ color: "#94a3b8" }}>{eco.category}</span>
                <span style={{ marginLeft: "auto", fontSize: 11, color: "#64748b" }}>
                  {new Date(eco.created_at).toLocaleDateString()}
                </span>
              </li>
            ))}
          </ul>
        </>
      )}
    </div>
  );
};
