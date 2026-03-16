// Log Stream view: real-time scrolling event log.
// See Architecture Section 26.2.
import React from "react";
import type { AxiomEvent } from "../types";

interface Props {
  events: AxiomEvent[];
  onClear: () => void;
}

export const LogStream: React.FC<Props> = ({ events, onClear }) => {
  return (
    <div className="view log-stream">
      <div className="log-header">
        <h2>Event Log ({events.length})</h2>
        <button onClick={onClear}>Clear</button>
      </div>
      <div className="log-entries">
        {events.length === 0 ? (
          <p>No events yet.</p>
        ) : (
          events.map((event, i) => (
            <div key={i} className={`log-entry log-${event.type}`}>
              <span className="log-time">{new Date(event.timestamp).toLocaleTimeString()}</span>
              <span className="log-type">[{event.type}]</span>
              {event.task_id && <span className="log-task">{event.task_id}</span>}
              {event.agent_type && <span className="log-agent">{event.agent_type}</span>}
              <span className="log-details">{JSON.stringify(event.details)}</span>
            </div>
          ))
        )}
      </div>
    </div>
  );
};
