// Task Tree view: hierarchical visualization with status indicators.
// See Architecture Section 26.2.
import React, { useState } from "react";
import type { Task } from "../types";

const STATUS_COLORS: Record<string, string> = {
  queued: "#6b7280",
  in_progress: "#3b82f6",
  in_review: "#8b5cf6",
  done: "#10b981",
  failed: "#ef4444",
  blocked: "#f59e0b",
  waiting_on_lock: "#f97316",
  cancelled_eco: "#9ca3af",
};

interface Props {
  tasks: Task[];
}

export const TaskTree: React.FC<Props> = ({ tasks }) => {
  const rootTasks = tasks.filter((t) => !t.parent_id);

  return (
    <div className="view task-tree">
      <h2>Task Tree</h2>
      {rootTasks.length === 0 ? (
        <p>No tasks yet. Awaiting SRS approval and task decomposition.</p>
      ) : (
        <ul className="tree">
          {rootTasks.map((task) => (
            <TaskNode key={task.id} task={task} allTasks={tasks} />
          ))}
        </ul>
      )}
    </div>
  );
};

const TaskNode: React.FC<{ task: Task; allTasks: Task[] }> = ({ task, allTasks }) => {
  const [expanded, setExpanded] = useState(false);
  const children = allTasks.filter((t) => t.parent_id === task.id);

  return (
    <li className="tree-node">
      <div className="node-header" onClick={() => setExpanded(!expanded)}>
        <span className="status-dot" style={{ backgroundColor: STATUS_COLORS[task.status] || "#ccc" }} />
        <span className="task-title">{task.title}</span>
        <span className="task-status">{task.status}</span>
        <span className="task-tier">[{task.tier}]</span>
        {children.length > 0 && <span className="expand">{expanded ? "[-]" : "[+]"}</span>}
      </div>
      {expanded && (
        <div className="node-details">
          <p>ID: {task.id} | Type: {task.task_type}</p>
          {task.description && <p>{task.description}</p>}
          {children.length > 0 && (
            <ul className="tree">
              {children.map((child) => (
                <TaskNode key={child.id} task={child} allTasks={allTasks} />
              ))}
            </ul>
          )}
        </div>
      )}
    </li>
  );
};
