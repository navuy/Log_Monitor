import React from 'react'
import ReactDOM from 'react-dom/client'
import { Activity, AlertTriangle, BarChart3, LogOut, Plus, Settings, ShieldCheck, Trash2, X } from 'lucide-react'
import './styles.css'

type Rule = { id:number; name:string; rule_type:'keyword'|'regex'; pattern:string; enabled:boolean; namespace_scopes:string[]; pod_scopes:string[]; container_scopes:string[] }
type Alert = { time:string; namespace:string; pod_name:string; container_name:string; level:string; match_type:string; rule_name:string; log:string }
type Count = { name:string; count:number }
type Dashboard = { total:number; namespaces:number; rules:number; trend:Count[]; by_namespace:Count[]; by_rule:Count[]; alerts:Alert[] }
type RuleForm = Omit<Rule, 'id'>
const emptyRule: RuleForm = { name:'', rule_type:'keyword', pattern:'', enabled:true, namespace_scopes:[], pod_scopes:[], container_scopes:[] }

async function api<T>(path:string, options:RequestInit = {}):Promise<T> {
  const token = localStorage.getItem('log-monitor-token')
  const response = await fetch(path, { ...options, headers:{ 'Content-Type':'application/json', ...(token ? {Authorization:`Bearer ${token}`} : {}), ...options.headers } })
  if (response.status === 401 && path !== '/api/login') { localStorage.removeItem('log-monitor-token'); location.reload() }
  if (!response.ok) { const body = await response.json().catch(() => ({})); throw new Error(body.error || `Request failed (${response.status})`) }
  return response.status === 204 ? undefined as T : response.json()
}

function Login({onLogin}:{onLogin:()=>void}) {
  const [error,setError]=React.useState('')
  async function submit(e:React.FormEvent<HTMLFormElement>) { e.preventDefault(); const data=new FormData(e.currentTarget); try { const result=await api<{token:string}>('/api/login',{method:'POST',body:JSON.stringify({username:data.get('username'),password:data.get('password')})}); localStorage.setItem('log-monitor-token',result.token); onLogin() } catch(err) { setError((err as Error).message) } }
  return <main className="login"><section className="login-card"><div className="brand-mark"><Activity/></div><p className="eyebrow">OPERATIONS CONSOLE</p><h1>Log Monitor</h1><p className="muted">See the signal. Quiet the noise.</p><form onSubmit={submit}><label>Username<input name="username" defaultValue="admin" autoFocus/></label><label>Password<input name="password" type="password" defaultValue="admin"/></label>{error&&<p className="error">{error}</p>}<button>Enter console</button></form></section></main>
}

function App() {
  const [loggedIn,setLoggedIn]=React.useState(!!localStorage.getItem('log-monitor-token')); const [page,setPage]=React.useState<'home'|'dashboard'|'settings'>('home')
  if(!loggedIn) return <Login onLogin={()=>setLoggedIn(true)}/>
  return <div className="app"><aside><div className="logo"><Activity/><span>LOG/<b>MON</b></span></div><nav><button className={page==='home'?'active':''} onClick={()=>setPage('home')}><ShieldCheck/>Overview</button><button className={page==='dashboard'?'active':''} onClick={()=>setPage('dashboard')}><BarChart3/>Dashboard</button><button className={page==='settings'?'active':''} onClick={()=>setPage('settings')}><Settings/>Settings</button></nav><button className="logout" onClick={()=>{localStorage.clear();setLoggedIn(false)}}><LogOut/>Sign out</button></aside><main className="content">{page==='home'?<Home open={setPage}/>:page==='dashboard'?<DashboardPage/>:<SettingsPage/>}</main></div>
}

function Home({open}:{open:(p:'dashboard'|'settings')=>void}) { return <><Header kicker="CONTROL CENTER" title="Everything important, one click away."/><div className="launch-grid"><button onClick={()=>open('dashboard')}><span className="tile-icon amber"><BarChart3/></span><div><h2>Dashboard</h2><p>Explore alert trends and inspect matched log data.</p></div><b>OPEN 01</b></button><button onClick={()=>open('settings')}><span className="tile-icon green"><Settings/></span><div><h2>Rule settings</h2><p>Create keyword and regex rules without redeploying.</p></div><b>OPEN 02</b></button></div></> }
function Header({kicker,title,action}:{kicker:string;title:string;action?:React.ReactNode}) { return <header><div><p className="eyebrow">{kicker}</p><h1>{title}</h1></div>{action}</header> }

function DashboardPage() {
  const [data,setData]=React.useState<Dashboard|null>(null),[range,setRange]=React.useState('24h'),[error,setError]=React.useState('')
  React.useEffect(()=>{setError('');api<Dashboard>(`/api/dashboard?range=${range}`).then(setData).catch(e=>setError(e.message))},[range])
  const max=Math.max(...(data?.trend.map(x=>x.count)||[1]))
  return <><Header kicker="LIVE INTELLIGENCE" title="Signal dashboard" action={<select value={range} onChange={e=>setRange(e.target.value)}><option value="1h">Last hour</option><option value="6h">Last 6 hours</option><option value="24h">Last 24 hours</option><option value="7d">Last 7 days</option></select>}/>{error&&<div className="notice error">{error}</div>}{!data?<div className="loading">Reading alert store...</div>:<><section className="metrics"><Metric label="Matched alerts" value={data.total}/><Metric label="Namespaces" value={data.namespaces}/><Metric label="Active signals" value={data.rules}/></section><section className="charts"><article><h3>Alert velocity</h3><div className="bars">{data.trend.length?data.trend.map(x=><div className="bar-wrap" key={x.name} title={`${x.name}: ${x.count}`}><div className="bar" style={{height:`${Math.max(8,x.count/max*100)}%`}}/><small>{x.name}</small></div>):<Empty/>}</div></article><article><h3>Top namespaces</h3><Rank items={data.by_namespace}/><h3 className="spaced">Rules firing</h3><Rank items={data.by_rule}/></article></section><section className="table-card"><div className="section-title"><div><p className="eyebrow">RAW DATA</p><h2>Recent matches</h2></div><span>{data.alerts.length} records</span></div><div className="table-scroll"><table><thead><tr><th>Time</th><th>Source</th><th>Rule</th><th>Message</th></tr></thead><tbody>{data.alerts.map((a,i)=><tr key={a.time+i}><td>{new Date(a.time).toLocaleString()}</td><td><b>{a.namespace}</b><small>{a.pod_name} / {a.container_name}</small></td><td><span className={`pill ${a.match_type}`}>{a.rule_name}</span></td><td className="logline">{a.log}</td></tr>)}</tbody></table>{!data.alerts.length&&<Empty/>}</div></section></>}</>
}
function Metric({label,value}:{label:string;value:number}) { return <article><span>{label}</span><strong>{value.toLocaleString()}</strong><Activity/></article> }
function Rank({items}:{items:Count[]}) { const max=Math.max(...items.map(x=>x.count),1); return <div className="rank">{items.slice(0,5).map(x=><div key={x.name}><span>{x.name||'Unknown'}</span><i><b style={{width:`${x.count/max*100}%`}}/></i><strong>{x.count}</strong></div>)}{!items.length&&<Empty/>}</div> }
function Empty(){return <p className="empty">No matches in this period.</p>}

function SettingsPage() {
  const [rules,setRules]=React.useState<Rule[]>([]),[editing,setEditing]=React.useState<Rule|null|undefined>(),[error,setError]=React.useState('')
  const load=()=>api<Rule[]>('/api/rules').then(setRules).catch(e=>setError(e.message)); React.useEffect(load,[])
  async function remove(id:number){if(!confirm('Delete this matching rule?'))return; await api(`/api/rules/${id}`,{method:'DELETE'});load()}
  async function toggle(rule:Rule){await api(`/api/rules/${rule.id}`,{method:'PUT',body:JSON.stringify({...rule,enabled:!rule.enabled})});load()}
  return <><Header kicker="RUNTIME CONFIGURATION" title="Matching rules" action={<button className="primary" onClick={()=>setEditing(null)}><Plus/>New rule</button>}/><p className="lede">Changes are stored in PostgreSQL and picked up by every classifier replica during its next refresh.</p>{error&&<div className="notice error">{error}</div>}<section className="rule-list">{rules.map(rule=><article key={rule.id}><button className={`switch ${rule.enabled?'on':''}`} onClick={()=>toggle(rule)} aria-label="Toggle rule"><i/></button><div><span className={`pill ${rule.rule_type}`}>{rule.rule_type}</span><h3>{rule.name}</h3><code>{rule.pattern}</code><p>{scopeText(rule)}</p></div><div className="rule-actions"><button onClick={()=>setEditing(rule)}>Edit</button><button className="danger" onClick={()=>remove(rule.id)}><Trash2/></button></div></article>)}</section>{editing!==undefined&&<RuleModal rule={editing} close={()=>setEditing(undefined)} saved={()=>{setEditing(undefined);load()}}/>}</>
}
function scopeText(r:Rule){const values=[r.namespace_scopes.length?`ns: ${r.namespace_scopes.join(', ')}`:'',r.pod_scopes.length?`pods: ${r.pod_scopes.join(', ')}`:'',r.container_scopes.length?`containers: ${r.container_scopes.join(', ')}`:''].filter(Boolean);return values.length?values.join(' / '):'All sources'}
function RuleModal({rule,close,saved}:{rule:Rule|null;close:()=>void;saved:()=>void}) {
  const [error,setError]=React.useState(''); const initial=rule||emptyRule
  async function submit(e:React.FormEvent<HTMLFormElement>){e.preventDefault();const f=new FormData(e.currentTarget),split=(v:FormDataEntryValue|null)=>String(v||'').split(',').map(x=>x.trim()).filter(Boolean);const payload={name:f.get('name'),rule_type:f.get('rule_type'),pattern:f.get('pattern'),enabled:f.get('enabled')==='on',namespace_scopes:split(f.get('namespaces')),pod_scopes:split(f.get('pods')),container_scopes:split(f.get('containers'))};try{await api(rule?`/api/rules/${rule.id}`:'/api/rules',{method:rule?'PUT':'POST',body:JSON.stringify(payload)});saved()}catch(err){setError((err as Error).message)}}
  return <div className="modal-bg"><form className="modal" onSubmit={submit}><div className="modal-head"><div><p className="eyebrow">RULE EDITOR</p><h2>{rule?'Edit rule':'Create rule'}</h2></div><button type="button" onClick={close}><X/></button></div><div className="form-grid"><label>Name<input name="name" defaultValue={initial.name} required/></label><label>Match type<select name="rule_type" defaultValue={initial.rule_type}><option value="keyword">Keyword</option><option value="regex">Regular expression</option></select></label><label className="wide">Pattern<input name="pattern" defaultValue={initial.pattern} placeholder="ERROR or timeout-[0-9]+" required/></label><label>Namespaces <small>comma-separated</small><input name="namespaces" defaultValue={initial.namespace_scopes.join(', ')}/></label><label>Pods <small>comma-separated</small><input name="pods" defaultValue={initial.pod_scopes.join(', ')}/></label><label>Containers <small>comma-separated</small><input name="containers" defaultValue={initial.container_scopes.join(', ')}/></label><label className="check"><input name="enabled" type="checkbox" defaultChecked={initial.enabled}/> Enabled</label></div>{error&&<p className="error">{error}</p>}<div className="modal-actions"><button type="button" onClick={close}>Cancel</button><button className="primary">Save rule</button></div></form></div>
}

ReactDOM.createRoot(document.getElementById('root')!).render(<React.StrictMode><App/></React.StrictMode>)
