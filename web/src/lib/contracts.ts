import { z } from "zod";

export const workflowSchema = z.object({
  id: z.uuid(),
  name: z.string(),
  description: z.string(),
  created_at: z.iso.datetime(),
  updated_at: z.iso.datetime(),
  version: z.object({
    id: z.uuid(),
    workflow_id: z.uuid(),
    version: z.number().int(),
    created_at: z.iso.datetime(),
    definition: z.object({
      name: z.string().optional(),
      nodes: z.array(z.object({
        id: z.string(),
        type: z.enum(["llm", "tool", "transform", "join"]),
        depends_on: z.array(z.string()).optional(),
        inputs: z.record(z.string(), z.unknown()).optional(),
      })),
    }).optional(),
  }).optional(),
});

export const runSchema = z.object({
  id: z.uuid(),
  workflow_version_id: z.uuid(),
  status: z.enum(["pending", "running", "succeeded", "failed", "cancelled"]),
  input: z.unknown(),
  output: z.unknown().optional(),
  error_code: z.string().nullable().optional(),
  error_message: z.string().nullable().optional(),
  created_at: z.iso.datetime(),
  started_at: z.iso.datetime().nullable().optional(),
  completed_at: z.iso.datetime().nullable().optional(),
});

export const nodeRunSchema = z.object({
  id: z.uuid(),
  node_key: z.string(),
  node_type: z.enum(["llm", "tool", "transform", "join"]),
  status: z.string(),
  current_attempt_no: z.number().int(),
  output: z.unknown().optional(),
  error_code: z.string().nullable().optional(),
  error_message: z.string().nullable().optional(),
  depends_on: z.array(z.string()),
});

export const nodeAttemptSchema = z.object({
  id: z.uuid(),
  node_run_id: z.uuid(),
  attempt_no: z.number().int().positive(),
  status: z.enum(["queued", "running", "succeeded", "failed", "cancelled", "expired"]),
  worker_id: z.string().nullable().optional(),
  fencing_token: z.number().int().positive(),
  lease_expires_at: z.iso.datetime().nullable().optional(),
  last_heartbeat_at: z.iso.datetime().nullable().optional(),
  input: z.unknown(),
  output: z.unknown().optional(),
  error_code: z.string().nullable().optional(),
  error_message: z.string().nullable().optional(),
  retryable: z.boolean().nullable().optional(),
  queued_at: z.iso.datetime(),
  started_at: z.iso.datetime().nullable().optional(),
  completed_at: z.iso.datetime().nullable().optional(),
});

export const eventSchema = z.object({
  id: z.uuid(),
  sequence: z.number().int(),
  event_type: z.string(),
  node_run_id: z.uuid().nullable().optional(),
  attempt_id: z.uuid().nullable().optional(),
  payload: z.unknown(),
  occurred_at: z.iso.datetime(),
});

export const usageSchema = z.object({
  items: z.array(z.object({
    id: z.uuid(), provider: z.string(), model: z.string(),
    input_tokens: z.number().int().nullable().optional(),
    output_tokens: z.number().int().nullable().optional(),
    cost_microusd: z.number().int().nullable().optional(),
    recorded_at: z.iso.datetime(),
  })),
  totals: z.object({
    input_tokens: z.number().int(),
    output_tokens: z.number().int(),
    cost_microusd: z.number().int().nullable(),
  }),
});

export const workflowListSchema = z.object({ items: z.array(workflowSchema) });
export const runListSchema = z.object({ items: z.array(runSchema) });
export const nodeListSchema = z.object({ items: z.array(nodeRunSchema) });
export const nodeAttemptListSchema = z.object({ items: z.array(nodeAttemptSchema) });
export const eventListSchema = z.object({ items: z.array(eventSchema) });

export type Workflow = z.infer<typeof workflowSchema>;
export type Run = z.infer<typeof runSchema>;
export type NodeRun = z.infer<typeof nodeRunSchema>;
export type NodeAttempt = z.infer<typeof nodeAttemptSchema>;
export type ExecutionEvent = z.infer<typeof eventSchema>;
export type Usage = z.infer<typeof usageSchema>;
