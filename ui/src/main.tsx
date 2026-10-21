import React from 'react'
import ReactDOM from 'react-dom/client'
import { Activity, BarChart3, ChevronLeft, ChevronRight, LogOut, Moon, Plus, Settings, ShieldCheck, Sun, Trash2, X } from 'lucide-react'
import './styles.css'

type Rule = { id:number; name:string; rule_type:'keyword'|'regex'; pattern:string; enabled:boolean; namespace_scopes:string[]; pod_scopes:string[]; container_scopes:string[] }
type Alert = { time:string; namespace:string; pod_name:string; container_name:string; level:string; match_type:string; rule_name:string; log:string }
type Count = { name:string; count:number }
type Series = { name:string; points:Count[] }
type Dashboard = { total:number; namespaces:number; rules:number; trend:Series[]; interval:string; by_namespace:Count[]; by_rule:Count[]; alerts:Alert[] }
type RuleForm = Omit<Rule, 'id'>
type Theme = 'light'|'dark'
const emptyRule: RuleForm = { name:'', rule_type:'keyword', pattern:'', enabled:true, namespace_scopes:[], pod_scopes:[], container_scopes:[] }
const chartColors = ['#35cfff','#ffb84d','#7ee787','#ff6b9a','#a98bff','#45ead4','#ff7b72']

class ScreenBoundary extends React.Component<{children:React.ReactNode},{failed:boolean}> {
  state = {failed:false}
  static getDerivedStateFromError(){return {failed:true}}
  render(){return this.state.failed?<main className="recovery"><Activity/><h1>Let’s reload this view.</h1><p>LOGMON received an unexpected response.</p><button className="primary" onClick={()=>location.reload()}>Reload LOGMON</button></main>:this.props.children}
}

async function api<T>(path:string, options:RequestInit = {}):Promise<T> {
  const token = localStorage.getItem('log-monitor-token')
  const response = await fetch(path, { ...options, headers:{ 'Content-Type':'application/json', ...(token ? {Authorization:`Bearer ${token}`} : {}), ...options.headers } })
  if (response.status === 401 && path !== '/api/login') { localStorage.removeItem('log-monitor-token'); location.reload() }
  if (!response.ok) { const body = await response.json().catch(() => ({})); throw new Error(body.error || `Something went wrong (${response.status})`) }
  return response.status === 204 ? undefined as T : response.json()
}

function ThemeButton({theme,setTheme}:{theme:Theme;setTheme:(value:Theme)=>void}) {
  return <button className="theme-button" onClick={()=>setTheme(theme==='dark'?'light':'dark')} aria-label={`Use ${theme==='dark'?'light':'dark'} mode`}>{theme==='dark'?<Sun/>:<Moon/>}</button>
}

function Login({onLogin,theme,setTheme}:{onLogin:()=>void;theme:Theme;setTheme:(value:Theme)=>void}) {
  const [error,setError]=React.useState('')
  async function submit(e:React.FormEvent<HTMLFormElement>) { e.preventDefault(); const data=new FormData(e.currentTarget); try { const result=await api<{token:string}>('/api/login',{method:'POST',body:JSON.stringify({username:data.get('username'),password:data.get('password')})}); localStorage.setItem('log-monitor-token',result.token); onLogin() } catch(err) { setError((err as Error).message) } }
  return <main className="login"><ThemeButton theme={theme} setTheme={setTheme}/><div className="login-orbit"/><section className="login-card"><Brand/><p className="eyebrow">WELCOME BACK</p><h1>Clarity starts here.</h1><p className="muted">Sign in to see what needs your attention.</p><form onSubmit={submit}><label>Username<input name="username" defaultValue="admin" autoFocus/></label><label>Password<input name="password" type="password" defaultValue="admin"/></label>{error&&<p className="error">{error}</p>}<button className="primary">Open LOGMON</button></form></section></main>
}

function Brand(){return <div className="brand"><span className="brand-icon"><Activity/></span><span>LOG<b>MON</b></span></div>}

function App() {
  const [loggedIn,setLoggedIn]=React.useState(!!localStorage.getItem('log-monitor-token'))
  const [page,setPage]=React.useState<'home'|'dashboard'|'settings'>('home')
  const [theme,setTheme]=React.useState<Theme>(()=>(localStorage.getItem('logmon-theme') as Theme)||'dark')
  const [menuOpen,setMenuOpen]=React.useState(()=>localStorage.getItem('logmon-menu')!=='closed')
  React.useEffect(()=>{document.documentElement.dataset.theme=theme;localStorage.setItem('logmon-theme',theme)},[theme])
  React.useEffect(()=>{localStorage.setItem('logmon-menu',menuOpen?'open':'closed')},[menuOpen])
  if(!loggedIn) return <Login onLogin={()=>setLoggedIn(true)} theme={theme} setTheme={setTheme}/>
  return <div className={`app ${menuOpen?'menu-open':'menu-closed'}`}><aside><Brand/><button className="menu-toggle" onClick={()=>setMenuOpen(value=>!value)} aria-label={menuOpen?'Close side menu':'Open side menu'} title={menuOpen?'Close menu':'Open menu'}>{menuOpen?<ChevronLeft/>:<ChevronRight/>}</button><nav><button className={page==='home'?'active':''} onClick={()=>setPage('home')} title="Home"><ShieldCheck/><span>Home</span></button><button className={page==='dashboard'?'active':''} onClick={()=>setPage('dashboard')} title="Activity"><BarChart3/><span>Activity</span></button><button className={page==='settings'?'active':''} onClick={()=>setPage('settings')} title="Rules"><Settings/><span>Rules</span></button></nav><div className="aside-actions"><ThemeButton theme={theme} setTheme={setTheme}/><button className="logout" onClick={()=>{localStorage.removeItem('log-monitor-token');setLoggedIn(false)}} title="Sign out"><LogOut/><span>Sign out</span></button></div></aside><main className="content" key={page}><ScreenBoundary>{page==='home'?<Home open={setPage}/>:page==='dashboard'?<DashboardPage/>:<SettingsPage/>}</ScreenBoundary></main></div>
}

function Home({open}:{open:(p:'dashboard'|'settings')=>void}) { return <><Header kicker="LOGMON HOME" title="See the signal, not the noise." subtitle="A clear view of important moments across your applications."/><div className="launch-grid"><button onClick={()=>open('dashboard')}><span className="tile-number">01</span><span className="tile-icon"><BarChart3/></span><div><h2>View activity</h2><p>Follow changes over time and review the latest matches.</p></div><b>OPEN VIEW →</b></button><button onClick={()=>open('settings')}><span className="tile-number">02</span><span className="tile-icon"><Settings/></span><div><h2>Manage rules</h2><p>Choose what LOGMON should watch for and where.</p></div><b>OPEN RULES →</b></button></div></> }
function Header({kicker,title,subtitle,action}:{kicker:string;title:string;subtitle?:string;action?:React.ReactNode}) { return <header><div><p className="eyebrow">{kicker}</p><h1>{title}</h1>{subtitle&&<p className="subtitle">{subtitle}</p>}</div>{action}</header> }

function DashboardPage() {
  const [data,setData]=React.useState<Dashboard|null>(null),[range,setRange]=React.useState('24h'),[error,setError]=React.useState('')
  React.useEffect(()=>{setData(null);setError('');api<Dashboard>(`/api/dashboard?range=${range}&limit=2000`).then(setData).catch(()=>setError('We could not load activity right now. Please try again.'))},[range])
  return <><Header kicker="ACTIVITY" title="What needs attention." subtitle="Patterns and recent matches, all in one place." action={<label className="range-select">Showing<select value={range} onChange={e=>setRange(e.target.value)}><option value="1h">Last hour</option><option value="6h">Last 6 hours</option><option value="24h">Last 24 hours</option><option value="7d">Last 7 days</option></select></label>}/>{error&&<div className="notice error">{error}</div>}{!data?<div className="loading"><i/><span>Gathering your activity...</span></div>:<><section className="metrics"><Metric label="Matches found" value={data.total}/><Metric label="Areas affected" value={data.namespaces}/><Metric label="Rules triggered" value={data.rules}/></section><section className="charts"><article className="panel velocity"><PanelTitle label="OVER TIME" title="Alert velocity" note={`One point every ${friendlyInterval(data.interval)}`}/><LineChart series={data.trend||[]}/></article><article className="panel namespaces"><PanelTitle label="BY NAMESPACE" title="Alerts in each namespace"/><NamespacePie items={data.by_namespace||[]}/></article></section><section className="panel table-card"><div className="section-title"><div><p className="eyebrow">LATEST ACTIVITY</p><h2>Recent matches</h2></div><span>{(data.alerts||[]).length} shown</span></div><div className="table-scroll"><table><thead><tr><th>When</th><th>From</th><th>Matched by</th><th>Message</th></tr></thead><tbody>{(data.alerts||[]).map((a,i)=><tr key={a.time+i}><td>{new Date(a.time).toLocaleString()}</td><td><b>{a.namespace}</b><small>{a.pod_name} / {a.container_name}</small></td><td><span className="pill">{a.rule_name}</span></td><td className="logline">{a.log}</td></tr>)}</tbody></table>{!(data.alerts||[]).length&&<Empty/>}</div></section></>}</>
}

function PanelTitle({label,title,note}:{label:string;title:string;note?:string}){return <div className="panel-title"><div><p className="eyebrow">{label}</p><h2>{title}</h2></div>{note&&<span>{note}</span>}</div>}
function Metric({label,value}:{label:string;value:number}) { return <article><div className="metric-corner"/><span>{label}</span><strong>{value.toLocaleString()}</strong><small>IN SELECTED PERIOD</small></article> }
function friendlyInterval(interval:string){return interval==='5m0s'?'5 min':interval==='30m0s'?'30 min':interval==='1h0m0s'?'hour':'6 hours'}

function LineChart({series}:{series:Series[]}) {
  const validSeries=series.filter(item=>Array.isArray(item.points))
  if(!validSeries.length)return <Empty/>
  const width=920,height=300,left=42,right=16,top=20,bottom=42
  const points=validSeries[0].points.length,max=Math.max(1,...validSeries.flatMap(s=>s.points.map(p=>p.count)))
  const x=(i:number)=>left+(i/Math.max(points-1,1))*(width-left-right)
  const y=(value:number)=>top+(1-value/max)*(height-top-bottom)
  const ticks=[0,.25,.5,.75,1].map(n=>Math.round(n*(points-1))).filter((n,i,a)=>a.indexOf(n)===i)
  return <div className="line-chart"><div className="legend">{validSeries.map((s,i)=><span key={s.name}><i style={{background:chartColors[i%chartColors.length]}}/>{s.name}</span>)}</div><svg viewBox={`0 0 ${width} ${height}`} role="img" aria-label="Alerts over time"><defs><filter id="glow"><feGaussianBlur stdDeviation="3" result="blur"/><feMerge><feMergeNode in="blur"/><feMergeNode in="SourceGraphic"/></feMerge></filter></defs>{[0,.25,.5,.75,1].map(n=><g key={n}><line className="grid-line" x1={left} x2={width-right} y1={y(max*n)} y2={y(max*n)}/><text className="axis-text" x={left-10} y={y(max*n)+4} textAnchor="end">{Math.round(max*n)}</text></g>)}{validSeries.map((s,si)=><g key={s.name}><polyline className="series-line" filter="url(#glow)" stroke={chartColors[si%chartColors.length]} points={s.points.map((p,i)=>`${x(i)},${y(p.count)}`).join(' ')}/>{s.points.map((p,i)=><circle key={i} className="series-point" cx={x(i)} cy={y(p.count)} r="3" stroke={chartColors[si%chartColors.length]}><title>{s.name}: {p.count} at {p.name}</title></circle>)}</g>)}{ticks.map(i=><text key={i} className="axis-text" x={x(i)} y={height-12} textAnchor="middle">{validSeries[0].points[i]?.name}</text>)}</svg></div>
}

function NamespacePie({items}:{items:Count[]}) { const visible=items.slice(0,7),total=visible.reduce((sum,item)=>sum+item.count,0);if(!total)return <Empty/>;let cursor=0;const slices=visible.map((item,index)=>{const start=cursor;cursor+=item.count/total*100;return `${chartColors[index%chartColors.length]} ${start}% ${cursor}%`});return <div className="pie-layout"><div className="pie" style={{background:`conic-gradient(${slices.join(',')})`}}><div><strong>{total}</strong><span>TOTAL</span></div></div><div className="pie-legend">{visible.map((item,index)=><div key={item.name}><i style={{background:chartColors[index%chartColors.length]}}/><span>{item.name||'Unassigned'}</span><strong>{item.count}</strong></div>)}</div></div>}
function Empty(){return <p className="empty">Nothing matched in this period.</p>}

function SettingsPage() {
  const [rules,setRules]=React.useState<Rule[]>([]),[editing,setEditing]=React.useState<Rule|null|undefined>(),[error,setError]=React.useState('')
  const load=()=>api<Rule[]>('/api/rules').then(setRules).catch(()=>setError('We could not load your rules. Please try again.')); React.useEffect(()=>{void load()},[])
  async function remove(id:number){if(!confirm('Delete this rule?'))return; await api(`/api/rules/${id}`,{method:'DELETE'});load()}
  async function toggle(rule:Rule){await api(`/api/rules/${rule.id}`,{method:'PUT',body:JSON.stringify({...rule,enabled:!rule.enabled})});load()}
  return <><Header kicker="RULES" title="Choose what matters." subtitle="Turn rules on or off, or add a new one at any time." action={<button className="primary" onClick={()=>setEditing(null)}><Plus/>Add rule</button>}/>{error&&<div className="notice error">{error}</div>}<section className="rule-list">{rules.map(rule=><article key={rule.id}><button className={`switch ${rule.enabled?'on':''}`} onClick={()=>toggle(rule)} aria-label={`${rule.enabled?'Disable':'Enable'} ${rule.name}`}><i/></button><div><span className="rule-kind">{rule.rule_type}</span><h3>{rule.name}</h3><code>{rule.pattern}</code><p>{scopeText(rule)}</p></div><div className="rule-actions"><button onClick={()=>setEditing(rule)}>Edit</button><button className="danger" onClick={()=>remove(rule.id)} aria-label="Delete rule"><Trash2/></button></div></article>)}</section>{editing!==undefined&&<RuleModal rule={editing} close={()=>setEditing(undefined)} saved={()=>{setEditing(undefined);load()}}/>}</>
}
function scopeText(r:Rule){const values=[r.namespace_scopes.length?`Areas: ${r.namespace_scopes.join(', ')}`:'',r.pod_scopes.length?`Applications: ${r.pod_scopes.join(', ')}`:'',r.container_scopes.length?`Parts: ${r.container_scopes.join(', ')}`:''].filter(Boolean);return values.length?values.join(' · '):'Watching everywhere'}
function RuleModal({rule,close,saved}:{rule:Rule|null;close:()=>void;saved:()=>void}) {
  const [error,setError]=React.useState(''); const initial=rule||emptyRule
  async function submit(e:React.FormEvent<HTMLFormElement>){e.preventDefault();const f=new FormData(e.currentTarget),split=(v:FormDataEntryValue|null)=>String(v||'').split(',').map(x=>x.trim()).filter(Boolean);const payload={name:f.get('name'),rule_type:f.get('rule_type'),pattern:f.get('pattern'),enabled:f.get('enabled')==='on',namespace_scopes:split(f.get('namespaces')),pod_scopes:split(f.get('pods')),container_scopes:split(f.get('containers'))};try{await api(rule?`/api/rules/${rule.id}`:'/api/rules',{method:rule?'PUT':'POST',body:JSON.stringify(payload)});saved()}catch(err){setError((err as Error).message)}}
  return <div className="modal-bg"><form className="modal" onSubmit={submit}><div className="modal-head"><div><p className="eyebrow">RULE DETAILS</p><h2>{rule?'Edit rule':'Add a new rule'}</h2></div><button type="button" onClick={close}><X/></button></div><div className="form-grid"><label>Rule name<input name="name" defaultValue={initial.name} required/></label><label>Rule type<select name="rule_type" defaultValue={initial.rule_type}><option value="keyword">Keyword</option><option value="regex">Regex</option></select></label><label className="wide">Pattern<input name="pattern" defaultValue={initial.pattern} placeholder="ERROR or timeout-[0-9]+" required/><span className="regex-guide"><b>Keyword example:</b> <code>ERROR</code> matches any message containing ERROR.<br/><b>Regex templates:</b> <code>timeout-[0-9]+</code> matches timeout- followed by numbers; <code>(ERROR|WARNING)</code> matches either word; <code>status=(4|5)[0-9]{'{2}'}</code> matches 4xx and 5xx status codes.</span></label><label>Namespaces <small>Separate names with commas; empty means all</small><input name="namespaces" defaultValue={initial.namespace_scopes.join(', ')}/></label><label>Pods <small>Separate names with commas; empty means all</small><input name="pods" defaultValue={initial.pod_scopes.join(', ')}/></label><label>Containers <small>Separate names with commas; empty means all</small><input name="containers" defaultValue={initial.container_scopes.join(', ')}/></label><label className="check"><input name="enabled" type="checkbox" defaultChecked={initial.enabled}/> Enable rule</label></div>{error&&<p className="error">{error}</p>}<div className="modal-actions"><button type="button" onClick={close}>Cancel</button><button className="primary">Save rule</button></div></form></div>
}

ReactDOM.createRoot(document.getElementById('root')!).render(<React.StrictMode><App/></React.StrictMode>)
