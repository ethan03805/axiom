// BudgetGauge: reusable budget progress bar component.
// Displays spent vs remaining budget with color thresholds.
// Turns yellow at warn_at_percent (default 80%) and red at 95%.
// See Architecture Section 26.2 (Cost Dashboard, Project Overview).
import React from "react";

interface Props {
  spent: number;
  budget: number;
  warnPercent?: number;
  showLabel?: boolean;
  height?: number;
}

export const BudgetGauge: React.FC<Props> = ({
  spent,
  budget,
  warnPercent = 80,
  showLabel = true,
  height = 20,
}) => {
  const pct = budget > 0 ? (spent / budget) * 100 : 0;
  const clampedPct = Math.min(pct, 100);

  let barColor = "#10b981"; // green
  if (pct >= 95) {
    barColor = "#ef4444"; // red
  } else if (pct >= warnPercent) {
    barColor = "#f59e0b"; // yellow/amber
  }

  const containerStyle: React.CSSProperties = {
    width: "100%",
    backgroundColor: "#1e293b",
    borderRadius: 4,
    overflow: "hidden",
    position: "relative",
    height,
    border: "1px solid #334155",
  };

  const barStyle: React.CSSProperties = {
    height: "100%",
    width: `${clampedPct}%`,
    backgroundColor: barColor,
    transition: "width 0.3s ease, background-color 0.3s ease",
    borderRadius: 4,
  };

  const labelStyle: React.CSSProperties = {
    position: "absolute",
    top: 0,
    left: 0,
    right: 0,
    bottom: 0,
    display: "flex",
    alignItems: "center",
    justifyContent: "center",
    fontSize: 11,
    fontWeight: 600,
    color: "#e2e8f0",
    textShadow: "0 1px 2px rgba(0,0,0,0.5)",
  };

  return (
    <div>
      <div style={containerStyle}>
        <div style={barStyle} />
        {showLabel && (
          <div style={labelStyle}>
            ${spent.toFixed(2)} / ${budget.toFixed(2)} ({pct.toFixed(1)}%)
          </div>
        )}
      </div>
      {pct >= 95 && (
        <div
          style={{
            color: "#ef4444",
            fontSize: 11,
            marginTop: 4,
            fontWeight: 600,
          }}
        >
          Budget nearly exhausted
        </div>
      )}
      {pct >= warnPercent && pct < 95 && (
        <div
          style={{
            color: "#f59e0b",
            fontSize: 11,
            marginTop: 4,
            fontWeight: 600,
          }}
        >
          Budget warning threshold reached
        </div>
      )}
    </div>
  );
};
