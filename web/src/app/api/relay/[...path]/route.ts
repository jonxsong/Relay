import type { NextRequest } from "next/server";

const safeSegment = /^[a-zA-Z0-9_-]+$/;
const requestHeaders = ["accept", "authorization", "content-type", "idempotency-key", "last-event-id"];
const responseHeaders = ["content-type", "cache-control", "location", "idempotency-replayed", "x-request-id"];

async function proxy(request: NextRequest, context: { params: Promise<{ path: string[] }> }) {
  const { path } = await context.params;
  if (path.length === 0 || path.some((segment) => !safeSegment.test(segment))) {
    return Response.json({ error: { code: "invalid_proxy_path", message: "Invalid Relay API path" } }, { status: 400 });
  }
  const base = process.env.RELAY_API_URL ?? "http://127.0.0.1:8080/api/v1";
  const trailingSlash = request.nextUrl.pathname.endsWith("/") ? "/" : "";
  const upstreamURL = new URL(`${base.replace(/\/$/, "")}/${path.join("/")}${trailingSlash}`);
  upstreamURL.search = request.nextUrl.search;
  const headers = new Headers();
  for (const name of requestHeaders) {
    const value = request.headers.get(name);
    if (value) headers.set(name, value);
  }
  try {
    const upstream = await fetch(upstreamURL, {
      method: request.method,
      headers,
      body: request.method === "GET" || request.method === "HEAD" ? undefined : request.body,
      cache: "no-store",
      redirect: "manual",
      duplex: "half",
    } as RequestInit & { duplex: "half" });
    const outgoing = new Headers();
    for (const name of responseHeaders) {
      const value = upstream.headers.get(name);
      if (value) outgoing.set(name, value);
    }
    return new Response(upstream.body, { status: upstream.status, headers: outgoing });
  } catch {
    return Response.json({ error: { code: "relay_unavailable", message: "The Relay backend is unavailable" } }, { status: 503 });
  }
}

export const GET = proxy;
export const POST = proxy;
