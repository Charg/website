// Easter egg: double-click the footer spark to travel back to 2006.
// Pages can listen for "myspace" on the document to add their own touches.
(function () {
  var spark = document.querySelector(".site-footer .zap");
  if (!spark) return;
  var root = document.documentElement;
  var pageTitle = document.title;
  var twinkles;

  // Scatter stars across the screen, each with its own pace and start time,
  // so only a few twinkle at once and in different places.
  function makeTwinkles() {
    var colors = ["#ffffff", "#ff66cc", "#33ccff", "#ffff66"];
    twinkles = document.createElement("div");
    twinkles.className = "twinkles";
    twinkles.setAttribute("aria-hidden", "true");
    for (var i = 0; i < 28; i++) {
      var star = document.createElement("span");
      var duration = 5 + Math.random() * 7; // 5–12s per cycle
      star.textContent = "✦";
      star.style.left = Math.random() * 100 + "%";
      star.style.top = Math.random() * 100 + "%";
      star.style.fontSize = 10 + Math.random() * 12 + "px";
      star.style.color = colors[i % colors.length];
      star.style.animationDuration = duration + "s";
      star.style.animationDelay = -Math.random() * duration + "s";
      twinkles.appendChild(star);
    }
    document.body.prepend(twinkles);
  }

  spark.addEventListener("dblclick", function () {
    if (!twinkles) makeTwinkles();
    var on = root.classList.toggle("myspace");
    document.title = on ? "~*~ " + pageTitle + " ~*~" : pageTitle;
    document.dispatchEvent(new CustomEvent("myspace", { detail: { on: on } }));
  });
})();
