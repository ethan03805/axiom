// Project Overview view: SRS summary, budget gauge, progress, elapsed time, ECO history.
// See Architecture Section 26.2.
import React from "react";
import type { ProjectStatus, ECO } from "../types";

interface Props {
  status: ProjectStatus | null;
  ecos: ECO[];
  onPause: () => void;
  onResume: () => void;
  onCancel: () => void;
}

export const ProjectOverview: React.FC<Props> = ({ status, ecos, onPause, onResume, onCancel }) => {
  if (!status) return <div className="view">Loading project status...</div>;

  const budgetPct = status.budget_max > 0 ? (status.budget_used / status.budget_max) * 100 : 0;

  return (
    <div className="view project-overview">
      <h2>Project: {status.name}</h2>
      <div className="stats-grid">
        <div className="stat">
          <label>Phase</label>
          <span>{status.phase}</span>
        </div>
        <div className="stat">
          <label>Progress</label>
          <span>{status.progress_pct.toFixed(0)}% ({status.done_tasks}/{status.total_tasks} tasks)</span>
        </div>
        <div className="stat">
          <label>Elapsed</label>
          <span>{status.elapsed_time}</span>
        </div>
        <div className="stat">
          <label>Active Meeseeks</label>
          <span>{status.active_meeseeks}</span>
        </div>
      </div>

      <h3>Budget</h3>
      <div className="budget-gauge">
        <div className="gauge-bar" style={{ width: `${Math.min(budgetPct, 100)}%` }} />
        <span>${status.budget_used.toFixed(2)} / ${status.budget_max.toFixed(2)} ({budgetPct.toFixed(1)}%)</span>
      </div>

      <h3>Controls</h3>
      <div className="controls">
        <button onClick={onPause}>Pause</button>
        <button onClick={onResume}>Resume</button>
        <button onClick={onCancel}>Cancel</button>
      </div>

      {ecos.length > 0 && (
        <>
          <h3>ECO History</h3>
          <ul>
            {ecos.map((eco) => (
              <li key={eco.id}>[{eco.eco_code}] {eco.category} - {eco.status}</li>
            ))}
          </ul>
        </>
      )}
    </div>
  );
};
