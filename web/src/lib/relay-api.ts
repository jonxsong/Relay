import { type ZodType } from "zod";

export class RelayAPIError extends Error {
  constructor(message: string, readonly status: number, readonly code?: string) {
    super(message);
  }
}

export async function relayGet<T>(path: string, schema: ZodType<T>, signal?: AbortSignal): Promise<T> {
  const response = await fetch(`/api/relay${path}`, {
    cache: "no-store",
    headers: { Accept: "application/json" },
    signal,
  });
  if (!response.ok) {
    const problem = await response.json().catch(() => null) as { error?: { code?: string; message?: string } } | null;
    throw new RelayAPIError(problem?.error?.message ?? `Relay API returned ${response.status}`, response.status, problem?.error?.code);
  }
  return schema.parse(await response.json());
}
