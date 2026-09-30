package main

// uiHTML is the SelfGuard control window, rendered by WebView2. Two panels:
// the adult "vault" (always-on, animated, live health) and the social tile grid
// (each site opens a profile with iPhone-style toggles and a schedule). It is
// data-driven: it calls the Go-bound bridge functions (sgStatus / sgCommand /
// sgServiceRunning / sgInstall) which talk to the local service API.
//
// Note: site tiles use brand-COLOURED MONOGRAM squares, not the real brand
// logos, which are trademarked and not reproduced here.
const uiHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>SelfGuard</title>
<style>
  :root{
    --paper:#F4F5F1;--panel:#fff;--panel-soft:#FBFCFA;--ink:#182420;--ink-soft:#586a62;
    --ink-faint:#8b998f;--line:#DDE2DA;--line-soft:#E9ECE5;
    --guard:#1E6E57;--guard-deep:#154A3B;--guard-tint:#E4F0EA;--off:#C9CEC7;
    --vault:#143f33;--vault2:#0f3529;--gold:#C9A24B;
    --wait:#A9761E;--wait-tint:#FBF0DA;--alert:#9E3B2E;--alert-tint:#F6E5E0;
    --font:"Segoe UI Variable Text","Segoe UI",system-ui,sans-serif;
  }
  *{box-sizing:border-box}
  html,body{margin:0;height:100%}
  body{font-family:var(--font);color:var(--ink);background:var(--paper);font-size:14px;-webkit-user-select:none;user-select:none}
  #app{display:flex;flex-direction:column;min-height:100%}
  .banner{padding:14px 20px;font-size:13px;line-height:1.5}
  .banner.warn{background:var(--wait-tint);color:#5c471a;border-bottom:1px solid #EAD6A6}
  .banner.err{background:var(--alert-tint);color:#5c2018;border-bottom:1px solid #E7C4BB}
  .btn{font-family:var(--font);font-weight:700;font-size:13px;cursor:pointer;border-radius:10px;padding:9px 14px;border:1px solid transparent}
  .btn.primary{background:var(--guard);color:#fff}.btn.primary:hover{background:var(--guard-deep)}
  .btn.ghost{background:#fff;border-color:var(--line);color:var(--ink)}.btn.ghost:hover{border-color:var(--ink-faint)}
  .btn.danger{background:#fff;border-color:#E7C4BB;color:var(--alert)}.btn.danger:hover{background:var(--alert-tint)}

  /* status strip */
  .status{display:flex;align-items:center;gap:14px;flex-wrap:wrap;padding:13px 20px;border-bottom:1px solid var(--line);background:var(--panel-soft)}
  .badge{display:inline-flex;align-items:center;gap:9px;font-weight:700;font-size:15px}
  .dot{width:11px;height:11px;border-radius:50%}
  .dot.on{background:var(--guard);box-shadow:0 0 0 4px var(--guard-tint)}
  .dot.setup{background:var(--wait);box-shadow:0 0 0 4px var(--wait-tint)}
  .status .sub{color:var(--ink-soft);font-size:12px;line-height:1.4;flex:1 1 260px;min-width:180px}
  .status .sub b{color:var(--ink)}
  .delay-ctl{display:flex;align-items:center;gap:8px;margin-left:auto}
  .delay-ctl label{font-size:12px;color:var(--ink-soft);font-weight:600}
  select.delay,input.txt{font-family:var(--font);font-size:13px;color:var(--ink);background:#fff;border:1px solid var(--line);border-radius:8px;padding:7px 10px}
  select.delay{font-weight:700;cursor:pointer}
  input.txt:focus,select.delay:focus{outline:2px solid var(--guard);outline-offset:1px}

  .body{display:grid;grid-template-columns:0.72fr 1fr;flex:1;min-height:0}
  .panel{padding:18px 20px;overflow:auto}
  .panel.adult{border-right:1px solid var(--line);background:linear-gradient(180deg,#fff,#fdfdfb 60%)}
  .phead{display:flex;align-items:center;justify-content:space-between;gap:10px;margin-bottom:2px}
  .phead h2{margin:0;font-size:13px;font-weight:800}
  .pnote{color:var(--ink-soft);font-size:12px;line-height:1.5;margin:4px 0 12px}

  /* ADULT: always-on pill, health, vault */
  .always{display:inline-flex;align-items:center;gap:6px;font-size:11px;font-weight:800;letter-spacing:.04em;padding:4px 10px;border-radius:999px;background:var(--vault);color:#fff}
  .health{display:flex;align-items:center;gap:10px;padding:9px 13px;border:1px solid var(--line);border-radius:11px;background:#fff;margin-bottom:12px}
  .hdot{width:9px;height:9px;border-radius:50%;background:var(--guard);position:relative;flex:none}
  .hdot::after{content:"";position:absolute;inset:-5px;border-radius:50%;border:2px solid var(--guard);opacity:.5;animation:ping 2.2s ease-out infinite}
  .hdot.bad{background:var(--alert)} .hdot.bad::after{border-color:var(--alert)}
  @keyframes ping{0%{transform:scale(.6);opacity:.6}80%,100%{transform:scale(1.9);opacity:0}}
  .health .htxt{font-size:12px;font-weight:600}.health .hmeta{color:var(--ink-faint);font-weight:500}
  .health .hstate{margin-left:auto;font-size:10.5px;font-weight:800;color:var(--guard-deep);background:var(--guard-tint);padding:3px 8px;border-radius:999px;letter-spacing:.03em}
  .health .hstate.bad{color:var(--alert);background:var(--alert-tint)}
  .vault-card{position:relative;overflow:hidden;border-radius:16px;background:linear-gradient(160deg,var(--vault),var(--vault2));color:#fff;padding:22px 16px 18px;text-align:center}
  .vault-card::before{content:"";position:absolute;inset:0;background:radial-gradient(380px 180px at 50% -30%,rgba(255,255,255,.10),transparent 70%)}
  .shieldwrap{position:relative;width:88px;height:88px;margin:0 auto 4px;display:grid;place-items:center}
  .ring{position:absolute;inset:0;border-radius:50%;border:1.5px solid rgba(201,162,75,.5);animation:breathe 3.6s ease-out infinite}
  .ring.r2{animation-delay:1.2s}.ring.r3{animation-delay:2.4s}
  @keyframes breathe{0%{transform:scale(.55);opacity:0}30%{opacity:.65}100%{transform:scale(1.25);opacity:0}}
  .count{font-size:34px;font-weight:800;letter-spacing:-.02em;font-variant-numeric:tabular-nums;line-height:1;margin-top:4px}
  .count-lbl{color:rgba(255,255,255,.72);font-size:12px;font-weight:600;margin-top:5px}
  .vault-foot{display:inline-flex;align-items:center;gap:7px;margin-top:12px;font-size:11px;font-weight:700;color:var(--gold);letter-spacing:.03em}
  .lockdot{width:6px;height:6px;border-radius:50%;background:var(--gold);animation:blink 2.6s ease-in-out infinite}
  @keyframes blink{0%,100%{opacity:1}50%{opacity:.35}}
  .guards{margin:14px 0 0;border:1px solid var(--line);border-radius:12px;background:#fff;overflow:hidden}
  .g{display:flex;align-items:flex-start;gap:11px;padding:12px 14px;border-top:1px solid var(--line-soft)}
  .g:first-child{border-top:0}
  .g .ck{width:22px;height:22px;border-radius:50%;background:var(--guard-tint);color:var(--guard-deep);display:grid;place-items:center;flex:none;font-size:13px;font-weight:800}
  .g .gt{font-weight:700;font-size:13px}.g .gd{color:var(--ink-soft);font-size:11.5px;margin-top:1px;line-height:1.4}
  .g .gon{margin-left:auto;font-size:10px;font-weight:800;color:var(--guard-deep);letter-spacing:.06em;align-self:center}
  .addlink{margin-top:12px;text-align:center}
  .addlink>button{background:none;border:0;color:var(--ink-faint);font-weight:700;font-size:12px;cursor:pointer;font-family:var(--font)}
  .addlink>button:hover{color:var(--ink)}
  .addbox{display:none;gap:8px;margin-top:10px}.addbox.show{display:flex}
  .addbox input{flex:1}
  .addlist{list-style:none;margin:8px 0 0;padding:0}
  .addlist li{display:flex;justify-content:space-between;font-size:12px;padding:6px 2px;border-top:1px solid var(--line-soft)}
  .addlist .x{color:var(--ink-faint);cursor:pointer;font-weight:700;padding:1px 6px;border-radius:6px}.addlist .x:hover{background:var(--alert-tint);color:var(--alert)}

  /* SOCIAL: tiles + profile */
  .tag{font-size:11px;font-weight:700;padding:3px 8px;border-radius:999px;background:var(--guard-tint);color:var(--guard-deep)}
  .tag.off{background:var(--line-soft);color:var(--ink-faint)}
  .schedbar{display:flex;align-items:center;gap:10px;flex-wrap:wrap;margin:10px 0;padding:10px 12px;border:1px solid var(--line);border-radius:11px;background:#fff}
  .schedbar .lbl{font-weight:700;font-size:12.5px}.schedbar .val{color:var(--ink-soft);font-size:12px}
  .link{color:var(--guard);font-weight:700;cursor:pointer;font-size:12px;background:none;border:0;font-family:var(--font)}.link:hover{text-decoration:underline}
  .grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(132px,1fr));gap:10px;margin-top:6px}
  .tile{border:1px solid var(--line);border-radius:14px;background:#fff;padding:14px 12px;cursor:pointer;text-align:center;transition:border-color .15s,transform .05s}
  .tile:hover{border-color:var(--ink-faint)}.tile:active{transform:scale(.985)}
  .logo{width:46px;height:46px;border-radius:13px;margin:0 auto 8px;display:grid;place-items:center;color:#fff;font-weight:800;font-size:19px}
  .tile .nm{font-weight:700;font-size:13px}.tile .st{font-size:11px;font-weight:600;margin-top:3px}
  .st-allowed{color:var(--ink-faint)}.st-noimg{color:#7C8A3A}.st-novid{color:var(--wait)}.st-text{color:var(--wait)}.st-blocked{color:var(--guard-deep)}
  .tile.add{display:grid;place-items:center;color:var(--ink-soft);border-style:dashed}
  .tile.add .plus{width:46px;height:46px;border-radius:13px;display:grid;place-items:center;background:var(--line-soft);font-size:24px;color:var(--ink-soft);margin-bottom:8px}
  .reqbox{display:none;gap:8px;margin-top:10px}.reqbox.show{display:flex}.reqbox input{flex:1}

  #pf{display:none}
  .back{display:inline-flex;align-items:center;gap:6px;color:var(--ink-soft);font-weight:700;font-size:13px;background:none;border:0;cursor:pointer;font-family:var(--font);padding:0;margin-bottom:12px}
  .back:hover{color:var(--ink)}
  .pf-head{display:flex;align-items:center;gap:12px;margin-bottom:16px}
  .pf-head h3{margin:0;font-size:18px;font-weight:800}.pf-head .dom{color:var(--ink-faint);font-size:12px;font-weight:600}
  .h4{font-size:11px;font-weight:800;letter-spacing:.04em;text-transform:uppercase;margin:14px 2px 4px;color:var(--ink)}
  .card{border:1px solid var(--line);border-radius:13px;background:#fff;padding:4px 14px}
  .row{display:flex;align-items:center;gap:12px;padding:13px 2px;border-top:1px solid var(--line-soft)}
  .row:first-child{border-top:0}.row .txt{flex:1}
  .row .rt{font-weight:700;font-size:14px}.row .rd{color:var(--ink-soft);font-size:11.5px;margin-top:2px;line-height:1.4}
  .row.dim .rt,.row.dim .rd{color:var(--ink-faint)}
  .sw{width:46px;height:28px;border-radius:999px;background:var(--off);position:relative;cursor:pointer;transition:background .2s;border:0;padding:0;flex:none}
  .sw.on{background:var(--guard)}
  .sw::after{content:"";position:absolute;top:3px;left:3px;width:22px;height:22px;border-radius:50%;background:#fff;transition:left .2s;box-shadow:0 1px 3px rgba(0,0,0,.28)}
  .sw.on::after{left:21px}
  .sw.disabled{opacity:.45;cursor:not-allowed}
  .sw:focus-visible{outline:2px solid var(--guard);outline-offset:2px}
  .days{display:flex;gap:6px;flex-wrap:wrap;padding:12px 2px 4px}
  .day{width:36px;height:36px;border-radius:10px;border:1px solid var(--line);background:#fff;font-weight:700;font-size:11.5px;cursor:pointer;color:var(--ink-soft);font-family:var(--font)}
  .day.on{background:var(--guard);color:#fff;border-color:var(--guard)}
  .schedwrap{display:none}.schedwrap.show{display:block}
  .schedsel{display:flex;gap:8px;align-items:center;padding:12px 2px}
  .schedsel select{font-family:var(--font);font-size:12.5px;border:1px solid var(--line);border-radius:8px;padding:6px 8px;background:#fff}

  .foot{display:flex;align-items:center;gap:14px;flex-wrap:wrap;padding:12px 20px;border-top:1px solid var(--line);background:var(--panel-soft)}
  .foot .hint{font-size:11.5px;color:var(--ink-faint);flex:1 1 200px}
  .toast{position:fixed;left:50%;bottom:16px;transform:translateX(-50%) translateY(16px);background:var(--ink);color:#fff;font-size:13px;font-weight:600;padding:10px 16px;border-radius:11px;opacity:0;transition:opacity .2s,transform .2s;z-index:50;max-width:90vw;text-align:center}
  .toast.show{opacity:1;transform:translateX(-50%) translateY(0)}.toast.err{background:var(--alert)}.toast.wait{background:var(--wait)}
  @media (prefers-reduced-motion:reduce){.ring,.hdot::after,.lockdot{animation:none}}
  #nudge{display:none;align-items:center;gap:12px;padding:10px 20px;background:var(--wait-tint);border-bottom:1px solid #EAD6A6;color:#5c471a;font-size:12.5px;font-weight:600}
  #nudge.show{display:flex}
  #nudge .x{margin-left:6px;color:#8a6d2a;cursor:pointer;font-weight:800;padding:0 6px}

  /* lockdown footer control + badge */
  .lockbtn{font-family:var(--font);font-weight:800;font-size:11.5px;letter-spacing:.03em;cursor:pointer;border-radius:999px;padding:6px 13px;border:1px solid var(--vault);background:#fff;color:var(--vault);user-select:none;display:inline-flex;align-items:center;gap:7px}
  .lockbtn:hover{background:var(--guard-tint)}
  .lockbadge{display:inline-flex;align-items:center;gap:7px;font-size:11px;font-weight:800;color:#fff;background:var(--vault);border-radius:999px;padding:6px 12px;letter-spacing:.04em}

  /* updating overlay (centered) */
  #updover{position:fixed;inset:0;background:rgba(244,245,241,.97);display:none;flex-direction:column;align-items:center;justify-content:center;gap:18px;z-index:120}
  #updover.show{display:flex;animation:ovfade .25s ease}
  .upd-card{display:flex;flex-direction:column;align-items:center;gap:16px;padding:34px 40px;border:1px solid var(--line);border-radius:16px;background:#fff;box-shadow:0 20px 50px -20px rgba(8,20,16,.4)}
  .upd-spin{width:44px;height:44px;border-radius:50%;border:4px solid var(--guard-tint);border-top-color:var(--guard);animation:spin 0.9s linear infinite}
  @keyframes spin{to{transform:rotate(360deg)}}
  .upd-title{font-weight:800;font-size:17px;color:var(--ink)}
  .upd-sub{font-size:12.5px;color:var(--ink-soft);max-width:34ch;text-align:center;line-height:1.5}

  /* device-lockdown overlay (restriction sequence, no code background) */
  #lockover{position:fixed;inset:0;background:radial-gradient(700px 460px at 50% 42%,#16463a,#0a251d 85%);display:none;flex-direction:column;align-items:center;justify-content:center;gap:20px;z-index:100;color:#eafff6}
  #lockover.show{display:flex;animation:ovfade .25s ease}
  @keyframes ovfade{from{opacity:0}to{opacity:1}}
  .ld-badge{position:relative;width:150px;height:150px;display:grid;place-items:center}
  .ld-badge svg.ring{transform:rotate(-90deg)}
  .ld-badge .track{fill:none;stroke:rgba(255,255,255,.12);stroke-width:7}
  .ld-badge .prog{fill:none;stroke:var(--gold);stroke-width:7;stroke-linecap:round;stroke-dasharray:427;stroke-dashoffset:427;transition:stroke-dashoffset .06s linear}
  .ld-glyph{position:absolute;color:#fff}
  .ld-title{font-family:var(--font);font-size:20px;font-weight:800;letter-spacing:.01em}
  .ld-steps{list-style:none;margin:0;padding:0;display:flex;flex-direction:column;gap:9px;min-width:260px}
  .ld-steps li{font-family:var(--font);font-size:13px;color:rgba(255,255,255,.45);display:flex;align-items:center;gap:10px;transition:color .2s}
  .ld-steps li::before{content:"";width:16px;height:16px;border-radius:50%;border:1.5px solid rgba(255,255,255,.35);flex:none;transition:all .2s}
  .ld-steps li.on{color:#eafff6}
  .ld-steps li.on::before{background:var(--gold);border-color:var(--gold)}
  .ld-hint{font-family:var(--font);font-size:11px;font-weight:800;letter-spacing:.16em;color:var(--gold)}
  /* seal */
  #lcseal{position:absolute;inset:0;z-index:3;display:none;flex-direction:column;align-items:center;justify-content:center;gap:16px;background:radial-gradient(600px 380px at 50% 45%,#123a2c,#0a251d 85%)}
  #lockover.sealed #lcseal{display:flex;animation:ovfade .3s ease}
  #lockover.sealed .ld-badge,#lockover.sealed .ld-title,#lockover.sealed .ld-steps,#lockover.sealed .ld-hint{display:none}
  .seal-badge{position:relative;width:150px;height:150px;display:grid;place-items:center}
  .seal-badge .ring{position:absolute;inset:0;border-radius:50%;border:2px solid var(--gold)}
  .seal-badge .ring.b{animation:sealburst .7s ease-out}
  @keyframes sealburst{0%{transform:scale(.7);opacity:.9}100%{transform:scale(1.7);opacity:0}}
  .seal-title{font-family:var(--font);font-size:24px;font-weight:800;letter-spacing:.02em;color:#fff}
  .seal-sub{font-family:var(--font);font-size:13px;color:rgba(255,255,255,.7);max-width:36ch;text-align:center;line-height:1.5}
  .seal-tag{font-family:var(--font);font-size:11px;font-weight:800;letter-spacing:.22em;color:var(--gold)}
  @media (max-width:820px){.body{grid-template-columns:1fr}.panel.adult{border-right:0;border-bottom:1px solid var(--line)}}
</style>
</head>
<body>
<div id="app">
  <div id="gate"></div>
  <div id="main" style="display:none;flex-direction:column;flex:1;min-height:0">
    <div id="updbar" style="display:none;align-items:center;gap:12px;padding:11px 20px;background:var(--guard-tint);border-bottom:1px solid #bfe0d1;color:var(--guard-deep);font-size:13px;font-weight:600">
      <span id="updtxt"></span>
      <button class="btn primary" id="updInstall" style="margin-left:auto;padding:7px 13px">Install &amp; restart</button>
    </div>
    <div id="nudge">
      <span>Blocking changed \u2014 restart your browser to clear cached pages.</span>
      <button class="btn primary" id="nudgeRestart" style="margin-left:auto;padding:6px 12px">Restart Chrome</button>
      <span class="x" id="nudgeClose" title="dismiss">\u2715</span>
    </div>
    <div class="status">
      <span class="badge"><span class="dot" id="dot"></span><span id="badgeText">—</span></span>
      <span class="sub" id="statusSub"></span>
      <div class="delay-ctl">
        <label for="delay">Lock delay</label>
        <select class="delay" id="delay">
          <option value="0">Off (setup)</option><option value="1">1 hour</option>
          <option value="12">12 hours</option><option value="24">24 hours</option>
          <option value="48">48 hours</option><option value="72">72 hours</option>
        </select>
      </div>
      <span id="lockslot"></span>
    </div>

    <div class="body">
      <!-- ADULT -->
      <section class="panel adult">
        <div class="phead"><h2>Adult content</h2>
          <span class="always"><svg width="12" height="13" viewBox="0 0 12 13" fill="none"><rect x="2" y="6" width="8" height="5.5" rx="1.2" fill="#fff"/><path d="M3.6 6V4.2a2.4 2.4 0 0 1 4.8 0V6" stroke="#fff" stroke-width="1.3" fill="none"/></svg>ALWAYS ON</span>
        </div>
        <p class="pnote">Locked on around the clock. Can't be switched off without waiting out the delay.</p>
        <div class="health">
          <span class="hdot" id="hdot"></span>
          <span class="htxt" id="htxt">Protection active — filtering normally <span class="hmeta">· checked <span id="ago">just now</span></span></span>
          <span class="hstate" id="hstate">HEALTHY</span>
        </div>
        <div class="vault-card">
          <div class="shieldwrap">
            <span class="ring"></span><span class="ring r2"></span><span class="ring r3"></span>
            <svg width="54" height="60" viewBox="0 0 18 20" fill="none" aria-hidden="true">
              <path d="M9 1 16.5 4v6c0 5-3.2 7.7-7.5 9C4.7 17.7 1.5 15 1.5 10V4L9 1Z" fill="rgba(255,255,255,.08)" stroke="rgba(255,255,255,.9)" stroke-width="1"/>
              <path d="M6 9.6 8.1 11.8 12.2 7.4" stroke="#fff" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/>
            </svg>
          </div>
          <div class="count" id="pornCount">0</div>
          <div class="count-lbl">sites on the block list</div>
          <div class="vault-foot"><span class="lockdot"></span>ACTIVELY ENFORCED</div>
        </div>
        <div class="guards">
          <div class="g"><span class="ck">✓</span><div><div class="gt">SafeSearch forced</div><div class="gd">Google, Bing, DuckDuckGo &amp; YouTube kept safe</div></div><span class="gon">ON</span></div>
          <div class="g"><span class="ck">✓</span><div><div class="gt">Encrypted DNS blocked</div><div class="gd">Closes the usual filter bypass</div></div><span class="gon">ON</span></div>
          <div class="g"><span class="ck">✓</span><div><div class="gt">Lists refresh daily</div><div class="gd">New sites added automatically</div></div><span class="gon">ON</span></div>
        </div>
        <div class="addlink">
          <button id="pornAddToggle">＋ block a specific site</button>
          <div class="addbox" id="pornAddBox"><input class="txt" id="pornInput" placeholder="e.g. example.com"><button class="btn primary" id="pornAdd">Block</button></div>
          <ul class="addlist" id="pornList"></ul>
        </div>
      </section>

      <!-- SOCIAL -->
      <section class="panel social">
        <div id="socialGrid">
          <div class="phead"><h2>Social media</h2><span class="tag" id="socialTag">Scheduled</span></div>
          <p class="pnote">Tap a site to set its rules. Each can follow the section schedule or keep its own.</p>
          <div class="schedbar">
            <span class="lbl">Section schedule</span><span class="val" id="secVal">Active at all times</span>
            <button class="link" id="editSec" style="margin-left:auto">Edit</button>
            <button class="link" id="toggleSocial"></button>
          </div>
          <div class="schedwrap" id="secWrap" style="border:1px solid var(--line);border-radius:11px;padding:4px 12px;margin-bottom:8px">
            <div class="schedsel"><select id="secMode"><option value="always">Always block</option><option value="windows">Only on set days/times</option></select></div>
            <div id="secDaysWrap" style="display:none"><div class="days" id="secDays"></div>
              <div class="schedsel"><label><input type="checkbox" id="secAllDay" checked> All day</label> <span id="secTimes" style="opacity:.4">from <input type="time" id="secFrom" value="09:00"> to <input type="time" id="secTo" value="17:00"></span> <button class="btn ghost" id="secSave">Save</button></div>
            </div>
          </div>
          <div class="grid" id="tiles"></div>
          <div class="reqbox" id="reqBox"><input class="txt" id="reqInput" placeholder="Request a site, e.g. discord.com"><button class="btn primary" id="reqSend">Request</button></div>
        </div>

        <div id="pf">
          <button class="back" id="back">‹ All sites</button>
          <div class="pf-head"><div class="logo" id="pfLogo">R</div><div><h3 id="pfName">Reddit</h3><div class="dom" id="pfDom">reddit.com</div></div></div>
          <div class="h4">Rules</div>
          <div class="card">
            <div class="row"><div class="txt"><div class="rt">Block this site</div><div class="rd">Nothing loads — the whole site is unavailable.</div></div><button class="sw" id="swBlock" role="switch"></button></div>
            <div class="row" id="rowImg"><div class="txt"><div class="rt">Hide images</div><div class="rd" id="rdImg">Photos and thumbnails don't load. Text still shows.</div></div><button class="sw" id="swImg" role="switch"></button></div>
            <div class="row" id="rowVid"><div class="txt"><div class="rt">Hide videos</div><div class="rd" id="rdVid">Video won't play. Text still shows.</div></div><button class="sw" id="swVid" role="switch"></button></div>
          </div>
          <div class="h4">Schedule</div>
          <div class="card">
            <div class="row"><div class="txt"><div class="rt">Follow the section schedule</div><div class="rd">Use the same times as the whole social section.</div></div><button class="sw" id="swFollow" role="switch"></button></div>
            <div class="schedwrap" id="pfSchedWrap">
              <div class="schedsel"><select id="pfMode"><option value="always">Always block this site</option><option value="windows">Only on set days/times</option></select></div>
              <div id="pfDaysWrap" style="display:none"><div class="rd" style="padding:8px 2px 0">Block on these days:</div><div class="days" id="pfDays"></div>
                <div class="schedsel"><label><input type="checkbox" id="pfAllDay" checked> All day</label> <span id="pfTimes" style="opacity:.4">from <input type="time" id="pfFrom" value="09:00"> to <input type="time" id="pfTo" value="17:00"></span></div>
              </div>
              <div class="schedsel"><button class="btn ghost" id="pfSchedSave">Save schedule</button></div>
            </div>
          </div>
        </div>
      </section>
    </div>

    <div class="foot">
      <button class="btn danger" id="uninstall">Request uninstall (delayed)</button>
      <span class="hint" style="flex:1">Stricter changes are instant. Looser ones wait out the delay. <span id="ver" style="color:var(--ink-faint)"></span></span>
      <button class="btn ghost" id="update">Update…</button>
      <button class="btn ghost" id="refresh">Refresh</button>
    </div>
  </div>

  <!-- updating overlay -->
  <div id="updover">
    <div class="upd-card">
      <div class="upd-spin"></div>
      <div class="upd-title" id="updtitle">Updating SelfGuard…</div>
      <div class="upd-sub" id="updsub">Installing the new version and restarting. This only takes a few seconds.</div>
    </div>
  </div>

  <!-- device-lockdown sequence overlay -->
  <div id="lockover">
    <div class="ld-badge">
      <svg class="ring" width="150" height="150" viewBox="0 0 150 150">
        <circle class="track" cx="75" cy="75" r="68"></circle>
        <circle class="prog" id="ldprog" cx="75" cy="75" r="68"></circle>
      </svg>
      <svg class="ld-glyph" width="56" height="62" viewBox="0 0 18 20" fill="none"><path d="M9 1 16.5 4v6c0 5-3.2 7.7-7.5 9C4.7 17.7 1.5 15 1.5 10V4L9 1Z" fill="rgba(255,255,255,.08)" stroke="#fff" stroke-width="1"/><rect x="6" y="9" width="6" height="4.6" rx="1" fill="#fff"/><path d="M7.2 9V7.4a1.8 1.8 0 0 1 3.6 0V9" stroke="#fff" stroke-width="1.1" fill="none"/></svg>
    </div>
    <div class="ld-title" id="ldtitle">Hold to lock down this device</div>
    <ul class="ld-steps" id="ldsteps">
      <li data-i="0">Securing service control</li>
      <li data-i="1">Restricting folder access</li>
      <li data-i="2">Enforcing DNS &amp; policy</li>
      <li data-i="3">Sealing the device</li>
    </ul>
    <div class="ld-hint" id="ldhint">PRESS AND HOLD</div>
    <div id="lcseal">
      <div class="seal-badge">
        <span class="ring"></span><span class="ring b"></span>
        <svg width="60" height="66" viewBox="0 0 18 20" fill="none"><path d="M9 1 16.5 4v6c0 5-3.2 7.7-7.5 9C4.7 17.7 1.5 15 1.5 10V4L9 1Z" fill="rgba(201,162,75,.10)" stroke="#C9A24B" stroke-width="1"/><rect x="6" y="9" width="6" height="4.6" rx="1" fill="#C9A24B"/><path d="M7.2 9V7.4a1.8 1.8 0 0 1 3.6 0V9" stroke="#C9A24B" stroke-width="1.1" fill="none"/></svg>
      </div>
      <div class="seal-tag">RESTRICTION ENFORCED</div>
      <div class="seal-title">DEVICE LOCKED DOWN</div>
      <div class="seal-sub">This device is sealed. Removal now requires your recovery key.</div>
    </div>
  </div>

<div class="toast" id="toast"></div>

<script>
"use strict";
var DAYNAMES=["Mon","Tue","Wed","Thu","Fri","Sat","Sun"];
var DAYKEY=["monday","tuesday","wednesday","thursday","friday","saturday","sunday"];
var FAV={ "reddit.com":["#FF4500","R"],"instagram.com":["#E1306C","I"],"x.com":["#111","X"],"twitter.com":["#1DA1F2","T"],
  "facebook.com":["#1877F2","F"],"tiktok.com":["#111","T"],"youtube.com":["#FF0000","Y"],"pinterest.com":["#E60023","P"],
  "snapchat.com":["#111","S"],"tumblr.com":["#35465C","T"],"discord.com":["#5865F2","D"],"linkedin.com":["#0A66C2","L"] };
var last=null, cur=null, lastOK=0;
function $(x){return document.querySelector(x)}
function esc(x){return String(x).replace(/[&<>"]/g,function(c){return{"&":"&amp;","<":"&lt;",">":"&gt;","\"":"&quot;"}[c]})}
function toast(m,k){var t=$("#toast");t.textContent=m;t.className="toast show"+(k?(" "+k):"");clearTimeout(t._t);t._t=setTimeout(function(){t.className="toast"+(k?(" "+k):"")},2600)}
function favOf(d){return FAV[d]||["#4b5563",(d[0]||"?").toUpperCase()]}
function levelFromToggles(b,i,v){if(b)return"blocked";if(i&&v)return"text";if(i)return"noimages";if(v)return"novideo";return"allowed"}
function togglesFromLevel(l){return{block:l==="blocked",img:(l==="noimages"||l==="text"),vid:(l==="novideo"||l==="text")}}
function statusOf(l){switch(l){case"blocked":return["Blocked","st-blocked"];case"text":return["Text only","st-text"];
  case"noimages":return["No images","st-noimg"];case"novideo":return["Videos hidden","st-novid"];default:return["Allowed","st-allowed"]}}

var TIGHTEN={block_domain:1,enable_category:1,add_site:1,lock_down:1};
function maybeNudge(kind,payload,ok){
  if(!ok) return;
  var t=TIGHTEN[kind];
  if(kind==="set_site_level"){ try{ var o=JSON.parse(payload); if(o.level!=="allowed") t=1; }catch(e){} }
  if(t){ $("#nudge").classList.add("show"); }
}
async function api(kind,payload){
  try{var r=await window.sgCommand(kind,payload||"");var o=JSON.parse(r);
    if(!o.ok){toast(o.message||"That didn't go through.","err");}
    else if(o.message){toast(o.message.split("\n")[0], /queued|waits|delay/i.test(o.message)?"wait":"");}
    maybeNudge(kind,payload,o.ok);
    await load();return o;
  }catch(e){toast("Couldn't reach the service.","err")}
}
var firstFail=0;
async function load(){
  var raw;
  try{raw=await window.sgStatus();}catch(e){ return softFail(); }
  var s;try{s=JSON.parse(raw);}catch(e){ return softFail(); }
  firstFail=0; hideConnecting();
  last=s;lastOK=Date.now();
  var g=$("#gate"); g.className=""; g.innerHTML=""; $("#main").style.display="flex";
  render(s);
}
// A brief status outage (service restarting during an update or at boot) shows a
// calm "Connecting" screen instead of the alarming "Protection isn't running"
// banner. Only a sustained outage (15s+) escalates to the real gate banner.
async function softFail(){
  if(!firstFail) firstFail=Date.now();
  if(Date.now()-firstFail < 15000){ showConnecting(); return; }
  hideConnecting();
  gate(await running());
}
function showConnecting(){ var ov=$("#updover"); if(!ov)return; $("#updtitle").textContent="Connecting to protection\u2026"; $("#updsub").textContent="One moment while SelfGuard starts up."; ov.classList.add("show"); }
function hideConnecting(){ var ov=$("#updover"); if(ov && $("#updtitle").textContent.indexOf("Connecting")===0) ov.classList.remove("show"); }
async function running(){try{return await window.sgServiceRunning()}catch(e){return false}}
function gate(isRunning){
  $("#main").style.display="none";var g=$("#gate");
  if(isRunning){g.className="banner warn";g.innerHTML="SelfGuard is running — filtering is active. Couldn't read live status yet. <button class='btn ghost' onclick='load()'>Try again</button>";}
  else{g.className="banner err";g.innerHTML="Protection isn't running yet. <button class='btn primary' id='ib'>Install &amp; start protection</button>";
    var b=$("#ib");if(b)b.onclick=async function(){try{await window.sgInstall();toast("Approve the admin prompt.")}catch(e){toast("Install failed.","err")}setTimeout(load,4000)};}
}

function render(s){
  var setup=s.delay_hours===0;
  // version label (fetch GUI build once; flag if service is a different version)
  try{ if(window.__guiver===undefined){ window.__guiver=null; window.sgGuiVersion().then(function(v){window.__guiver=v;paintVer(s);}); } else { paintVer(s); } }catch(e){}
  // update banner: a newer build is downloaded and verified, waiting to install.
  var ub=$("#updbar");
  if(s.update_ready){
    ub.style.display="flex";
    $("#updtxt").textContent="Update "+s.update_ready+" downloaded and verified — install when ready.";
  } else { ub.style.display="none"; }
  // lockdown footer control reflects device state
  var ls=$("#lockslot");
  if(s.locked_down){
    ls.innerHTML="<span class='lockbadge'>🔒 Device locked down</span>";
  } else {
    ls.innerHTML="<button class='lockbtn' id='lockbtn'>🔒 Lock down this device</button>";
    var lb=$("#lockbtn"); if(lb) armHold(lb);
  }
  // one-time "you were updated" note when the running version changed.
  try{
    var seen=window.__seenVer;
    if(s.version && seen && seen!==s.version){ toast("Updated to "+s.version+"."); }
    window.__seenVer=s.version;
  }catch(e){}
  $("#dot").className="dot "+(setup?"setup":"on");
  $("#badgeText").textContent=setup?"Protection active · Setup mode":"Protection active · Locked";
  $("#statusSub").innerHTML=setup?"No delay yet. Every change applies right away — test, then set a lock.":"Loosening anything waits <b>"+s.delay_hours+" hours</b>. Blocking is instant.";
  $("#delay").value=String(s.delay_hours);
  // adult
  var porn=(s.categories||[]).find(function(c){return c.name==="porn"});
  animateCount($("#pornCount"), porn?porn.domains:0);
  var healthy=!!s.net_time_ok;
  $("#hdot").className="hdot"+(healthy?"":" bad");
  $("#hstate").className="hstate"+(healthy?"":" bad");$("#hstate").textContent=healthy?"HEALTHY":"CHECK";
  $("#htxt").firstChild.textContent=healthy?"Protection active — filtering normally ":"Protection active — network time unreachable ";
  var pl=$("#pornList");pl.innerHTML="";
  (s.custom_block||[]).forEach(function(d){var li=document.createElement("li");li.innerHTML="<span>"+esc(d)+"</span><span class='x' title='unblock (delayed)'>✕</span>";li.querySelector(".x").onclick=function(){api("unblock_custom",d)};pl.appendChild(li)});
  // social header
  var social=(s.categories||[]).find(function(c){return c.name==="social"});
  var socialOn=social?social.enabled:true;
  $("#socialTag").className="tag "+(socialOn?"":"off");$("#socialTag").textContent=socialOn?"Scheduled":"Off";
  $("#secVal").textContent=!socialOn?"Category off":((social&&social.mode==="windows")?"Active on set days/times":"Active at all times");
  var tg=$("#toggleSocial");tg.textContent=socialOn?"Turn category off":"Turn category on";
  tg.onclick=function(){api(socialOn?"disable_category":"enable_category","social")};
  if(!cur) renderTiles(s.sites||[]); else renderProfile();
}
function animateCount(el,target){
  var from=parseInt((el.textContent||"0").replace(/,/g,""),10)||0;
  if(from===target){el.textContent=target.toLocaleString();return}
  var start=performance.now(),dur=900;
  function tick(now){var p=Math.min(1,(now-start)/dur),e=1-Math.pow(1-p,3);el.textContent=Math.round(from+(target-from)*e).toLocaleString();if(p<1)requestAnimationFrame(tick)}
  requestAnimationFrame(tick);
}
function paintVer(s){
  var el=$("#ver"); if(!el) return;
  var gv=window.__guiver||"?", sv=(s&&s.version)||"?";
  el.textContent=(gv===sv)?("v"+gv):("app v"+gv+" \u00b7 service v"+sv+" \u2014 reopen to finish update");
}
function renderTiles(sites){
  var g=$("#tiles");g.innerHTML="";
  sites.forEach(function(site){
    var st=statusOf(site.level||"blocked");var f=favOf(site.domain);
    var el=document.createElement("div");el.className="tile";
    el.innerHTML="<div class='logo' style='background:"+f[0]+"'>"+f[1]+"</div><div class='nm'>"+esc(niceName(site.domain))+"</div><div class='st "+st[1]+"'>"+st[0]+"</div>";
    el.onclick=function(){cur=site.domain;renderProfile()};
    g.appendChild(el);
  });
  var add=document.createElement("div");add.className="tile add";add.innerHTML="<div class='plus'>+</div><div class='nm'>Request a site</div>";
  add.onclick=function(){var b=$("#reqBox");b.classList.toggle("show");if(b.classList.contains("show"))$("#reqInput").focus()};
  g.appendChild(add);
}
function niceName(d){var n=d.replace(/\.com$|\.tv$/,"");return n.charAt(0).toUpperCase()+n.slice(1)}
function siteByDomain(d){return (last.sites||[]).find(function(x){return x.domain===d})}

function renderProfile(){
  var site=siteByDomain(cur);if(!site){cur=null;renderTiles(last.sites||[]);return}
  $("#socialGrid").style.display="none";$("#pf").style.display="block";
  var f=favOf(site.domain);$("#pfLogo").style.background=f[0];$("#pfLogo").textContent=f[1];
  $("#pfName").textContent=niceName(site.domain);$("#pfDom").textContent=site.domain;
  var t=togglesFromLevel(site.level||"blocked");
  setSw($("#swBlock"),t.block);setSw($("#swImg"),t.img);setSw($("#swVid"),t.vid);
  // capabilities
  var imgOK=site.can_images && !t.block, vidOK=site.can_video && !t.block;
  $("#rowImg").classList.toggle("dim",!imgOK);$("#rowVid").classList.toggle("dim",!vidOK);
  $("#swImg").classList.toggle("disabled",!imgOK);$("#swVid").classList.toggle("disabled",!vidOK);
  $("#rdImg").textContent=t.block?"Whole site is blocked.":(site.can_images?"Photos and thumbnails don't load. Text still shows.":"Not available for this site.");
  $("#rdVid").textContent=t.block?"Whole site is blocked.":(site.can_video?"Video won't play. Text still shows.":"Can't be separated on this site.");
  // schedule
  setSw($("#swFollow"),site.inherits);
  $("#pfSchedWrap").classList.toggle("show",!site.inherits);
  $("#pfMode").value=(site.mode==="windows")?"windows":"always";
  $("#pfDaysWrap").style.display=($("#pfMode").value==="windows")?"block":"none";
  renderDays($("#pfDays"), pfDays);
}
var pfDays=[true,true,true,true,true,false,false], secDays=[true,true,true,true,true,false,false];
function setSw(el,on){el.classList.toggle("on",!!on)}
function renderDays(wrap,arr){wrap.innerHTML="";DAYNAMES.forEach(function(nm,i){var b=document.createElement("button");b.className="day"+(arr[i]?" on":"");b.textContent=nm;b.onclick=function(){arr[i]=!arr[i];b.classList.toggle("on",arr[i])};wrap.appendChild(b)})}
function buildSchedule(mode,arr,from,to,allDay){
  if(mode!=="windows")return{mode:"always"};
  var win={};DAYKEY.forEach(function(k,i){if(arr[i])win[k]=[{start:allDay?"00:00":from,end:allDay?"23:59":to}]});
  return{mode:"windows",windows:win};
}

// toggle handlers -> compute level -> send
function pushLevel(){
  var site=siteByDomain(cur);if(!site)return;
  var b=$("#swBlock").classList.contains("on"),i=$("#swImg").classList.contains("on"),v=$("#swVid").classList.contains("on");
  api("set_site_level",JSON.stringify({domain:cur,level:levelFromToggles(b,i,v)}));
}
$("#swBlock").onclick=function(){setSw(this,!this.classList.contains("on"));pushLevel()};
$("#swImg").onclick=function(){if(this.classList.contains("disabled"))return;setSw(this,!this.classList.contains("on"));pushLevel()};
$("#swVid").onclick=function(){if(this.classList.contains("disabled"))return;setSw(this,!this.classList.contains("on"));pushLevel()};
$("#swFollow").onclick=function(){var on=!this.classList.contains("on");setSw(this,on);$("#pfSchedWrap").classList.toggle("show",!on);
  if(on)api("set_site_schedule",JSON.stringify({domain:cur,schedule:{mode:"inherit"}}))};
$("#pfMode").onchange=function(){$("#pfDaysWrap").style.display=this.value==="windows"?"block":"none"};
$("#pfAllDay").onchange=function(){$("#pfTimes").style.opacity=this.checked?".4":"1"};
$("#pfSchedSave").onclick=function(){var sc=buildSchedule($("#pfMode").value,pfDays,$("#pfFrom").value,$("#pfTo").value,$("#pfAllDay").checked);
  api("set_site_schedule",JSON.stringify({domain:cur,schedule:sc}))};
$("#back").onclick=function(){cur=null;$("#pf").style.display="none";$("#socialGrid").style.display="block";renderTiles(last.sites||[])};

// section schedule
$("#editSec").onclick=function(){$("#secWrap").classList.toggle("show");renderDays($("#secDays"),secDays)};
$("#secMode").onchange=function(){$("#secDaysWrap").style.display=this.value==="windows"?"block":"none"};
$("#secAllDay").onchange=function(){$("#secTimes").style.opacity=this.checked?".4":"1"};
$("#secSave").onclick=function(){var sc=$("#secMode").value==="always"?{mode:"always"}:buildSchedule("windows",secDays,$("#secFrom").value,$("#secTo").value,$("#secAllDay").checked);
  api("set_schedule",JSON.stringify({category:"social",schedule:sc}))};

// adult add
$("#pornAddToggle").onclick=function(){var b=$("#pornAddBox");b.classList.toggle("show");if(b.classList.contains("show"))$("#pornInput").focus()};
$("#pornAdd").onclick=function(){var v=$("#pornInput").value.trim();if(v){api("block_domain",v);$("#pornInput").value=""}};
$("#pornInput").onkeydown=function(e){if(e.key==="Enter")$("#pornAdd").click()};

// request + delay + uninstall + refresh
$("#reqSend").onclick=function(){var v=$("#reqInput").value.trim();if(!v)return;api("request_site",v);$("#reqInput").value="";$("#reqBox").classList.remove("show")};
$("#reqInput").onkeydown=function(e){if(e.key==="Enter")$("#reqSend").click()};
$("#delay").onchange=function(){api("set_delay",this.value)};
$("#uninstall").onclick=function(){if(confirm("Request uninstall? If a delay is set, this waits it out before removing anything."))api("uninstall","")};
$("#refresh").onclick=load;
$("#nudgeClose").onclick=function(){ $("#nudge").classList.remove("show"); };
$("#nudgeRestart").onclick=async function(){
  try{ await window.sgRestartBrowser(); toast("Restarting Chrome\u2026"); }
  catch(e){ toast("Couldn't launch Chrome \u2014 restart it manually (chrome://restart).","err"); }
  $("#nudge").classList.remove("show");
};
$("#updInstall").onclick=function(){ installUpdate(); };
async function installUpdate(){
  var ov=$("#updover"); ov.classList.add("show");
  $("#updtitle").textContent="Updating SelfGuard…";
  $("#updsub").textContent="Installing the new version and restarting. This only takes a few seconds.";
  try{ await window.sgCommand("apply_update",""); }catch(e){}
  // give the service a moment to begin its own swap+restart
  await new Promise(function(r){ setTimeout(r,3000); });
  $("#updsub").textContent="Applying the app update…";
  try{
    var relaunched = await window.sgFinishUpdate(); // if true, this process is exiting
    if(!relaunched){
      // couldn't self-apply (e.g. locked folder handoff pending) — ask for a reopen
      $("#updtitle").textContent="Almost done";
      $("#updsub").textContent="Update installed. Please close and reopen SelfGuard to finish.";
    }
  }catch(e){
    // process is likely exiting to relaunch; nothing to do
  }
}
$("#update").onclick=async function(){
  // If a verified update is already downloaded, install it. Otherwise ask the
  // service to check GitHub now. No local file search.
  if(last && last.update_ready){ installUpdate(); return; }
  toast("Checking for updates…");
  try{
    var o=JSON.parse(await window.sgCommand("check_update",""));
    toast(o.message||"Checked.", o.ok?"":"err");
  }catch(e){ toast("Couldn't check for updates.","err"); }
  setTimeout(load,1500);
};

// live "checked ago"
setInterval(function(){var el=$("#ago");if(!lastOK){return}var s=Math.floor((Date.now()-lastOK)/1000);el.textContent=s<5?"just now":(s<60?s+"s ago":Math.floor(s/60)+"m ago")},1000);

// ---- device-lockdown press-and-hold sequence (no code stream) ----
var HOLD_MS=3000, LD_CIRC=427;
function armHold(btn){
  var over=$("#lockover"), prog=$("#ldprog"), steps=$("#ldsteps").querySelectorAll("li");
  var t0=0, raf=0, done=false;
  function reset(){ over.className=""; prog.style.transition="stroke-dashoffset .2s ease"; prog.style.strokeDashoffset=LD_CIRC;
    steps.forEach(function(li){li.classList.remove("on");}); $("#ldtitle").textContent="Hold to lock down this device";
    $("#ldhint").textContent="PRESS AND HOLD"; cancelAnimationFrame(raf); done=false; }
  function tick(now){ var p=Math.min(1,(now-t0)/HOLD_MS);
    prog.style.transition="none"; prog.style.strokeDashoffset=String(LD_CIRC*(1-p));
    var lit=Math.floor(p*steps.length); steps.forEach(function(li,i){ li.classList.toggle("on", i<lit); });
    $("#ldtitle").textContent="Locking down this device…";
    if(p<1){ raf=requestAnimationFrame(tick); } else if(!done){ done=true; seal(); } }
  function start(e){ e.preventDefault(); over.className="show"; $("#ldhint").textContent="KEEP HOLDING…";
    t0=performance.now(); raf=requestAnimationFrame(tick);
    window.addEventListener("pointerup",end,{once:true}); window.addEventListener("pointercancel",end,{once:true}); }
  function end(){ if(done) return; cancelAnimationFrame(raf); $("#ldhint").textContent="RELEASED — no changes made";
    setTimeout(function(){ if(!done) reset(); }, 600); }
  async function seal(){ steps.forEach(function(li){li.classList.add("on");}); $("#ldtitle").textContent="Sealing…"; $("#ldhint").textContent="";
    var ok=false; try{ var o=JSON.parse(await window.sgCommand("lock_down","")); ok=!!o.ok; }catch(e){ ok=false; }
    if(ok){ over.className="show sealed"; setTimeout(function(){ over.className=""; reset(); load(); }, 2400); }
    else { $("#ldtitle").textContent="Couldn't lock down"; $("#ldhint").textContent="NOTHING WAS CHANGED — TRY AGAIN";
      setTimeout(function(){ over.className=""; reset(); load(); }, 2000); } }
  btn.addEventListener("pointerdown", start);
}

load();setInterval(load,5000);
</script>
</body>
</html>`
