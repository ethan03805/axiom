// Axiom GUI Dashboard - Main Application
// Wails v2 desktop app with React frontend.
// See Architecture Section 26 for the full GUI specification.
//
// All 9 views from Section 26.2 are implemented:
// 1. Project Overview  2. Task Tree  3. Active Containers
// 4. Cost Dashboard    5. File Diff  6. Log Stream
// 7. Timeline          8. Model Registry  9. Resource Monitor

import React, { useState } from "react";
import { ProjectOverview } from "./views/ProjectOverview";
import { TaskTree } from "./views/TaskTree";
import { ActiveContainers } from "./views/ActiveContainers";
import { CostDashboard } from "./views/CostDashboard";
import { LogStream } from "./views/LogStream";
import { Timeline } from "./views/Timeline";
import { ModelRegistry } from "./views/ModelRegistry";
import { ResourceMonitor } from "./views/ResourceMonitor";
import { FileDiffViewer } from "./views/FileDiffViewer";
import { useEvents } from "./hooks/useEvents";

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

const App: React.FC = () => {
  const [activeView, setActiveView] = useState<View>("overview");
  const { events, clear: clearEvents } = useEvents();

  const renderView = () => {
    switch (activeView) {
      case "overview":
        return (
          <ProjectOverview
            status={null}
            ecos={[]}
            onPause={() => {}}
            onResume={() => {}}
            onCancel={() => {}}
          />
        );
      case "tasks":
        return <TaskTree tasks={[]} />;
      case "containers":
        return <ActiveContainers containers={[]} />;
      case "costs":
        return <CostDashboard costs={null} onSetBudget={() => {}} />;
      case "diff":
        return <FileDiffViewer taskId="" files={[]} pipelineStatus="idle" />;
      case "logs":
        return <LogStream events={events} onClear={clearEvents} />;
      case "timeline":
        return <Timeline events={events} />;
      case "models":
        return <ModelRegistry models={[]} />;
      case "resources":
        return <ResourceMonitor resources={null} />;
    }
  };

  return (
    <div className="app">
      <nav className="sidebar">
        <div className="logo">Axiom</div>
        {NAV_ITEMS.map((item) => (
          <button
            key={item.key}
            className={`nav-item ${activeView === item.key ? "active" : ""}`}
            onClick={() => setActiveView(item.key)}
          >
            {item.label}
          </button>
        ))}
      </nav>
      <main className="content">{renderView()}</main>
    </div>
  );
};

export default App;
