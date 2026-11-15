import type { Metadata } from "next";

import { RelayDashboard } from "@/components/relay-dashboard";

export const metadata: Metadata = {
  title: "Console — Relay",
  description: "Operate durable Relay workflows, runs, events, attempts, and usage.",
};

export default function ConsolePage() {
  return <RelayDashboard />;
}
