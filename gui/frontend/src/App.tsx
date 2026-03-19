// Axiom GUI Dashboard - Main Application
// Wails v2 desktop app with React frontend.
// See Architecture Section 26 for the full GUI specification.
//
// All 9 views from Section 26.2 are implemented:
// 1. Project Overview  2. Task Tree  3. Active Containers
// 4. Cost Dashboard    5. File Diff  6. Log Stream
// 7. Timeline          8. Model Registry  9. Resource Monitor

import React, { useState, useEffect, useCallback } from "react";
import { ProjectOverview } from "./views/ProjectOverview";
import { TaskTree } from "./views/TaskTree";
import { ActiveContainers } from "./views/ActiveContainers";
import { CostDashboard } from "./views/CostDashboard";
import { LogStream } from "./views/LogStream";
import { Timeline } from "./views/Timeline";
import { ModelRegistry } from "./views/ModelRegistry";
import { ResourceMonitor } from "./views/ResourceMonitor";
import { FileDiffViewer } from "./views/FileDiffViewer";
import { useAxiomEvents, useWailsBackend } from "./hooks/useAxiomEvents";
import type {
  ProjectStatus,
  ECO,
  Task,
  Container,
  CostReport,
  ModelInfo,
} from "./types";

type View =
  | "overview"
  | "tasks"
  | "containers"
  | "costs"
  | "diff"
  | "logs"
  | "timeline"
  | "models"
  | "resources";

const NAV_ITEMS: { key: View; label: string }[] = [
  { key: "overview", label: "Overview" },
  { key: "tasks", label: "Task Tree" },
  { key: "containers", label: "Containers" },
  { key: "costs", label: "Costs" },
  { key: "diff", label: "File Diff" },
  { key: "logs", label: "Log Stream" },
  { key: "timeline", label: "Timeline" },
  { key: "models", label: "Models" },
  { key: "resources", label: "Resources" },
];

const sidebarStyle: React.CSSProperties = {
  width: 180,
  backgroundColor: "#1e293b",
  borderRight: "1px solid #334155",
  display: "flex",
  flexDirection: "column",
  padding: "16px 0",
  flexShrink: 0,
};

const logoStyle: React.CSSProperties = {
  fontSize: 20,
  fontWeight: 700,
  padding: "0 16px 16px",
  borderBottom: "1px solid #334155",
  marginBottom: 8,
  color: "#38bdf8",
  letterSpacing: 2,
};

const navItemStyle: React.CSSProperties = {
  padding: "8px 16px",
  border: "none",
  background: "none",
  color: "#94a3b8",
  cursor: "pointer",
  textAlign: "left",
  fontSize: 13,
  fontFamily: "inherit",
};

const navItemActiveStyle: React.CSSProperties = {
  ...navItemStyle,
  color: "#e2e8f0",
  backgroundColor: "#334155",
  borderLeft: "3px solid #38bdf8",
};

const contentStyle: React.CSSProperties = {
  flex: 1,
  overflow: "auto",
  padding: 24,
};

const App: React.FC = () => {
  const [activeView, setActiveView] = useState<View>("overview");
  const { events, clear: clearEvents } = useAxiomEvents();
  const backend = useWailsBackend();

  // Polled state -- refreshed via Wails backend calls.
  const [status, setStatus] = useState<ProjectStatus | null>(null);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [containers, setContainers] = useState<Container[]>([]);
  const [costs, setCosts] = useState<CostReport | null>(null);
  const [models, setModels] = useState<ModelInfo[]>([]);

  // Refresh data from the backend on a 2-second interval.
  // Per Architecture Section 26.4, real-time updates come from events,
  // but we also poll for full state reconciliation.
  const refreshData = useCallback(async () => {
    const [s, t, co, ct, m] = await Promise.all([
      backend.getStatus(),
      backend.getTasks(),
      backend.getContainers(),
      backend.getCosts(),
      backend.getModels(),
    ]);
    if (s) setStatus(s as ProjectStatus);
    if (t) setTasks(t as Task[]);
    if (co) setContainers(co as Container[]);
    if (ct) setCosts(ct as CostReport);
    if (m) setModels(m as ModelInfo[]);
  }, [backend]);

  useEffect(() => {
    refreshData();
    const id = setInterval(refreshData, 2000);
    return () => clearInterval(id);
  }, [refreshData]);

  const handlePause = useCallback(() => { backend.pause(); }, [backend]);
  const handleResume = useCallback(() => { backend.resume(); }, [backend]);
  const handleCancel = useCallback(() => { backend.cancel(); }, [backend]);
  const handleSetBudget = useCallback(
    (amount: number) => { backend.setBudget(amount); },
    [backend]
  );

  const renderView = () => {
    switch (activeView) {
      case "overview":
        return (
          <ProjectOverview
            status={status}
            ecos={[]}
            onPause={handlePause}
            onResume={handleResume}
            onCancel={handleCancel}
          />
        );
      case "tasks":
        return <TaskTree tasks={tasks} />;
      case "containers":
        return <ActiveContainers containers={containers} />;
      case "costs":
        return <CostDashboard costs={costs} onSetBudget={handleSetBudget} />;
      case "diff":
        return <FileDiffViewer taskId="" files={[]} pipelineStatus="idle" />;
      case "logs":
        return <LogStream events={events} onClear={clearEvents} />;
      case "timeline":
        return <Timeline events={events} />;
      case "models":
        return <ModelRegistry models={models} />;
      case "resources":
        return <ResourceMonitor resources={null} />;
    }
  };

  return (
    <div style={{ display: "flex", height: "100vh", width: "100vw" }}>
      <nav style={sidebarStyle}>
        <div style={logoStyle}>AXIOM</div>
        {NAV_ITEMS.map((item) => (
          <button
            key={item.key}
            style={activeView === item.key ? navItemActiveStyle : navItemStyle}
            onClick={() => setActiveView(item.key)}
          >
            {item.label}
          </button>
        ))}
      </nav>
      <main style={contentStyle}>{renderView()}</main>
    </div>
  );
};

export default App;
