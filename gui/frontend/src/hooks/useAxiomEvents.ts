// Custom hook that subscribes to Wails events from the Axiom engine.
// Uses @wailsio/runtime EventsOn() to receive real-time engine events.
// The engine emits events via runtime.EventsEmit(ctx, "axiom:event", event)
// and this hook captures them for the React UI.
// See Architecture Section 26.4.

import { useState, useEffect, useCallback, useRef } from "react";
import type { AxiomEvent } from "../types";

// Wails v2 runtime event subscription functions.
// These are provided by @wailsio/runtime when running inside a Wails app.
// During development outside of Wails, they are stubbed below.
let EventsOn: (eventName: string, callback: (...data: unknown[]) => void) => () => void;
let EventsOff: (eventName: string) => void;

try {
  // eslint-disable-next-line @typescript-eslint/no-var-requires
  const runtime = await import("@wailsio/runtime");
  EventsOn = runtime.EventsOn;
  EventsOff = runtime.EventsOff;
} catch {
  // Stub for development outside Wails.
  EventsOn = (_eventName: string, _callback: (...data: unknown[]) => void) => {
    return () => {};
  };
  EventsOff = (_eventName: string) => {};
}

const AXIOM_EVENT_CHANNEL = "axiom:event";

/**
 * useAxiomEvents subscribes to the Wails "axiom:event" channel
 * and returns a stream of AxiomEvent objects, newest first.
 *
 * @param maxEvents - maximum number of events to retain (default 500)
 */
export function useAxiomEvents(maxEvents = 500) {
  const [events, setEvents] = useState<AxiomEvent[]>([]);
  const maxRef = useRef(maxEvents);
  maxRef.current = maxEvents;

  useEffect(() => {
    const unsubscribe = EventsOn(AXIOM_EVENT_CHANNEL, (...data: unknown[]) => {
      const event = data[0] as AxiomEvent;
      if (!event || !event.type) return;
      setEvents((prev) => {
        const updated = [event, ...prev];
        if (updated.length > maxRef.current) {
          return updated.slice(0, maxRef.current);
        }
        return updated;
      });
    });

    return () => {
      if (unsubscribe) unsubscribe();
      EventsOff(AXIOM_EVENT_CHANNEL);
    };
  }, []);

  const clear = useCallback(() => setEvents([]), []);

  return { events, clear };
}

/**
 * useWailsBackend provides typed wrappers around Wails Go backend calls.
 * In a Wails app, window.go.main.App exposes all bound Go methods.
 * Outside of Wails, these return stub data.
 */
export function useWailsBackend() {
  // The Wails-generated bindings live at window.go.main.App.
  // Each method corresponds to a bound Go method on gui.App.
  const app = (window as Record<string, unknown>).go
    ? ((window as Record<string, unknown>).go as Record<string, Record<string, Record<string, (...args: unknown[]) => Promise<unknown>>>>)
        .main?.App
    : null;

  const callBackend = useCallback(
    async <T>(method: string, ...args: unknown[]): Promise<T | null> => {
      if (app && typeof app[method] === "function") {
        return (await app[method](...args)) as T;
      }
      return null;
    },
    [app]
  );

  return {
    getStatus: () => callBackend("GetStatus"),
    getTasks: () => callBackend("GetTasks"),
    getCosts: () => callBackend("GetCosts"),
    getContainers: () => callBackend("GetContainers"),
    getEvents: (limit: number) => callBackend("GetEvents", limit),
    getModels: () => callBackend("GetModels"),
    newProject: (prompt: string, budget: number) => callBackend<string>("NewProject", prompt, budget),
    approveSRS: () => callBackend("ApproveSRS"),
    rejectSRS: (feedback: string) => callBackend("RejectSRS", feedback),
    approveECO: (ecoID: number) => callBackend("ApproveECO", ecoID),
    rejectECO: (ecoID: number) => callBackend("RejectECO", ecoID),
    pause: () => callBackend("Pause"),
    resume: () => callBackend("Resume"),
    cancel: () => callBackend("Cancel"),
    setBudget: (amount: number) => callBackend("SetBudget", amount),
    bitNetStart: () => callBackend("BitNetStart"),
    bitNetStop: () => callBackend("BitNetStop"),
    tunnelStart: () => callBackend<string>("TunnelStart"),
    tunnelStop: () => callBackend("TunnelStop"),
  };
}
