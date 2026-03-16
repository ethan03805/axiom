// TypeScript types matching the Go backend data structures.
// See Architecture.md Section 26.2 for the view data requirements.

export interface Task {
  id: string;
  parent_id: string;
  title: string;
  description: string;
  status: TaskStatus;
  tier: string;
  task_type: string;
  base_snapshot: string;
  created_at: string;
  completed_at: string | null;
}

export type TaskStatus =
  | "queued"
  | "in_progress"
  | "in_review"
  | "done"
  | "failed"
  | "blocked"
  | "waiting_on_lock"
  | "cancelled_eco";

export interface Container {
  id: string;
  task_id: string;
  container_type: string;
  image: string;
  model_id: string;
  cpu_limit: number;
  mem_limit: string;
  started_at: string;
}

export interface CostReport {
  project_total_usd: number;
  by_task: Record<string, number>;
  by_model: Record<string, number>;
  by_agent_type: Record<string, number>;
  budget_max_usd: number;
  budget_used_pct: number;
  remaining_usd: number;
  projected_total_usd: number;
  external_mode: boolean;
  disclaimer?: string;
}

export interface AxiomEvent {
  type: string;
  task_id: string;
  agent_type: string;
  agent_id: string;
  details: Record<string, unknown>;
  timestamp: string;
}

export interface ModelInfo {
  id: string;
  family: string;
  source: string;
  tier: string;
  context_window: number;
  max_output: number;
  prompt_per_million: number;
  completion_per_million: number;
  strengths: string[];
  weaknesses: string[];
  historical_success_rate: number | null;
}

export interface ProjectStatus {
  name: string;
  slug: string;
  phase: string;
  progress_pct: number;
  elapsed_time: string;
  budget_used: number;
  budget_max: number;
  active_meeseeks: number;
  total_tasks: number;
  done_tasks: number;
}

export interface ECO {
  id: number;
  eco_code: string;
  category: string;
  description: string;
  status: string;
  created_at: string;
}
