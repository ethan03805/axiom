// Model Registry view: browsable model catalog.
// See Architecture Section 26.2.
import React, { useState } from "react";
import type { ModelInfo } from "../types";

interface Props {
  models: ModelInfo[];
}

export const ModelRegistry: React.FC<Props> = ({ models }) => {
  const [tierFilter, setTierFilter] = useState<string>("");
  const [familyFilter, setFamilyFilter] = useState<string>("");

  const filtered = models.filter((m) => {
    if (tierFilter && m.tier !== tierFilter) return false;
    if (familyFilter && m.family !== familyFilter) return false;
    return true;
  });

  const tiers = [...new Set(models.map((m) => m.tier))];
  const families = [...new Set(models.map((m) => m.family))];

  return (
    <div className="view model-registry">
      <h2>Model Registry ({filtered.length} models)</h2>
      <div className="filters">
        <select value={tierFilter} onChange={(e) => setTierFilter(e.target.value)}>
          <option value="">All Tiers</option>
          {tiers.map((t) => <option key={t} value={t}>{t}</option>)}
        </select>
        <select value={familyFilter} onChange={(e) => setFamilyFilter(e.target.value)}>
          <option value="">All Families</option>
          {families.map((f) => <option key={f} value={f}>{f}</option>)}
        </select>
      </div>
      <table>
        <thead>
          <tr>
            <th>Model</th><th>Family</th><th>Tier</th><th>Context</th>
            <th>Prompt $/M</th><th>Completion $/M</th><th>Success Rate</th>
          </tr>
        </thead>
        <tbody>
          {filtered.map((m) => (
            <tr key={m.id}>
              <td>{m.id}</td>
              <td>{m.family}</td>
              <td>{m.tier}</td>
              <td>{m.context_window.toLocaleString()}</td>
              <td>${m.prompt_per_million.toFixed(2)}</td>
              <td>${m.completion_per_million.toFixed(2)}</td>
              <td>{m.historical_success_rate != null ? `${(m.historical_success_rate * 100).toFixed(0)}%` : "-"}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
};
