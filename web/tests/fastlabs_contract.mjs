import assert from 'node:assert/strict';
import {chromium} from 'playwright-core';

const {FASTCAS_CONTRACT_LABS_ORIGIN:origin,FASTCAS_CONTRACT_ISSUER:issuer,FASTCAS_CONTRACT_SUBJECT:subject}=process.env;
if(!origin||!issuer||!subject)throw Error('FastLabs browser contract configuration missing');
const browser=await chromium.launch({executablePath:process.env.CHROME_BIN||'/usr/bin/google-chrome',headless:true,args:['--no-sandbox']});
const context=await browser.newContext({viewport:{width:1280,height:900}});
const page=await context.newPage();page.setDefaultTimeout(10000);
let approval;
async function status(){return page.evaluate(()=>fetch('/api/fastcas/status').then(response=>response.json()))}
async function tasks(){return page.evaluate(()=>fetch('/api/tasks').then(response=>response.json()))}
try{
 await page.goto(origin,{waitUntil:'domcontentloaded'});
 await page.locator('button.settings-button[data-action=settings]').click();
 await page.getByRole('heading',{name:'FastCAS 关联'}).waitFor();
 const initial=await status();
 const originalTasks=(await tasks()).tasks;
 assert.equal(initial.link,null);
 await page.locator('button[data-action=fastcas-pair]').click();
 await page.getByRole('link',{name:'FastCAS 设备确认页'}).waitFor();
 const pending=await status();
 assert.ok(pending.pairing?.user_code);
 assert.equal(pending.link,null,'pairing started without local confirmation');
 [approval]=await Promise.all([context.waitForEvent('page'),page.getByRole('link',{name:'FastCAS 设备确认页'}).click()]);
 approval.setDefaultTimeout(10000);
 await approval.getByRole('link',{name:'登录'}).click();
 await approval.locator('input[name=email]').fill('alice@example.test');
 await approval.locator('input[name=password]').fill('correct horse battery staple');
 await approval.getByRole('button',{name:'登录',exact:true}).click();
 await approval.getByRole('heading',{name:'确认设备'}).waitFor();
 await approval.getByText(pending.pairing.user_code,{exact:true}).waitFor();
 await approval.getByRole('button',{name:'允许这台设备'}).click();
 await approval.getByText('设备请求已处理').waitFor();
 assert.equal((await status()).link,null,'provider approval bypassed local confirmation');
 await page.locator('button[data-action=fastcas-confirm]').waitFor({timeout:20000});
 assert.equal((await status()).link,null,'polling bypassed local confirmation');
 await page.locator('button[data-action=fastcas-confirm]').click();
 await page.getByText('已关联身份：'+subject).waitFor();
 const linked=await status();
 assert.equal(linked.link.subject,subject);
 assert.equal(linked.link.installation_id,initial.installation_id);
 assert.deepEqual((await tasks()).tasks,originalTasks,'pairing changed local tasks');
 await approval.goto(issuer+'/console/',{waitUntil:'domcontentloaded'});
 await approval.getByRole('button',{name:'本机安装'}).click();
 const installation=approval.locator('article').filter({hasText:initial.installation_id});
 await installation.getByText('FastLab desktop').waitFor();
 await installation.getByText('关联于').waitFor();
 const oldest=approval.getByText('550e8400-e29b-41d4-a716-000000000001');
 assert.equal(await oldest.count(),0,'oldest installation appeared before pagination');
 await approval.getByRole('button',{name:'加载更多安装'}).click();
 await oldest.waitFor();
 const oldInstallation=approval.locator('article').filter({hasText:'550e8400-e29b-41d4-a716-000000000001'});
 approval.once('dialog',dialog=>dialog.accept());
 await oldInstallation.getByRole('button',{name:'撤销关联'}).click();
 await oldInstallation.getByText('已撤销').waitFor();
 approval.once('dialog',dialog=>dialog.accept());
 await installation.getByRole('button',{name:'撤销关联'}).click();
 await installation.getByText('已撤销').waitFor();
 assert.equal((await status()).link.subject,subject,'center revoke changed local state without a local status check');
 const began=Date.now();let converged=false;
 while(Date.now()-began<25000){if((await status()).link===null){converged=true;break}await page.waitForTimeout(500)}
 assert.equal(converged,true,'local status did not converge after center revoke');
 const revokeConvergenceMs=Date.now()-began;
 await page.locator('button[data-action=fastcas-pair]').waitFor();
 assert.equal((await status()).link,null);
 assert.deepEqual((await tasks()).tasks,originalTasks,'center revoke changed local tasks');
 console.log(`FastLabs browser contract passed: device approval, local confirmation, paged central installation list/revoke, local status convergence ${revokeConvergenceMs}ms and task isolation`);
}catch(error){console.error('browser state:',page.url(),(await page.locator('body').innerText().catch(()=>'' )).slice(0,700));console.error('local state:',await page.evaluate(async()=>({remote:await fetch('/api/fastcas/status').then(r=>r.json()),view:state.fastcas,dirty:state.mainDirty,active:document.activeElement?.tagName,selection:window.getSelection()?.toString()})).catch(()=>null));console.error('FastCAS state:',approval?.url()||'unopened',approval?(await approval.locator('body').innerText().catch(()=>'' )).slice(0,700):'');throw error}finally{await browser.close()}
