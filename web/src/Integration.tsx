import {useEffect,useState} from 'react';

type API=<T>(path:string)=>Promise<T>;
type Application={id:string;name:string;redirect_uris:string[];grant_types:string[];scopes:string[];resources:string[];public:boolean;events_uri?:string;backchannel_logout_uri?:string};

export function Integration({api,busy,run}:{api:API;busy:boolean;run:(work:()=>Promise<void>)=>Promise<void>}){
 const [issuer,setIssuer]=useState('');
 const [issuerError,setIssuerError]=useState('');
 const [applications,setApplications]=useState<Application[]>([]);
 const [cursor,setCursor]=useState('');
 async function load(after=''){
  const items=await api<Application[]>('/admin/applications'+(after?'?after='+encodeURIComponent(after):''));
  setApplications(old=>after?[...old,...items]:items);
  setCursor(items.length===100?items[items.length-1].id:'');
 }
 useEffect(()=>{
  void run(()=>load());
  void fetch('/.well-known/openid-configuration',{credentials:'same-origin'})
   .then(async response=>{if(!response.ok)throw new Error('Discovery 不可用');return response.json() as Promise<{issuer:string}>})
   .then(data=>setIssuer(data.issuer))
   .catch(()=>setIssuerError('无法读取当前 issuer，请检查 FastCAS 的服务配置'));
 },[]);
 return <>
  <div className="grid">
   <section><h2>接入顺序</h2><ol>
    <li>在“应用管理”登记每个部署实例，配置精确回调地址、允许的 scope 与资源受众。不同实例使用不同客户端 ID。</li>
    <li>服务端安装同版本 SDK，保存只显示一次的客户端密钥；浏览器代码只能使用无密钥入口。</li>
    <li>先保留项目原登录和注册，再分别接入“认证已有账号”与“使用 FastCAS 登录”。登录未找到绑定时由项目按原政策让用户选择。</li>
    <li>在项目数据库持久化一次性事务与绑定版本，接收签名撤销和退出通知；仅终止 FastCAS 来源会话。</li>
    <li>使用真实项目与 FastCAS 联调，再运行全项目契约门禁。配置显示为已登记，不代表端点已连通。</li>
   </ol></section>
   <section><h2>提供方信息</h2><p>Issuer 以 OIDC discovery 的实际响应为准。</p>
    {issuer?<p><code>{issuer}</code></p>:<p>{issuerError||'正在读取…'}</p>}
    <p><a href="/.well-known/openid-configuration" target="_blank" rel="noreferrer">查看 OIDC discovery</a></p>
    <p>当前三语言 SDK 为 0.1.0 本地开发版本，尚未发布 npm、PyPI 或 Go tag。生产接入前应固定同一组已验收的版本并完成包发布。</p>
   </section>
  </div>
  <section><h2>三语言 SDK</h2><p>以下命令用于当前本地工作区；部署环境应使用经过验证的正式包版本。</p>
   <div className="grid">
    <div><h3>TypeScript · Node/Bun</h3><pre>cd FastCAS/sdk/typescript &amp;&amp; npm ci &amp;&amp; npm run build</pre><p>服务端入口为 <code>@fastrd/fastcas/server</code>，浏览器入口为 <code>@fastrd/fastcas/browser</code>。</p></div>
    <div><h3>Go · net/http/Gin</h3><pre>go mod edit -replace github.com/FastR-D/FastCAS/sdk/go=/absolute/path/FastCAS/sdk/go</pre><p>生产代码使用持久事务存储；Gin 通知适配器在 <code>sdk/go/ginadapter</code>。</p></div>
    <div><h3>Python · sync/async/FastAPI</h3><pre>python -m pip install -e ./FastCAS/sdk/python</pre><p>FastAPI 适配器为可选依赖；其他 Python 服务可以直接使用 SDK。</p></div>
   </div>
   <p>可运行的服务身份示例位于 <code>FastCAS/examples/service-tokens</code>；账号接入参考实现位于 FastWrite、FastTask、FastRead、FastResearch 和 FastNews 的认证模块。</p>
  </section>
  <section><h2>应用登记状态</h2><p>这里显示已登记配置，不探测项目健康状态，也不展示客户端密钥。修改请使用“应用管理”。</p>
   {!applications.length&&<p>暂无已登记应用。</p>}
   {applications.map(item=><article key={item.id}><div><strong>{item.name}</strong><p><code>{item.id}</code> · {item.public?'公开客户端':'保密客户端'}</p>
    <small>授权：{item.grant_types?.join(', ')||'未设置'}<br/>回调：{item.redirect_uris?.length?`${item.redirect_uris.length} 个已登记`:'未登记'}<br/>绑定事件：{item.events_uri?'已登记':'未登记'} · 退出通知：{item.backchannel_logout_uri?'已登记':'未登记'}<br/>Scope：{item.scopes?.join(', ')||'无'} · 资源：{item.resources?.join(', ')||'无'}</small></div></article>)}
   {cursor&&<button disabled={busy} onClick={()=>void run(()=>load(cursor))}>加载更多应用配置</button>}
  </section>
  <section><h2>常见错误</h2><dl>
   <dt><code>invalid_grant</code></dt><dd>授权码已用、过期，或回调地址、PKCE 校验不一致；重新从项目发起，不重复兑换旧码。</dd>
   <dt><code>authentication_required</code></dt><dd>FastCAS 会话或近期身份确认失效；引导用户重新登录或验证。</dd>
   <dt><code>conflict</code></dt><dd>绑定关系与现有账号冲突；请用户登录原项目账号并走显式认证，不按相同邮箱自动合并。</dd>
   <dt>通知返回非 2xx</dt><dd>中心会重试并记录投递历史；项目接收端应按事件 ID 去重，并在本地事务完成后才返回成功。</dd>
  </dl></section>
 </>;
}
