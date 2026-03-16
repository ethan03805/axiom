// Cost Dashboard view: spend charts, budget gauge, projections.
// See Architecture Section 26.2.
import React from "react";
import type { CostReport } from "../types";

interface Props {
  costs: CostReport | null;
  onSetBudget: (amount: number) => void;
}

export const CostDashboard: React.FC<Props> = ({ costs, onSetBudget }) => {
  if (!costs) return <div className="view">Loading cost data...</div>;

  return (
    <div className="view cost-dashboard">
      <h2>Cost Dashboard</h2>

      {costs.external_mode && costs.disclaimer && (
        <div className="disclaimer">{costs.disclaimer}</div>
      )}

      <div className="stats-grid">
        <div className="stat">
          <label>Total Spent</label>
          <span>${costs.project_total_usd.toFixed(4)}</span>
        </div>
        <div className="stat">
          <label>Budget Remaining</label>
          <span>${costs.remaining_usd.toFixed(4)}</span>
        </div>
        <div className="stat">
          <label>Budget Used</label>
          <span>{costs.budget_used_pct.toFixed(1)}%</span>
        </div>
        <div className="stat">
          <label>Projected Total</label>
          <span>${costs.projected_total_usd.toFixed(4)}</span>
        </div>
      </div>

      <h3>Budget Gauge</h3>
      <div className="budget-gauge">
        <div
          className="gauge-bar"
          style={{
            width: `${Math.min(costs.budget_used_pct, 100)}%`,
            backgroundColor: costs.budget_used_pct > 80 ? "#ef4444" : "#10b981",
          }}
        />
      </div>

      <h3>By Model</h3>
      <table>
        <thead><tr><th>Model</th><th>Cost</th></tr></thead>
        <tbody>
          {Object.entries(costs.by_model).map(([model, cost]) => (
            <tr key={model}><td>{model}</td><td>${cost.toFixed(4)}</td></tr>
          ))}
        </tbody>
      </table>

      <h3>By Agent Type</h3>
      <table>
        <thead><tr><th>Agent</th><th>Cost</th></tr></thead>
        <tbody>
          {Object.entries(costs.by_agent_type).map(([agent, cost]) => (
            <tr key={agent}><td>{agent}</td><td>${cost.toFixed(4)}</td></tr>
          ))}
        </tbody>
      </table>
    </div>
  );
};
