import type { Metadata } from "next";
import type { ReactNode } from "react";
import "./globals.css";

export const metadata: Metadata = {
  title: "Relay — Agent Execution Engine",
  description: "Build, run, inspect, and replay durable AI agent workflows.",
  icons: {
    icon: [{ url: "/relay-mark.svg", type: "image/svg+xml" }],
    shortcut: "/relay-mark.svg",
  },
};

export default function RootLayout({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <html lang="en" className="h-full antialiased">
      <body className="min-h-full">{children}</body>
    </html>
  );
}
