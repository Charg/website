// Trippy theme: a morphing Julia set behind an endless tunnel of screens,
// each showing this page. Loaded by theme.js only when trippy is picked.
(function () {
  var root = document.documentElement;
  var reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)");

  var PERIOD = 9; // seconds to fall one screen deeper

  // What the controls panel can change. Each screen sits inside the last:
  // shrink smaller, turned by turn degrees, its colours shifted by tint.
  var CONTROLS = [
    { key: "speed", label: "speed", min: 0, max: 3, step: 0.05, value: 1, unit: "×", motion: true },
    { key: "zoom", label: "zoom", min: 0.5, max: 4, step: 0.05, value: 1, unit: "×" },
    { key: "hue", label: "colour", min: 0, max: 360, step: 1, value: 0, unit: "°" },
    { key: "depth", label: "screens", min: 0, max: 12, step: 1, value: 8, unit: "", apply: refillSoon },
    { key: "shrink", label: "shrink", min: 0.4, max: 0.85, step: 0.01, value: 0.62, unit: "", apply: placeScreens },
    { key: "turn", label: "twist", min: -30, max: 30, step: 1, value: 7, unit: "°", apply: placeScreens },
    { key: "tint", label: "tint", min: 0, max: 180, step: 1, value: 45, unit: "°", apply: placeScreens },
  ];
  var settings = {};

  function resetSettings() {
    CONTROLS.forEach(function (c) { settings[c.key] = c.value; });
  }

  function loadSettings() {
    resetSettings();
    try {
      var saved = JSON.parse(localStorage.getItem("trippy-settings")) || {};
      CONTROLS.forEach(function (c) {
        var v = Number(saved[c.key]);
        if (saved[c.key] != null && isFinite(v)) settings[c.key] = Math.min(c.max, Math.max(c.min, v));
      });
    } catch (e) {}
  }

  function saveSettings() {
    try { localStorage.setItem("trippy-settings", JSON.stringify(settings)); } catch (e) {}
  }

  loadSettings();

  // The scene is slow and soft, so half the usual frame rate is plenty.
  var FRAME_MS = 1000 / 30;
  // Longest step the clock takes in one frame, in seconds, so a tab
  // coming back from the background carries on instead of leaping ahead.
  var MAX_STEP = 0.1;
  var START_TIME = 20; // where in the scene the clock starts, in seconds

  // Screens only show the top of the page: this many window heights of it,
  // looked for this many levels down.
  var SNAPSHOT_SCREENS = 1.2;
  var TRIM_DEPTH = 4;
  var REFRESH_MS = 500; // quiet time after a page change before the screens catch up
  var DEBOUNCE_MS = 300;
  var SETTLE_MS = 150; // quiet time after a depth drag before the screens are rebuilt

  // Each is null while the theme is off.
  var fractal = null; // { canvas, gl, uRes, uTime, uZoom, uHue }
  var tunnel = null; // { el, screens, watcher, refresh }
  var wobble, raf = null;

  function active() {
    return root.dataset.theme === "trippy" && !root.classList.contains("myspace");
  }

  // Headings wobble through an SVG filter, which CSS can only reference when it's in the page.
  function addWobbleFilter() {
    if (wobble) return;
    var svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    svg.setAttribute("aria-hidden", "true");
    svg.setAttribute("width", "0");
    svg.setAttribute("height", "0");
    svg.style.position = "absolute";
    svg.innerHTML =
      '<filter id="wobble">' +
      '<feTurbulence type="fractalNoise" baseFrequency="0.012 0.04" numOctaves="2" seed="3">' +
      '<animate attributeName="baseFrequency" dur="9s" repeatCount="indefinite"' +
      ' values="0.012 0.04;0.02 0.06;0.012 0.04"/>' +
      "</feTurbulence>" +
      '<feDisplacementMap in="SourceGraphic" scale="6"/>' +
      "</filter>";
    document.body.appendChild(svg);
    wobble = svg;
  }

  // Fractal: a Julia set whose constant walks the edge of the Mandelbrot cardioid, where the shapes are most intricate.
  var FRAG = [
    "precision highp float;",
    "uniform vec2 res;",
    "uniform float t;",
    "uniform float zoom;",
    "uniform float hue;",
    "void main() {",
    "  vec2 p = (gl_FragCoord.xy - 0.5 * res) / min(res.x, res.y) * 1.7 / zoom;",
    "  float a = t * 0.04;",
    "  p = mat2(cos(a), -sin(a), sin(a), cos(a)) * p;",
    "  float w = 1.2 + 1.1 * sin(t * 0.05);",
    "  vec2 e1 = vec2(cos(w), sin(w)), e2 = vec2(cos(2.0 * w), sin(2.0 * w));",
    "  vec2 c = 0.5 * e1 - 0.25 * e2;",
    "  vec2 z = p;",
    "  float n = 0.0;",
    "  float trap = 10.0;",
    "  for (int i = 0; i < 160; i++) {",
    "    z = vec2(z.x * z.x - z.y * z.y, 2.0 * z.x * z.y) + c;",
    "    trap = min(trap, abs(length(z) - 0.6));",
    "    if (dot(z, z) > 64.0) break;",
    "    n += 1.0;",
    "  }",
    "  if (n > 159.0) {",
    "    float m = length(z) * 1.5 + atan(z.y, z.x) * 0.16 + t * 0.2;",
    "    gl_FragColor = vec4(0.35 + 0.35 * cos(6.2831 * (m + hue + vec3(0.6, 0.9, 0.2))), 1.0);",
    "    return;",
    "  }",
    "  float k = log2(n - log2(log2(dot(z, z))) + 2.0) * 0.6 + log(trap + 0.02) * 0.25 - t * 0.12;",
    "  gl_FragColor = vec4(0.5 + 0.5 * cos(6.2831 * (k + hue + vec3(0.0, 0.33, 0.67))), 1.0);",
    "}",
  ].join("\n");

  var VERT = "attribute vec2 v; void main() { gl_Position = vec4(v, 0.0, 1.0); }";

  function startFractal() {
    var canvas = document.createElement("canvas");
    canvas.className = "fractal";
    canvas.setAttribute("aria-hidden", "true");
    var gl = canvas.getContext("webgl");
    if (!gl) return;
    var f = { canvas: canvas, gl: gl };
    fractal = f;

    var prog = gl.createProgram();
    [[gl.VERTEX_SHADER, VERT], [gl.FRAGMENT_SHADER, FRAG]].forEach(function (s) {
      var sh = gl.createShader(s[0]);
      gl.shaderSource(sh, s[1]);
      gl.compileShader(sh);
      gl.attachShader(prog, sh);
    });
    gl.bindAttribLocation(prog, 0, "v");
    gl.linkProgram(prog);
    if (!gl.getProgramParameter(prog, gl.LINK_STATUS)) { stopFractal(); return; }
    gl.useProgram(prog);

    // One triangle that covers the whole screen.
    gl.bindBuffer(gl.ARRAY_BUFFER, gl.createBuffer());
    gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1, -1, 3, -1, -1, 3]), gl.STATIC_DRAW);
    gl.enableVertexAttribArray(0);
    gl.vertexAttribPointer(0, 2, gl.FLOAT, false, 0, 0);
    f.uRes = gl.getUniformLocation(prog, "res");
    f.uTime = gl.getUniformLocation(prog, "t");
    f.uZoom = gl.getUniformLocation(prog, "zoom");
    f.uHue = gl.getUniformLocation(prog, "hue");

    // A lost context leaves a blank canvas, so fall back to the colour wheel.
    canvas.addEventListener("webglcontextlost", function () {
      if (fractal === f) stopFractal();
    });

    document.body.appendChild(canvas);
    root.classList.add("fractal-on");
  }

  // Browsers only allow a handful of live contexts, so hand this one back.
  function stopFractal() {
    if (!fractal) return;
    var lose = fractal.gl.getExtension("WEBGL_lose_context");
    fractal.canvas.remove();
    fractal = null;
    root.classList.remove("fractal-on");
    if (lose) lose.loseContext();
  }

  // Half resolution: it's soft and colourful anyway, and four times cheaper.
  function drawFractal(t) {
    var f = fractal, gl = f.gl;
    var w = Math.ceil(innerWidth / 2), h = Math.ceil(innerHeight / 2);
    if (f.canvas.width !== w || f.canvas.height !== h) {
      f.canvas.width = w;
      f.canvas.height = h;
      gl.viewport(0, 0, w, h);
    }
    gl.uniform2f(f.uRes, w, h);
    gl.uniform1f(f.uTime, t);
    gl.uniform1f(f.uZoom, settings.zoom);
    gl.uniform1f(f.uHue, settings.hue / 360);
    gl.drawArrays(gl.TRIANGLES, 0, 3);
  }

  // Tunnel: copies of the page on screens nested inside each other.

  // Screens only show the top of the page, so drop whatever starts below
  // the first screenful: long pages would otherwise be copied whole, many times over.
  function trim(orig, copy, limit, depth) {
    for (var i = orig.children.length - 1; i >= 0; i--) {
      var top = orig.children[i].getBoundingClientRect().top + scrollY;
      if (top > limit) copy.children[i].remove();
      else if (depth < TRIM_DEPTH) trim(orig.children[i], copy.children[i], limit, depth + 1);
    }
  }

  function snapshot() {
    var wrap = document.querySelector(".wrap");
    var copy = wrap.cloneNode(true);
    trim(wrap, copy, innerHeight * SNAPSHOT_SCREENS, 0);
    copy.querySelectorAll("script, canvas, iframe, video, .bait").forEach(function (el) { el.remove(); });
    copy.querySelectorAll("[id]").forEach(function (el) { el.removeAttribute("id"); });
    // Same-named radios would join the real theme switch's group.
    copy.querySelectorAll("[name]").forEach(function (el) { el.removeAttribute("name"); });
    // The tunnel is inert, but browsers without inert would still tab into the copies.
    copy.querySelectorAll("a, button, input, select, textarea, summary, [tabindex]").forEach(function (el) {
      el.tabIndex = -1;
    });
    return copy;
  }

  function startTunnel() {
    var el = document.createElement("div");
    el.className = "tunnel";
    el.setAttribute("aria-hidden", "true");
    el.inert = true;
    var screens = document.createElement("div");
    screens.className = "tunnel-spin";
    el.appendChild(screens);
    // Set before cloning, so stop() can still clean up if cloning throws.
    tunnel = { el: el, screens: screens, watcher: null, refresh: null };

    fillTunnel();
    document.body.appendChild(el);

    // Pages that fill themselves in after loading would otherwise be stuck
    // showing their empty state on every screen.
    tunnel.watcher = new MutationObserver(function () {
      clearTimeout(tunnel.refresh);
      tunnel.refresh = setTimeout(fillTunnel, REFRESH_MS);
    });
    tunnel.watcher.observe(document.querySelector(".wrap"), { childList: true, subtree: true, characterData: true });
  }

  // The outermost screen starts larger than the window, so the loop has
  // somewhere to fall from.
  function fillTunnel() {
    var page = snapshot();
    var screens = tunnel.screens;
    screens.textContent = "";
    for (var i = 0; i < settings.depth; i++) {
      var screen = document.createElement("div");
      screen.className = "screen";
      screen.appendChild(page.cloneNode(true));
      screens.appendChild(screen);
    }
    placeScreens();
  }

  function placeScreens() {
    var screens = tunnel.screens;
    for (var i = 0; i < screens.children.length; i++) {
      var screen = screens.children[i];
      screen.style.transform = "scale(" + Math.pow(settings.shrink, i - 1) + ") rotate(" + i * settings.turn + "deg)";
      screen.style.filter = "hue-rotate(" + i * settings.tint + "deg)";
    }
  }

  function stopTunnel() {
    if (tunnel.watcher) tunnel.watcher.disconnect();
    clearTimeout(tunnel.refresh);
    tunnel.el.remove();
    tunnel = null;
  }

  // Growing by one screen and turning back by one leaves every screen
  // exactly where the next one out started, so the fall loops seamlessly.
  function placeTunnel(t) {
    var p = (t / PERIOD) % 1;
    tunnel.screens.style.transform = "scale(" + Math.pow(settings.shrink, -p) + ") rotate(" + -settings.turn * p + "deg)";
  }

  // Motion: its own clock, so pausing freezes the scene and playing picks up where it left off.

  var clock = START_TIME;
  var last = null;
  var button;
  var paused = false;
  try { paused = localStorage.getItem("trippy-paused") === "1"; } catch (e) {}

  function render() {
    if (fractal) drawFractal(clock);
    placeTunnel(clock);
  }

  function frame(now) {
    raf = requestAnimationFrame(frame);
    // The 1ms of slack keeps a 60Hz display on every other frame despite jitter.
    if (last !== null && now - last < FRAME_MS - 1) return;
    if (last !== null) clock += Math.min((now - last) / 1000, MAX_STEP) * settings.speed;
    last = now;
    render();
  }

  function startLoop() {
    if (raf !== null) return;
    last = null;
    raf = requestAnimationFrame(frame);
  }

  function stopLoop() {
    cancelAnimationFrame(raf);
    raf = null;
  }

  // While the loop is stopped, the canvas only needs redrawing when the window changes size.
  function redraw() { if (raf === null) render(); }

  // Pausing also freezes the CSS animations and the SVG wobble.
  function applyPause() {
    root.classList.toggle("motion-paused", paused);
    paused ? wobble.pauseAnimations() : wobble.unpauseAnimations();
    button.textContent = paused ? "▶ play motion" : "❚❚ pause motion";
    paused ? stopLoop() : startLoop();
  }

  // First in <body> so it's first in tab order, and outside the reading
  // column, whose backdrop filter would pin a fixed button to the column.
  function addButton() {
    button = document.createElement("button");
    button.type = "button";
    button.className = "motion-toggle";
    button.addEventListener("click", togglePause);
    document.body.prepend(button);
  }

  // Controls: a panel pinned to the right edge that tucks away to a tab.

  var panel;
  var refillTimer;

  function panelOpen() {
    try {
      var saved = localStorage.getItem("trippy-panel");
      if (saved) return saved === "open";
    } catch (e) {}
    // Out of the way by default where it would cover the text.
    return innerWidth >= 1000;
  }

  function setPanelOpen(open) {
    panel.classList.toggle("collapsed", !open);
    var tab = panel.querySelector(".trippy-tab");
    tab.setAttribute("aria-expanded", open ? "true" : "false");
    tab.textContent = open ? "❯" : "❮";
    try { localStorage.setItem("trippy-panel", open ? "open" : "closed"); } catch (e) {}
  }

  function format(c, v) {
    return (c.step < 1 ? v.toFixed(2) : String(v)) + c.unit;
  }

  // Re-cloning the page for every step of a drag would stutter, so screens
  // are only added or removed once the slider settles.
  function refillSoon() {
    clearTimeout(refillTimer);
    refillTimer = setTimeout(fillTunnel, SETTLE_MS);
  }

  function addPanel() {
    panel = document.createElement("aside");
    panel.className = "trippy-panel";
    panel.setAttribute("aria-label", "Background controls");

    var tab = document.createElement("button");
    tab.type = "button";
    tab.className = "trippy-tab";
    tab.title = "background controls";
    tab.setAttribute("aria-controls", "trippy-controls");
    tab.addEventListener("click", function () { setPanelOpen(panel.classList.contains("collapsed")); });

    var body = document.createElement("div");
    body.className = "trippy-controls";
    body.id = "trippy-controls";
    var heading = document.createElement("p");
    heading.className = "trippy-heading";
    heading.textContent = "background";
    body.appendChild(heading);

    var inputs = [];
    CONTROLS.forEach(function (c) {
      if (c.motion && reducedMotion.matches) return;
      var label = document.createElement("label");
      var name = document.createElement("span");
      name.textContent = c.label;
      var out = document.createElement("output");
      var input = document.createElement("input");
      input.type = "range";
      input.min = c.min;
      input.max = c.max;
      input.step = c.step;
      input.value = settings[c.key];
      out.textContent = format(c, settings[c.key]);
      input.addEventListener("input", function () {
        settings[c.key] = Number(input.value);
        out.textContent = format(c, settings[c.key]);
        saveSettings();
        if (c.apply) c.apply();
        redraw();
      });
      label.appendChild(name);
      label.appendChild(out);
      label.appendChild(input);
      body.appendChild(label);
      inputs.push({ c: c, input: input, out: out });
    });

    var reset = document.createElement("button");
    reset.type = "button";
    reset.className = "trippy-reset";
    reset.textContent = "reset";
    reset.addEventListener("click", function () {
      resetSettings();
      saveSettings();
      inputs.forEach(function (x) {
        x.input.value = settings[x.c.key];
        x.out.textContent = format(x.c, settings[x.c.key]);
      });
      fillTunnel();
      redraw();
    });
    body.appendChild(reset);

    panel.appendChild(tab);
    panel.appendChild(body);
    document.body.prepend(panel);
    setPanelOpen(panelOpen());
  }

  var lastToggle = 0;

  function togglePause() {
    // One key press can arrive as both a keydown and a click in some browsers.
    if (Date.now() - lastToggle < DEBOUNCE_MS) return;
    lastToggle = Date.now();
    paused = !paused;
    try { localStorage.setItem("trippy-paused", paused ? "1" : "0"); } catch (e) {}
    applyPause();
  }

  // Clicking the trippy stop while already trippy pauses too. Click handlers
  // run before the switch's change event updates the theme, so a click that
  // is picking trippy still sees the old theme here and is left alone.
  function watchSwitch() {
    var radio = document.querySelector('.switch input[value="trippy"]');
    if (!radio) return;
    radio.addEventListener("click", function () {
      if (button && root.dataset.theme === "trippy") togglePause();
    });
    // Chrome doesn't click a radio that's already checked when Space is pressed.
    radio.addEventListener("keydown", function (e) {
      if ((e.key === " " || e.key === "Enter") && button && root.dataset.theme === "trippy") {
        e.preventDefault();
        togglePause();
      }
    });
  }

  function start() {
    if (tunnel) return;
    addWobbleFilter();
    // Both go at the end of <body>, after the real page, so page scripts
    // looking things up find the real elements first. The fractal goes in
    // first to sit underneath.
    startFractal();
    startTunnel();
    render();
    addEventListener("resize", redraw);
    addPanel();
    // Reduced motion already gets a still frame, so there's nothing to pause.
    if (!reducedMotion.matches) {
      addButton();
      applyPause();
      root.classList.add("motion-ready");
    }
  }

  function stop() {
    if (!tunnel) return;
    stopLoop();
    removeEventListener("resize", redraw);
    stopTunnel();
    stopFractal();
    wobble.remove();
    if (button) button.remove();
    clearTimeout(refillTimer);
    if (panel) panel.remove();
    wobble = button = panel = null;
    root.classList.remove("motion-paused", "motion-ready");
  }

  function update() { active() ? start() : stop(); }

  function init() {
    watchSwitch();
    new MutationObserver(update).observe(root, { attributes: true, attributeFilter: ["data-theme", "class"] });
    // Start over when the setting changes, so the scene matches it either way.
    reducedMotion.addEventListener("change", function () {
      if (tunnel) { stop(); start(); }
    });
    update();
  }

  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", init);
  else init();
})();
