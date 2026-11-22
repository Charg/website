// Loaded as a blocking script in <head>, so the saved theme applies before
// the page paints. The theme switch is wired up once the DOM is ready.
(function () {
  var root = document.documentElement;
  // Keep in step with the radios in templates/header.html and the rules in site.css.
  // generative.js runs right after this and builds the generative theme.
  var themes = ["light", "dark", "generative"];

  try {
    var saved = localStorage.getItem("theme");
    // The generative theme replaced the old trippy theme.
    if (saved === "trippy") saved = "generative";
    if (themes.indexOf(saved) !== -1) root.dataset.theme = saved;
  } catch (e) {}

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
      try { localStorage.setItem("theme", e.target.value); } catch (err) {}
    });

    systemDark.addEventListener("change", render);
    render();
    // Hidden in the markup so it doesn't show up useless without JavaScript.
    themeSwitch.hidden = false;
  });
})();
