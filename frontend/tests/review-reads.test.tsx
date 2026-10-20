import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createHttpClient, jsonPost } from '../src/api/client';
import { SessionBoundary } from '../src/api/session-boundary';
import { readMediaChanges, type MediaSnapshot } from '../src/features/media/readMediaChanges';
import type { ProjectMedia } from '../src/api/project-media';

test('pending GETs coalesce without caching, crossing writes, sharing mutable values or losing session fencing', async () => {
  let calls = 0; const releases: ((value: Response) => void)[] = [];
  const fetcher = async () => { calls++; return new Promise<Response>(resolve => releases.push(resolve)); };
  const boundary = new SessionBoundary(), client = createHttpClient(fetcher, boundary);
  const first = client.json<any>('/same'), second = client.json<any>('/same');
  assert.equal(calls, 1); releases.shift()!(Response.json({value: 1}));
  const [a,b] = await Promise.all([first,second]); a.value = 2; assert.equal(b.value,1);
  const next = client.json<any>('/same'); assert.equal(calls,2);
  const writeClient = createHttpClient(async () => Response.json({saved:true}), boundary);
  await writeClient.json('/write',jsonPost({revision:1}));
  const afterWrite = client.json<any>('/same'); assert.equal(calls,3);
  releases.shift()!(Response.json({value:'old'})); releases.shift()!(Response.json({value:'fresh'}));
  assert.equal((await next).value,'old'); assert.equal((await afterWrite).value,'fresh');
  const stale = client.json('/same'); boundary.reset();
  const current = client.json('/same'); assert.equal(calls,5);
  releases.shift()!(Response.json({private:'old'})); releases.shift()!(Response.json({private:'new'}));
  await assert.rejects(stale,(e:any)=>e.code==='SESSION_CHANGED'); assert.deepEqual(await current,{private:'new'});
});
const task = (id:string, assetId:string, status:ProjectMedia['status']='succeeded', revision=1): ProjectMedia =>
  ({id,projectId:'p',kind:'image',target:{assetId},status,revision,credit:null,result:null} as ProjectMedia);
function pages(fetcher:(url:URL)=>unknown) {globalThis.fetch=async url=>Response.json(fetcher(new URL(String(url),'http://fixture.invalid')));}
test('older active target is refreshed by ID, cold discovery reaches page two, unknown and newer revisions survive',async()=>{
  let list=0,get=0;
  const old:MediaSnapshot={head:'boundary',items:[task('boundary','B'),task('active','A','running',4)]};
  pages(url=>{
    if(url.pathname.endsWith('/active')) {get++;return task('active','A','succeeded',5);}
    list++; return url.searchParams.has('cursor')
      ? {items:[task('boundary','B')],nextCursor:null}
      : {items:Array.from({length:100},(_,i)=>task('new'+i,'other'+i)),nextCursor:'older'};
  });
  let result=await readMediaChanges('p','image',old,()=>true,{assetId:'A'});
  assert.equal(list,2);assert.equal(get,1);assert.equal(result.items.find(t=>t.id==='active')?.status,'succeeded');
  list=0;pages(url=>{list++;return url.searchParams.has('cursor') ? {items:[task('active','A','unknown',4)],nextCursor:null} : {items:[task('other','B')],nextCursor:'older'};});
  result=await readMediaChanges('p','image',{items:[]},()=>true,{assetId:'A'});
  assert.equal(list,2);assert.equal(result.items.find(t=>t.id==='active')?.status,'unknown');
  pages(url=>url.pathname.endsWith('/active')?task('active','A','succeeded',3):{items:[task('boundary','B')],nextCursor:'older'});
  result=await readMediaChanges('p','image',{head:'boundary',items:[task('active','A','unknown',4)]},()=>true);
  assert.equal(result.items.find(t=>t.id==='active')?.status,'unknown');
});
test('card history scans once, subsequent hints read only new history, and abandoned scopes stop pagination',async()=>{
  let reads=0;
  pages(url=>{reads++; const page=Number(url.searchParams.get('cursor')||0);return {items:Array.from({length:100},(_,i)=>task(String(page*100+i),'asset'+i)),nextCursor:page<9?String(page+1):null};});
  const initial=await readMediaChanges('p','image',{items:[]},()=>true);assert.equal(reads,10);assert.equal(initial.items.length,1000);
  const refreshed=await readMediaChanges('p','image',initial,()=>true);assert.equal(reads,11);assert.equal(refreshed.items.length,1000);
  let alive=true;pages(()=>{reads++;alive=false;return {items:[task('new','A')],nextCursor:'next'};});
  await assert.rejects(readMediaChanges('p','image',{items:[]},()=>alive),{name:'AbortError'});assert.equal(reads,12);
});

test('project changes refresh terminal target views without dropping loaded history or its cursor',async()=>{
  const old:MediaSnapshot={head:'head',revision:1,items:[task('head','B'),{...task('old','A'),stale:false,adoptedOutputId:'previous'}]};
  let gets=0;
  pages(url=>{
    if(url.pathname.endsWith('/old')){gets++;return {...task('old','A'),stale:true,adoptedOutputId:''};}
    return {items:[task('head','B')],nextCursor:'older'};
  });
  const result=await readMediaChanges('p','image',old,()=>true,{assetId:'A'},2);
  assert.equal(gets,1);assert.equal(result.items.find(t=>t.id==='old')?.stale,true);
  assert.equal(result.items.find(t=>t.id==='old')?.adoptedOutputId,'');assert.equal(result.items.length,2);
});
