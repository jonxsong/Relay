import { type ZodType } from "zod";

export class RelayAPIError extends Error {
  constructor(message: string, readonly status: number, readonly code?: string) {
    super(message);
  }
}

async function relayError(response: Response) {
  const problem = await response.json().catch(() => null) as { error?: { code?: string; message?: string } } | null;
  if (response.status === 401 && typeof window !== "undefined") {
    window.dispatchEvent(new Event("relay:unauthorized"));
  }
  return new RelayAPIError(problem?.error?.message ?? `Relay API returned ${response.status}`, response.status, problem?.error?.code);
}

export async function relayGet<T>(path: string, schema: ZodType<T>, signal?: AbortSignal): Promise<T> {
  const response = await fetch(`/api/relay${path}`, {
    cache: "no-store",
    headers: { Accept: "application/json" },
    signal,
  });
  if (!response.ok) {
    throw await relayError(response);
  }
  return schema.parse(await response.json());
}

export async function relayPost<T>(
  path: string,
  body: unknown,
  schema: ZodType<T>,
  headers: Record<string, string> = {},
): Promise<T> {
  const response = await fetch(`/api/relay${path}`, {
    method: "POST",
    cache: "no-store",
    headers: { Accept: "application/json", "Content-Type": "application/json", ...headers },
    body: JSON.stringify(body),
  });
  if (!response.ok) {
    throw await relayError(response);
  }
  return schema.parse(await response.json());
}
