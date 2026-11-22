// The generative theme: every page load rolls a fresh look (background pattern, colours,
// type and frame) from a random seed, and clicking its stop again tears through to another.
// Loaded as a blocking script in <head>, after theme.js, so the look applies before the page paints.
(function () {
  var root = document.documentElement;
  var reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)");
  var TEAR_MS = 600;

  var sheet = null;
  var look = null; // what the current seed generated, or null while the theme is off
  var rolling = false;

  // ---- Generating a look ----

  function mulberry32(a) {
    return function () {
      a = (a + 0x6d2b79f5) | 0;
      var t = Math.imul(a ^ (a >>> 15), 1 | a);
      t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
      return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
    };
  }

  function hsl(h, s, l) {
    h = ((h % 360) + 360) % 360;
    var a = s * Math.min(l, 1 - l);
    function f(n) {
      var k = (n + h / 30) % 12;
      return l - a * Math.max(-1, Math.min(k - 3, 9 - k, 1));
    }
    return [f(0), f(8), f(4)];
  }

  function luminance(c) {
    var v = c.map(function (x) { return x <= 0.03928 ? x / 12.92 : Math.pow((x + 0.055) / 1.055, 2.4); });
    return 0.2126 * v[0] + 0.7152 * v[1] + 0.0722 * v[2];
  }

  function contrast(a, b) {
    var x = luminance(a), y = luminance(b);
    return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05);
  }

  // Walk the lightness away from the background until the colour is readable on it.
  function readable(h, s, l, bg, dark, min) {
    while (contrast(hsl(h, s, l), bg) < min && l > 0 && l < 1) l += dark ? 0.02 : -0.02;
    return hsl(h, s, Math.min(1, Math.max(0, l)));
  }

  function rgb(c, alpha) {
    var v = c.map(function (x) { return Math.round(x * 255); }).join(" ");
    return alpha === undefined ? "rgb(" + v + ")" : "rgb(" + v + " / " + alpha + ")";
  }

  var BODY_FONTS = [
    'ui-monospace, "SF Mono", "Cascadia Code", "Roboto Mono", Menlo, Consolas, "DejaVu Sans Mono", monospace',
    '"Iowan Old Style", Charter, Georgia, "Times New Roman", serif',
    'system-ui, -apple-system, "Segoe UI", Roboto, sans-serif',
    '"Courier New", Courier, monospace',
    'ui-rounded, "Arial Rounded MT Bold", "Varela Round", Nunito, system-ui, sans-serif',
  ];
  var HEADINGS = [
    "",
    "text-transform: uppercase; letter-spacing: 0.08em;",
    'font-family: Impact, "Arial Narrow Bold", "Haettenschweiler", sans-serif; font-weight: 400; text-transform: uppercase; letter-spacing: 0.03em;',
    'font-family: Georgia, "Times New Roman", serif; font-style: italic;',
  ];
  var FRAMES = [
    function (w) { return "border: 3px solid " + w.text + "; border-radius: 0; box-shadow: 0.5rem 0.5rem 0 " + w.link + ";"; },
    function (w) { return "border: 6px double " + w.rule + "; border-radius: 0;"; },
    function () { return "border-radius: 1.5rem; box-shadow: 0 1rem 3rem rgb(0 0 0 / 0.35);"; },
    function (w) { return "border: 2px dashed " + w.link + "; border-radius: 0.5rem;"; },
    function () { return "border-radius: 0.25rem;"; },
  ];
  var SCHEMES = [[30, 60], [150, 210], [120, 240], [180, 30], [90, 270], [-40, 40]];

  function generate(seed) {
    var r = mulberry32(seed);
    function pick(list) { return list[Math.floor(r() * list.length)]; }

    var dark = r() < 0.55;
    var h = r() * 360;
    var spread = pick(SCHEMES);
    var bg = hsl(h, 0.2 + r() * 0.4, dark ? 0.06 + r() * 0.06 : 0.92 + r() * 0.05);

    var w = {
      dark: dark,
      bg: rgb(bg),
      panel: rgb(bg, 0.9),
      text: rgb(readable(h, 0.15, dark ? 0.85 : 0.2, bg, dark, 7)),
      muted: rgb(readable(h + spread[0], 0.25, dark ? 0.55 : 0.5, bg, dark, 4.5)),
      link: rgb(readable(h + spread[0], 0.85, 0.5, bg, dark, 4.5)),
      visited: rgb(readable(h + spread[1], 0.75, 0.5, bg, dark, 4.5)),
      rule: rgb(hsl(h, 0.3, dark ? 0.26 : 0.8)),
      codeBg: rgb(hsl(h, 0.3, dark ? 0.14 : 0.87)),
      // Background colours: a quiet base and two loud accents.
      c0: hsl(h, 0.55, dark ? 0.1 : 0.86),
      c1: hsl(h + spread[0], 0.85, dark ? 0.45 : 0.62),
      c2: hsl(h + spread[1], 0.9, dark ? 0.65 : 0.45),
      body: pick(BODY_FONTS),
      heading: pick(HEADINGS),
      frame: pick(FRAMES),
      tilt: r() < 0.35 ? (r() - 0.5) * 1.2 : 0,
      // Shader dials.
      pattern: Math.floor(r() * 5),
      scale: 1.5 + r() * 3.5,
      warp: r() * 1.5,
      angle: r() * Math.PI,
      speed: 0.3 + r() * 0.9,
      misreg: r() < 0.5 ? 0 : 0.004 + r() * 0.01,
      grain: r() * 0.12,
      levels: r() < 0.5 ? 0 : 3 + Math.floor(r() * 4),
      off: [r() * 100, r() * 100],
    };
    return w;
  }

  // Scoped with :not(.myspace) so the 2006 easter egg still wins, and html:root to
  // outrank the light/dark variables (including the pages' own).
  function styles(w) {
    var s = 'html:root[data-theme="generative"]:not(.myspace)';
    return [
      s + " { color-scheme: " + (w.dark ? "dark" : "light") + "; --bg: " + w.bg + "; --text: " + w.text +
        "; --muted: " + w.muted + "; --link: " + w.link + "; --visited: " + w.visited + "; --rule: " + w.rule +
        "; --code-bg: " + w.codeBg + "; --track: " + w.codeBg + "; --knob: " + w.bg + "; }",
      // Stands in for the WebGL background until it starts, or if it can't.
      s + " body { font-family: " + w.body + "; background:" +
        " radial-gradient(circle at 15% 20%, " + rgb(w.c1) + ", transparent 55%)," +
        " radial-gradient(circle at 85% 75%, " + rgb(w.c2) + ", transparent 55%), " + rgb(w.c0) + "; }",
      s + " :is(h1, h2, h3, .site .name) { " + w.heading + " }",
      // The reading column (and anything a page marks .glass) is a frosted panel over the background.
      s + " :is(.wrap, .glass) { margin-block: clamp(0.5rem, 4vh, 2.5rem); background: " + w.panel +
        "; -webkit-backdrop-filter: blur(6px); backdrop-filter: blur(6px); }",
      s + " .wrap { width: calc(100% - 1rem); rotate: " + w.tilt.toFixed(2) + "deg; " + w.frame(w) + " }",
    ].join("\n");
  }

  // ---- Rolling, rerolling and leaving ----

  function active() { return root.dataset.theme === "generative"; }

  function apply() {
    if (!sheet) {
      if (!("adoptedStyleSheets" in document)) return;
      sheet = new CSSStyleSheet();
      document.adoptedStyleSheets = document.adoptedStyleSheets.concat(sheet);
    }
    look = generate((Math.random() * 4294967296) >>> 0);
    sheet.replaceSync(styles(look));
    if (document.body) {
      startCanvas();
      if (canvas && reducedMotion.matches) draw(0);
    }
  }

  function leave() {
    if (!look) return;
    look = null;
    if (sheet) sheet.replaceSync("");
    stopCanvas();
  }

  // The page tears into slices and the new look lands halfway through.
  function roll() {
    if (rolling) return;
    if (reducedMotion.matches) return apply();
    rolling = true;
    root.classList.add("gen-tearing");
    setTimeout(apply, TEAR_MS / 2);
    setTimeout(function () {
      root.classList.remove("gen-tearing");
      rolling = false;
    }, TEAR_MS);
  }

  // Follows the theme switch: picking the theme tears in a look, leaving takes it down.
  function sync() {
    if (active() && !look) roll();
    else if (!active() && look) leave();
  }

  // ---- Background: one full-screen shader whose dials the universe sets ----

  var VERT = "attribute vec2 v; void main() { gl_Position = vec4(v, 0.0, 1.0); }";
  var FRAG = [
    "#ifdef GL_FRAGMENT_PRECISION_HIGH",
    "precision highp float;",
    "#else",
    "precision mediump float;",
    "#endif",
    "uniform vec2 res, off;",
    "uniform float t, pattern, scale, warp, angle, misreg, grain, levels;",
    "uniform vec3 c0, c1, c2;",
    "float hash(vec2 p) { return fract(sin(dot(p, vec2(127.1, 311.7))) * 43758.5453); }",
    "vec2 hash2(vec2 p) { return fract(sin(vec2(dot(p, vec2(127.1, 311.7)), dot(p, vec2(269.5, 183.3)))) * 43758.5453); }",
    "float noise(vec2 p) {",
    "  vec2 i = floor(p), f = fract(p), u = f * f * (3.0 - 2.0 * f);",
    "  return mix(mix(hash(i), hash(i + vec2(1, 0)), u.x), mix(hash(i + vec2(0, 1)), hash(i + vec2(1, 1)), u.x), u.y);",
    "}",
    "float fbm(vec2 p) {",
    "  float v = 0.0, a = 0.5;",
    "  for (int i = 0; i < 4; i++) { v += a * noise(p); p = p * 2.03 + 17.1; a *= 0.5; }",
    "  return v;",
    "}",
    "mat2 rot(float a) { float c = cos(a), s = sin(a); return mat2(c, -s, s, c); }",
    "float field(vec2 p) {",
    "  float s = t * 0.05;",
    "  vec2 q = p * 2.0 + warp * vec2(fbm(p * 2.0 + off + s), fbm(p * 2.0 - off + vec2(5.2, 1.3) - s));",
    // Halftone dots, sized by a drifting noise field.
    "  if (pattern < 0.5) {",
    "    vec2 g = rot(angle) * p * scale * 12.0;",
    "    float r = fbm(q * 0.7 + off) * 0.85;",
    "    return 1.0 - smoothstep(r - 0.05, r + 0.05, length(fract(g) - 0.5));",
    "  }",
    // Topographic contour lines over a soft noise.
    "  if (pattern < 1.5) {",
    "    float v = fbm(q);",
    "    float band = fract(v * scale * 3.0);",
    "    float line = smoothstep(0.0, 0.08, band) * smoothstep(0.0, 0.08, 1.0 - band);",
    "    return mix(1.0, v * 0.6, line);",
    "  }",
    // Wandering cells with dark seams.
    "  if (pattern < 2.5) {",
    "    vec2 g = q * scale, i = floor(g), f = fract(g), id = vec2(0.0);",
    "    float f1 = 8.0, f2 = 8.0;",
    "    for (int y = -1; y <= 1; y++) for (int x = -1; x <= 1; x++) {",
    "      vec2 n = vec2(float(x), float(y));",
    "      vec2 pt = 0.5 + 0.5 * sin(t * 0.3 + 6.2831 * hash2(i + n));",
    "      float d = length(n + pt - f);",
    "      if (d < f1) { f2 = f1; f1 = d; id = i + n; } else if (d < f2) { f2 = d; }",
    "    }",
    "    return smoothstep(0.0, 0.08, f2 - f1) * (0.35 + 0.65 * hash(id));",
    "  }",
    // Bent stripes.
    "  if (pattern < 3.5) {",
    "    float x = (rot(angle) * q).x * scale * 2.0;",
    "    return smoothstep(-0.25, 0.25, sin(x * 3.14159 + t * 0.4));",
    "  }",
    // Plasma.
    "  return fbm(q * scale * 0.5 + fbm(q + s));",
    "}",
    "vec3 ramp(float v) { return v < 0.5 ? mix(c0, c1, v * 2.0) : mix(c1, c2, v * 2.0 - 1.0); }",
    "void main() {",
    "  vec2 p = (gl_FragCoord.xy - 0.5 * res) / res.y;",
    "  vec3 col;",
    // Misregistration: red and blue plates printed slightly off the green.
    "  if (misreg > 0.0) col = vec3(ramp(field(p + vec2(misreg, 0.0))).r, ramp(field(p)).g, ramp(field(p - vec2(misreg, 0.0))).b);",
    "  else col = ramp(field(p));",
    "  if (levels > 0.5) col = floor(col * levels + hash(gl_FragCoord.xy) * 0.9 + 0.05) / levels;",
    "  col += (hash(gl_FragCoord.xy + fract(t) * 100.0) - 0.5) * grain;",
    "  gl_FragColor = vec4(col, 1.0);",
    "}",
  ].join("\n");

  var canvas = null, gl = null, u = null, raf = null, last = 0;

  function startCanvas() {
    if (canvas) return;
    var c = document.createElement("canvas");
    c.className = "gen-canvas";
    c.setAttribute("aria-hidden", "true");
    var g = c.getContext("webgl", { antialias: false, depth: false });
    if (!g) return;

    var prog = g.createProgram();
    [[g.VERTEX_SHADER, VERT], [g.FRAGMENT_SHADER, FRAG]].forEach(function (s) {
      var sh = g.createShader(s[0]);
      g.shaderSource(sh, s[1]);
      g.compileShader(sh);
      g.attachShader(prog, sh);
    });
    g.bindAttribLocation(prog, 0, "v");
    g.linkProgram(prog);
    if (!g.getProgramParameter(prog, g.LINK_STATUS)) return;
    g.useProgram(prog);

    // One triangle that covers the whole screen.
    g.bindBuffer(g.ARRAY_BUFFER, g.createBuffer());
    g.bufferData(g.ARRAY_BUFFER, new Float32Array([-1, -1, 3, -1, -1, 3]), g.STATIC_DRAW);
    g.enableVertexAttribArray(0);
    g.vertexAttribPointer(0, 2, g.FLOAT, false, 0, 0);
    u = {};
    ["res", "off", "t", "pattern", "scale", "warp", "angle", "misreg", "grain", "levels", "c0", "c1", "c2"].forEach(function (name) {
      u[name] = g.getUniformLocation(prog, name);
    });

    c.addEventListener("webglcontextlost", function () { if (canvas === c) stopCanvas(); });
    canvas = c;
    gl = g;
    document.body.appendChild(c);
    root.classList.add("gen-gl");
    resize();
    addEventListener("resize", resize);
    document.addEventListener("visibilitychange", loop);
    loop();
  }

  function stopCanvas() {
    if (!canvas) return;
    cancelAnimationFrame(raf);
    raf = null;
    removeEventListener("resize", resize);
    document.removeEventListener("visibilitychange", loop);
    var lose = gl.getExtension("WEBGL_lose_context");
    canvas.remove();
    canvas = gl = u = null;
    root.classList.remove("gen-gl");
    if (lose) lose.loseContext();
  }

  // Drawn at well under screen resolution: the patterns are soft and it keeps phones cool.
  function resize() {
    var scale = Math.min(devicePixelRatio || 1, 2) * 0.5;
    canvas.width = Math.max(1, Math.round(innerWidth * scale));
    canvas.height = Math.max(1, Math.round(innerHeight * scale));
    gl.viewport(0, 0, canvas.width, canvas.height);
    if (reducedMotion.matches) draw(0);
  }

  function draw(now) {
    if (!look) return;
    var w = look;
    gl.uniform2f(u.res, canvas.width, canvas.height);
    gl.uniform2f(u.off, w.off[0], w.off[1]);
    gl.uniform1f(u.t, ((now / 1000) * w.speed) % 1000);
    gl.uniform1f(u.pattern, w.pattern);
    gl.uniform1f(u.scale, w.scale);
    gl.uniform1f(u.warp, w.warp);
    gl.uniform1f(u.angle, w.angle);
    gl.uniform1f(u.misreg, w.misreg);
    gl.uniform1f(u.grain, w.grain);
    gl.uniform1f(u.levels, w.levels);
    gl.uniform3fv(u.c0, w.c0);
    gl.uniform3fv(u.c1, w.c1);
    gl.uniform3fv(u.c2, w.c2);
    gl.drawArrays(gl.TRIANGLES, 0, 3);
  }

  // About 30 frames a second, and none while the tab is hidden or motion is unwelcome.
  function loop() {
    cancelAnimationFrame(raf);
    raf = null;
    if (!canvas) return;
    if (reducedMotion.matches || document.hidden) return draw(0);
    raf = requestAnimationFrame(function frame(now) {
      if (now - last >= 33) { last = now; draw(now); }
      raf = requestAnimationFrame(frame);
    });
  }

  // ---- Wiring ----

  if (active()) apply();

  document.addEventListener("DOMContentLoaded", function () {
    if (look) startCanvas();

    // Clicking the generative stop while it's already picked rolls a new look.
    // (Click comes before change, so the theme still says what was picked.)
    var stop = document.querySelector('.switch input[value="generative"]');
    if (stop) stop.addEventListener("click", function () { if (active() && look) roll(); });

    new MutationObserver(sync).observe(root, { attributes: true, attributeFilter: ["data-theme"] });
    reducedMotion.addEventListener("change", loop);
  });
})();
