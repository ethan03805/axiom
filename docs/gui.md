# GUI Dashboard

Axiom includes a desktop dashboard built with Wails v2 (Go backend + React frontend) providing real-time visibility into project execution.

---

## Technology Stack

| Component | Technology |
|-----------|-----------|
| Framework | Wails v2 |
| Frontend | React 18 + TypeScript |
| Build tool | Vite 5 |
| Styling | Inline CSS (Tailwind color palette) |
| Communication | Wails RPC + event system |

---

## Building the GUI

```bash
make gui

# Or manually:
cd gui/frontend
npm install
npm run build
```

---

## Views

The dashboard has nine views accessible via the left sidebar:

### 1. Project Overview

Displays project-level metrics and controls:
- Current phase and progress percentage
- Elapsed time and active Meeseeks count
- Budget gauge with color-coded thresholds
- Control buttons: Pause, Resume, Cancel
- ECO history list with status badges

### 2. Task Tree

Hierarchical visualization of the task tree:
- Expandable nodes showing task details (ID, type, description, timestamps)
- Color-coded status badges for each task
- Tier display (local, cheap, standard, premium)
- Parent-child relationships with indentation
- Recursive tree rendering

### 3. Active Containers

Real-time table of running containers:
- Container ID, task, type (meeseeks, reviewer, validator, sub_orchestrator)
- Model being used
- Docker image
- CPU and memory limits
- Elapsed time since spawn
- Status indicator

### 4. Cost Dashboard

Budget tracking and cost analysis:
- Stat cards: Total Spent, Budget Remaining, Budget Used %, Projected Total
- Budget gauge with warning at 80% and danger at 95%
- Budget adjustment input
- Three breakdown tables: by task, by model, by agent type
- External mode disclaimer when applicable

### 5. File Diff Viewer

Side-by-side view of Meeseeks output:
- File operations color-coded: add (green), modify (blue), delete (red), rename (orange)
- Task ID and pipeline status header
- Old content (left pane) vs new content (right pane)
- Monospace font for code display

### 6. Log Stream

Real-time scrolling event log:
- Color-coded event type tags (32 event types supported)
- Auto-scroll to newest events
- Timestamp, event type, task ID, agent type, and details
- Clear button to reset

### 7. Timeline

Chronological event visualization:
- Vertical timeline with animated dots
- Color-coded event bands
- Event labels and detail information
- 46 event type mappings with custom colors

### 8. Model Registry

Browsable model catalog:
- Filter by tier (local, cheap, standard, premium)
- Filter by family (anthropic, openai, meta, local)
- Columns: Model ID, Family, Tier, Context Window, Pricing, Success Rate
- Expandable rows showing strengths, weaknesses, max output, source

### 9. Resource Monitor

System resource visualization:
- System CPU and memory usage
- Container CPU and memory consumption
- BitNet server status, threads, active requests, memory
- Color-coded bar gauges (green < 70%, yellow 70-90%, red > 90%)
- Overload warning banner when system > 70% utilized

---

## Controls

The GUI provides interactive controls for:

| Control | Location | Action |
|---------|----------|--------|
| Pause | Project Overview | Stop spawning new Meeseeks |
| Resume | Project Overview | Resume paused execution |
| Cancel | Project Overview | Kill all containers, cancel project |
| Set Budget | Cost Dashboard | Modify the budget ceiling |
| Approve SRS | (planned) | Approve the generated specification |
| Reject SRS | (planned) | Reject SRS with feedback |
| Approve ECO | (planned) | Approve an Engineering Change Order |
| Reject ECO | (planned) | Reject an ECO |

---

## Real-Time Updates

The GUI receives events from the engine via the Wails event system:

1. Engine emits events via `runtime.EventsEmit(ctx, "axiom:event", event)`
2. React subscribes via the `useAxiomEvents` hook
3. UI updates within 500ms of event receipt
4. Data is polled from the Go backend every 2 seconds for status updates
5. The GUI never polls SQLite directly

---

## Design

- **Dark theme**: Background `#0f172a`, cards `#1e293b`, text `#e2e8f0`
- **Color coding**: Status badges, event types, and gauges use semantic colors
- **Layout**: Two-column with 180px sidebar navigation + scrollable content area
- **Font**: System fonts with monospace for code
- **No external UI library**: All components are custom-built

---

## Architecture

```
gui/
  app.go                    # Wails app definition
  frontend/
    src/
      App.tsx               # Root component, navigation, data polling
      main.tsx              # React entry point
      types/index.ts        # TypeScript interfaces (mirrors Go structs)
      components/
        BudgetGauge.tsx     # Progress bar with color thresholds
        StatusBadge.tsx     # Status indicator with colored dot
      hooks/
        useAxiomEvents.ts   # Wails event subscription + backend RPC
        useEvents.ts        # Alternative event hook
      views/
        ProjectOverview.tsx # Overview dashboard
        TaskTree.tsx        # Hierarchical task visualization
        ActiveContainers.tsx # Container monitoring
        CostDashboard.tsx   # Budget tracking
        FileDiffViewer.tsx  # File diff display
        LogStream.tsx       # Real-time event log
        Timeline.tsx        # Chronological events
        ModelRegistry.tsx   # Model catalog
        ResourceMonitor.tsx # System resources
```

The frontend communicates with the Go backend through Wails-generated bindings. All 16 backend methods (getStatus, getTasks, getContainers, getCosts, getEvents, getModels, newProject, approveSRS, rejectSRS, approveECO, rejectECO, pause, resume, cancel, setBudget, bitNetStart, bitNetStop, tunnelStart, tunnelStop) are available as typed TypeScript functions.
