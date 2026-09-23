import assert from 'node:assert/strict';
import {chromium} from 'playwright-core';

const {FASTCAS_CONTRACT_NEWS_ORIGIN:origin}=process.env;
if(!origin)throw Error('FastNews browser contract configuration missing');
const browser=await chromium.launch({executablePath:process.env.CHROME_BIN||'/usr/bin/google-chrome',headless:true,args:['--no-sandbox']});
const first=await browser.newContext({viewport:{width:1440,height:900}});
const page=await first.newPage();
page.setDefaultTimeout(10000);
async function authorize(target){
 await target.locator('input[name=email]').fill('alice@example.test');
 await target.locator('input[name=password]').fill('correct horse battery staple');
 await target.getByRole('button',{name:'登录',exact:true}).click();
 await target.getByRole('button',{name:'确认并继续'}).click();
}
async function me(target){return target.evaluate(()=>fetch('/api/auth/me').then(response=>response.json()))}
async function impression(target){return target.evaluate(()=>fetch('/api/content/impression').then(response=>response.json()))}
try{
 const sections=['/','/top-conf/index.html','/secnews/index.html','/field-briefing/index.html','/summary-brief/index.html','/inbox/index.html','/impression/index.html'];
 for(const section of sections){
  const response=await page.goto(origin+section,{waitUntil:'domcontentloaded'});
  assert.equal(response.status(),200,`generated page ${section} is unavailable`);
  await page.locator('.app-shell').waitFor({state:'visible'});
  assert.equal(await page.locator('#fastnews-account-link').count(),1,`account entry missing on ${section}`);
  assert.equal(await page.locator('#fastnews-account-link').getAttribute('href'),section==='/'?'login':'../login');
 }
 await page.locator('#fastnews-account-link').click();
 await page.waitForURL(origin+'/login');
 await page.getByRole('heading',{name:'登录 FastNews'}).waitFor();
 await page.getByLabel('邮箱').fill('alice@example.test');
 await page.getByLabel('密码',{exact:true}).fill('original-local-password');
 await page.locator('#submit').click();
 await page.waitForURL(origin+'/');
 await page.locator('.app-shell').waitFor({state:'visible'});
 await page.goto(origin+'/login',{waitUntil:'domcontentloaded'});
 await page.getByText('当前账号：alice@example.test').waitFor();
 const local=await me(page);
 assert.equal(local.source,'local');
 assert.equal((await impression(page)).impression.text,'Browser preserved research notes');
 await page.getByText('尚未认证',{exact:true}).waitFor();
 await page.getByLabel('本地密码').fill('original-local-password');
 await page.getByRole('button',{name:'认证当前账号'}).click();
 await authorize(page);
 await page.getByText('已通过 FastCAS 认证').waitFor();

 const second=await browser.newContext({viewport:{width:1440,height:900}});
 try{
  const login=await second.newPage();
  login.setDefaultTimeout(10000);
  await login.goto(origin+'/login',{waitUntil:'domcontentloaded'});
  await login.getByRole('link',{name:'使用 FastCAS 登录'}).click();
  await authorize(login);
  await login.getByText('当前账号：alice@example.test').waitFor();
  const cas=await me(login);
  assert.equal(cas.user_id,local.user_id,'FastCAS entered another local account');
  assert.equal(cas.source,'fastcas');
  assert.equal((await impression(login)).impression.text,'Browser preserved research notes','FastCAS login lost original content');
  await page.getByLabel('本地密码').fill('original-local-password');
  await page.getByRole('button',{name:'解除认证'}).click();
  await page.getByText('尚未认证',{exact:true}).waitFor();
  assert.equal(await login.evaluate(()=>fetch('/api/auth/me').then(response=>response.status)),401);
  assert.equal((await me(page)).user_id,local.user_id,'local account lost after unlink');
  assert.equal((await impression(page)).impression.text,'Browser preserved research notes');
 }finally{await second.close()}
 console.log('FastNews browser contract passed: seven generated pages, account navigation, local login, explicit link, FastCAS login, personal content and account preservation, revoke isolation');
}catch(error){
 console.error('browser state:',page.url(),(await page.locator('body').innerText().catch(()=>'' )).slice(0,700));
 throw error;
}finally{await browser.close()}
