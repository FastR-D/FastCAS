import assert from 'node:assert/strict';
import {chromium} from 'playwright-core';

const {FASTCAS_CONTRACT_READ_ORIGIN:origin}=process.env;
if(!origin)throw Error('FastRead browser contract configuration missing');
const browser=await chromium.launch({executablePath:process.env.CHROME_BIN||'/usr/bin/google-chrome',headless:true,args:['--no-sandbox']});
const first=await browser.newContext({viewport:{width:1440,height:900}});
const page=await first.newPage();
page.setDefaultTimeout(10000);
const account=target=>target.getByRole('button',{name:'alice@example.test',exact:true});
async function authorize(target){
 await target.locator('input[name=email]').fill('alice@example.test');
 await target.locator('input[name=password]').fill('correct horse battery staple');
 await target.getByRole('button',{name:'登录',exact:true}).click();
 await target.getByRole('button',{name:'确认并继续'}).click();
 await account(target).waitFor();
}
async function me(target){return target.evaluate(()=>fetch('/api/auth/me').then(response=>response.json()))}
async function papers(target){return target.evaluate(()=>fetch('/api/papers').then(response=>response.json()))}
try{
 await page.goto(origin,{waitUntil:'domcontentloaded'});
 await page.getByRole('heading',{name:'登录资料库'}).waitFor();
 await page.getByLabel('邮箱').fill('alice@example.test');
 await page.getByLabel('密码').fill('local-original-password');
 await page.getByRole('button',{name:'确认',exact:true}).click();
 await account(page).waitFor();
 const local=await me(page);
 assert.ok(local.workspace_id&&local.email==='alice@example.test');
 assert.ok((await papers(page)).items.some(paper=>paper.title==='Browser preserved paper'),'original paper missing');
 await account(page).click();
 await page.getByRole('heading',{name:'FastCAS 账号认证'}).waitFor();
 await page.getByText('尚未认证',{exact:true}).waitFor();
 await page.getByLabel('当前账号密码').fill('local-original-password');
 await page.getByRole('button',{name:'认证当前账号'}).click();
 await authorize(page);
 await account(page).click();
 await page.getByText('已通过 FastCAS 认证').waitFor();

 const second=await browser.newContext({viewport:{width:1440,height:900}});
 try{
  const login=await second.newPage();
  login.setDefaultTimeout(10000);
  await login.goto(origin,{waitUntil:'domcontentloaded'});
  await login.getByRole('link',{name:'使用 FastCAS 登录'}).click();
  await authorize(login);
  const cas=await me(login);
  assert.equal(cas.workspace_id,local.workspace_id,'FastCAS entered another workspace');
  assert.equal(cas.role,local.role,'FastCAS changed project role');
  assert.ok((await papers(login)).items.some(paper=>paper.title==='Browser preserved paper'),'FastCAS login lost original paper');
  await page.getByLabel('当前账号密码').fill('local-original-password');
  await page.getByRole('button',{name:'解除认证'}).click();
  await page.getByText('尚未认证',{exact:true}).waitFor();
  assert.equal(await login.evaluate(()=>fetch('/api/auth/me').then(response=>response.status)),401);
  assert.equal((await me(page)).workspace_id,local.workspace_id,'local workspace lost after unlink');
 }finally{await second.close()}
 console.log('FastRead browser contract passed: local login, explicit link, FastCAS login, paper and workspace preservation, revoke isolation');
}catch(error){
 console.error('browser state:',page.url(),(await page.locator('body').innerText().catch(()=>'' )).slice(0,700));
 throw error;
}finally{await browser.close()}
