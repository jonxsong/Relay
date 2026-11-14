import { NextRequest, NextResponse } from "next/server";

import { relaySessionCookie, sealAPIKey } from "@/lib/session";

const sessionTTL = 8 * 60 * 60;

function sameOrigin(request: NextRequest) {
  const origin = request.headers.get("origin");
  if (!origin) return true;
  try {
    const originURL = new URL(origin);
    const forwardedHost = request.headers.get("x-forwarded-host")?.split(",", 1)[0]?.trim();
    const requestHost = forwardedHost || request.headers.get("host") || request.nextUrl.host;
    const forwardedProtocol = request.headers.get("x-forwarded-proto")?.split(",", 1)[0]?.trim();
    const requestProtocol = forwardedProtocol || request.nextUrl.protocol.replace(":", "");
    return originURL.host === requestHost && originURL.protocol === `${requestProtocol}:`;
  } catch {
    return false;
  }
}

export async function POST(request: NextRequest) {
  if (!sameOrigin(request)) {
    return Response.json({ error: { code: "invalid_origin", message: "Cross-origin session requests are not allowed" } }, { status: 403 });
  }
  const body = await request.json().catch(() => null) as { apiKey?: unknown } | null;
  const apiKey = typeof body?.apiKey === "string" ? body.apiKey.trim() : "";
  if (apiKey.length < 16 || apiKey.length > 512) {
    return Response.json({ error: { code: "invalid_api_key", message: "Enter a valid Relay API key" } }, { status: 400 });
  }

  const base = process.env.RELAY_API_URL ?? "http://127.0.0.1:8080/api/v1";
  let upstream: Response;
  try {
    upstream = await fetch(`${base.replace(/\/$/, "")}/workflows/?page_size=1`, {
      cache: "no-store",
      headers: { Accept: "application/json", Authorization: `Bearer ${apiKey}` },
    });
  } catch {
    return Response.json({ error: { code: "relay_unavailable", message: "The Relay backend is unavailable" } }, { status: 503 });
  }
  if (upstream.status === 401 || upstream.status === 403) {
    return Response.json({ error: { code: "invalid_api_key", message: "Relay rejected this API key" } }, { status: 401 });
  }
  if (!upstream.ok) {
    return Response.json({ error: { code: "relay_unavailable", message: "Relay could not validate this API key" } }, { status: 502 });
  }

  let sealed: string;
  try {
    sealed = await sealAPIKey(apiKey);
  } catch {
    return Response.json({ error: { code: "session_unavailable", message: "Secure console sessions are not configured" } }, { status: 503 });
  }
  const response = NextResponse.json({ authenticated: true });
  response.cookies.set(relaySessionCookie, sealed, {
    httpOnly: true,
    secure: process.env.NODE_ENV === "production",
    sameSite: "strict",
    path: "/",
    maxAge: sessionTTL,
  });
  return response;
}

export function DELETE(request: NextRequest) {
  if (!sameOrigin(request)) {
    return Response.json({ error: { code: "invalid_origin", message: "Cross-origin session requests are not allowed" } }, { status: 403 });
  }
  const response = NextResponse.json({ authenticated: false });
  response.cookies.set(relaySessionCookie, "", {
    httpOnly: true,
    secure: process.env.NODE_ENV === "production",
    sameSite: "strict",
    path: "/",
    maxAge: 0,
  });
  return response;
}
