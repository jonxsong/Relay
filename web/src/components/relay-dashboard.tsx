"use client";

import {
  Activity, BarChart3, Blocks, Bot, Boxes, Braces, ChevronDown,
  CircleDollarSign, Clock3, Database, GitBranch, LayoutDashboard,
  ListFilter, Play, RefreshCw, RotateCcw, Search, Settings,
  ShieldCheck, Workflow as WorkflowIcon, Wrench, Zap,
} from "lucide-react";
import {
  Background, Controls, Handle, MiniMap, Position, ReactFlow,
  type Node, type NodeProps, type NodeTypes,
} from "@xyflow/react";
import { useCallback, useEffect, useMemo, useState } from "react";

import {
  eventListSchema, type ExecutionEvent, nodeListSchema, type NodeRun,
  type Run, runListSchema, type Usage, usageSchema, type Workflow,
  workflowListSchema,
} from "@/lib/contracts";
import { relayGet } from "@/lib/relay-api";

type ConnectionState = "loading" | "live" | "unavailable";
type ExecutionNodeData = { node: NodeRun };
type ExecutionNode = Node<ExecutionNodeData, "execution">;

const nav = [
  [LayoutDashboard, "Overview"], [WorkflowIcon, "Workflows"],
  [Play, "Runs"], [Activity, "Activity"], [BarChart3, "Usage"],
  [Blocks, "Connections"],
] as const;

const statusColor: Record<string, string> = {
  pending: "#7c859b", ready: "#a78bfa", queued: "#60a5fa",
  running: "#22d3ee", succeeded: "#4ade80", failed: "#fb7185",
  cancelled: "#fbbf24", skipped: "#64748b",
};

function ExecutionNodeCard({ data }: NodeProps<ExecutionNode>) {
  const icon = data.node.node_type === "llm" ? <Bot size={15} /> : data.node.node_type === "tool" ? <Wrench size={15} /> : <Braces size={15} />;
  return (
    <div className={`execution-node status-${data.node.status}`}>
      <Handle type="target" position={Position.Left} className="flow-handle" />
      <div className="execution-node-icon">{icon}</div>
      <div><strong>{data.node.node_key}</strong><span>{data.node.node_type} · attempt {Math.max(1, data.node.current_attempt_no)}</span></div>
      <i style={{ background: statusColor[data.node.status] ?? statusColor.pending }} />
      <Handle type="source" position={Position.Right} className="flow-handle" />
    </div>
  );
}

const nodeTypes: NodeTypes = { execution: ExecutionNodeCard };

function formatDuration(run: Run) {
  if (!run.started_at) return "—";
  const end = run.completed_at ? Date.parse(run.completed_at) : Date.now();
  const seconds = Math.max(0, Math.round((end - Date.parse(run.started_at)) / 1000));
  if (seconds < 60) return `${seconds}s`;
  return `${Math.floor(seconds / 60)}m ${seconds % 60}s`;
}

function compactNumber(value: number) {
  return new Intl.NumberFormat("en", { notation: "compact", maximumFractionDigits: 1 }).format(value);
}

function Metric({ value, label, Icon, tone }: { value: string; label: string; Icon: typeof Activity; tone: string }) {
  return <article className="metric-card"><div><span>{label}</span><Icon size={17} style={{ color: tone }} /></div><strong style={{ color: tone }}>{value}</strong></article>;
}

export function RelayDashboard() {
  const [workflows, setWorkflows] = useState<Workflow[]>([]);
  const [runs, setRuns] = useState<Run[]>([]);
  const [selectedRunID, setSelectedRunID] = useState<string | null>(null);
  const [nodes, setNodes] = useState<NodeRun[]>([]);
  const [events, setEvents] = useState<ExecutionEvent[]>([]);
  const [usage, setUsage] = useState<Usage | null>(null);
  const [connection, setConnection] = useState<ConnectionState>("loading");
  const [error, setError] = useState<string | null>(null);
  const [updatedAt, setUpdatedAt] = useState<Date | null>(null);

  const refresh = useCallback(async (signal?: AbortSignal) => {
    setConnection("loading");
    setError(null);
    try {
      const [workflowData, runData] = await Promise.all([
        relayGet("/workflows?page_size=100", workflowListSchema, signal),
        relayGet("/runs?page_size=100", runListSchema, signal),
      ]);
      setWorkflows(workflowData.items);
      setRuns(runData.items);
      if (runData.items.length === 0) {
        setNodes([]);
        setEvents([]);
        setUsage(null);
      }
      setSelectedRunID((current) => current && runData.items.some((run) => run.id === current) ? current : runData.items[0]?.id ?? null);
      setConnection("live");
      setUpdatedAt(new Date());
    } catch (reason) {
      if (signal?.aborted) return;
      setConnection("unavailable");
      setError(reason instanceof Error ? reason.message : "Unable to reach Relay");
    }
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    const load = () => void refresh(controller.signal);
    const initial = window.setTimeout(load, 0);
    const timer = window.setInterval(load, 10_000);
    return () => { controller.abort(); window.clearTimeout(initial); window.clearInterval(timer); };
  }, [refresh]);

  useEffect(() => {
    if (!selectedRunID || connection !== "live") {
      return;
    }
    const controller = new AbortController();
    void Promise.all([
      relayGet(`/runs/${selectedRunID}/nodes`, nodeListSchema, controller.signal),
      relayGet(`/runs/${selectedRunID}/events?after=0`, eventListSchema, controller.signal),
      relayGet(`/runs/${selectedRunID}/usage`, usageSchema, controller.signal),
    ]).then(([nodeData, eventData, usageData]) => {
      setNodes(nodeData.items); setEvents(eventData.items); setUsage(usageData);
    }).catch((reason) => {
      if (!controller.signal.aborted) setError(reason instanceof Error ? reason.message : "Run detail unavailable");
    });
    return () => controller.abort();
  }, [selectedRunID, connection]);

  const selectedRun = runs.find((run) => run.id === selectedRunID) ?? null;
  const terminalRuns = runs.filter((run) => ["succeeded", "failed", "cancelled"].includes(run.status));
  const successRate = terminalRuns.length === 0 ? "—" : `${((terminalRuns.filter((run) => run.status === "succeeded").length / terminalRuns.length) * 100).toFixed(1)}%`;
  const retries = nodes.reduce((total, node) => total + Math.max(0, node.current_attempt_no - 1), 0);
  const tokenTotal = (usage?.totals.input_tokens ?? 0) + (usage?.totals.output_tokens ?? 0);
  const knownCost = usage?.totals.cost_microusd;
  const flowNodes = useMemo<ExecutionNode[]>(() => nodes.map((node, index) => ({
    id: node.id, type: "execution", data: { node },
    position: { x: 64 + (index % 4) * 248, y: 72 + Math.floor(index / 4) * 132 },
  })), [nodes]);

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="product-lockup"><div className="product-mark"><Zap size={22} /></div><div><strong>Relay</strong><span>Agent Execution Engine</span></div></div>
        <nav aria-label="Primary navigation">{nav.map(([Icon, label], index) => <button className={index === 0 ? "active" : ""} key={label}><Icon size={17} /><span>{label}</span>{label === "Runs" && runs.length > 0 ? <b>{runs.length}</b> : null}</button>)}</nav>
        <div className="sidebar-section"><span>RUNTIME</span><button><Database size={17} /><span>PostgreSQL</span><i className={connection === "live" ? "dot-live" : "dot-offline"} /></button><button><ShieldCheck size={17} /><span>Security</span></button><button><Settings size={17} /><span>Settings</span></button></div>
        <div className="sidebar-status"><div><i className={connection === "live" ? "dot-live" : "dot-offline"} /><strong>{connection === "live" ? "Connected" : connection === "loading" ? "Connecting" : "Offline"}</strong></div><span>{updatedAt ? `Updated ${updatedAt.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}` : "Waiting for backend"}</span></div>
      </aside>

      <main className="workspace">
        <header className="topbar">
          <div><div className="title-row"><h1>Execution overview</h1><span className={`runtime-pill ${connection}`}><i />{connection === "live" ? "Live" : connection === "loading" ? "Connecting" : "Unavailable"}</span></div><p>Durable workflow orchestration and run intelligence</p></div>
          <div className="topbar-actions"><label className="search-box"><Search size={15} /><input aria-label="Search runs" placeholder="Search runs" /></label><button className="icon-button" onClick={() => void refresh()} aria-label="Refresh"><RefreshCw size={17} /></button><button className="primary-action"><Play size={15} fill="currentColor" />New run</button></div>
        </header>

        {error ? <div className="system-banner"><span><Database size={16} />{error}</span><button onClick={() => void refresh()}>Retry connection</button></div> : null}

        <section className="metrics" aria-label="Execution metrics">
          <Metric value={String(runs.filter((run) => ["pending", "running"].includes(run.status)).length)} label="Active runs" Icon={Activity} tone="#a78bfa" />
          <Metric value={successRate} label="Success rate" Icon={ShieldCheck} tone="#4ade80" />
          <Metric value={String(retries)} label="Retries · selected run" Icon={RotateCcw} tone="#60a5fa" />
          <Metric value={tokenTotal === 0 ? "—" : compactNumber(tokenTotal)} label="Tokens · selected run" Icon={Braces} tone="#22d3ee" />
          <Metric value={knownCost == null ? "Unknown" : `$${(knownCost / 1_000_000).toFixed(4)}`} label="Cost · selected run" Icon={CircleDollarSign} tone="#f0abfc" />
          <Metric value={selectedRun ? formatDuration(selectedRun) : "—"} label="Duration · selected run" Icon={Clock3} tone="#fbbf24" />
        </section>

        <section className="graph-section">
          <div className="section-bar"><div><span className="section-index">01</span><div><h2>Live execution graph</h2><p>{selectedRun ? `Run ${selectedRun.id.slice(0, 8)} · ${selectedRun.status}` : "Select a run to inspect its durable node state"}</p></div></div><div className="section-actions"><button><ListFilter size={15} />All nodes</button><button><GitBranch size={15} />Auto layout</button></div></div>
          <div className="graph-card">
            {flowNodes.length > 0 ? <ReactFlow nodes={flowNodes} edges={[]} nodeTypes={nodeTypes} fitView minZoom={0.45} maxZoom={1.8} nodesDraggable={false} nodesConnectable={false} proOptions={{ hideAttribution: true }}><Background color="#34354b" gap={24} size={1} /><Controls showInteractive={false} /><MiniMap nodeColor={(node) => statusColor[(node.data as ExecutionNodeData).node.status] ?? statusColor.pending} maskColor="rgba(8,9,15,.72)" /></ReactFlow> : <div className="graph-empty"><div><WorkflowIcon size={28} /></div><h3>{connection === "live" ? "No execution nodes yet" : "Connect the Relay backend"}</h3><p>{connection === "live" ? "Create a workflow and submit a run. Its durable node states will appear here in real time." : "Start the API, control plane, worker, PostgreSQL, and NATS stack to populate this console with real execution data."}</p><button className="primary-action"><WorkflowIcon size={15} />Create workflow</button></div>}
            <div className="graph-legend">{["running", "succeeded", "failed", "queued"].map((status) => <span key={status}><i style={{ background: statusColor[status] }} />{status}</span>)}</div>
          </div>
        </section>

        <section className="lower-grid">
          <div className="panel run-panel">
            <div className="panel-heading"><div><span className="section-index">02</span><h2>Recent runs</h2></div><button>View all <ChevronDown size={14} /></button></div>
            <div className="run-table" role="table" aria-label="Recent workflow runs"><div className="run-row run-header" role="row"><span>Run</span><span>Status</span><span>Created</span><span>Duration</span></div>{runs.length === 0 ? <div className="table-empty">No runs have been submitted.</div> : runs.slice(0, 7).map((run) => <button className={`run-row ${run.id === selectedRunID ? "selected" : ""}`} key={run.id} onClick={() => setSelectedRunID(run.id)} role="row"><span><code>{run.id.slice(0, 8)}</code><small>{run.workflow_version_id.slice(0, 8)}</small></span><span><i className={`status-dot ${run.status}`} />{run.status}</span><span>{new Date(run.created_at).toLocaleString([], { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" })}</span><span>{formatDuration(run)}</span></button>)}</div>
          </div>
          <div className="panel event-panel">
            <div className="panel-heading"><div><span className="section-index">03</span><h2>Event stream</h2></div><span className="event-count">{events.length} events</span></div>
            <div className="event-list">{events.length === 0 ? <div className="table-empty">No events for the selected run.</div> : events.slice(-8).reverse().map((event) => <article key={event.id}><i style={{ background: event.event_type.includes("failed") ? statusColor.failed : event.event_type.includes("succeeded") ? statusColor.succeeded : statusColor.running }} /><div><strong>{event.event_type.replaceAll(".", " ")}</strong><span>sequence {event.sequence}</span></div><time>{new Date(event.occurred_at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" })}</time></article>)}</div>
          </div>
        </section>

        <footer className="workspace-footer"><span>{workflows.length} workflows · {runs.length} runs</span><span><Boxes size={13} />PostgreSQL is authoritative · JetStream dispatches</span></footer>
      </main>
    </div>
  );
}
