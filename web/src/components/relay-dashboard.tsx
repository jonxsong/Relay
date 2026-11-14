"use client";

import {
  Activity, BarChart3, Blocks, Bot, Boxes, Braces, ChevronDown,
  CircleDollarSign, Clock3, Database, GitBranch, LayoutDashboard,
  Ban, ListFilter, Play, Plus, RefreshCw, RotateCcw, Search, Settings,
  ShieldCheck, Workflow as WorkflowIcon, Wrench, X, Zap,
} from "lucide-react";
import {
  Background, Controls, Handle, MiniMap, Position, ReactFlow,
  type Edge, type Node, type NodeProps, type NodeTypes,
} from "@xyflow/react";
import { type FormEvent, useCallback, useEffect, useMemo, useState } from "react";

import {
  eventListSchema, type ExecutionEvent, nodeListSchema, type NodeRun,
  type Run, runListSchema, runSchema, type Usage, usageSchema, type Workflow,
  workflowListSchema, workflowSchema,
} from "@/lib/contracts";
import { relayGet, relayPost } from "@/lib/relay-api";

type ConnectionState = "loading" | "live" | "unavailable";
type DialogKind = "workflow" | "run" | null;
type ConsoleView = "overview" | "workflows" | "runs" | "activity" | "usage" | "connections";
type ExecutionNodeData = { node: NodeRun };
type ExecutionNode = Node<ExecutionNodeData, "execution">;

const nav = [
  ["overview", LayoutDashboard, "Overview"], ["workflows", WorkflowIcon, "Workflows"],
  ["runs", Play, "Runs"], ["activity", Activity, "Activity"], ["usage", BarChart3, "Usage"],
  ["connections", Blocks, "Connections"],
] as const;

const viewMeta: Record<ConsoleView, [string, string]> = {
  overview: ["Execution overview", "Durable workflow orchestration and run intelligence"],
  workflows: ["Workflows", "Immutable DAG definitions and versions"],
  runs: ["Runs", "Inspect durable executions and terminal outcomes"],
  activity: ["Activity", "Replay the ordered event history for a selected run"],
  usage: ["Usage", "Provider tokens and configured model cost"],
  connections: ["Connections", "Runtime boundaries and dependency health"],
};

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
  const [dialog, setDialog] = useState<DialogKind>(null);
  const [workflowName, setWorkflowName] = useState("");
  const [workflowDescription, setWorkflowDescription] = useState("");
  const [workflowDefinition, setWorkflowDefinition] = useState(`{
  "nodes": [
    {
      "id": "respond",
      "type": "llm",
      "inputs": { "prompt": { "from": "$.input.prompt" } }
    }
  ]
}`);
  const [runWorkflowID, setRunWorkflowID] = useState("");
  const [runInput, setRunInput] = useState(`{ "prompt": "Explain why durable agent execution matters." }`);
  const [mutationError, setMutationError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [searchQuery, setSearchQuery] = useState("");
  const [activeView, setActiveView] = useState<ConsoleView>("overview");
  const selectedRun = runs.find((run) => run.id === selectedRunID) ?? null;

  const refresh = useCallback(async (signal?: AbortSignal) => {
    setConnection("loading");
    setError(null);
    try {
      const [workflowData, runData] = await Promise.all([
        relayGet("/workflows/?page_size=100", workflowListSchema, signal),
        relayGet("/runs/?page_size=100", runListSchema, signal),
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

  useEffect(() => {
    if (!selectedRunID || !selectedRun || ["succeeded", "failed", "cancelled"].includes(selectedRun.status)) return;
    const stream = new EventSource(`/api/relay/runs/${selectedRunID}/stream`);
    const eventTypes = ["run.started", "run.succeeded", "run.failed", "run.cancelled", "node.queued", "node.running", "node.succeeded", "node.failed", "node.retry_scheduled", "node.cancelled"];
    const update = () => void refresh();
    for (const eventType of eventTypes) stream.addEventListener(eventType, update);
    stream.onerror = () => stream.close();
    return () => stream.close();
  }, [selectedRunID, selectedRun, refresh]);

  const openRunDialog = () => {
    setRunWorkflowID((current) => current || workflows[0]?.id || "");
    setMutationError(null);
    setDialog("run");
  };

  const submitWorkflow = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setSubmitting(true);
    setMutationError(null);
    try {
      const definition = JSON.parse(workflowDefinition) as unknown;
      await relayPost("/workflows/", { name: workflowName, description: workflowDescription, definition }, workflowSchema);
      setWorkflowName("");
      setWorkflowDescription("");
      setDialog(null);
      await refresh();
    } catch (reason) {
      setMutationError(reason instanceof Error ? reason.message : "Unable to create workflow");
    } finally {
      setSubmitting(false);
    }
  };

  const submitRun = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setSubmitting(true);
    setMutationError(null);
    try {
      const input = JSON.parse(runInput) as unknown;
      const created = await relayPost(`/workflows/${runWorkflowID}/runs`, { input }, runSchema, { "Idempotency-Key": crypto.randomUUID() });
      setDialog(null);
      await refresh();
      setSelectedRunID(created.id);
    } catch (reason) {
      setMutationError(reason instanceof Error ? reason.message : "Unable to submit run");
    } finally {
      setSubmitting(false);
    }
  };

  const cancelSelectedRun = async () => {
    if (!selectedRunID) return;
    setMutationError(null);
    try {
      await relayPost(`/runs/${selectedRunID}/cancel`, {}, runSchema);
      await refresh();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Unable to cancel run");
    }
  };

  const terminalRuns = runs.filter((run) => ["succeeded", "failed", "cancelled"].includes(run.status));
  const successRate = terminalRuns.length === 0 ? "—" : `${((terminalRuns.filter((run) => run.status === "succeeded").length / terminalRuns.length) * 100).toFixed(1)}%`;
  const retries = nodes.reduce((total, node) => total + Math.max(0, node.current_attempt_no - 1), 0);
  const tokenTotal = (usage?.totals.input_tokens ?? 0) + (usage?.totals.output_tokens ?? 0);
  const knownCost = usage?.totals.cost_microusd;
  const visibleRuns = runs.filter((run) => {
    const query = searchQuery.trim().toLowerCase();
    return query === "" || run.id.toLowerCase().includes(query) || run.status.toLowerCase().includes(query) || run.workflow_version_id.toLowerCase().includes(query);
  });
  const flowNodes = useMemo<ExecutionNode[]>(() => nodes.map((node) => ({
    id: node.id,
    type: "execution",
    data: { node },
    position: (() => {
      const byKey = new Map(nodes.map((candidate) => [candidate.node_key, candidate]));
      const memo = new Map<string, number>();
      const depth = (candidate: NodeRun, visiting = new Set<string>()): number => {
        const cached = memo.get(candidate.node_key);
        if (cached !== undefined) return cached;
        if (visiting.has(candidate.node_key)) return 0;
        visiting.add(candidate.node_key);
        const value = candidate.depends_on.length === 0 ? 0 : 1 + Math.max(...candidate.depends_on.map((key) => {
          const dependency = byKey.get(key);
          return dependency ? depth(dependency, visiting) : 0;
        }));
        visiting.delete(candidate.node_key);
        memo.set(candidate.node_key, value);
        return value;
      };
      const column = depth(node);
      const row = nodes.filter((candidate) => depth(candidate) === column).findIndex((candidate) => candidate.id === node.id);
      return { x: 64 + column * 260, y: 72 + Math.max(0, row) * 116 };
    })(),
  })), [nodes]);
  const flowEdges = useMemo<Edge[]>(() => {
    const byKey = new Map(nodes.map((node) => [node.node_key, node.id]));
    return nodes.flatMap((node) => node.depends_on.flatMap((dependency) => {
      const source = byKey.get(dependency);
      return source ? [{
        id: `${source}-${node.id}`,
        source,
        target: node.id,
        animated: node.status === "running",
        style: { stroke: node.status === "succeeded" ? "#4ade80" : "#655bd4", strokeWidth: 1.6 },
      }] : [];
    }));
  }, [nodes]);

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="product-lockup"><div className="product-mark"><Zap size={22} /></div><div><strong>Relay</strong><span>Agent Execution Engine</span></div></div>
        <nav aria-label="Primary navigation">{nav.map(([view, Icon, label]) => <button className={activeView === view ? "active" : ""} key={label} onClick={() => setActiveView(view)}><Icon size={17} /><span>{label}</span>{label === "Runs" && runs.length > 0 ? <b>{runs.length}</b> : null}</button>)}</nav>
        <div className="sidebar-section"><span>RUNTIME</span><button><Database size={17} /><span>PostgreSQL</span><i className={connection === "live" ? "dot-live" : "dot-offline"} /></button><button><ShieldCheck size={17} /><span>Security</span></button><button><Settings size={17} /><span>Settings</span></button></div>
        <div className="sidebar-status"><div><i className={connection === "live" ? "dot-live" : "dot-offline"} /><strong>{connection === "live" ? "Connected" : connection === "loading" ? "Connecting" : "Offline"}</strong></div><span>{updatedAt ? `Updated ${updatedAt.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}` : "Waiting for backend"}</span></div>
      </aside>

      <main className="workspace">
        <header className="topbar">
          <div><div className="title-row"><h1>{viewMeta[activeView][0]}</h1><span className={`runtime-pill ${connection}`}><i />{connection === "live" ? "Live" : connection === "loading" ? "Connecting" : "Unavailable"}</span></div><p>{viewMeta[activeView][1]}</p></div>
          <div className="topbar-actions"><label className="search-box"><Search size={15} /><input aria-label="Search runs" placeholder="Search runs" value={searchQuery} onChange={(event) => setSearchQuery(event.target.value)} /></label><button className="icon-button" onClick={() => void refresh()} aria-label="Refresh"><RefreshCw size={17} /></button><button className="primary-action" onClick={openRunDialog} disabled={connection !== "live" || workflows.length === 0}><Play size={15} fill="currentColor" />New run</button></div>
        </header>

        {error ? <div className="system-banner"><span><Database size={16} />{error}</span><button onClick={() => void refresh()}>Retry connection</button></div> : null}

        <div hidden={activeView !== "overview"}>
        <section className="metrics" aria-label="Execution metrics">
          <Metric value={String(runs.filter((run) => ["pending", "running"].includes(run.status)).length)} label="Active runs" Icon={Activity} tone="#a78bfa" />
          <Metric value={successRate} label="Success rate" Icon={ShieldCheck} tone="#4ade80" />
          <Metric value={String(retries)} label="Retries · selected run" Icon={RotateCcw} tone="#60a5fa" />
          <Metric value={tokenTotal === 0 ? "—" : compactNumber(tokenTotal)} label="Tokens · selected run" Icon={Braces} tone="#22d3ee" />
          <Metric value={knownCost == null ? "Unknown" : `$${(knownCost / 1_000_000).toFixed(4)}`} label="Cost · selected run" Icon={CircleDollarSign} tone="#f0abfc" />
          <Metric value={selectedRun ? formatDuration(selectedRun) : "—"} label="Duration · selected run" Icon={Clock3} tone="#fbbf24" />
        </section>

        <section className="graph-section">
          <div className="section-bar"><div><span className="section-index">01</span><div><h2>Live execution graph</h2><p>{selectedRun ? `Run ${selectedRun.id.slice(0, 8)} · ${selectedRun.status}` : "Select a run to inspect its durable node state"}</p></div></div><div className="section-actions"><button><ListFilter size={15} />All nodes</button><button><GitBranch size={15} />Auto layout</button>{selectedRun && ["pending", "running"].includes(selectedRun.status) ? <button className="danger-action" onClick={() => void cancelSelectedRun()}><Ban size={14} />Cancel run</button> : null}</div></div>
          <div className="graph-card">
            {flowNodes.length > 0 ? <ReactFlow nodes={flowNodes} edges={flowEdges} nodeTypes={nodeTypes} fitView minZoom={0.45} maxZoom={1.8} nodesDraggable={false} nodesConnectable={false}><Background color="#34354b" gap={24} size={1} /><Controls showInteractive={false} /><MiniMap nodeColor={(node) => statusColor[(node.data as ExecutionNodeData).node.status] ?? statusColor.pending} maskColor="rgba(8,9,15,.72)" /></ReactFlow> : <div className="graph-empty"><div><WorkflowIcon size={28} /></div><h3>{connection === "live" ? "No execution nodes yet" : "Connect the Relay backend"}</h3><p>{connection === "live" ? "Create a workflow and submit a run. Its durable node states will appear here in real time." : "Start the API, control plane, worker, PostgreSQL, and NATS stack to populate this console with real execution data."}</p><button className="primary-action" onClick={() => { setMutationError(null); setDialog("workflow"); }} disabled={connection !== "live"}><WorkflowIcon size={15} />Create workflow</button></div>}
            <div className="graph-legend">{["running", "succeeded", "failed", "queued"].map((status) => <span key={status}><i style={{ background: statusColor[status] }} />{status}</span>)}</div>
          </div>
        </section>

        <section className="lower-grid">
          <div className="panel run-panel">
            <div className="panel-heading"><div><span className="section-index">02</span><h2>Recent runs</h2></div><button>View all <ChevronDown size={14} /></button></div>
            <div className="run-table" role="table" aria-label="Recent workflow runs"><div className="run-row run-header" role="row"><span>Run</span><span>Status</span><span>Created</span><span>Duration</span></div>{visibleRuns.length === 0 ? <div className="table-empty">{runs.length === 0 ? "No runs have been submitted." : "No runs match this search."}</div> : visibleRuns.slice(0, 7).map((run) => <button className={`run-row ${run.id === selectedRunID ? "selected" : ""}`} key={run.id} onClick={() => setSelectedRunID(run.id)} role="row"><span><code>{run.id.slice(0, 8)}</code><small>{run.workflow_version_id.slice(0, 8)}</small></span><span><i className={`status-dot ${run.status}`} />{run.status}</span><span>{new Date(run.created_at).toLocaleString([], { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" })}</span><span>{formatDuration(run)}</span></button>)}</div>
          </div>
          <div className="panel event-panel">
            <div className="panel-heading"><div><span className="section-index">03</span><h2>Event stream</h2></div><span className="event-count">{events.length} events</span></div>
            <div className="event-list">{events.length === 0 ? <div className="table-empty">No events for the selected run.</div> : events.slice(-8).reverse().map((event) => <article key={event.id}><i style={{ background: event.event_type.includes("failed") ? statusColor.failed : event.event_type.includes("succeeded") ? statusColor.succeeded : statusColor.running }} /><div><strong>{event.event_type.replaceAll(".", " ")}</strong><span>sequence {event.sequence}</span></div><time>{new Date(event.occurred_at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" })}</time></article>)}</div>
          </div>
        </section>
        </div>

        {activeView === "workflows" ? <section className="view-page"><div className="view-page-heading"><div><span className="section-index">WF</span><div><h2>Workflow catalog</h2><p>{workflows.length} immutable workflow{workflows.length === 1 ? "" : "s"}</p></div></div><button className="primary-action" onClick={() => { setMutationError(null); setDialog("workflow"); }} disabled={connection !== "live"}><Plus size={15} />New workflow</button></div><div className="workflow-cards">{workflows.length === 0 ? <div className="view-empty"><WorkflowIcon size={24} /><h3>No workflows yet</h3><p>Create a validated DAG to begin executing agent work.</p></div> : workflows.map((workflow) => <article key={workflow.id}><div className="workflow-card-mark"><WorkflowIcon size={18} /></div><div><h3>{workflow.name}</h3><p>{workflow.description || "No description"}</p><span><code>{workflow.id.slice(0, 8)}</code> · created {new Date(workflow.created_at).toLocaleDateString()}</span></div><button onClick={() => { setRunWorkflowID(workflow.id); setMutationError(null); setDialog("run"); }}><Play size={14} />Run</button></article>)}</div></section> : null}

        {activeView === "runs" ? <section className="view-page"><div className="view-page-heading"><div><span className="section-index">RUN</span><div><h2>All runs</h2><p>Select a run to load its graph, events, and usage.</p></div></div><button className="primary-action" onClick={openRunDialog} disabled={connection !== "live" || workflows.length === 0}><Play size={15} />New run</button></div><div className="panel"><div className="run-table"><div className="run-row run-header"><span>Run</span><span>Status</span><span>Created</span><span>Duration</span></div>{visibleRuns.map((run) => <button className={`run-row ${run.id === selectedRunID ? "selected" : ""}`} key={run.id} onClick={() => { setSelectedRunID(run.id); setActiveView("overview"); }}><span><code>{run.id}</code><small>{run.workflow_version_id}</small></span><span><i className={`status-dot ${run.status}`} />{run.status}</span><span>{new Date(run.created_at).toLocaleString()}</span><span>{formatDuration(run)}</span></button>)}</div></div></section> : null}

        {activeView === "activity" ? <section className="view-page"><div className="view-page-heading"><div><span className="section-index">EVT</span><div><h2>Ordered event replay</h2><p>{selectedRun ? `Run ${selectedRun.id} · ${events.length} events` : "Select a run from the Runs view"}</p></div></div></div><div className="panel activity-timeline">{events.length === 0 ? <div className="view-empty"><Activity size={24} /><h3>No events selected</h3><p>Execution events are append-only and monotonically sequenced per run.</p></div> : events.slice().reverse().map((event) => <article key={event.id}><span className="event-sequence">#{event.sequence}</span><i style={{ background: event.event_type.includes("failed") ? statusColor.failed : event.event_type.includes("succeeded") ? statusColor.succeeded : statusColor.running }} /><div><strong>{event.event_type}</strong><code>{JSON.stringify(event.payload)}</code></div><time>{new Date(event.occurred_at).toLocaleString()}</time></article>)}</div></section> : null}

        {activeView === "usage" ? <section className="view-page"><div className="view-page-heading"><div><span className="section-index">$</span><div><h2>Selected run usage</h2><p>{selectedRun ? `Run ${selectedRun.id}` : "Select a run to inspect usage"}</p></div></div></div><div className="usage-summary"><Metric value={compactNumber(usage?.totals.input_tokens ?? 0)} label="Input tokens" Icon={Braces} tone="#a78bfa" /><Metric value={compactNumber(usage?.totals.output_tokens ?? 0)} label="Output tokens" Icon={Braces} tone="#22d3ee" /><Metric value={knownCost == null ? "Unknown" : `$${(knownCost / 1_000_000).toFixed(6)}`} label="Configured cost" Icon={CircleDollarSign} tone="#f0abfc" /></div><div className="panel usage-records">{usage?.items.length ? usage.items.map((item) => <article key={item.id}><div><strong>{item.provider}</strong><span>{item.model}</span></div><code>{(item.input_tokens ?? 0) + (item.output_tokens ?? 0)} tokens</code><code>{item.cost_microusd == null ? "unknown cost" : `$${(item.cost_microusd / 1_000_000).toFixed(6)}`}</code><time>{new Date(item.recorded_at).toLocaleString()}</time></article>) : <div className="view-empty"><CircleDollarSign size={24} /><h3>No usage records</h3><p>Transform and join nodes do not record model usage.</p></div>}</div></section> : null}

        {activeView === "connections" ? <section className="view-page"><div className="view-page-heading"><div><span className="section-index">IO</span><div><h2>Runtime boundaries</h2><p>Only health that Relay can verify is marked connected.</p></div></div></div><div className="connection-grid"><article><Database size={20} /><div><h3>Relay API + PostgreSQL</h3><p>Authoritative workflows, runs, attempts, events, and usage.</p></div><span className={connection === "live" ? "connection-ok" : "connection-bad"}>{connection}</span></article><article><Boxes size={20} /><div><h3>NATS JetStream</h3><p>Private dispatch plane. Health is enforced by the control plane, not exposed by this API.</p></div><span>server-side</span></article><article><Bot size={20} /><div><h3>Model provider</h3><p>Mock or OpenAI is selected at worker startup; pricing remains configuration.</p></div><span>worker-owned</span></article><article><Wrench size={20} /><div><h3>MCP tools</h3><p>Connections and tools fail closed unless explicitly allowlisted.</p></div><span>allowlist</span></article></div></section> : null}

        <footer className="workspace-footer"><span>{workflows.length} workflows · {runs.length} runs</span><span><Boxes size={13} />PostgreSQL is authoritative · JetStream dispatches</span></footer>
      </main>

      {dialog === "workflow" ? <div className="dialog-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) setDialog(null); }}><section className="operator-dialog" role="dialog" aria-modal="true" aria-labelledby="workflow-dialog-title"><div className="dialog-heading"><div><span className="section-index">NEW</span><div><h2 id="workflow-dialog-title">Create workflow</h2><p>Relay validates and stores this DAG as immutable version 1.</p></div></div><button className="icon-button" onClick={() => setDialog(null)} aria-label="Close dialog"><X size={16} /></button></div><form onSubmit={submitWorkflow}><label>Name<input required minLength={1} maxLength={120} value={workflowName} onChange={(event) => setWorkflowName(event.target.value)} placeholder="Research brief" /></label><label>Description<input maxLength={500} value={workflowDescription} onChange={(event) => setWorkflowDescription(event.target.value)} placeholder="Parallel research with a final synthesis" /></label><label>Workflow definition<textarea required spellCheck={false} rows={13} value={workflowDefinition} onChange={(event) => setWorkflowDefinition(event.target.value)} /></label>{mutationError ? <p className="form-error">{mutationError}</p> : null}<div className="dialog-actions"><button type="button" onClick={() => setDialog(null)}>Cancel</button><button className="primary-action" type="submit" disabled={submitting}><Plus size={15} />{submitting ? "Creating…" : "Create workflow"}</button></div></form></section></div> : null}

      {dialog === "run" ? <div className="dialog-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) setDialog(null); }}><section className="operator-dialog compact-dialog" role="dialog" aria-modal="true" aria-labelledby="run-dialog-title"><div className="dialog-heading"><div><span className="section-index">RUN</span><div><h2 id="run-dialog-title">Submit a run</h2><p>An idempotency key is generated for this submission.</p></div></div><button className="icon-button" onClick={() => setDialog(null)} aria-label="Close dialog"><X size={16} /></button></div><form onSubmit={submitRun}><label>Workflow<select required value={runWorkflowID} onChange={(event) => setRunWorkflowID(event.target.value)}>{workflows.map((workflow) => <option value={workflow.id} key={workflow.id}>{workflow.name}</option>)}</select></label><label>Run input<textarea required spellCheck={false} rows={8} value={runInput} onChange={(event) => setRunInput(event.target.value)} /></label>{mutationError ? <p className="form-error">{mutationError}</p> : null}<div className="dialog-actions"><button type="button" onClick={() => setDialog(null)}>Cancel</button><button className="primary-action" type="submit" disabled={submitting || !runWorkflowID}><Play size={15} />{submitting ? "Submitting…" : "Submit run"}</button></div></form></section></div> : null}
    </div>
  );
}
