// Active Containers view: live list of running Meeseeks/reviewers.
// See Architecture Section 26.2.
import React from "react";
import type { Container } from "../types";

interface Props {
  containers: Container[];
}

export const ActiveContainers: React.FC<Props> = ({ containers }) => {
  return (
    <div className="view active-containers">
      <h2>Active Containers ({containers.length})</h2>
      {containers.length === 0 ? (
        <p>No active containers.</p>
      ) : (
        <table>
          <thead>
            <tr>
              <th>ID</th>
              <th>Task</th>
              <th>Type</th>
              <th>Model</th>
              <th>Image</th>
              <th>CPU</th>
              <th>Memory</th>
              <th>Started</th>
            </tr>
          </thead>
          <tbody>
            {containers.map((c) => (
              <tr key={c.id}>
                <td>{c.id.substring(0, 20)}</td>
                <td>{c.task_id}</td>
                <td>{c.container_type}</td>
                <td>{c.model_id || "-"}</td>
                <td>{c.image}</td>
                <td>{c.cpu_limit}</td>
                <td>{c.mem_limit}</td>
                <td>{new Date(c.started_at).toLocaleTimeString()}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
};
