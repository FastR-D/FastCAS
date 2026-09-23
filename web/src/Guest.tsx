import {useState} from 'react';

type Flow='invite'|'recovery'|'mfa_reset';
export type InitialCredential={flow:Flow;token:string}|null;

// Remove one-time credentials from the address bar before React starts any requests.
export function takeInitialCredential():InitialCredential{
 const query=new URLSearchParams(window.location.search);
 const flow=query.get('flow');const token=query.get('token');
 if(!flow&&!token)return null;
 window.history.replaceState(window.history.state,'',window.location.pathname+window.location.hash);
 return flow==='invite'||flow==='recovery'||flow==='mfa_reset'?{flow,token:token??''}:null;
}

export function Guest({initial}:{initial:InitialCredential}){
 const [flow,setFlow]=useState<Flow>(initial?.flow??'invite');
 const [token,setToken]=useState(initial?.token??'');
 const [name,setName]=useState('');
 const [password,setPassword]=useState('');
 const [confirmation,setConfirmation]=useState('');
 const [busy,setBusy]=useState(false);
 const [complete,setComplete]=useState(false);
 const [error,setError]=useState('');
 async function redeem(event:React.FormEvent<HTMLFormElement>){
  event.preventDefault();setError('');
  if(flow!=='mfa_reset'&&password!==confirmation){setError('两次输入的密码不一致');return}
  setBusy(true);
  try{
   const response=await fetch('/api/v1/'+(flow==='invite'?'register':flow==='recovery'?'recover':'recover-mfa'),{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/json'},body:JSON.stringify(flow==='mfa_reset'?{token,password}:{token,name:flow==='invite'?name:'',password})});
   if(!response.ok){setError(response.status===429?'尝试次数过多，请稍后再试':'凭证无效、已使用或已过期');return}
   setComplete(true);setToken('');setPassword('');setConfirmation('');
  }catch{setError('网络不可用，请稍后重试')}finally{setBusy(false)}
 }
 return <div className="grid"><section><h2>登录 FastCAS</h2><p>已有项目账号仍可使用项目原有方式登录。</p><a className="primary" href="/login">前往登录</a></section>
 <section><h2>{flow==='invite'?'接受邀请':flow==='recovery'?'恢复 FastCAS 密码':'重置 FastCAS 双重验证'}</h2>{complete?<><p role="status">{flow==='invite'?'账号已创建':flow==='recovery'?'密码已更新':'双重验证已重置'}。现在可以登录 FastCAS。</p><a href="/login">前往登录</a></>:<>
 <p>此操作仅针对 FastCAS 身份。各项目账号和本地登录保持独立。</p>
 <div className="flow-choices"><button type="button" aria-current={flow==='invite'?'page':undefined} onClick={()=>{setFlow('invite');setError('')}}>邀请注册</button><button type="button" aria-current={flow==='recovery'?'page':undefined} onClick={()=>{setFlow('recovery');setError('')}}>密码恢复</button><button type="button" aria-current={flow==='mfa_reset'?'page':undefined} onClick={()=>{setFlow('mfa_reset');setError('')}}>双重验证重置</button></div>
 <form onSubmit={e=>void redeem(e)}><label>一次性凭证<input autoComplete="off" required value={token} onChange={e=>setToken(e.target.value)}/></label>
 {flow==='invite'&&<label>姓名<input autoComplete="name" required value={name} onChange={e=>setName(e.target.value)}/></label>}
 <label>{flow==='mfa_reset'?'当前 FastCAS 密码':'新密码'}<input type="password" autoComplete={flow==='mfa_reset'?'current-password':'new-password'} required minLength={12} value={password} onChange={e=>setPassword(e.target.value)}/></label>
 {flow!=='mfa_reset'&&<label>确认新密码<input type="password" autoComplete="new-password" required minLength={12} value={confirmation} onChange={e=>setConfirmation(e.target.value)}/></label>}
 {error&&<p role="alert" className="error">{error}</p>}<button className="primary" disabled={busy}>{busy?'正在提交…':flow==='invite'?'创建 FastCAS 账号':flow==='recovery'?'更新 FastCAS 密码':'重置双重验证'}</button></form>
 </>}</section></div>
}
