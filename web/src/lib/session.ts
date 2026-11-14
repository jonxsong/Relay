import "server-only";

const encoder = new TextEncoder();
export const relaySessionCookie = "relay_session";

function sessionSecret() {
  const value = process.env.RELAY_SESSION_SECRET;
  if (!value || value.length < 32) {
    throw new Error("RELAY_SESSION_SECRET must contain at least 32 characters");
  }
  return value;
}

async function sessionKey() {
  const digest = await crypto.subtle.digest("SHA-256", encoder.encode(sessionSecret()));
  return crypto.subtle.importKey("raw", digest, "AES-GCM", false, ["encrypt", "decrypt"]);
}

function encode(value: Uint8Array) {
  return Buffer.from(value).toString("base64url");
}

function decode(value: string) {
  return new Uint8Array(Buffer.from(value, "base64url"));
}

export async function sealAPIKey(apiKey: string) {
  const iv = crypto.getRandomValues(new Uint8Array(12));
  const encrypted = new Uint8Array(await crypto.subtle.encrypt(
    { name: "AES-GCM", iv },
    await sessionKey(),
    encoder.encode(apiKey),
  ));
  return `${encode(iv)}.${encode(encrypted)}`;
}

export async function openAPIKey(value: string) {
  const [encodedIV, encodedCiphertext, extra] = value.split(".");
  if (!encodedIV || !encodedCiphertext || extra) return null;
  try {
    const decrypted = await crypto.subtle.decrypt(
      { name: "AES-GCM", iv: decode(encodedIV) },
      await sessionKey(),
      decode(encodedCiphertext),
    );
    return new TextDecoder().decode(decrypted);
  } catch {
    return null;
  }
}
