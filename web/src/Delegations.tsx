import {useEffect,useState} from 'react';

type API=<T>(path:string,body?:unknown)=>Promise<T>;
type Option={caller_client:string;target_client:string;resource:string;scope:string;active:boolean;consented?:boolean};
type Props={api:API;busy:boolean;run:(work:()=>Promise<void>)=>Promise<void>;admin?:boolean};

export function Delegations({api,busy,run,admin=false}:Props){
 const [items,setItems]=useState<Option[]>([]);
 const [caller,setCaller]=useState(''),[target,setTarget]=useState(''),[resource,setResource]=useState(''),[scope,setScope]=useState('');
 const endpoint=admin?'/admin/exchange-policies':'/me/delegations';
 useEffect(()=>{void run(async()=>setItems(await api<Option[]>(endpoint)))},[endpoint]);
 async function change(item:Option,active:boolean){await api(endpoint,{caller_client:item.caller_client,target_client:item.target_client,resource:item.resource,scope:item.scope,active});setItems(await api<Option[]>(endpoint))}
 return <section><h2>{admin?'跨项目授权策略':'跨项目委托'}</h2><p>{admin?'只登记明确的调用应用、目标应用、资源和权限。策略启用后仍须用户同意并绑定两个项目。':'仅已绑定两个项目的授权显示在这里。许可只用于指定资源和权限；撤销不影响项目本地登录。'}</p>
  {!items.length&&<p>当前没有可用授权。</p>}
  {items.map(item=><article key={`${item.caller_client}:${item.target_client}:${item.resource}:${item.scope}`}><div><strong>{item.caller_client} → {item.target_client}</strong><p>{item.resource} · {item.scope}</p><small>{admin?(item.active?'策略已启用':'策略已停用'):(item.consented?'已授权':'未授权')}</small></div><button disabled={busy} onClick={()=>void run(()=>change(item,admin?!item.active:!item.consented))}>{admin?(item.active?'停用策略':'启用策略'):(item.consented?'撤销授权':'允许调用')}</button></article>)}
  {admin&&<form onSubmit={event=>{event.preventDefault();void run(async()=>{await change({caller_client:caller,target_client:target,resource,scope,active:true},true);setCaller('');setTarget('');setResource('');setScope('')})}}><h3>登记调用链</h3><label>调用应用 ID<input required value={caller} onChange={event=>setCaller(event.target.value)}/></label><label>目标应用 ID<input required value={target} onChange={event=>setTarget(event.target.value)}/></label><label>目标资源<input required value={resource} onChange={event=>setResource(event.target.value)}/></label><label>权限 Scope<input required value={scope} onChange={event=>setScope(event.target.value)}/></label><button disabled={busy}>登记策略</button></form>}
 </section>;
}
