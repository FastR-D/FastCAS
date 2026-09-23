import assert from 'node:assert/strict';
import {chromium} from 'playwright-core';

const {FASTCAS_CONTRACT_TASK_ORIGIN: origin, FASTCAS_CONTRACT_ISSUER: issuer} = process.env;
if(!origin||!issuer)throw Error('FastTask browser contract configuration missing');
const browser=await chromium.launch({executablePath:process.env.CHROME_BIN||'/usr/bin/google-chrome',headless:true,args:['--no-sandbox']});
const first=await browser.newContext({viewport:{width:1440,height:900}});
const page=await first.newPage();
page.setDefaultTimeout(10000);
const meResponse=target=>target.waitForResponse(response=>response.url()===`${origin}/api/v1/me`&&response.status()===200);

async function signInAtCAS(target){
 await target.locator('input[name=email]').fill('alice@example.test');
 await target.locator('input[name=password]').fill('correct horse battery staple');
 await target.getByRole('button',{name:'登录',exact:true}).click();
 await target.getByRole('button',{name:'确认并继续'}).click();
}
async function openAccount(target){
 await target.locator('mdui-navigation-rail-item[value="account"]').click();
 await target.getByRole('heading',{name:'账号认证'}).waitFor();
}
async function fillMDUI(target,label,value){
 const input=target.locator(`mdui-text-field[label="${label}"]`).locator('input');
 await input.fill(value);
 await input.press('Tab');
}

try{
 await page.goto(origin,{waitUntil:'networkidle'});
 await fillMDUI(page,'账号','admin');
 await fillMDUI(page,'密码','password-for-tests');
 const localMe=meResponse(page);
 await page.getByText('开始今天',{exact:true}).click();
 const local=await(await localMe).json();
 assert.ok(local?.id&&local?.role==='admin',`local administrator missing: ${JSON.stringify(local)}`);
 await openAccount(page);
 await page.getByText('当前账号未认证').waitFor();
 await fillMDUI(page,'确认本地 FastTask 密码','password-for-tests');
 await page.getByText('认证当前账号',{exact:true}).click();
 await signInAtCAS(page);
 await page.locator('mdui-navigation-rail-item[value="account"]').waitFor();
 await openAccount(page);
 await page.getByText('已完成 FastCAS 认证').waitFor();

 const second=await browser.newContext({viewport:{width:1440,height:900}});
 try{
  const login=await second.newPage();
  login.setDefaultTimeout(10000);
  await login.goto(origin,{waitUntil:'networkidle'});
  const casMe=meResponse(login);
  await login.getByText('使用 FastCAS 登录',{exact:true}).click();
  await signInAtCAS(login);
  const cas=await(await casMe).json();
  assert.equal(cas.id,local.id,'FastCAS login entered another project account');
  assert.equal(cas.role,local.role,'FastCAS login changed project role');
  await openAccount(login);
  await login.getByText('已完成 FastCAS 认证').waitFor();
  const storage=await login.evaluate(()=>({
   keys:Object.keys(localStorage).sort(),
   offlineUser:localStorage.getItem('fasttask_offline_user'),
   sessionKeys:Object.keys(sessionStorage),
  }));
  assert.deepEqual(storage.keys,['fasttask_offline_user','fasttask_refresh'],'unexpected browser storage');
  assert.deepEqual(storage.sessionKeys,[],'unexpected session storage');
  const offlineUser=JSON.parse(storage.offlineUser);
  assert.deepEqual(Object.keys(offlineUser).sort(),['$schema','created_at','display_name','id','identifier','locale','revision','role','status','timezone','updated_at'],'offline user cache contains unexpected fields');
  assert.deepEqual(offlineUser,cas,'offline user cache differs from project profile');
  await fillMDUI(page,'确认本地 FastTask 密码','password-for-tests');
  await page.getByText('解除 FastCAS 认证',{exact:true}).click();
  await page.getByText('当前账号未认证').waitFor();
  await login.reload({waitUntil:'networkidle'});
  await login.getByText('使用 FastCAS 登录',{exact:true}).waitFor();
  const restoredMe=meResponse(page);
  await page.reload({waitUntil:'networkidle'});
  assert.equal((await(await restoredMe).json()).id,local.id,'local session did not survive unlink');
 }finally{await second.close()}
 console.log('FastTask browser contract passed: local administrator, explicit link, FastCAS login, revoke isolation, stable user and role');
}catch(error){
 console.error('browser state:',page.url(),(await page.locator('body').innerText().catch(()=>'' )).slice(0,700));
 throw error;
}finally{await browser.close()}
