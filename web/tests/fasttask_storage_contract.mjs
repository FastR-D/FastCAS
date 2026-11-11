import assert from 'node:assert/strict';

const openKey='fasttask.research-assistant.open';
const widthKey='fasttask.research-assistant.width';
const profileKey='fasttask_offline_user';
const profileFields=['$schema','created_at','display_name','id','identifier','locale','revision','role','status','timezone','updated_at'];

export async function captureTaskStorage(page){
 return page.evaluate(()=>({local:Object.fromEntries(Object.entries(localStorage)),session:Object.fromEntries(Object.entries(sessionStorage))}));
}

export function assertTaskStorage(storage,profile){
 assert.deepEqual(Object.keys(storage.local).sort(),[openKey,widthKey,profileKey,'fasttask_refresh'],'unexpected browser storage');
 assert.deepEqual(Object.keys(storage.session),[],'unexpected session storage');
 // Task App writes String(boolean). ResearchWorkspace rounds and clamps width
 // to 320..800, then writes String(number). Its displayed width may be smaller
 // on a narrow workspace; End still persists 800. Read coercion is not a
 // licence to retain noncanonical strings or credentials in layout preferences.
 assert.ok(['true','false'].includes(storage.local[openKey]),'invalid assistant open preference');
 const width=storage.local[widthKey];
 assert.ok(typeof width==='string'&&/^[1-9]\d*$/.test(width)&&Number.isInteger(Number(width))&&Number(width)>=320&&Number(width)<=800,'invalid assistant width preference');
 const offlineUser=JSON.parse(storage.local[profileKey]);
 assert.deepEqual(Object.keys(offlineUser).sort(),profileFields,'offline user cache contains unexpected fields');
 assert.deepEqual(offlineUser,profile,'offline user cache differs from project profile');
}

// Exercise the exact checker used by the live flow against actual browser
// Storage. Only synthetic values are injected; restore before unlink/reload.
export async function verifyTaskStorageBoundary(page,profile){
 const original=await captureTaskStorage(page);
 assertTaskStorage(original,profile);
 const results=[];
 const set=async(area,key,value)=>page.evaluate(({area,key,value})=>{
  const storage=area==='local'?localStorage:sessionStorage;
  if(value===null)storage.removeItem(key);else storage.setItem(key,value);
 },{area,key,value});
 const restore=()=>page.evaluate(snapshot=>{
  localStorage.clear();sessionStorage.clear();
  for(const [key,value] of Object.entries(snapshot.local))localStorage.setItem(key,value);
  for(const [key,value] of Object.entries(snapshot.session))sessionStorage.setItem(key,value);
 },original);
 async function rejected(name,area,key,value,message){
  try{
   await set(area,key,value);
   const observed=await captureTaskStorage(page);
   assert.throws(()=>assertTaskStorage(observed,profile),message,`${name} was accepted`);
   results.push({name,rejected:true});
  }finally{await restore()}
 }
 try{
  for(const open of ['true','false'])for(const width of ['320','400','560','561','799','800']){
   await set('local',openKey,open);await set('local',widthKey,width);
   assertTaskStorage(await captureTaskStorage(page),profile);
   results.push({name:`canonical-${open}-${width}`,accepted:true});
  }
  await restore();
  for(const key of ['unexpected','fasttask.research-assistant.token','fasttask.research-assistant.open.extra','access_token','id_token','fastcas_token','fasttask_access','fasttask_refresh_backup'])
   await rejected(`unknown-local-${key}`,'local',key,'synthetic-credential',/unexpected browser storage/);
  for(const key of [openKey,widthKey,profileKey,'fasttask_refresh'])
   await rejected(`missing-${key}`,'local',key,null,/unexpected browser storage/);
  for(const value of ['','TRUE','False','1','0','null',' true','false ','synthetic-secret','{"token":"synthetic-secret"}'])
   await rejected(`invalid-open-${JSON.stringify(value)}`,'local',openKey,value,/invalid assistant open preference/);
  for(const value of ['','319','801','-320','320.5','800.5','0320','0800','+320','3.2e2','8e2','0x140',' 320','320 ','NaN','Infinity','synthetic-secret','{"token":"synthetic-secret"}'])
   await rejected(`invalid-width-${JSON.stringify(value)}`,'local',widthKey,value,/invalid assistant width preference/);
  for(const key of ['unexpected','access_token','id_token','fastcas_token','fasttask_refresh',openKey,widthKey])
   await rejected(`session-${key}`,'session',key,'synthetic-credential',/unexpected session storage/);
  for(const key of ['access_token','id_token','refresh_token','fastcas_token','credential','assistant_open'])
   await rejected(`profile-extra-${key}`,'local',profileKey,JSON.stringify({...profile,[key]:'synthetic-credential'}),/offline user cache contains unexpected fields/);
  for(const key of profileFields){
   const missing={...profile};delete missing[key];
   await rejected(`profile-missing-${key}`,'local',profileKey,JSON.stringify(missing),/offline user cache contains unexpected fields/);
   await rejected(`profile-changed-${key}`,'local',profileKey,JSON.stringify({...profile,[key]:'synthetic-mismatch'}),/offline user cache differs from project profile/);
  }
 }finally{await restore()}
 assert.deepEqual(await captureTaskStorage(page),original,'storage boundary probes did not restore the browser');
 assertTaskStorage(await captureTaskStorage(page),profile);
 return results;
}
