"use client";

import {
  Activity, BarChart3, Blocks, Bot, Boxes, Braces, ChevronDown,
  CircleDollarSign, Clock3, Database, GitBranch, LayoutDashboard,
  Ban, KeyRound, ListFilter, Play, Plus, RefreshCw, RotateCcw, Search, Settings,
  ShieldCheck, Workflow as WorkflowIcon, Wrench, X, Zap,
} from "lucide-react";
import {
  Background, Controls, Handle, MiniMap, Position, ReactFlow,
  type Edge, type Node, type NodeProps, type NodeTypes,
} from "@xyflow/react";
import { type FormEvent, useCallback, useEffect, useMemo, useState } from "react";

import {
  eventListSchema, type ExecutionEvent, nodeAttemptListSchema, type NodeAttempt, nodeListSchema, type NodeRun,
  type Run, runListSchema, runSchema, type Usage, usageSchema, type Workflow,
  workflowListSchema, workflowSchema,
} from "@/lib/contracts";
import { relayClient } from "@/lib/openapi-client";
import { relayGet, relayPost } from "@/lib/relay-api";

type ConnectionState = "loading" | "live" | "unavailable";
type DialogKind = "workflow" | "run" | "auth" | null;
type ConsoleView = "overview" | "workflows" | "runs" | "activity" | "usage" | "connections";
type ExecutionNodeData = { node: NodeRun };
type ExecutionNode = Node<ExecutionNodeData, "execution">;
type WorkflowNodeData = { id: string; type: string };
type WorkflowGraphNode = Node<WorkflowNodeData, "workflowDefinition">;
type WorkflowDefinitionNode = { id: string; type: "llm" | "tool" | "transform" | "join"; depends_on?: string[] };

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

function WorkflowNodeCard({ data }: NodeProps<WorkflowGraphNode>) {
  const Icon = data.type === "llm" ? Bot : data.type === "tool" ? Wrench : data.type === "join" ? GitBranch : Braces;
  return <div className="workflow-node"><Handle type="target" position={Position.Left} className="flow-handle" /><div><Icon size={16} /></div><span><strong>{data.id}</strong><small>{data.type}</small></span><Handle type="source" position={Position.Right} className="flow-handle" /></div>;
}

const nodeTypes: NodeTypes = { execution: ExecutionNodeCard, workflowDefinition: WorkflowNodeCard };

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

function buildWorkflowGraph(definitions: WorkflowDefinitionNode[]) {
  const byID = new Map(definitions.map((node) => [node.id, node]));
  const depths = new Map<string, number>();
  const depth = (id: string, visiting = new Set<string>()): number => {
    const cached = depths.get(id);
    if (cached !== undefined) return cached;
    if (visiting.has(id)) return 0;
    visiting.add(id);
    const dependencies = byID.get(id)?.depends_on ?? [];
    const value = dependencies.length === 0 ? 0 : 1 + Math.max(...dependencies.map((dependency) => depth(dependency, visiting)));
    visiting.delete(id);
    depths.set(id, value);
    return value;
  };
  const graphNodes: WorkflowGraphNode[] = definitions.map((node) => {
    const column = depth(node.id);
    const row = definitions.filter((candidate) => depth(candidate.id) === column).findIndex((candidate) => candidate.id === node.id);
    return { id: node.id, type: "workflowDefinition", data: { id: node.id, type: node.type }, position: { x: 54 + column * 240, y: 60 + Math.max(0, row) * 100 } };
  });
  const graphEdges: Edge[] = definitions.flatMap((node) => (node.depends_on ?? []).filter((dependency) => byID.has(dependency)).map((dependency) => ({ id: `${dependency}-${node.id}`, source: dependency, target: node.id, style: { stroke: "#655bd4", strokeWidth: 1.5 } })));
  return { nodes: graphNodes, edges: graphEdges };
}

function Metric({ value, label, Icon, tone }: { value: string; label: string; Icon: typeof Activity; tone: string }) {
  return <article className="metric-card"><div><span>{label}</span><Icon size={17} style={{ color: tone }} /></div><strong style={{ color: tone }}>{value}</strong></article>;
}

export function RelayDashboard() {
  const [workflows, setWorkflows] = useState<Workflow[]>([]);
  const [selectedWorkflowID, setSelectedWorkflowID] = useState<string | null>(null);
  const [selectedWorkflow, setSelectedWorkflow] = useState<Workflow | null>(null);
  const [runs, setRuns] = useState<Run[]>([]);
  const [selectedRunID, setSelectedRunID] = useState<string | null>(null);
  const [nodes, setNodes] = useState<NodeRun[]>([]);
  const [selectedNodeID, setSelectedNodeID] = useState<string | null>(null);
  const [attempts, setAttempts] = useState<NodeAttempt[]>([]);
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
  const [apiKey, setAPIKey] = useState("");
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
        setSelectedNodeID(null);
        setAttempts([]);
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
    let confirmationTimer: number | undefined;
    const requireAuthentication = () => {
      window.clearTimeout(confirmationTimer);
      confirmationTimer = window.setTimeout(() => {
        void fetch("/api/relay/workflows/?page_size=1", { cache: "no-store" }).then((response) => {
          if (response.status === 401) {
            setMutationError(null);
            setDialog("auth");
          }
        }).catch(() => undefined);
      }, 150);
    };
    window.addEventListener("relay:unauthorized", requireAuthentication);
    return () => {
      window.clearTimeout(confirmationTimer);
      window.removeEventListener("relay:unauthorized", requireAuthentication);
    };
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
    if (!selectedWorkflowID || connection !== "live") return;
    const controller = new AbortController();
    void relayClient.GET("/workflows/{workflowId}", {
      params: { path: { workflowId: selectedWorkflowID } },
      signal: controller.signal,
    }).then(({ data, error: responseError }) => {
      if (!data) throw new Error(responseError?.error.message ?? "Workflow definition unavailable");
      setSelectedWorkflow(workflowSchema.parse(data));
    })
      .catch((reason) => {
        if (!controller.signal.aborted) setError(reason instanceof Error ? reason.message : "Workflow definition unavailable");
      });
    return () => controller.abort();
  }, [selectedWorkflowID, connection]);

  useEffect(() => {
    if (!selectedRunID || !selectedNodeID || connection !== "live") {
      return;
    }
    const controller = new AbortController();
    void relayGet(`/runs/${selectedRunID}/nodes/${selectedNodeID}/attempts`, nodeAttemptListSchema, controller.signal)
      .then((data) => setAttempts(data.items))
      .catch((reason) => {
        if (!controller.signal.aborted) setError(reason instanceof Error ? reason.message : "Attempt history unavailable");
      });
    return () => controller.abort();
  }, [selectedRunID, selectedNodeID, connection, nodes]);

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

  const submitAuthentication = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setSubmitting(true);
    setMutationError(null);
    try {
      const response = await fetch("/api/session", {
        method: "POST",
        cache: "no-store",
        headers: { Accept: "application/json", "Content-Type": "application/json" },
        body: JSON.stringify({ apiKey }),
      });
      if (!response.ok) {
        const problem = await response.json().catch(() => null) as { error?: { message?: string } } | null;
        throw new Error(problem?.error?.message ?? "Unable to unlock Relay");
      }
      setAPIKey("");
      setDialog(null);
      await refresh();
    } catch (reason) {
      setMutationError(reason instanceof Error ? reason.message : "Unable to unlock Relay");
    } finally {
      setSubmitting(false);
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
  const workflowGraph = useMemo(() => {
    const definitions = selectedWorkflow?.version?.definition?.nodes ?? [];
    return buildWorkflowGraph(definitions);
  }, [selectedWorkflow]);
  const workflowDraftGraph = useMemo(() => {
    try {
      const draft = JSON.parse(workflowDefinition) as { nodes?: unknown };
      if (!Array.isArray(draft.nodes)) return null;
      const nodes = draft.nodes.filter((node): node is WorkflowDefinitionNode => {
        if (!node || typeof node !== "object") return false;
        const candidate = node as Record<string, unknown>;
        return typeof candidate.id === "string" && ["llm", "tool", "transform", "join"].includes(String(candidate.type)) && (candidate.depends_on === undefined || Array.isArray(candidate.depends_on));
      });
      if (nodes.length !== draft.nodes.length || nodes.length === 0) return null;
      return buildWorkflowGraph(nodes);
    } catch {
      return null;
    }
  }, [workflowDefinition]);

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
          <div className="topbar-actions"><label className="search-box"><Search size={15} /><input aria-label="Search runs" placeholder="Search runs" value={searchQuery} onChange={(event) => setSearchQuery(event.target.value)} /></label><button className="icon-button" onClick={() => { setMutationError(null); setDialog("auth"); }} aria-label="Unlock console"><KeyRound size={17} /></button><button className="icon-button" onClick={() => void refresh()} aria-label="Refresh"><RefreshCw size={17} /></button><button className="primary-action" onClick={openRunDialog} disabled={connection !== "live" || workflows.length === 0}><Play size={15} fill="currentColor" />New run</button></div>
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
            {flowNodes.length > 0 ? <ReactFlow nodes={flowNodes} edges={flowEdges} nodeTypes={nodeTypes} fitView minZoom={0.45} maxZoom={1.8} nodesDraggable={false} nodesConnectable={false} onNodeClick={(_, node) => { setAttempts([]); setSelectedNodeID(node.id); }}><Background color="#34354b" gap={24} size={1} /><Controls showInteractive={false} /><MiniMap nodeColor={(node) => statusColor[(node.data as ExecutionNodeData).node.status] ?? statusColor.pending} maskColor="rgba(8,9,15,.72)" /></ReactFlow> : <div className="graph-empty"><div><WorkflowIcon size={28} /></div><h3>{connection === "live" ? "No execution nodes yet" : "Connect the Relay backend"}</h3><p>{connection === "live" ? "Create a workflow and submit a run. Its durable node states will appear here in real time." : "Start the API, control plane, worker, PostgreSQL, and NATS stack to populate this console with real execution data."}</p><button className="primary-action" onClick={() => { setMutationError(null); setDialog("workflow"); }} disabled={connection !== "live"}><WorkflowIcon size={15} />Create workflow</button></div>}
            {selectedNodeID ? <aside className="attempt-inspector" aria-label="Node attempt inspector"><header><div><span>NODE ATTEMPTS</span><strong>{nodes.find((node) => node.id === selectedNodeID)?.node_key ?? selectedNodeID.slice(0, 8)}</strong></div><button onClick={() => { setAttempts([]); setSelectedNodeID(null); }} aria-label="Close attempt inspector"><X size={15} /></button></header><div className="attempt-list">{attempts.length === 0 ? <p>No attempt has been queued for this node.</p> : attempts.slice().reverse().map((attempt) => <article key={attempt.id}><div><span>Attempt {attempt.attempt_no}</span><b className={`attempt-status ${attempt.status}`}>{attempt.status}</b></div><dl><div><dt>Fence</dt><dd>{attempt.fencing_token}</dd></div><div><dt>Worker</dt><dd>{attempt.worker_id ?? "unclaimed"}</dd></div><div><dt>Queued</dt><dd>{new Date(attempt.queued_at).toLocaleTimeString()}</dd></div><div><dt>Duration</dt><dd>{attempt.started_at ? `${Math.max(0, Math.round(((attempt.completed_at ? Date.parse(attempt.completed_at) : updatedAt?.getTime() ?? Date.parse(attempt.started_at)) - Date.parse(attempt.started_at)) / 1000))}s` : "—"}</dd></div></dl>{attempt.error_message ? <p className="attempt-error"><strong>{attempt.error_code ?? "execution_failed"}</strong>{attempt.error_message}</p> : null}<details><summary>Structured input</summary><pre>{JSON.stringify(attempt.input, null, 2)}</pre></details>{attempt.output !== undefined ? <details><summary>Structured output</summary><pre>{JSON.stringify(attempt.output, null, 2)}</pre></details> : null}</article>)}</div></aside> : null}
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

        {activeView === "workflows" ? <section className="view-page"><div className="view-page-heading"><div><span className="section-index">WF</span><div><h2>Workflow catalog</h2><p>{workflows.length} immutable workflow{workflows.length === 1 ? "" : "s"}</p></div></div><button className="primary-action" onClick={() => { setMutationError(null); setDialog("workflow"); }} disabled={connection !== "live"}><Plus size={15} />New workflow</button></div><div className="workflow-layout"><div className="workflow-cards">{workflows.length === 0 ? <div className="view-empty"><WorkflowIcon size={24} /><h3>No workflows yet</h3><p>Create a validated DAG to begin executing agent work.</p></div> : workflows.map((workflow) => <article className={selectedWorkflowID === workflow.id ? "selected" : ""} key={workflow.id}><div className="workflow-card-mark"><WorkflowIcon size={18} /></div><div><h3>{workflow.name}</h3><p>{workflow.description || "No description"}</p><span><code>{workflow.id.slice(0, 8)}</code> · created {new Date(workflow.created_at).toLocaleDateString()}</span></div><div className="workflow-card-actions"><button onClick={() => { setSelectedWorkflow(null); setSelectedWorkflowID(workflow.id); }}><GitBranch size={14} />Inspect</button><button onClick={() => { setRunWorkflowID(workflow.id); setMutationError(null); setDialog("run"); }}><Play size={14} />Run</button></div></article>)}</div><div className="workflow-preview panel">{selectedWorkflow && workflowGraph.nodes.length > 0 ? <><header><div><span>VERSION {selectedWorkflow.version?.version}</span><h3>{selectedWorkflow.name}</h3></div><code>{workflowGraph.nodes.length} nodes</code></header><div><ReactFlow nodes={workflowGraph.nodes} edges={workflowGraph.edges} nodeTypes={nodeTypes} fitView nodesDraggable={false} nodesConnectable={false} minZoom={0.45} maxZoom={1.6}><Background color="#34354b" gap={22} size={1} /><Controls showInteractive={false} /></ReactFlow></div></> : <div className="view-empty"><GitBranch size={24} /><h3>Inspect a workflow</h3><p>Select a workflow to view the immutable DAG version that Relay will execute.</p></div>}</div></div></section> : null}

        {activeView === "runs" ? <section className="view-page"><div className="view-page-heading"><div><span className="section-index">RUN</span><div><h2>All runs</h2><p>Select a run to load its graph, events, and usage.</p></div></div><button className="primary-action" onClick={openRunDialog} disabled={connection !== "live" || workflows.length === 0}><Play size={15} />New run</button></div><div className="panel"><div className="run-table"><div className="run-row run-header"><span>Run</span><span>Status</span><span>Created</span><span>Duration</span></div>{visibleRuns.map((run) => <button className={`run-row ${run.id === selectedRunID ? "selected" : ""}`} key={run.id} onClick={() => { setSelectedRunID(run.id); setActiveView("overview"); }}><span><code>{run.id}</code><small>{run.workflow_version_id}</small></span><span><i className={`status-dot ${run.status}`} />{run.status}</span><span>{new Date(run.created_at).toLocaleString()}</span><span>{formatDuration(run)}</span></button>)}</div></div></section> : null}

        {activeView === "activity" ? <section className="view-page"><div className="view-page-heading"><div><span className="section-index">EVT</span><div><h2>Ordered event replay</h2><p>{selectedRun ? `Run ${selectedRun.id} · ${events.length} events` : "Select a run from the Runs view"}</p></div></div></div><div className="panel activity-timeline">{events.length === 0 ? <div className="view-empty"><Activity size={24} /><h3>No events selected</h3><p>Execution events are append-only and monotonically sequenced per run.</p></div> : events.slice().reverse().map((event) => <article key={event.id}><span className="event-sequence">#{event.sequence}</span><i style={{ background: event.event_type.includes("failed") ? statusColor.failed : event.event_type.includes("succeeded") ? statusColor.succeeded : statusColor.running }} /><div><strong>{event.event_type}</strong><code>{JSON.stringify(event.payload)}</code></div><time>{new Date(event.occurred_at).toLocaleString()}</time></article>)}</div></section> : null}

        {activeView === "usage" ? <section className="view-page"><div className="view-page-heading"><div><span className="section-index">$</span><div><h2>Selected run usage</h2><p>{selectedRun ? `Run ${selectedRun.id}` : "Select a run to inspect usage"}</p></div></div></div><div className="usage-summary"><Metric value={compactNumber(usage?.totals.input_tokens ?? 0)} label="Input tokens" Icon={Braces} tone="#a78bfa" /><Metric value={compactNumber(usage?.totals.output_tokens ?? 0)} label="Output tokens" Icon={Braces} tone="#22d3ee" /><Metric value={knownCost == null ? "Unknown" : `$${(knownCost / 1_000_000).toFixed(6)}`} label="Configured cost" Icon={CircleDollarSign} tone="#f0abfc" /></div><div className="panel usage-records">{usage?.items.length ? usage.items.map((item) => <article key={item.id}><div><strong>{item.provider}</strong><span>{item.model}</span></div><code>{(item.input_tokens ?? 0) + (item.output_tokens ?? 0)} tokens</code><code>{item.cost_microusd == null ? "unknown cost" : `$${(item.cost_microusd / 1_000_000).toFixed(6)}`}</code><time>{new Date(item.recorded_at).toLocaleString()}</time></article>) : <div className="view-empty"><CircleDollarSign size={24} /><h3>No usage records</h3><p>Transform and join nodes do not record model usage.</p></div>}</div></section> : null}

        {activeView === "connections" ? <section className="view-page"><div className="view-page-heading"><div><span className="section-index">IO</span><div><h2>Runtime boundaries</h2><p>Only health that Relay can verify is marked connected.</p></div></div></div><div className="connection-grid"><article><Database size={20} /><div><h3>Relay API + PostgreSQL</h3><p>Authoritative workflows, runs, attempts, events, and usage.</p></div><span className={connection === "live" ? "connection-ok" : "connection-bad"}>{connection}</span></article><article><Boxes size={20} /><div><h3>NATS JetStream</h3><p>Private dispatch plane. Health is enforced by the control plane, not exposed by this API.</p></div><span>server-side</span></article><article><Bot size={20} /><div><h3>Model provider</h3><p>Mock or OpenAI is selected at worker startup; pricing remains configuration.</p></div><span>worker-owned</span></article><article><Wrench size={20} /><div><h3>MCP tools</h3><p>Connections and tools fail closed unless explicitly allowlisted.</p></div><span>allowlist</span></article></div></section> : null}

        <footer className="workspace-footer"><span>{workflows.length} workflows · {runs.length} runs</span><span><Boxes size={13} />PostgreSQL is authoritative · JetStream dispatches</span></footer>
      </main>

      {dialog === "workflow" ? <div className="dialog-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) setDialog(null); }}><section className="operator-dialog workflow-dialog" role="dialog" aria-modal="true" aria-labelledby="workflow-dialog-title"><div className="dialog-heading"><div><span className="section-index">NEW</span><div><h2 id="workflow-dialog-title">Create workflow</h2><p>Relay validates and stores this DAG as immutable version 1.</p></div></div><button className="icon-button" onClick={() => setDialog(null)} aria-label="Close dialog"><X size={16} /></button></div><form onSubmit={submitWorkflow}><div className="workflow-form-meta"><label>Name<input required minLength={1} maxLength={120} value={workflowName} onChange={(event) => setWorkflowName(event.target.value)} placeholder="Research brief" /></label><label>Description<input maxLength={500} value={workflowDescription} onChange={(event) => setWorkflowDescription(event.target.value)} placeholder="Parallel research with a final synthesis" /></label></div><div className="workflow-definition-editor"><label>Workflow definition<textarea required spellCheck={false} rows={16} value={workflowDefinition} onChange={(event) => setWorkflowDefinition(event.target.value)} /></label><div className="draft-preview" aria-label="Workflow draft preview">{workflowDraftGraph ? <ReactFlow nodes={workflowDraftGraph.nodes} edges={workflowDraftGraph.edges} nodeTypes={nodeTypes} fitView nodesDraggable={false} nodesConnectable={false} minZoom={0.4} maxZoom={1.4}><Background color="#34354b" gap={20} size={1} /><Controls showInteractive={false} /></ReactFlow> : <div className="view-empty"><GitBranch size={22} /><h3>Valid graph preview</h3><p>Enter a non-empty node array to preview the execution topology.</p></div>}</div></div>{mutationError ? <p className="form-error">{mutationError}</p> : null}<div className="dialog-actions"><button type="button" onClick={() => setDialog(null)}>Cancel</button><button className="primary-action" type="submit" disabled={submitting}><Plus size={15} />{submitting ? "Creating…" : "Create workflow"}</button></div></form></section></div> : null}

      {dialog === "run" ? <div className="dialog-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) setDialog(null); }}><section className="operator-dialog compact-dialog" role="dialog" aria-modal="true" aria-labelledby="run-dialog-title"><div className="dialog-heading"><div><span className="section-index">RUN</span><div><h2 id="run-dialog-title">Submit a run</h2><p>An idempotency key is generated for this submission.</p></div></div><button className="icon-button" onClick={() => setDialog(null)} aria-label="Close dialog"><X size={16} /></button></div><form onSubmit={submitRun}><label>Workflow<select required value={runWorkflowID} onChange={(event) => setRunWorkflowID(event.target.value)}>{workflows.map((workflow) => <option value={workflow.id} key={workflow.id}>{workflow.name}</option>)}</select></label><label>Run input<textarea required spellCheck={false} rows={8} value={runInput} onChange={(event) => setRunInput(event.target.value)} /></label>{mutationError ? <p className="form-error">{mutationError}</p> : null}<div className="dialog-actions"><button type="button" onClick={() => setDialog(null)}>Cancel</button><button className="primary-action" type="submit" disabled={submitting || !runWorkflowID}><Play size={15} />{submitting ? "Submitting…" : "Submit run"}</button></div></form></section></div> : null}

      {dialog === "auth" ? <div className="dialog-backdrop" role="presentation"><section className="operator-dialog compact-dialog" role="dialog" aria-modal="true" aria-labelledby="auth-dialog-title"><div className="dialog-heading"><div><span className="section-index">KEY</span><div><h2 id="auth-dialog-title">Unlock Relay</h2><p>The key is exchanged for an encrypted, HttpOnly session and is never stored in browser JavaScript.</p></div></div>{connection === "live" ? <button className="icon-button" onClick={() => setDialog(null)} aria-label="Close dialog"><X size={16} /></button> : null}</div><form onSubmit={submitAuthentication}><label>Relay API key<input required minLength={16} maxLength={512} type="password" autoComplete="current-password" value={apiKey} onChange={(event) => setAPIKey(event.target.value)} placeholder="Paste your Relay API key" /></label>{mutationError ? <p className="form-error">{mutationError}</p> : null}<div className="dialog-actions">{connection === "live" ? <button type="button" onClick={() => setDialog(null)}>Cancel</button> : null}<button className="primary-action" type="submit" disabled={submitting || apiKey.trim().length < 16}><KeyRound size={15} />{submitting ? "Unlocking…" : "Unlock console"}</button></div></form></section></div> : null}
    </div>
  );
}
