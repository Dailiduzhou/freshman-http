(() => {
  'use strict';

  const $ = (id) => document.getElementById(id);
  const storageKey = 'chroma-marquee:v1';
  const defaults = {
    text: '这是一条彩虹跑马灯',
    fontPreset: 'sans',
    customFont: '',
    fontWeight: 700,
    fontSize: 160,
    scrollSpeed: 100,
    colorSpeed: 40,
    angle: 45,
    background: '#000000',
  };
  const ranges = {
    fontWeight: [100, 900, ''],
    fontSize: [24, 360, ' px'],
    scrollSpeed: [10, 400, ' px/s'],
    colorSpeed: [5, 200, ' px/s'],
    angle: [0, 180, '°'],
  };
  const fonts = {
    sans: '"PingFang SC", "Microsoft YaHei", system-ui, sans-serif',
    serif: '"Songti SC", "Noto Serif CJK SC", SimSun, serif',
    mono: '"SFMono-Regular", Consolas, "Liberation Mono", monospace',
  };
  let state = { ...defaults };
  let storageAvailable = true;
  try {
    const saved = JSON.parse(localStorage.getItem(storageKey));
    if (saved && typeof saved === 'object') {
      for (const [key, [min, max]] of Object.entries(ranges)) {
        if (Number.isFinite(saved[key])) state[key] = Math.min(max, Math.max(min, saved[key]));
      }
      if (typeof saved.text === 'string') state.text = saved.text.slice(0, 5000);
      if (typeof saved.customFont === 'string') state.customFont = saved.customFont.slice(0, 200);
      if (['sans', 'serif', 'mono', 'custom'].includes(saved.fontPreset)) state.fontPreset = saved.fontPreset;
      if (typeof saved.background === 'string' && /^#[0-9a-f]{6}$/i.test(saved.background)) state.background = saved.background;
    }
  } catch {
    storageAvailable = false;
  }

  const track = $('track');
  const measure = $('measure');
  const stage = $('stage');
  const cycle = 900;
  let unitWidth = 1;
  let offset = 0;
  let colorPhase = 0;
  let paused = matchMedia('(prefers-reduced-motion: reduce)').matches;
  let lastTime = null;
  let frameId = null;
  let uploadedFont = null;
  let uploadVersion = 0;
  let noticeTimer;
  let saveTimer;

  function notice(message) {
    $('notice').textContent = message;
    $('notice').hidden = false;
    clearTimeout(noticeTimer);
    noticeTimer = setTimeout(() => { $('notice').hidden = true; }, 4000);
  }

  function save() {
    clearTimeout(saveTimer);
    saveTimer = setTimeout(() => {
      try {
        // Font binaries are deliberately session-only; persist a usable fallback.
        localStorage.setItem(storageKey, JSON.stringify({
          ...state,
          fontPreset: state.fontPreset === 'uploaded' ? 'sans' : state.fontPreset,
        }));
        storageAvailable = true;
      } catch {
        storageAvailable = false;
      }
      $('saveStatus').textContent = storageAvailable ? '设置已保存在本机' : '浏览器禁止存储 · 仅本次有效';
    }, 200);
  }

  function setSettings(open, returnFocus = false) {
    $('settings').hidden = !open;
    $('settingsButton').setAttribute('aria-expanded', String(open));
    if (returnFocus) $('settingsButton').focus();
  }

  function family() {
    if (state.fontPreset === 'uploaded' && uploadedFont) return `"${uploadedFont.family}", ${fonts.sans}`;
    if (state.fontPreset === 'custom' && state.customFont.trim()) {
      // Quote one family name, rather than interpreting user text as CSS syntax.
      const safeName = state.customFont.trim().replace(/[\\"\n\r\f]/g, '');
      return `"${safeName}", ${fonts.sans}`;
    }
    return fonts[state.fontPreset] || fonts.sans;
  }

  function paint() {
    const radians = state.angle * Math.PI / 180;
    track.style.transform = `translate3d(${-offset}px, 0, 0)`;
    // Compensate for text movement: the rainbow belongs to the viewport, not
    // to individual repeated spans. Move along the gradient normal, wrapping
    // by exactly one color period so neither text nor color has a loop seam.
    track.style.backgroundPosition = `${offset + Math.sin(radians) * colorPhase}px ${-Math.cos(radians) * colorPhase}px`;
  }

  function rebuild() {
    const text = state.text.replace(/\s*\n\s*/g, ' ').replace(/\r/g, ' ');
    const progress = offset / unitWidth;
    for (const element of [track, measure]) {
      element.style.fontFamily = family();
      element.style.fontWeight = state.fontWeight;
      element.style.fontSize = `${state.fontSize}px`;
    }
    measure.textContent = text;
    unitWidth = Math.max(1, measure.getBoundingClientRect().width);
    offset = (progress * unitWidth) % unitWidth;
    const count = Math.ceil(stage.clientWidth / unitWidth) + 2;
    const fragment = document.createDocumentFragment();
    for (let index = 0; index < count; index++) {
      const item = document.createElement('span');
      item.className = 'marquee-item';
      item.textContent = text;
      fragment.append(item);
    }
    track.replaceChildren(fragment);
    stage.setAttribute('aria-label', text.trim() || '暂无字幕，请在设置中输入文本');
    paint();
  }

  function setGradient() {
    // A 45° gradient normal produces boundaries sloping from upper left to
    // lower right. Matching endpoints make the repeating spectrum seamless.
    const colors = ['#ff557d', '#ffc857', '#b9f46a', '#50e4c2', '#66b3ff', '#b18aff', '#ff557d'];
    track.style.backgroundImage = `repeating-linear-gradient(${state.angle}deg, ${colors.map((color, i) => `${color} ${i * cycle / 6}px`).join(', ')})`;
    const radians = state.angle * Math.PI / 180;
    const period = (component) => Math.abs(component) < 0.00001 ? cycle : cycle / Math.abs(component);
    // CSS repeats the entire background image too. Its tile dimensions must
    // each cover an exact spectral period, otherwise that tile edge is a seam.
    track.style.backgroundSize = `${period(Math.sin(radians))}px ${period(Math.cos(radians))}px`;
    paint();
  }

  function updateControls() {
    $('textInput').value = state.text;
    $('fontPreset').value = state.fontPreset;
    $('customFont').value = state.customFont;
    $('customFontRow').hidden = state.fontPreset !== 'custom';
    for (const [key, [, , suffix]] of Object.entries(ranges)) {
      $(key).value = state[key];
      $(`${key}Value`).textContent = state[key] + suffix;
    }
    $('background').value = state.background;
    $('backgroundValue').textContent = state.background.toUpperCase();
    $('display').style.backgroundColor = state.background;
  }

  function tick(time) {
    frameId = null;
    if (paused || document.hidden) { lastTime = null; return; }
    if (lastTime !== null) {
      const elapsed = Math.min((time - lastTime) / 1000, 0.1);
      offset = (offset + elapsed * state.scrollSpeed) % unitWidth;
      colorPhase = (colorPhase + elapsed * state.colorSpeed) % cycle;
      paint();
    }
    lastTime = time;
    frameId = requestAnimationFrame(tick);
  }

  function syncPlayback() {
    if (frameId !== null) cancelAnimationFrame(frameId);
    frameId = null;
    lastTime = null;
    $('pauseButton').setAttribute('aria-pressed', String(paused));
    $('pauseIcon').textContent = paused ? '▶' : 'Ⅱ';
    $('pauseLabel').textContent = paused ? '继续' : '暂停';
    $('playState').textContent = paused ? 'PAUSED' : 'LIVE';
    $('liveDot').classList.toggle('paused', paused);
    if (!paused && !document.hidden) frameId = requestAnimationFrame(tick);
  }

  $('textInput').addEventListener('input', (event) => {
    state.text = event.target.value;
    rebuild();
    save();
  });
  $('fontPreset').addEventListener('change', (event) => {
    state.fontPreset = event.target.value;
    $('customFontRow').hidden = state.fontPreset !== 'custom';
    rebuild();
    save();
  });
  $('customFont').addEventListener('input', (event) => {
    state.customFont = event.target.value;
    rebuild();
    save();
  });
  for (const [key, [, , suffix]] of Object.entries(ranges)) {
    $(key).addEventListener('input', (event) => {
      state[key] = Number(event.target.value);
      $(`${key}Value`).textContent = state[key] + suffix;
      if (key === 'fontWeight' || key === 'fontSize') rebuild();
      if (key === 'angle') setGradient();
      save();
    });
  }
  $('background').addEventListener('input', (event) => {
    state.background = event.target.value;
    $('backgroundValue').textContent = state.background.toUpperCase();
    $('display').style.backgroundColor = state.background;
    save();
  });

  $('fontFile').addEventListener('change', async (event) => {
    const file = event.target.files[0];
    if (!file) return;
    const version = ++uploadVersion;
    event.target.value = '';
    if (!/\.(woff2?|ttf|otf)$/i.test(file.name)) { notice('请选择 WOFF2、WOFF、TTF 或 OTF 字体文件。'); return; }
    if (file.size > 30 * 1024 * 1024) { notice('字体文件不能超过 30 MB。'); return; }
    $('fontStatus').textContent = '正在加载字体…';
    try {
      const font = new FontFace(`ChromaLocal${version}`, await file.arrayBuffer(), { weight: '100 900' });
      await font.load();
      if (version !== uploadVersion) return;
      document.fonts.add(font);
      if (uploadedFont) document.fonts.delete(uploadedFont);
      uploadedFont = font;
      state.fontPreset = 'uploaded';
      $('fontPreset').querySelector('[value="uploaded"]').disabled = false;
      $('fontStatus').textContent = `${file.name} · 已加载，仅本次有效`;
      updateControls();
      rebuild();
      save();
    } catch {
      if (version !== uploadVersion) return;
      $('fontStatus').textContent = '加载失败，请检查字体文件后重试。';
      notice('无法读取该字体，已保留当前字体。');
    }
  });

  $('pauseButton').addEventListener('click', () => { paused = !paused; syncPlayback(); });
  $('settingsButton').addEventListener('click', () => {
    const open = $('settings').hidden;
    setSettings(open);
    if (open) $('textInput').focus({ preventScroll: true });
  });
  $('closeSettings').addEventListener('click', () => setSettings(false, true));
  document.addEventListener('keydown', (event) => {
    if (event.key === 'Escape' && !$('settings').hidden) setSettings(false, true);
  });
  $('fullscreenButton').addEventListener('click', async () => {
    try {
      if (document.fullscreenElement) await document.exitFullscreen();
      else if (document.documentElement.requestFullscreen) await document.documentElement.requestFullscreen();
      else notice('此浏览器不支持页面全屏，可收起设置或使用浏览器全屏。');
    } catch {
      notice('未能进入全屏，请使用浏览器的全屏功能。');
    }
  });
  document.addEventListener('fullscreenchange', () => {
    const active = Boolean(document.fullscreenElement);
    $('fullscreenLabel').textContent = active ? '退出全屏' : '全屏';
    if (active) setSettings(false, true);
    rebuild();
  });
  $('resetButton').addEventListener('click', () => {
    ++uploadVersion;
    if (uploadedFont) document.fonts.delete(uploadedFont);
    uploadedFont = null;
    $('fontPreset').querySelector('[value="uploaded"]').disabled = true;
    $('fontStatus').textContent = '仅在浏览器中加载，不上传；重新打开需再选。';
    state = { ...defaults };
    offset = 0;
    colorPhase = 0;
    updateControls();
    setGradient();
    rebuild();
    save();
    notice('已恢复默认设置。');
  });

  new ResizeObserver(rebuild).observe(stage);
  document.fonts.addEventListener('loadingdone', rebuild);
  document.addEventListener('visibilitychange', syncPlayback);
  window.addEventListener('pagehide', () => {
    // Flush a final edit even if the user closes the page during the debounce.
    try { localStorage.setItem(storageKey, JSON.stringify({ ...state, fontPreset: state.fontPreset === 'uploaded' ? 'sans' : state.fontPreset })); } catch { /* Private browsing may deny storage. */ }
  });
  updateControls();
  setGradient();
  rebuild();
  syncPlayback();
  if (!storageAvailable) $('saveStatus').textContent = '无法读取存储 · 当前设置仍可用';
  if (paused) notice('已遵循系统“减少动态效果”设置，点击“继续”开始播放。');
})();
