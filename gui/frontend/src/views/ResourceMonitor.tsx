// Resource Monitor view: system CPU/memory, container resources, BitNet load.
// See Architecture Section 26.2.
import React from "react";

interface ResourceData {
  system_cpus: number;
  container_cpu_used: number;
  bitnet_threads: number;
  bitnet_active_requests: number;
  bitnet_running: boolean;
  total_memory_gb: number;
  containers_active: number;
  overloaded: boolean;
}

interface Props {
  resources: ResourceData | null;
}

export const ResourceMonitor: React.FC<Props> = ({ resources }) => {
  if (!resources) return <div className="view">Loading resource data...</div>;

  const cpuUsed = resources.container_cpu_used + resources.bitnet_threads;
  const cpuPct = resources.system_cpus > 0 ? (cpuUsed / resources.system_cpus) * 100 : 0;

  return (
    <div className="view resource-monitor">
      <h2>Resource Monitor</h2>
      {resources.overloaded && (
        <div className="warning">System overloaded: combined CPU usage exceeds 90%</div>
      )}
      <div className="stats-grid">
        <div className="stat">
          <label>System CPUs</label>
          <span>{resources.system_cpus}</span>
        </div>
        <div className="stat">
          <label>CPU Usage</label>
          <span>{cpuUsed} / {resources.system_cpus} ({cpuPct.toFixed(0)}%)</span>
        </div>
        <div className="stat">
          <label>Active Containers</label>
          <span>{resources.containers_active}</span>
        </div>
        <div className="stat">
          <label>BitNet Server</label>
          <span>{resources.bitnet_running ? `Running (${resources.bitnet_threads} threads)` : "Stopped"}</span>
        </div>
        <div className="stat">
          <label>BitNet Active Requests</label>
          <span>{resources.bitnet_active_requests}</span>
        </div>
      </div>
    </div>
  );
};
