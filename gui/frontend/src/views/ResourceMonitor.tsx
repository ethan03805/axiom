// Resource Monitor view: system CPU/memory, container resources, BitNet load.
// Displays warnings when system utilization exceeds 70% per Architecture Section 12.5.
// See Architecture Section 26.2.
import React from "react";

interface ResourceData {
  system_cpus: number;
  system_memory_gb: number;
  container_cpu_used: number;
  container_memory_used_gb: number;
  containers_active: number;
  bitnet_running: boolean;
  bitnet_threads: number;
  bitnet_active_requests: number;
  bitnet_memory_gb: number;
  overloaded: boolean;
}

interface Props {
  resources: ResourceData | null;
}

const statGridStyle: React.CSSProperties = {
  display: "grid",
  gridTemplateColumns: "repeat(auto-fill, minmax(200px, 1fr))",
  gap: 12,
  marginBottom: 20,
};

const cardStyle: React.CSSProperties = {
  backgroundColor: "#1e293b",
  borderRadius: 6,
  padding: 14,
  border: "1px solid #334155",
};

const labelStyle: React.CSSProperties = {
  fontSize: 11,
  color: "#94a3b8",
  textTransform: "uppercase",
  letterSpacing: 1,
  marginBottom: 4,
};

const valueStyle: React.CSSProperties = {
  fontSize: 18,
  fontWeight: 600,
  color: "#e2e8f0",
};

const warningStyle: React.CSSProperties = {
  padding: "10px 14px",
  backgroundColor: "#f59e0b22",
  border: "1px solid #f59e0b44",
  borderRadius: 6,
  color: "#f59e0b",
  fontWeight: 600,
  fontSize: 13,
  marginBottom: 16,
};

function barGauge(used: number, total: number, unit: string): React.ReactNode {
  const pct = total > 0 ? (used / total) * 100 : 0;
  let color = "#10b981";
  if (pct >= 90) color = "#ef4444";
  else if (pct >= 70) color = "#f59e0b";

  return (
    <div style={{ marginTop: 6 }}>
      <div
        style={{
          width: "100%",
          height: 8,
          backgroundColor: "#0f172a",
          borderRadius: 4,
          overflow: "hidden",
        }}
      >
        <div
          style={{
            width: `${Math.min(pct, 100)}%`,
            height: "100%",
            backgroundColor: color,
            borderRadius: 4,
            transition: "width 0.3s ease",
          }}
        />
      </div>
      <div style={{ fontSize: 11, color: "#64748b", marginTop: 2 }}>
        {used.toFixed(1)} / {total.toFixed(1)} {unit} ({pct.toFixed(0)}%)
      </div>
    </div>
  );
}

export const ResourceMonitor: React.FC<Props> = ({ resources }) => {
  if (!resources) {
    return <div style={{ color: "#94a3b8" }}>Loading resource data...</div>;
  }

  const totalCpuUsed = resources.container_cpu_used + resources.bitnet_threads;

  return (
    <div>
      <h2 style={{ marginBottom: 16, fontSize: 20 }}>Resource Monitor</h2>

      {resources.overloaded && (
        <div style={warningStyle}>
          System overloaded: combined CPU/memory usage exceeds safe thresholds.
          Consider reducing max_meeseeks or pausing execution.
        </div>
      )}

      <h3 style={{ marginBottom: 8 }}>System</h3>
      <div style={statGridStyle}>
        <div style={cardStyle}>
          <div style={labelStyle}>CPU Cores</div>
          <div style={valueStyle}>{resources.system_cpus}</div>
          {barGauge(totalCpuUsed, resources.system_cpus, "cores")}
        </div>
        <div style={cardStyle}>
          <div style={labelStyle}>System Memory</div>
          <div style={valueStyle}>{resources.system_memory_gb.toFixed(1)} GB</div>
        </div>
      </div>

      <h3 style={{ marginBottom: 8 }}>Containers</h3>
      <div style={statGridStyle}>
        <div style={cardStyle}>
          <div style={labelStyle}>Active Containers</div>
          <div style={valueStyle}>{resources.containers_active}</div>
        </div>
        <div style={cardStyle}>
          <div style={labelStyle}>Container CPU</div>
          <div style={valueStyle}>{resources.container_cpu_used.toFixed(1)} cores</div>
          {barGauge(resources.container_cpu_used, resources.system_cpus, "cores")}
        </div>
        <div style={cardStyle}>
          <div style={labelStyle}>Container Memory</div>
          <div style={valueStyle}>{resources.container_memory_used_gb.toFixed(1)} GB</div>
        </div>
      </div>

      <h3 style={{ marginBottom: 8 }}>BitNet Local Inference</h3>
      <div style={statGridStyle}>
        <div style={cardStyle}>
          <div style={labelStyle}>Server Status</div>
          <div style={valueStyle}>
            {resources.bitnet_running ? (
              <span style={{ color: "#10b981" }}>Running</span>
            ) : (
              <span style={{ color: "#6b7280" }}>Stopped</span>
            )}
          </div>
        </div>
        <div style={cardStyle}>
          <div style={labelStyle}>CPU Threads</div>
          <div style={valueStyle}>{resources.bitnet_threads}</div>
        </div>
        <div style={cardStyle}>
          <div style={labelStyle}>Active Requests</div>
          <div style={valueStyle}>{resources.bitnet_active_requests}</div>
        </div>
        <div style={cardStyle}>
          <div style={labelStyle}>Memory Usage</div>
          <div style={valueStyle}>{resources.bitnet_memory_gb.toFixed(1)} GB</div>
        </div>
      </div>
    </div>
  );
};
