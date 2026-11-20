// Loaded as a blocking script in <head>, so the saved theme applies before
// the page paints. The theme switch is wired up once the DOM is ready.
(function () {
  var root = document.documentElement;
  // Keep in step with the radios in templates/header.html and the rules in site.css.
  var themes = ["light", "dark", "trippy"];

  try {
    var saved = localStorage.getItem("theme");
    if (themes.indexOf(saved) !== -1) root.dataset.theme = saved;
  } catch (e) {}

  // The trippy theme's fractal and tunnel are heavy, so they load only when picked.
  var trippyLoaded = false;
  function loadTrippy() {
    if (trippyLoaded) return;
    trippyLoaded = true;
    var script = document.createElement("script");
    script.src = "/assets/trippy.js";
    // Let the next pick try again if this one never arrives.
    script.onerror = function () {
      script.remove();
      trippyLoaded = false;
    };
    document.head.appendChild(script);
  }
  if (root.dataset.theme === "trippy") loadTrippy();

  document.addEventListener("DOMContentLoaded", function () {
    var themeSwitch = document.querySelector(".switch");
    if (!themeSwitch) return;
    var systemDark = window.matchMedia("(prefers-color-scheme: dark)");

    // Until a theme is picked, the switch follows the system setting.
    function render() {
      var theme = root.dataset.theme || (systemDark.matches ? "dark" : "light");
      themeSwitch.querySelector('input[value="' + theme + '"]').checked = true;
    }

    themeSwitch.addEventListener("change", function (e) {
      root.dataset.theme = e.target.value;
      if (e.target.value === "trippy") loadTrippy();
      try { localStorage.setItem("theme", e.target.value); } catch (err) {}
    });

    systemDark.addEventListener("change", render);
    render();
    // Hidden in the markup so it doesn't show up useless without JavaScript.
    themeSwitch.hidden = false;
  });
})();
