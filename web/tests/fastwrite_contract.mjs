import assert from 'node:assert/strict';
import {chromium} from 'playwright-core';

const {FASTCAS_CONTRACT_WRITE_ORIGIN:origin,FASTCAS_CONTRACT_WRITE_USER_ID:userId,FASTCAS_CONTRACT_WRITE_PROJECT_ID:projectId}=process.env;
if(!origin||!userId||!projectId)throw Error('FastWrite browser contract configuration missing');
const browser=await chromium.launch({executablePath:process.env.CHROME_BIN||'/usr/bin/google-chrome',headless:true,args:['--no-sandbox']});
const first=await browser.newContext({viewport:{width:1440,height:900}});
const page=await first.newPage();page.setDefaultTimeout(10000);
async function authorize(target){
 await target.locator('input[name=email]').fill('alice@example.test');
 await target.locator('input[name=password]').fill('correct horse battery staple');
 await target.getByRole('button',{name:'登录',exact:true}).click();
 await target.getByRole('button',{name:'确认并继续'}).click();
}
async function me(target){return target.evaluate(()=>fetch('/api/auth/me',{headers:{authorization:`Bearer ${localStorage.getItem('fastwrite.session-token')}`}}).then(response=>response.json()))}
try{
 await page.goto(origin+'/projects',{waitUntil:'domcontentloaded'});
 await page.getByRole('button',{name:'Sign in'}).first().click();
 await page.getByRole('dialog',{name:'Sign in'}).waitFor();
 await page.getByLabel('Email').fill('alice@example.test');
 await page.getByLabel('Password').fill('local-password-contract-32-characters');
 await page.getByRole('dialog',{name:'Sign in'}).getByRole('button',{name:'Sign in',exact:true}).click();
 await page.getByRole('button',{name:'Account authentication'}).waitFor();
 assert.equal((await me(page)).id,userId);
 await page.getByText('Browser preserved writing project').waitFor();
 await page.getByRole('button',{name:'Account authentication'}).click();
 await page.getByText('This account is not authenticated with FastCAS.').waitFor();
 await page.getByLabel('Confirm your FastWrite password').fill('local-password-contract-32-characters');
 await page.getByRole('button',{name:'Authenticate this account with FastCAS'}).click();
 await authorize(page);
 await page.getByRole('button',{name:'Account authentication'}).waitFor();
 await page.getByRole('button',{name:'Account authentication'}).click();
 await page.getByText('This account is authenticated with FastCAS.').waitFor();

 const second=await browser.newContext({viewport:{width:1440,height:900}});
 try{
  const login=await second.newPage();login.setDefaultTimeout(10000);
  await login.route('**/api/auth/refresh',async route=>{await new Promise(resolve=>setTimeout(resolve,1200));await route.continue()});
  await login.goto(origin+'/projects',{waitUntil:'domcontentloaded'});
  await login.getByRole('button',{name:'Sign in'}).first().click();
  await login.getByRole('button',{name:'Continue with FastCAS'}).click();
  await authorize(login);
  await login.getByRole('button',{name:'Account authentication'}).waitFor();
  assert.equal((await me(login)).id,userId,'FastCAS entered another FastWrite account');
  await login.getByText('Browser preserved writing project').waitFor();
  await page.getByLabel('Confirm your FastWrite password').fill('local-password-contract-32-characters');
  await page.getByRole('button',{name:'Remove FastCAS authentication'}).click();
  await page.getByText('This account is not authenticated with FastCAS.').waitFor();
  assert.equal(await login.evaluate(()=>fetch('/api/auth/me',{headers:{authorization:`Bearer ${localStorage.getItem('fastwrite.session-token')}`}}).then(response=>response.status)),401);
  assert.equal((await me(page)).id,userId,'local login lost after unlink');
  assert.equal(await page.evaluate(id=>fetch(`/api/projects/${id}`,{headers:{authorization:`Bearer ${localStorage.getItem('fastwrite.session-token')}`}}).then(response=>response.status),projectId),200);
 }finally{await second.close()}
 console.log('FastWrite browser contract passed: local login, explicit link, FastCAS login, project and account preservation, revoke isolation');
}catch(error){console.error('browser state:',page.url(),(await page.locator('body').innerText().catch(()=>'' )).slice(0,700));throw error}finally{await browser.close()}
