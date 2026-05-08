export type PlanStatus = 'plan' | 'running' | 'done';

export interface Plan {
  id: string;
  title: string;
  content: string;
  status: PlanStatus;
  tags: string;
  creator: string;
  created_at: string;
  updated_at: string;
  todo_items?: TodoItemWithDeps[];
}

export interface TodoItemWithDeps {
  id: string;
  plan_id: string;
  content: string;
  completed: boolean;
  sort_order: number;
  created_at: string;
  depends_on: string[];
  blockers: string[];
}

export interface PlanListResponse {
  plans: Plan[];
  total: number;
}
