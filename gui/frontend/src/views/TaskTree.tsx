// Task Tree view: hierarchical visualization with status indicators.
// Expandable nodes showing TaskSpec details, attempt history, and cost.
// See Architecture Section 26.2.
import React, { useState } from "react";
import type { Task } from "../types";
import { StatusBadge } from "../components/StatusBadge";

interface Props {
  tasks: Task[];
}

const nodeStyle: React.CSSProperties = {
  listStyle: "none",
  marginBottom: 2,
};

const headerStyle: React.CSSProperties = {
  display: "flex",
  alignItems: "center",
  gap: 8,
  padding: "6px 10px",
  cursor: "pointer",
  borderRadius: 4,
  backgroundColor: "#1e293b",
  border: "1px solid #334155",
  fontSize: 13,
};

const detailsStyle: React.CSSProperties = {
  marginLeft: 24,
  marginTop: 4,
  padding: "8px 12px",
  backgroundColor: "#0f172a",
  borderRadius: 4,
  border: "1px solid #1e293b",
  fontSize: 12,
  color: "#94a3b8",
};

export const TaskTree: React.FC<Props> = ({ tasks }) => {
  const rootTasks = tasks.filter((t) => !t.parent_id);

  return (
    <div>
      <h2 style={{ marginBottom: 16, fontSize: 20 }}>Task Tree</h2>
      {rootTasks.length === 0 ? (
        <p style={{ color: "#94a3b8" }}>
          No tasks yet. Awaiting SRS approval and task decomposition.
        </p>
      ) : (
        <ul style={{ listStyle: "none", padding: 0 }}>
          {rootTasks.map((task) => (
            <TaskNode key={task.id} task={task} allTasks={tasks} depth={0} />
          ))}
        </ul>
      )}
    </div>
  );
};

const TaskNode: React.FC<{ task: Task; allTasks: Task[]; depth: number }> = ({
  task,
  allTasks,
  depth,
}) => {
  const [expanded, setExpanded] = useState(false);
  const children = allTasks.filter((t) => t.parent_id === task.id);

  return (
    <li style={{ ...nodeStyle, marginLeft: depth * 16 }}>
      <div style={headerStyle} onClick={() => setExpanded(!expanded)}>
        {children.length > 0 && (
          <span style={{ fontFamily: "monospace", width: 14, color: "#64748b" }}>
            {expanded ? "-" : "+"}
          </span>
        )}
        <StatusBadge status={task.status} size="sm" showLabel={false} />
        <span style={{ color: "#e2e8f0", fontWeight: 500 }}>{task.title}</span>
        <span style={{ marginLeft: "auto", color: "#64748b", fontSize: 11 }}>
          [{task.tier}]
        </span>
        <StatusBadge status={task.status} size="sm" />
      </div>
      {expanded && (
        <div style={detailsStyle}>
          <div>ID: {task.id} | Type: {task.task_type}</div>
          {task.description && <div style={{ marginTop: 4 }}>{task.description}</div>}
          {task.base_snapshot && (
            <div style={{ marginTop: 4 }}>
              Base Snapshot: <code>{task.base_snapshot.substring(0, 8)}</code>
            </div>
          )}
          {task.created_at && (
            <div style={{ marginTop: 4 }}>
              Created: {new Date(task.created_at).toLocaleString()}
            </div>
          )}
          {task.completed_at && (
            <div style={{ marginTop: 4 }}>
              Completed: {new Date(task.completed_at).toLocaleString()}
            </div>
          )}
          {children.length > 0 && (
            <ul style={{ listStyle: "none", padding: 0, marginTop: 8 }}>
              {children.map((child) => (
                <TaskNode
                  key={child.id}
                  task={child}
                  allTasks={allTasks}
                  depth={depth + 1}
                />
              ))}
            </ul>
          )}
        </div>
      )}
    </li>
  );
};
