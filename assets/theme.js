// Loaded as a blocking script in <head>, so the saved theme applies before
// the page paints. The toggle is wired up once the DOM is ready.
(function () {
  var root = document.documentElement;
  try {
    var saved = localStorage.getItem("theme");
    if (saved) root.dataset.theme = saved;
  } catch (e) {}

  document.addEventListener("DOMContentLoaded", function () {
    var button = document.querySelector(".toggle");
    if (!button) return;
    var systemDark = window.matchMedia("(prefers-color-scheme: dark)");

    function isDark() {
      return root.dataset.theme ? root.dataset.theme === "dark" : systemDark.matches;
    }

    // Show the icon for the mode you'll switch to.
    function render() {
      button.textContent = isDark() ? "🌞" : "🌜";
    }

    button.addEventListener("click", function () {
      var next = isDark() ? "light" : "dark";
      root.dataset.theme = next;
      try { localStorage.setItem("theme", next); } catch (e) {}
      render();
      // Restart the spin-in animation for the new icon.
      button.classList.remove("spin");
      void button.offsetWidth;
      button.classList.add("spin");
    });

    systemDark.addEventListener("change", render);
    render();
    // Hidden in the markup so it doesn't show up useless without JavaScript.
    button.hidden = false;
  });
})();
