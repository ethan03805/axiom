// Cost Dashboard view: spend charts, budget gauge, per-task breakdown, projections.
// Includes external mode budget disclaimer per Architecture Section 21.4.
// See Architecture Section 26.2.
import React, { useState } from "react";
import type { CostReport } from "../types";
import { BudgetGauge } from "../components/BudgetGauge";

interface Props {
  costs: CostReport | null;
  onSetBudget: (amount: number) => void;
}

const sectionStyle: React.CSSProperties = {
  marginBottom: 20,
};

const tableStyle: React.CSSProperties = {
  width: "100%",
  borderCollapse: "collapse",
  fontSize: 13,
};

const thStyle: React.CSSProperties = {
  textAlign: "left",
  padding: "6px 12px",
  borderBottom: "1px solid #334155",
  color: "#94a3b8",
  fontSize: 11,
  textTransform: "uppercase",
};

const tdStyle: React.CSSProperties = {
  padding: "6px 12px",
  borderBottom: "1px solid #1e293b",
  color: "#e2e8f0",
};

const disclaimerStyle: React.CSSProperties = {
  padding: "8px 12px",
  backgroundColor: "#1e293b",
  border: "1px solid #f59e0b44",
  borderRadius: 4,
  color: "#f59e0b",
  fontSize: 12,
  marginBottom: 16,
};

export const CostDashboard: React.FC<Props> = ({ costs, onSetBudget }) => {
  const [budgetInput, setBudgetInput] = useState("");

  if (!costs) {
    return <div style={{ color: "#94a3b8" }}>Loading cost data...</div>;
  }

  const handleSetBudget = () => {
    const amount = parseFloat(budgetInput);
    if (!isNaN(amount) && amount > 0) {
      onSetBudget(amount);
      setBudgetInput("");
    }
  };

  return (
    <div>
      <h2 style={{ marginBottom: 16, fontSize: 20 }}>Cost Dashboard</h2>

      {costs.external_mode && costs.disclaimer && (
        <div style={disclaimerStyle}>{costs.disclaimer}</div>
      )}

      <div style={sectionStyle}>
        <div
          style={{
            display: "grid",
            gridTemplateColumns: "repeat(auto-fill, minmax(160px, 1fr))",
            gap: 12,
            marginBottom: 16,
          }}
        >
          <StatCard label="Total Spent" value={`$${costs.project_total_usd.toFixed(4)}`} />
          <StatCard label="Budget Remaining" value={`$${costs.remaining_usd.toFixed(4)}`} />
          <StatCard label="Budget Used" value={`${costs.budget_used_pct.toFixed(1)}%`} />
          <StatCard label="Projected Total" value={`$${costs.projected_total_usd.toFixed(4)}`} />
        </div>
      </div>

      <div style={sectionStyle}>
        <h3 style={{ marginBottom: 8 }}>Budget Gauge</h3>
        <BudgetGauge spent={costs.project_total_usd} budget={costs.budget_max_usd} />
        <div style={{ marginTop: 8, display: "flex", gap: 8, alignItems: "center" }}>
          <input
            type="number"
            placeholder="New budget ($)"
            value={budgetInput}
            onChange={(e) => setBudgetInput(e.target.value)}
            style={{
              padding: "4px 8px",
              backgroundColor: "#0f172a",
              border: "1px solid #334155",
              borderRadius: 4,
              color: "#e2e8f0",
              fontSize: 13,
              width: 140,
            }}
          />
          <button
            onClick={handleSetBudget}
            style={{
              padding: "4px 12px",
              border: "1px solid #475569",
              borderRadius: 4,
              backgroundColor: "#334155",
              color: "#e2e8f0",
              cursor: "pointer",
              fontSize: 13,
            }}
          >
            Set Budget
          </button>
        </div>
      </div>

      <div style={sectionStyle}>
        <h3 style={{ marginBottom: 8 }}>Spend by Task</h3>
        <table style={tableStyle}>
          <thead>
            <tr><th style={thStyle}>Task</th><th style={thStyle}>Cost</th></tr>
          </thead>
          <tbody>
            {Object.entries(costs.by_task).map(([task, cost]) => (
              <tr key={task}>
                <td style={tdStyle}>{task}</td>
                <td style={tdStyle}>${cost.toFixed(4)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <div style={sectionStyle}>
        <h3 style={{ marginBottom: 8 }}>Spend by Model</h3>
        <table style={tableStyle}>
          <thead>
            <tr><th style={thStyle}>Model</th><th style={thStyle}>Cost</th></tr>
          </thead>
          <tbody>
            {Object.entries(costs.by_model).map(([model, cost]) => (
              <tr key={model}>
                <td style={tdStyle}>{model}</td>
                <td style={tdStyle}>${cost.toFixed(4)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <div style={sectionStyle}>
        <h3 style={{ marginBottom: 8 }}>Spend by Agent Type</h3>
        <table style={tableStyle}>
          <thead>
            <tr><th style={thStyle}>Agent</th><th style={thStyle}>Cost</th></tr>
          </thead>
          <tbody>
            {Object.entries(costs.by_agent_type).map(([agent, cost]) => (
              <tr key={agent}>
                <td style={tdStyle}>{agent}</td>
                <td style={tdStyle}>${cost.toFixed(4)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
};

const StatCard: React.FC<{ label: string; value: string }> = ({ label, value }) => (
  <div
    style={{
      backgroundColor: "#1e293b",
      borderRadius: 6,
      padding: 14,
      border: "1px solid #334155",
    }}
  >
    <div style={{ fontSize: 11, color: "#94a3b8", textTransform: "uppercase", letterSpacing: 1, marginBottom: 4 }}>
      {label}
    </div>
    <div style={{ fontSize: 18, fontWeight: 600, color: "#e2e8f0" }}>{value}</div>
  </div>
);
