// Custom hook for subscribing to real-time Axiom engine events.
// Events are delivered via the Wails event system, not SQLite polling.
// See Architecture Section 26.4.

import { useState, useEffect, useCallback } from "react";
import type { AxiomEvent } from "../types";

// In a real Wails app, this would use:
// import { EventsOn, EventsOff } from "@wailsapp/runtime";

export function useEvents(maxEvents = 100) {
  const [events, setEvents] = useState<AxiomEvent[]>([]);

  useEffect(() => {
    // Subscribe to axiom:event from the Wails runtime.
    // runtime.EventsOn("axiom:event", (event: AxiomEvent) => { ... });
    const handler = (event: AxiomEvent) => {
      setEvents((prev) => {
        const updated = [event, ...prev];
        return updated.slice(0, maxEvents);
      });
    };

    // Placeholder: in Wails, this registers with EventsOn.
    // EventsOn("axiom:event", handler);
    void handler;

    return () => {
      // EventsOff("axiom:event");
    };
  }, [maxEvents]);

  const clear = useCallback(() => setEvents([]), []);

  return { events, clear };
}

export function usePolledData<T>(
  fetcher: () => Promise<T>,
  intervalMs = 1000
) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let mounted = true;
    const poll = async () => {
      try {
        const result = await fetcher();
        if (mounted) {
          setData(result);
          setError(null);
        }
      } catch (e) {
        if (mounted) setError(String(e));
      }
    };

    poll();
    const id = setInterval(poll, intervalMs);
    return () => {
      mounted = false;
      clearInterval(id);
    };
  }, [fetcher, intervalMs]);

  return { data, error };
}
