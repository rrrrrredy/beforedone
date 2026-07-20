#!/usr/bin/env node

// FALLBACK ONLY. This renderer reconstructs terminal output in HTML and is not
// accepted launch media. Its outputs are isolated under
// media/fallback-html-output so it cannot overwrite the real window captures.

const fs = require("node:fs");
const path = require("node:path");
const { pathToFileURL } = require("node:url");
const { spawnSync } = require("node:child_process");
const { chromium } = require("playwright");

const root = path.resolve(__dirname, "..");
const mediaRoot = path.join(root, "media");
const source = path.join(mediaRoot, "source");
const media = path.join(mediaRoot, "fallback-html-output");
const rendered = path.join(media, "rendered");
const gallery = path.join(media, "gallery");
const timeline = JSON.parse(fs.readFileSync(path.join(source, "frames.json"), "utf8"));

const edgeCandidates = [
  process.env.BEFOREDONE_EDGE,
  "C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe",
  "C:\\Program Files\\Microsoft\\Edge\\Application\\msedge.exe",
].filter(Boolean);
const edge = edgeCandidates.find((candidate) => fs.existsSync(candidate));
if (!edge) {
  throw new Error("Microsoft Edge was not found. Set BEFOREDONE_EDGE to its executable.");
}

fs.mkdirSync(rendered, { recursive: true });
fs.mkdirSync(gallery, { recursive: true });

function escapeHTML(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}

function pngDataURL(file) {
  return `data:image/png;base64,${fs.readFileSync(file).toString("base64")}`;
}

function lineClass(line) {
  if (/^(PS>|\$)/.test(line)) return "prompt";
  if (/\bPASS\b|fresh=true|exit 0|SUPPORTED/.test(line)) return "pass";
  if (/\bFAIL\b|BLOCK|decision.*block|CONTRADICTED|-func/.test(line)) return "fail";
  if (/DRY RUN|exact_event|ignored|No verifier/.test(line)) return "warn";
  if (/^(Receipt|Fingerprint|HTML:|JSON:|Replay:|FOD:|First Observable)/.test(line)) return "datum";
  return "plain";
}

function terminalLines(content, animated = false) {
  return content.split("\n").map((line, index) => {
    const delay = animated ? ` style="--line-delay:${Math.min(index * 115, 1700)}ms"` : "";
    return `<div class="line ${lineClass(line)}"${delay}>${escapeHTML(line) || "&nbsp;"}</div>`;
  }).join("");
}

const commonCSS = `
  :root{color-scheme:dark;--ink:#f7f7f4;--muted:#a7acb7;--panel:#101216;--line:#2a2e35;--red:#e54836;--green:#36b86d;--yellow:#f0c752}
  *{box-sizing:border-box}html,body{width:100%;height:100%;margin:0;overflow:hidden;background:#090a0c;color:var(--ink);font-family:Inter,Segoe UI,Arial,sans-serif}
  body{background-image:linear-gradient(rgba(255,255,255,.024) 1px,transparent 1px),linear-gradient(90deg,rgba(255,255,255,.024) 1px,transparent 1px);background-size:36px 36px}
  .shell{height:100%;padding:28px 34px 24px;display:flex;flex-direction:column;gap:18px;background:radial-gradient(circle at 82% 8%,rgba(229,72,54,.10),transparent 30%)}
  .mast{height:48px;display:flex;align-items:center;justify-content:space-between;border-bottom:1px solid #32363e;padding-bottom:16px;letter-spacing:.08em;text-transform:uppercase;font-size:12px;color:var(--muted)}
  .brand{font-weight:800;font-size:22px;letter-spacing:-.04em;text-transform:none;color:var(--ink)}.brand b{color:var(--red)}
  .proof{display:flex;align-items:center;gap:9px}.proof:before{content:"";width:8px;height:8px;border-radius:50%;background:var(--green);box-shadow:0 0 0 5px rgba(54,184,109,.12)}
  .stage{flex:1;min-height:0;border:1px solid #373c45;background:rgba(13,15,18,.96);box-shadow:0 20px 70px rgba(0,0,0,.35);display:flex;flex-direction:column}
  .bar{height:56px;border-bottom:1px solid var(--line);display:flex;align-items:center;gap:14px;padding:0 20px;color:#c4c8d0;font:12px/1 ui-monospace,SFMono-Regular,Cascadia Code,Consolas,monospace;letter-spacing:.08em}
  .dots{display:flex;gap:7px;margin-right:8px}.dots i{width:10px;height:10px;border-radius:50%;display:block}.dots i:nth-child(1){background:#e54836}.dots i:nth-child(2){background:#f0c752}.dots i:nth-child(3){background:#36b86d}
  .bar .step{margin-left:auto;color:var(--red);font-weight:800}
  .terminal{flex:1;min-height:0;padding:24px 28px;overflow:hidden;font:16px/1.48 ui-monospace,SFMono-Regular,Cascadia Code,Consolas,monospace;white-space:pre-wrap;word-break:break-word}
  .line{min-height:1.48em}.line.prompt{color:#fff;font-weight:700}.line.pass{color:#62e693}.line.fail{color:#ff7667}.line.warn{color:#f5d46f}.line.datum{color:#a9c7ff}.line.plain{color:#cbd0d9}
  .audit{height:30px;display:flex;align-items:center;justify-content:space-between;color:#7f8591;font:11px/1 ui-monospace,SFMono-Regular,Cascadia Code,Consolas,monospace;text-transform:uppercase;letter-spacing:.08em}
`;

function galleryHTML(scene) {
  return `<!doctype html><html><head><meta charset="utf-8"><style>${commonCSS}
    .terminal{font-size:15px;line-height:1.43;padding:20px 26px}.line{min-height:1.43em}
  </style></head><body><main class="shell">
    <header class="mast"><span class="brand">BeforeDone<b>/</b></span><span class="proof">Recorded core run · ${timeline.run_id}</span></header>
    <section class="stage"><div class="bar"><span class="dots"><i></i><i></i><i></i></span><span>${escapeHTML(scene.label)}</span><span class="step">${scene.step} / 08</span></div><div class="terminal">${terminalLines(scene.content)}</div></section>
    <footer class="audit"><span>REAL OUTPUT · ${escapeHTML(scene.source)}</span><span>CLI SHA256 · ${timeline.cli_sha256.slice(0, 12)}</span></footer>
  </main></body></html>`;
}

function coverHTML(scene) {
  return `<!doctype html><html><head><meta charset="utf-8"><style>${commonCSS}
    .cover{height:100%;padding:34px 40px;display:grid;grid-template-columns:440px 1fr;gap:34px;align-items:center;background:radial-gradient(circle at 82% 15%,rgba(229,72,54,.15),transparent 34%)}
    .copy h1{font-size:71px;line-height:.92;letter-spacing:-.065em;margin:22px 0}.copy p{font-size:21px;line-height:1.35;color:#c8ccd4;max-width:390px}.eyebrow{color:var(--green);font:12px/1 ui-monospace,Consolas,monospace;letter-spacing:.16em;text-transform:uppercase}.copy .brand{font-size:25px}
    .stage{height:530px}.bar{font-size:11px}.terminal{font-size:12px;line-height:1.42;padding:22px}.line{min-height:1.42em}
  </style></head><body><main class="cover"><section class="copy"><span class="brand">BeforeDone<b>/</b></span><h1>Make agents<br>prove<br>they're done.</h1><p>Fresh evidence before completion. Replayable incidents after failure.</p><span class="eyebrow">Real Stop hook output →</span></section><section class="stage"><div class="bar"><span class="dots"><i></i><i></i><i></i></span><span>CODEX STOP HOOK · WIRE INVOCATION</span></div><div class="terminal">${terminalLines(scene.content)}</div></section></main></body></html>`;
}

function playerHTML(scenes) {
  const payload = JSON.stringify(scenes).replaceAll("<", "\\u003c");
  return `<!doctype html><html><head><meta charset="utf-8"><style>${commonCSS}
    .shell{padding:34px 52px 28px;gap:20px}.mast{height:54px;font-size:14px}.brand{font-size:27px}
    .stage{position:relative}.bar{height:68px;font-size:14px;padding:0 26px}.terminal{font-size:22px;line-height:1.45;padding:34px 42px}.line{min-height:1.45em;opacity:0;transform:translateY(6px);animation:reveal .3s forwards;animation-delay:var(--line-delay)}
    @keyframes reveal{to{opacity:1;transform:none}}
    .audit{height:28px;font-size:12px}.caption{height:70px;display:flex;align-items:center;justify-content:center;background:#f7f7f4;color:#111319;font-size:24px;font-weight:700;letter-spacing:-.012em;text-align:center;padding:0 42px;border-left:8px solid var(--red)}
    .progress{position:absolute;left:0;right:0;bottom:0;height:4px;background:#20242a}.progress i{height:100%;display:block;background:var(--red);width:0}
    .report{position:absolute;inset:68px 0 0;background:#090a0c;overflow:hidden}.report img{position:absolute;inset:0;width:100%;height:100%;object-fit:cover;object-position:top;transition:opacity .55s ease}.report img.timeline{opacity:0}.report.second img.fod{opacity:0}.report.second img.timeline{opacity:1}
    .site{position:absolute;inset:68px 0 0;overflow:hidden;background:#090a0c}.site>img{width:100%;height:100%;object-fit:cover;filter:brightness(.66)}.urls{position:absolute;left:56px;right:56px;bottom:46px;display:grid;gap:14px}.url{background:rgba(9,10,12,.94);border:1px solid #545b66;border-left:8px solid var(--red);padding:17px 22px;font:24px/1.2 ui-monospace,Consolas,monospace;color:#fff;box-shadow:0 14px 40px rgba(0,0,0,.35)}
  </style></head><body><main class="shell">
    <header class="mast"><span class="brand">BeforeDone<b>/</b></span><span class="proof">Recorded core run · ${timeline.run_id}</span></header>
    <section class="stage" id="stage"><div class="bar"><span class="dots"><i></i><i></i><i></i></span><span id="label"></span><span class="step" id="step"></span></div><div class="terminal" id="terminal"></div><div class="progress"><i id="progress"></i></div></section>
    <div class="caption" id="caption"></div>
    <footer class="audit"><span id="source"></span><span>REAL CLI · REAL HOOK · REAL REPORT</span></footer>
  </main><script>
    const scenes=${payload};
    let startedAt=0, active=-1, raf=0;
    const total=scenes.reduce((n,s)=>n+s.duration_ms,0);
    const stage=document.getElementById('stage'),terminal=document.getElementById('terminal');
    function esc(v){return String(v).replaceAll('&','&amp;').replaceAll('<','&lt;').replaceAll('>','&gt;').replaceAll('"','&quot;')}
    function cls(line){if(/^(PS>|\\$)/.test(line))return'prompt';if(/\\bPASS\\b|fresh=true|exit 0|SUPPORTED/.test(line))return'pass';if(/\\bFAIL\\b|BLOCK|decision.*block|CONTRADICTED|-func/.test(line))return'fail';if(/DRY RUN|exact_event|ignored|No verifier/.test(line))return'warn';if(/^(Receipt|Fingerprint|HTML:|JSON:|Replay:|FOD:|First Observable)/.test(line))return'datum';return'plain'}
    function setScene(index){
      active=index;const s=scenes[index];stage.querySelectorAll('.report,.site').forEach(n=>n.remove());
      document.getElementById('label').textContent=s.label;document.getElementById('step').textContent=s.step+' / 08';document.getElementById('caption').textContent=s.caption;document.getElementById('source').textContent='REAL OUTPUT · '+s.source;
      if(s.content){terminal.style.display='block';terminal.innerHTML=s.content.split('\\n').map((line,i)=>'<div class="line '+cls(line)+'" style="--line-delay:'+Math.min(i*115,1700)+'ms">'+(esc(line)||'&nbsp;')+'</div>').join('');}
      else{terminal.style.display='none';if(s.id==='incident-report'){const box=document.createElement('div');box.className='report';box.innerHTML='<img class="fod" src="'+s.images[0]+'"><img class="timeline" src="'+s.images[1]+'">';stage.appendChild(box)}else{const box=document.createElement('div');box.className='site';box.innerHTML='<img src="'+s.image+'"><div class="urls">'+s.urls.map(u=>'<div class="url">'+esc(u)+'</div>').join('')+'</div>';stage.appendChild(box)}}
    }
    function tick(now){
      const elapsed=Math.max(0,now-startedAt);let cursor=0,index=scenes.length-1,local=0;
      for(let i=0;i<scenes.length;i++){if(elapsed<cursor+scenes[i].duration_ms){index=i;local=elapsed-cursor;break}cursor+=scenes[i].duration_ms}
      if(index!==active)setScene(index);const scene=scenes[index];document.getElementById('progress').style.width=(Math.max(0,Math.min(1,local/scene.duration_ms))*100)+'%';
      const report=stage.querySelector('.report');if(report)report.classList.toggle('second',local>scene.duration_ms*.53);
      if(elapsed<total+250)raf=requestAnimationFrame(tick);
    }
    window.startDemo=()=>{cancelAnimationFrame(raf);startedAt=performance.now();active=-1;raf=requestAnimationFrame(tick)};
    setScene(0);
  <\/script></body></html>`;
}

async function screenshotHTML(page, html, output, viewport) {
  await page.setViewportSize(viewport);
  await page.setContent(html, { waitUntil: "load" });
  await page.screenshot({ path: output });
}

async function main() {
  const browser = await chromium.launch({ executablePath: edge, headless: true });
  const page = await browser.newPage({ viewport: { width: 1270, height: 760 }, deviceScaleFactor: 1 });
  const byID = Object.fromEntries(timeline.scenes.map((scene) => [scene.id, scene]));

  await screenshotHTML(page, galleryHTML(byID["stop-wire"]), path.join(gallery, "01-stop-hook-block.png"), { width: 1270, height: 760 });
  await screenshotHTML(page, galleryHTML(byID["fresh-pass"]), path.join(gallery, "02-fresh-pass-receipt.png"), { width: 1270, height: 760 });
  await screenshotHTML(page, galleryHTML(byID["replay-verify"]), path.join(gallery, "04-replay-verify-dry-run.png"), { width: 1270, height: 760 });
  await screenshotHTML(page, coverHTML(byID["stop-wire"]), path.join(media, "youtube-cover.png"), { width: 1280, height: 720 });

  const reportURL = pathToFileURL(path.join(source, "artifacts", "incident-report.html")).href;
  await page.setViewportSize({ width: 1270, height: 760 });
  await page.goto(reportURL, { waitUntil: "load" });
  await page.screenshot({ path: path.join(gallery, "03-incident-report.png") });

  await page.setViewportSize({ width: 1920, height: 1080 });
  await page.goto(reportURL, { waitUntil: "load" });
  await page.screenshot({ path: path.join(rendered, "incident-fod-video.png") });
  const timelineHeading = page.getByRole("heading", { name: "Timeline", exact: true });
  await timelineHeading.scrollIntoViewIfNeeded();
  await page.screenshot({ path: path.join(rendered, "incident-timeline-video.png") });

  await page.goto(pathToFileURL(path.join(root, "site", "index.html")).href, { waitUntil: "commit", timeout: 10_000 });
  await page.waitForTimeout(1_000);
  await page.screenshot({ path: path.join(rendered, "site-video.png") });
  await browser.close();

  const videoScenes = timeline.scenes.map((scene) => ({ ...scene }));
  const incident = videoScenes.find((scene) => scene.id === "incident-report");
  const incidentFrame = path.join(rendered, "incident-fod-video.png");
  incident.images = [pngDataURL(incidentFrame), pngDataURL(incidentFrame)];
  const links = videoScenes.find((scene) => scene.id === "public-links");
  links.image = pngDataURL(path.join(rendered, "site-video.png"));

  const playerPath = path.join(rendered, "player.html");
  fs.writeFileSync(playerPath, playerHTML(videoScenes));
  const videoDir = path.join(rendered, "video");
  fs.rmSync(videoDir, { recursive: true, force: true });
  fs.mkdirSync(videoDir, { recursive: true });

  // Build the video from browser-rendered frames instead of recording browser
  // startup. This guarantees the first frame is functional Stop-hook evidence,
  // while the slow zoom keeps the terminal/report capture visibly alive.
  const videoBrowser = await chromium.launch({ executablePath: edge, headless: true });
  const sceneFrames = [];
  for (const scene of videoScenes) {
    // A fresh page prevents top-level playback constants from a prior scene
    // surviving setContent and blanking subsequent frames.
    const videoPage = await videoBrowser.newPage({ viewport: { width: 1920, height: 1080 }, deviceScaleFactor: 1 });
    await videoPage.setContent(playerHTML([scene]), { waitUntil: "load" });
    await videoPage.addStyleTag({ content: ".line{opacity:1!important;transform:none!important;animation:none!important}" });
    await videoPage.waitForTimeout(250);
    const frame = path.join(videoDir, `${scene.step}-${scene.id}.png`);
    await videoPage.screenshot({ path: frame });
    sceneFrames.push(frame);
    await videoPage.close();
  }
  await videoBrowser.close();

  const totalDuration = videoScenes.reduce((sum, scene) => sum + scene.duration_ms, 0);

  const finalVideo = path.join(media, "beforedone-demo.mp4");
  const ffmpeg = process.env.BEFOREDONE_FFMPEG || "ffmpeg";
  const inputArgs = sceneFrames.flatMap((frame) => ["-i", frame]);
  const filters = videoScenes.map((scene, index) => {
    const frames = Math.round(scene.duration_ms * 30 / 1000);
    return `[${index}:v]scale=1920:1080,zoompan=z='min(zoom+0.00004,1.012)':x='iw/2-(iw/zoom/2)':y='ih/2-(ih/zoom/2)':d=${frames}:s=1920x1080:fps=30,format=yuv420p,setpts=PTS-STARTPTS[v${index}]`;
  });
  const concatInputs = videoScenes.map((_, index) => `[v${index}]`).join("");
  filters.push(`${concatInputs}concat=n=${videoScenes.length}:v=1:a=0[outv]`);
  const encoded = spawnSync(ffmpeg, [
    "-y", ...inputArgs, "-filter_complex", filters.join(";"), "-map", "[outv]",
    "-c:v", "libx264", "-preset", "medium", "-crf", "18", "-r", "30",
    "-pix_fmt", "yuv420p", "-movflags", "+faststart", "-an", finalVideo,
  ], { encoding: "utf8" });
  if (encoded.status !== 0) {
    throw new Error(`ffmpeg failed:\n${encoded.stderr}`);
  }
  console.log(`Rendered ${path.relative(root, finalVideo)} (${(totalDuration / 1000).toFixed(1)}s timeline)`);
}

main().catch((error) => {
  console.error(error.stack || error.message);
  process.exit(1);
});
