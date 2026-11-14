import createClient from "openapi-fetch";

import type { paths } from "@/lib/openapi.generated";

// Compile-time request and response types come directly from docs/openapi.yaml.
// Runtime payloads are still parsed with Zod at the application boundary.
export const relayClient = createClient<paths>({ baseUrl: "/api/relay" });
