import React, { useMemo, useState } from 'react'
import { createRoot } from 'react-dom/client'
import './styles.css'

type Issue = { code: string; field?: string; message: string }
type Report = { record: Record<string, unknown>; warnings?: Issue[]; errors?: Issue[] }

const sample = JSON.stringify({
  source: 'braintrust',
  version: 'example',
  payload: {
    name: 'answer',
    model: 'example-model',
    scores: { groundedness: 0.91 }
  }
}, null, 2)

function App() {
  const [api, setApi] = useState(import.meta.env.VITE_NORMALIZE_API ?? 'http://localhost:8080')
  const [input, setInput] = useState(sample)
  const [report, setReport] = useState<Report | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const issues = useMemo(() => [...(report?.errors ?? []), ...(report?.warnings ?? [])], [report])

  async function normalize() {
    setBusy(true); setError('')
    try {
      JSON.parse(input)
      const res = await fetch(`${api.replace(/\/$/, '')}/normalize`, { method: 'POST', headers: { 'content-type': 'application/json' }, body: input })
      const body = await res.json()
      setReport(body)
      if (!res.ok && !body.errors) setError(`HTTP ${res.status}`)
    } catch (e) {
      setReport(null); setError(e instanceof Error ? e.message : String(e))
    } finally { setBusy(false) }
  }

  return <main>
    <header>
      <div><p className="eyebrow">GenAI observability</p><h1>Normalization Inspector</h1></div>
      <p className="lede">Inspect how framework-specific telemetry becomes a canonical record without hiding ambiguity or information loss.</p>
    </header>
    <section className="toolbar">
      <label>Normalizer API<input value={api} onChange={e => setApi(e.target.value)} /></label>
      <button onClick={normalize} disabled={busy}>{busy ? 'Normalizing…' : 'Normalize payload'}</button>
    </section>
    <section className="grid">
      <article><h2>Source payload</h2><textarea value={input} onChange={e => setInput(e.target.value)} spellCheck={false}/></article>
      <article><h2>Canonical record</h2><pre>{report ? JSON.stringify(report.record, null, 2) : 'Run normalization to inspect the canonical record.'}</pre></article>
    </section>
    <section className="issues">
      <div className="section-title"><h2>Semantic findings</h2><span>{issues.length} findings</span></div>
      {error && <div className="issue error"><b>REQUEST ERROR</b><p>{error}</p></div>}
      {!error && issues.length === 0 && <div className="empty">No warnings or errors returned.</div>}
      {issues.map((i, n) => <div className={`issue ${report?.errors?.includes(i) ? 'error' : 'warning'}`} key={`${i.code}-${n}`}>
        <div><b>{i.code}</b>{i.field && <span>{i.field}</span>}</div><p>{i.message}</p>
      </div>)}
    </section>
  </main>
}

createRoot(document.getElementById('root')!).render(<React.StrictMode><App /></React.StrictMode>)
