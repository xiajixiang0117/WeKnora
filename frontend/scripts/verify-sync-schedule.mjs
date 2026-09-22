import assert from 'node:assert/strict';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { createServer } from 'vite';
import vue from '@vitejs/plugin-vue';

const { chromium } = await import(pathToFileURL(process.env.BROWSERSKILL_TEST_PLAYWRIGHT).href);
const root = fileURLToPath(new URL('../', import.meta.url));
const entry = `
import {createApp,h,ref} from 'vue';
import TDesign from 'tdesign-vue-next';
import 'tdesign-vue-next/es/style/index.css';
import i18n from '/src/i18n/index.ts';
import Editor from '/src/views/knowledge/settings/SyncScheduleEditor.vue';
const value=ref('CRON_TZ=Asia/Shanghai 30 9 * * 1,3');
createApp({setup(){return ()=>h('main',{style:'max-width:480px;padding:24px;margin:auto'},[
 h(Editor,{modelValue:value.value,'onUpdate:modelValue':v=>value.value=v}),
 h('output',value.value)
])}}).use(i18n).use(TDesign).mount('#app');
`;
const server = await createServer({ root, configFile:false, optimizeDeps:{entries:[]}, plugins:[vue(),{
  name:'schedule-fixture',
  resolveId(id){if(id==='/schedule-fixture.js') return '\0schedule-fixture';},
  load(id){if(id==='\0schedule-fixture') return entry;},
  configureServer(server){server.middlewares.use(async(req,res,next)=>{
    if(req.url!=='/schedule-fixture.html') return next();
    res.setHeader('Content-Type','text/html');
    res.end(await server.transformIndexHtml(req.url,'<!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1"></head><body><div id="app"></div><script type="module" src="/schedule-fixture.js"></script></body></html>'));
  });}
}],resolve:{alias:{'@':root+'src'}},server:{host:'127.0.0.1',port:0}});
let browser;
try {
  await server.listen();
  browser=await chromium.launch({executablePath:process.env.BROWSERSKILL_TEST_CHROMIUM,headless:true});
  const page=await browser.newPage();
  const errors=[];
  page.on('pageerror',error=>errors.push(error.message));
  await page.addInitScript(()=>localStorage.setItem('locale','zh-CN'));
  await page.goto(server.resolvedUrls.local[0]+'schedule-fixture.html');
  await page.locator('.schedule-preview li').first().waitFor();
  assert.equal(await page.locator('.schedule-preview li').count(),3);
  await page.locator('input[type=time]').fill('10:45');
  await page.locator('input[type=time]').dispatchEvent('change');
  await page.waitForFunction(()=>document.querySelector('output').textContent.includes('45 10'));
  for(const width of [1280,390]) {
    await page.setViewportSize({width,height:800});
    assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'no horizontal overflow');
    await page.screenshot({path:'/tmp/weknora-sync-schedule-'+width+'.png',fullPage:true});
  }
  await page.locator('.sync-schedule-editor .t-select').first().click();
  await page.getByText('高级 Cron 表达式',{exact:true}).click();
  const input=page.locator('.schedule-field input').first();
  await input.fill('bad cron');
  await input.blur();
  await page.getByRole('alert').waitFor();
  await input.fill('0 9 * * 1-5');
  await input.blur();
  await page.locator('.schedule-preview li').first().waitFor();
  assert.equal(await page.getByRole('alert').count(),0);
  assert.deepEqual(errors,[]);
  console.log('PASS: visual weekly editing, Chinese preview, advanced validation, desktop/mobile layout');
} finally {
  await browser?.close();
  await server.close();
}
