// Model Registry view: browsable model catalog with capabilities, pricing, history.
// See Architecture Section 26.2.
import React, { useState } from "react";
import type { ModelInfo } from "../types";

interface Props {
  models: ModelInfo[];
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
  position: "sticky",
  top: 0,
  backgroundColor: "#0f172a",
};

const tdStyle: React.CSSProperties = {
  padding: "8px 12px",
  borderBottom: "1px solid #1e293b",
  color: "#e2e8f0",
};

const selectStyle: React.CSSProperties = {
  padding: "4px 8px",
  backgroundColor: "#0f172a",
  border: "1px solid #334155",
  borderRadius: 4,
  color: "#e2e8f0",
  fontSize: 13,
};

const TIER_COLORS: Record<string, string> = {
  local: "#6b7280",
  cheap: "#10b981",
  standard: "#3b82f6",
  premium: "#f59e0b",
};

export const ModelRegistry: React.FC<Props> = ({ models }) => {
  const [tierFilter, setTierFilter] = useState<string>("");
  const [familyFilter, setFamilyFilter] = useState<string>("");
  const [expandedModel, setExpandedModel] = useState<string | null>(null);

  const filtered = models.filter((m) => {
    if (tierFilter && m.tier !== tierFilter) return false;
    if (familyFilter && m.family !== familyFilter) return false;
    return true;
  });

  const tiers = [...new Set(models.map((m) => m.tier))];
  const families = [...new Set(models.map((m) => m.family))];

  return (
    <div>
      <h2 style={{ marginBottom: 16, fontSize: 20 }}>
        Model Registry ({filtered.length} models)
      </h2>

      <div style={{ display: "flex", gap: 8, marginBottom: 16 }}>
        <select
          style={selectStyle}
          value={tierFilter}
          onChange={(e) => setTierFilter(e.target.value)}
        >
          <option value="">All Tiers</option>
          {tiers.map((t) => (
            <option key={t} value={t}>{t}</option>
          ))}
        </select>
        <select
          style={selectStyle}
          value={familyFilter}
          onChange={(e) => setFamilyFilter(e.target.value)}
        >
          <option value="">All Families</option>
          {families.map((f) => (
            <option key={f} value={f}>{f}</option>
          ))}
        </select>
      </div>

      {filtered.length === 0 ? (
        <p style={{ color: "#94a3b8" }}>
          No models available. Run the backend to populate the model registry.
        </p>
      ) : (
        <table style={tableStyle}>
          <thead>
            <tr>
              <th style={thStyle}>Model</th>
              <th style={thStyle}>Family</th>
              <th style={thStyle}>Tier</th>
              <th style={thStyle}>Context</th>
              <th style={thStyle}>Prompt $/M</th>
              <th style={thStyle}>Completion $/M</th>
              <th style={thStyle}>Success Rate</th>
            </tr>
          </thead>
          <tbody>
            {filtered.map((m) => (
              <React.Fragment key={m.id}>
                <tr
                  style={{ cursor: "pointer" }}
                  onClick={() =>
                    setExpandedModel(expandedModel === m.id ? null : m.id)
                  }
                >
                  <td style={tdStyle}>
                    <span style={{ fontWeight: 500 }}>{m.id}</span>
                  </td>
                  <td style={tdStyle}>{m.family}</td>
                  <td style={tdStyle}>
                    <span
                      style={{
                        color: TIER_COLORS[m.tier] || "#94a3b8",
                        fontWeight: 500,
                      }}
                    >
                      {m.tier}
                    </span>
                  </td>
                  <td style={tdStyle}>{m.context_window.toLocaleString()}</td>
                  <td style={tdStyle}>${m.prompt_per_million.toFixed(2)}</td>
                  <td style={tdStyle}>${m.completion_per_million.toFixed(2)}</td>
                  <td style={tdStyle}>
                    {m.historical_success_rate != null
                      ? `${(m.historical_success_rate * 100).toFixed(0)}%`
                      : "-"}
                  </td>
                </tr>
                {expandedModel === m.id && (
                  <tr>
                    <td colSpan={7} style={{ padding: "8px 12px", backgroundColor: "#1e293b" }}>
                      <div style={{ display: "flex", gap: 24, fontSize: 12, color: "#94a3b8" }}>
                        <div>
                          <strong style={{ color: "#e2e8f0" }}>Strengths:</strong>{" "}
                          {m.strengths.length > 0 ? m.strengths.join(", ") : "N/A"}
                        </div>
                        <div>
                          <strong style={{ color: "#e2e8f0" }}>Weaknesses:</strong>{" "}
                          {m.weaknesses.length > 0 ? m.weaknesses.join(", ") : "N/A"}
                        </div>
                        <div>
                          <strong style={{ color: "#e2e8f0" }}>Max Output:</strong>{" "}
                          {m.max_output.toLocaleString()} tokens
                        </div>
                        <div>
                          <strong style={{ color: "#e2e8f0" }}>Source:</strong> {m.source}
                        </div>
                      </div>
                    </td>
                  </tr>
                )}
              </React.Fragment>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
};
