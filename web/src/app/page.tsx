import type { Metadata } from "next";
import Image from "next/image";
import Link from "next/link";
import {
  Activity, ArrowRight, Bot, Braces, Check, CircleDot, Clock3,
  Database, ExternalLink, GitBranch, Github, KeyRound, Layers3,
  Radio, RefreshCw, Route, ShieldCheck, Sparkles, Workflow,
} from "lucide-react";

export const metadata: Metadata = {
  title: "Relay — Durable AI Agent Execution Engine",
  description: "Define, execute, recover, and inspect durable AI agent workflows with immutable DAGs, fenced workers, and replayable events.",
};

const capabilities = [
  { icon: GitBranch, number: "01", title: "Define a workflow", copy: "Author immutable DAGs composed of model, tool, transform, and join nodes. Relay validates the topology before any work begins.", detail: "Versioned workflows · bounded inputs" },
  { icon: RefreshCw, number: "02", title: "Execute nodes durably", copy: "PostgreSQL records each transition. Leases, fencing tokens, retries, deadlines, and recovery scans coordinate work across failures.", detail: "At-least-once · stale work rejected" },
  { icon: Activity, number: "03", title: "Inspect runs and attempts", copy: "Watch the live DAG, inspect every attempt, replay ordered events, track token usage, and cancel active work from one console.", detail: "SSE replay · usage records" },
];

const reliability = [
  [Database, "PostgreSQL is the source of truth", "Run state, attempts, leases, and outbox records are committed transactionally."],
  [ShieldCheck, "Fencing rejects stale completions", "A worker cannot commit a result after its lease has been reassigned."],
  [Clock3, "Deadlines bound failed attempts", "Bounded retries and recovery scans identify and reschedule incomplete work."],
  [KeyRound, "MCP tools require explicit access", "Connections use configured allowlists, schemas, and permissions."],
] as const;

export default function Home() {
  return (
    <main className="landing-page">
      <nav className="landing-nav" aria-label="Primary navigation">
        <Link className="landing-brand" href="/" aria-label="Relay home">
          <Image src="/relay-mark.svg" width={42} height={42} alt="" priority />
          <span><strong>Relay</strong><small>Agent Execution Engine</small></span>
        </Link>
        <div className="landing-nav-links">
          <a href="#product">Product</a><a href="#architecture">Architecture</a><a href="#reliability">Reliability</a>
          <a href="https://github.com/jonxsong/Relay" target="_blank" rel="noreferrer"><Github size={15} />GitHub</a>
        </div>
        <Link className="landing-console-link" href="/console"><span>Open console</span><ArrowRight size={15} /></Link>
      </nav>

      <section className="landing-hero">
        <div className="landing-hero-copy">
          <div className="landing-eyebrow"><span><i />OPEN SOURCE</span><b>AI AGENT EXECUTION ENGINE</b></div>
          <h1>Durable execution for AI agent workflows</h1>
          <p>Relay executes versioned workflow DAGs with leased workers, bounded retries, and replayable events. Operators can inspect every run, node attempt, model call, tool call, and usage record from one console.</p>
          <div className="landing-hero-actions">
            <Link className="landing-primary" href="/console">Open operator console <ArrowRight size={17} /></Link>
            <a className="landing-secondary" href="#architecture">Review the architecture <Route size={16} /></a>
          </div>
          <div className="landing-proof-row"><span><Check size={14} />Apache-2.0</span><span><Check size={14} />OpenAI Responses API</span><span><Check size={14} />PostgreSQL durable state</span></div>
        </div>

        <div className="landing-engine-preview" aria-label="Illustrative Relay execution graph">
          <div className="preview-toolbar"><div><span className="preview-mark"><Workflow size={16} /></span><span><strong>Research brief</strong><small>RUN 7FC1 · LIVE</small></span></div><span className="preview-live"><i />EXECUTING</span></div>
          <div className="preview-canvas">
            <svg aria-hidden="true" viewBox="0 0 720 390" preserveAspectRatio="none"><path d="M108 194 C170 194 170 94 235 94" /><path d="M108 194 C170 194 170 194 235 194" /><path d="M108 194 C170 194 170 294 235 294" /><path d="M343 94 C412 94 398 194 472 194" /><path d="M343 194 H472" /><path d="M343 294 C412 294 398 194 472 194" /><path d="M580 194 H650" /></svg>
            <div className="preview-node preview-start"><span><Sparkles size={14} /></span><div><strong>Run input</strong><small>accepted</small></div><i /></div>
            <div className="preview-node preview-a"><span><Bot size={14} /></span><div><strong>Research</strong><small>gpt-5-mini</small></div><i /></div>
            <div className="preview-node preview-b running"><span><Bot size={14} /></span><div><strong>Analyze</strong><small>attempt 1</small></div><i /></div>
            <div className="preview-node preview-c"><span><Braces size={14} /></span><div><strong>Validate</strong><small>schema</small></div><i /></div>
            <div className="preview-node preview-join"><span><Layers3 size={14} /></span><div><strong>Synthesize</strong><small>waiting 2/3</small></div><i /></div>
            <div className="preview-output"><CircleDot size={15} /><span><strong>Durable result</strong><small>pending</small></span></div>
          </div>
          <div className="preview-events"><span>EVENT 0041</span><code>node.started</code><b>analyze · fence 18</b><time>now</time></div>
        </div>
      </section>

      <section className="landing-signal-bar" aria-label="Relay runtime foundations">
        <div><Database size={18} /><span><strong>PostgreSQL 17</strong><small>source of truth</small></span></div>
        <div><Radio size={18} /><span><strong>NATS JetStream</strong><small>durable notification</small></span></div>
        <div><Bot size={18} /><span><strong>OpenAI Responses</strong><small>model execution</small></span></div>
        <div><Workflow size={18} /><span><strong>Replayable events</strong><small>ordered run history</small></span></div>
      </section>

      <section className="landing-section landing-product" id="product">
        <div className="landing-section-heading"><div><span className="landing-kicker">EXECUTION LIFECYCLE</span><h2>Define, execute, and inspect workflows</h2></div><p>Relay manages the lifecycle from an immutable workflow definition to durable node execution. The console exposes the current state and complete event history for each run.</p></div>
        <div className="landing-capability-grid">{capabilities.map(({ icon: Icon, number, title, copy, detail }) => <article key={number}><header><span>{number}</span><Icon size={21} /></header><h3>{title}</h3><p>{copy}</p><footer>{detail}</footer></article>)}</div>
      </section>

      <section className="landing-section landing-architecture" id="architecture">
        <div className="landing-section-heading compact"><div><span className="landing-kicker">ARCHITECTURE</span><h2>PostgreSQL coordinates durable state</h2></div><p>PostgreSQL commits workflow state and publishes dispatch records through an outbox. JetStream delivers notifications to replaceable workers, while fencing tokens protect committed results.</p></div>
        <div className="landing-architecture-map">
          <div className="architecture-lane request-lane"><span className="architecture-label">REQUEST</span><div className="architecture-box"><Workflow size={22} /><span><strong>Console + API</strong><small>validate · authenticate</small></span></div></div>
          <div className="architecture-arrow"><span>1</span><i /></div>
          <div className="architecture-lane authority-lane"><span className="architecture-label">DURABLE AUTHORITY</span><div className="architecture-box"><Database size={22} /><span><strong>PostgreSQL</strong><small>runs · attempts · outbox</small></span></div></div>
          <div className="architecture-arrow async"><span>2</span><i /></div>
          <div className="architecture-lane dispatch-lane"><span className="architecture-label">DISPATCH</span><div className="architecture-box"><Radio size={22} /><span><strong>JetStream</strong><small>at-least-once wakeup</small></span></div></div>
          <div className="architecture-arrow async"><span>3</span><i /></div>
          <div className="architecture-lane execution-lane"><span className="architecture-label">EXECUTION</span><div className="architecture-box"><Bot size={22} /><span><strong>Fenced workers</strong><small>OpenAI · MCP tools</small></span></div></div>
          <div className="architecture-return"><span>fenced completion + ordered event</span><i /></div>
        </div>
        <div className="architecture-caption"><span><i className="solid" />transactional authority</span><span><i className="dashed" />replaceable delivery</span><a href="https://github.com/jonxsong/Relay/blob/main/docs/architecture.md" target="_blank" rel="noreferrer">Read the architecture notes <ExternalLink size={13} /></a></div>
      </section>

      <section className="landing-section landing-reliability" id="reliability">
        <div className="landing-section-heading compact"><div><span className="landing-kicker">RELIABILITY CONTRACT</span><h2>Reliability guarantees and limitations</h2></div><p>Relay provides at-least-once execution with duplicate-safe internal state transitions. External systems remain subject to their own delivery and idempotency behavior.</p></div>
        <div className="landing-reliability-grid">{reliability.map(([Icon, title, copy], index) => <article key={title}><span>0{index + 1}</span><Icon size={21} /><h3>{title}</h3><p>{copy}</p></article>)}</div>
        <div className="landing-contract-note"><ShieldCheck size={18} /><p><strong>External side effects:</strong> Calls that modify another system require idempotency keys or compensating actions to handle duplicate execution safely.</p></div>
      </section>

      <section className="landing-final-cta"><div><span className="landing-kicker">OPERATOR CONSOLE</span><h2>Create and monitor a workflow</h2></div><div><p>Use the console to define a DAG, submit a run, execute model and MCP tool nodes, and review attempts, events, and usage.</p><Link className="landing-primary" href="/console">Open operator console <ArrowRight size={17} /></Link></div></section>

      <footer className="landing-footer"><Link className="landing-brand" href="/"><Image src="/relay-mark.svg" width={36} height={36} alt="" /><span><strong>Relay</strong><small>Durable agent execution</small></span></Link><p>Apache-2.0 · <a href="https://github.com/jonxsong/Relay" target="_blank" rel="noreferrer">Source on GitHub</a></p><Link href="/console">Operator console <ArrowRight size={14} /></Link></footer>
    </main>
  );
}
